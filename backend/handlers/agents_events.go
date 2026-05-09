// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// PAI-331 — server-side endpoints for the auto-watch sync engine.
//
//   GET /api/projects/{id}/agents/events       — Server-Sent Events
//                                                stream of canonical-
//                                                state changes scoped
//                                                to the project + the
//                                                authenticated user's
//                                                device.
//   GET /api/projects/{id}/agents/{name}.rev   — cheap revision string
//                                                for polling-fallback
//                                                clients that can't
//                                                hold an SSE
//                                                connection.
//
// The events stream wire format follows the SSE spec verbatim:
//   data: {"type": "agent_changed", "name": "ops", "rev": "abcd1234", "project_id": 7}
//
// PAI-341 will publish additional Type values (`memory_changed`,
// `runbook_changed`, …) through the same endpoint via
// sse.GlobalBroker(); this handler is kind-agnostic so PAI-341 only
// needs to wire publishers, not edit this file.

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/sse"
)

// sseHeartbeatInterval is how often the handler emits a ":heartbeat\n\n"
// comment line. Cloudflare and most reverse proxies time out idle SSE
// connections at ~30–60s; 25s gives us a safety margin.
const sseHeartbeatInterval = 25 * time.Second

// AgentsEventsStream serves the SSE stream the CLI's `paimos sync watch`
// subscribes to. The stream stays open until the client disconnects, the
// browser tab closes, or the auto-watch row is toggled OFF (which the
// auto-watch handler handles by calling sse.GlobalBroker().Disconnect).
//
// Auth: route is auth.Middleware-gated by main.go; this handler trusts
// auth.GetUser. The auto-watch row check enforces opt-in: subscribers
// without `enabled=1` get a 403 immediately so a misbehaving CLI can't
// camp on the connection.
func AgentsEventsStream(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	if !userCanViewProject(user, projectID) {
		jsonError(w, "project not found", http.StatusNotFound)
		return
	}
	deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if deviceID == "" || len(deviceID) > deviceIDMaxLen {
		jsonError(w, "device_id required", http.StatusBadRequest)
		return
	}

	// Opt-in gate: implicit enrol on first connect — the CLI's
	// `paimos sync watch` is the only realistic source of new
	// (device, project) rows. The browser UI uses the toggle endpoint
	// directly.
	if err := upsertAutoWatchEnabled(user.ID, deviceID, projectID); err != nil {
		jsonError(w, "subscription init failed", http.StatusInternalServerError)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: disable proxy buffering.

	// Initial open frame so the client receives bytes promptly and any
	// network-edge buffering gets flushed.
	fmt.Fprintf(w, ":connected\n\n")
	flusher.Flush()

	sub := sse.GlobalBroker().Subscribe(user.ID, deviceID, projectID)
	defer sse.GlobalBroker().Close(sub)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				// Broker closed our channel (auto-watch toggled OFF).
				// Tell the client cleanly.
				fmt.Fprintf(w, "event: disconnect\ndata: {\"reason\":\"auto-watch off\"}\n\n")
				flusher.Flush()
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ":heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// upsertAutoWatchEnabled flips the (user, device, project) row to
// enabled=1, creating it if missing. Used by the SSE handshake so a
// fresh CLI invocation doesn't have to call the toggle endpoint
// separately. The browser UI does call the toggle endpoint, which is
// fine — both paths converge on the same row.
func upsertAutoWatchEnabled(userID int64, deviceID string, projectID int64) error {
	_, err := db.DB.Exec(`
		INSERT INTO auto_watch_subscriptions(user_id, device_id, project_id, enabled, created_at, updated_at)
		VALUES (?, ?, ?, 1, datetime('now'), datetime('now'))
		ON CONFLICT(user_id, device_id, project_id) DO UPDATE SET
			enabled = 1,
			updated_at = datetime('now')
	`, userID, deviceID, projectID)
	return err
}

// AgentRevHandler serves the cheap-poll revision string for an agent.
// Returns plain text (not JSON) so curl users can compare without a
// JSON parser. The body is the same short hash that PAI-330's
// rendered-file header embeds (sha256(canonical_json) truncated to 12
// hex chars), so a polling client can do
//
//	if curl /api/projects/$id/agents/$name.rev != cached_rev: re-render
//
// without having to parse the artifact itself.
func AgentRevHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := projectIDFromRequest(r); !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	rawName := chi.URLParam(r, "name")
	agentName := strings.TrimSpace(rawName)
	if agentName == "" {
		jsonError(w, "agent name required", http.StatusBadRequest)
		return
	}

	// Build the same artifact the .json endpoint serves and hash it.
	// We re-use buildProjectAgentArtifact so the rev stays consistent
	// with the canonical artifact body (PAI-330's render header
	// derives its rev from the same JSON bytes).
	artifact, buildErr := buildProjectAgentArtifact(r)
	if buildErr != nil {
		jsonError(w, buildErr.msg, buildErr.status)
		return
	}
	body, err := json.Marshal(artifact)
	if err != nil {
		jsonError(w, "encode failed", http.StatusInternalServerError)
		return
	}
	// Re-encode through interface{} to normalise whitespace exactly
	// like adapters.canonicalRev does, otherwise this rev would drift
	// from the rev embedded in the rendered file's header.
	var doc any
	if err := json.Unmarshal(body, &doc); err == nil {
		if normalised, err := json.Marshal(doc); err == nil {
			body = normalised
		}
	}
	sum := sha256.Sum256(body)
	rev := hex.EncodeToString(sum[:])[:12]

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(rev + "\n"))
}

// PublishAgentChanged is the publisher hook agent CRUD handlers call
// after a successful write. PAI-331 wires it from
// CreateProjectAgent / UpdateProjectAgent / DeleteProjectAgent (those
// edits invalidate the canonical artifact). Decoupled from the broker
// itself so future kinds (PAI-341) follow the same call shape.
func PublishAgentChanged(projectID int64, agentName, rev string) {
	sse.GlobalBroker().PublishProject(projectID, sse.Event{
		Type: "agent_changed",
		Name: agentName,
		Rev:  rev,
	})
}
