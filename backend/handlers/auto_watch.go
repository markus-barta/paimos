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

// PAI-331 — auto-watch sync subscription endpoints.
//
// The persisted state lives in M98's auto_watch_subscriptions table. The
// browser UI hits these endpoints via fetch; the CLI never does (the
// CLI manages its own subscription via the SSE handshake — toggling the
// row ON happens implicitly on first connect, OFF happens explicitly
// here from the browser).
//
//  GET  /api/auth/auto-watch                      — list current user's
//                                                    (device, project)
//                                                    subscriptions.
//  PUT  /api/auth/auto-watch/{deviceID}/{projectID} — upsert toggle
//                                                    state.
//  DELETE /api/auth/auto-watch/{deviceID}/{projectID} — explicit
//                                                    invalidation; the
//                                                    server's broker
//                                                    closes any live
//                                                    SSE connection
//                                                    matching the row.

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
	"github.com/markus-barta/paimos/backend/sse"
)

// autoWatchRow is the JSON shape returned by the list endpoint and
// echoed by upsert. Mirrors the table 1:1.
type autoWatchRow struct {
	DeviceID  string `json:"device_id"`
	ProjectID int64  `json:"project_id"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// deviceIDMaxLen mirrors the agent-attribution cap from PAI-324. The
// schema does not enforce it (TEXT column), but the handler does so
// the table never grows pathological values.
const deviceIDMaxLen = 64

// ListAutoWatch returns every (device, project) row for the
// authenticated user. Empty array (never null) when none.
func ListAutoWatch(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	rows, err := db.DB.Query(`
		SELECT device_id, project_id, enabled, created_at, updated_at
		FROM auto_watch_subscriptions WHERE user_id = ?
		ORDER BY project_id ASC, device_id ASC
	`, user.ID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []autoWatchRow{}
	for rows.Next() {
		var rowItem autoWatchRow
		var enabledInt int
		if err := rows.Scan(&rowItem.DeviceID, &rowItem.ProjectID, &enabledInt, &rowItem.CreatedAt, &rowItem.UpdatedAt); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		rowItem.Enabled = enabledInt == 1
		out = append(out, rowItem)
	}
	jsonOK(w, out)
}

// UpsertAutoWatch toggles the row at (user, device, project). The body
// is `{"enabled": true|false}`. Returns the upserted row. When the new
// state is OFF, also invalidates any active SSE subscription for that
// (device, project) pair so the running CLI watch process learns it
// should stop.
func UpsertAutoWatch(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	deviceID := strings.TrimSpace(chi.URLParam(r, "deviceID"))
	if deviceID == "" || len(deviceID) > deviceIDMaxLen {
		jsonError(w, "invalid device id", http.StatusBadRequest)
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "projectID"), 10, 64)
	if err != nil || projectID <= 0 {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	if !userCanViewProject(user, projectID) {
		// Match the project-membership semantics: 404 (don't leak whether
		// the project exists at all to non-members).
		jsonError(w, "project not found", http.StatusNotFound)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	enabledInt := 0
	if body.Enabled {
		enabledInt = 1
	}
	if _, err := db.DB.Exec(`
		INSERT INTO auto_watch_subscriptions(user_id, device_id, project_id, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(user_id, device_id, project_id) DO UPDATE SET
			enabled = excluded.enabled,
			updated_at = datetime('now')
	`, user.ID, deviceID, projectID, enabledInt); err != nil {
		jsonError(w, "upsert failed", http.StatusInternalServerError)
		return
	}

	// PAI-331: when the user flips a row OFF, eagerly drop any active
	// SSE connection so the CLI's watch loop sees its stream close
	// instead of waiting for a server-side sweep.
	if !body.Enabled {
		sse.GlobalBroker().Disconnect(user.ID, deviceID, projectID)
	}

	out := autoWatchRow{
		DeviceID:  deviceID,
		ProjectID: projectID,
		Enabled:   body.Enabled,
	}
	// Return the row reading-back (so the client always sees server-
	// authoritative timestamps).
	if err := db.DB.QueryRow(`
		SELECT created_at, updated_at FROM auto_watch_subscriptions
		WHERE user_id = ? AND device_id = ? AND project_id = ?
	`, user.ID, deviceID, projectID).Scan(&out.CreatedAt, &out.UpdatedAt); err != nil {
		// Read-back fail is non-fatal — return what we have.
		out.CreatedAt = ""
		out.UpdatedAt = ""
	}
	jsonOK(w, out)
}

// DeleteAutoWatch is the explicit invalidation path. It removes the
// row entirely (rather than flipping enabled=0) so the user can shed
// stale device entries. Server-side SSE disconnect happens regardless.
func DeleteAutoWatch(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	deviceID := strings.TrimSpace(chi.URLParam(r, "deviceID"))
	if deviceID == "" {
		jsonError(w, "invalid device id", http.StatusBadRequest)
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "projectID"), 10, 64)
	if err != nil || projectID <= 0 {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec(`
		DELETE FROM auto_watch_subscriptions
		WHERE user_id = ? AND device_id = ? AND project_id = ?
	`, user.ID, deviceID, projectID); err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	sse.GlobalBroker().Disconnect(user.ID, deviceID, projectID)
	w.WriteHeader(http.StatusNoContent)
}

// userCanViewProject is a thin wrapper around the existing access
// helper. Defined here (rather than in auth) to keep the import graph
// flat — the auto-watch handler is the only caller right now.
func userCanViewProject(user *models.User, projectID int64) bool {
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	var lvl string
	err := db.DB.QueryRow(`
		SELECT access_level FROM project_members WHERE user_id = ? AND project_id = ?
	`, user.ID, projectID).Scan(&lvl)
	if err != nil {
		return false
	}
	return lvl != "" && lvl != "none"
}
