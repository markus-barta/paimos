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

// PAI-341 — server-side knowledge-plane sync wiring.
//
// This file ships three concerns layered on top of PAI-353's hooks:
//
//   1. Per-kind PublishXxxChanged helpers wrapping sse.GlobalBroker so
//      knowledge CRUD handlers don't have to know the broker shape.
//      Each helper publishes "<kind>_changed" events the CLI sync
//      watcher consumes via sync.EventKind.
//
//   2. publishKnowledgeChange is the dispatcher PAI-353's createKnowledge
//      Entry / updateKnowledgeEntry / deleteKnowledgeEntry call after the
//      DB tx commits. It maps the type discriminator to the right
//      Publish helper so the on-disk hook in knowledge_writes.go stays
//      type-agnostic.
//
//   3. KnowledgeRevHandler serves /api/projects/{id}/{alias}/{slug}.rev
//      mirroring AgentRevHandler. The rev format matches sync.KnowledgeRev
//      so polling clients see the same string the CLI computed locally.
//
// PAI-331's broker is kind-agnostic: PublishProject takes any Event.Type
// string. We keep the Publish helpers explicit per kind (rather than a
// single generic PublishKnowledgeChanged(typ, …)) so callers get a
// compile-time check on the kind name and so the file reads as a
// catalogue of supported events.

package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers/knowledge"
	"github.com/markus-barta/paimos/backend/sse"
)

// PublishMemoryChanged publishes a `memory_changed` SSE event. Called
// after a successful CREATE / UPDATE / DELETE on a memory knowledge
// entry. The slug is the addressable identifier; the rev is the short
// content hash matching sync.KnowledgeRev (empty when the caller
// hasn't pre-computed it — the CLI watcher refetches anyway).
func PublishMemoryChanged(projectID int64, slug, rev string) {
	publishKnowledgeEvent(projectID, "memory_changed", slug, rev)
}

// PublishRunbookChanged publishes a `runbook_changed` SSE event.
// Mirror of PublishMemoryChanged for the runbook kind.
func PublishRunbookChanged(projectID int64, slug, rev string) {
	publishKnowledgeEvent(projectID, "runbook_changed", slug, rev)
}

// PublishExternalSystemChanged publishes an `external_system_changed`
// SSE event. Note the underscore in the type name — the CLI's
// sync.EventKind handler trims `_changed` once, leaving the kind as
// `external_system` (the registered Resource kind), so the convention
// holds without a special case.
func PublishExternalSystemChanged(projectID int64, slug, rev string) {
	publishKnowledgeEvent(projectID, "external_system_changed", slug, rev)
}

// PublishRelatedProjectChanged publishes a `related_project_changed`
// SSE event.
func PublishRelatedProjectChanged(projectID int64, slug, rev string) {
	publishKnowledgeEvent(projectID, "related_project_changed", slug, rev)
}

// PublishGuidelineChanged publishes a `guideline_changed` SSE event.
func PublishGuidelineChanged(projectID int64, slug, rev string) {
	publishKnowledgeEvent(projectID, "guideline_changed", slug, rev)
}

// publishKnowledgeEvent is the shared body of the per-kind helpers.
// Centralised so any future cross-cutting concern (e.g. structured
// logging, metric counter) lands in one place.
func publishKnowledgeEvent(projectID int64, eventType, slug, rev string) {
	sse.GlobalBroker().PublishProject(projectID, sse.Event{
		Type: eventType,
		Name: slug,
		Rev:  rev,
	})
}

// publishKnowledgeChange dispatches a publish call to the right
// per-kind helper based on the issues.type discriminator. Used by
// PAI-353's write hooks (createKnowledgeEntry / updateKnowledgeEntry /
// deleteKnowledgeEntry) so they don't need a switch on Module.Type().
//
// The rev parameter is the short content hash — knowledge_writes.go
// computes it from the just-written Output payload via
// sync.KnowledgeRev so subscribers get the same value they'd get
// from the .rev polling endpoint.
//
// Unknown discriminators are a no-op: the broker would route to no
// subscriber anyway, and a server-side panic on a stray type doesn't
// help the user.
func publishKnowledgeChange(projectID int64, typ, slug, rev string) {
	switch typ {
	case "memory":
		PublishMemoryChanged(projectID, slug, rev)
	case "runbook":
		PublishRunbookChanged(projectID, slug, rev)
	case "external_system":
		PublishExternalSystemChanged(projectID, slug, rev)
	case "related_project":
		PublishRelatedProjectChanged(projectID, slug, rev)
	case "guideline":
		PublishGuidelineChanged(projectID, slug, rev)
	}
}

// knowledgeRevForOutput computes the canonical rev for a
// knowledge.Output. Mirrors sync.KnowledgeRev — duplicated here
// (rather than imported from the CLI sync package) to keep the
// server-side handlers free of CLI dependencies. The hash inputs
// match field-for-field, so the rev space stays consistent.
func knowledgeRevForOutput(out knowledge.Output) string {
	probe := struct {
		Slug     string         `json:"slug"`
		Title    string         `json:"title"`
		Body     string         `json:"body"`
		Status   string         `json:"status"`
		Metadata map[string]any `json:"metadata"`
		Type     string         `json:"type"`
	}{
		Slug:     out.Slug,
		Title:    out.Title,
		Body:     out.Body,
		Status:   out.Status,
		Metadata: out.Metadata,
		Type:     out.Type,
	}
	body, err := json.Marshal(probe)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:12]
}

// MakeKnowledgeRevHandler returns the .rev polling-fallback handler
// for a knowledge URL alias. Routed under
// /api/projects/{id}/{alias}/{slug}.rev. Plain text response (the
// 12-char hex hash) so curl users can compare without a JSON parser.
//
// The factory shape mirrors knowledge.MakeListHandler so the router
// wiring stays uniform across the alias loop.
//
// Auth is project-view only — same gate as the GET-by-slug path. The
// .rev never leaks anything beyond the fact that a slug exists, but
// keeping the gate consistent removes a per-route bookkeeping burden.
func MakeKnowledgeRevHandler(alias string) http.HandlerFunc {
	mod, err := knowledge.RouteByPath(alias)
	if err != nil {
		// init-time misconfiguration — fail loudly.
		panic(fmt.Errorf("knowledge .rev handler: %w", err))
	}
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := projectIDFromRequest(r)
		if !ok {
			jsonError(w, "invalid project id", http.StatusBadRequest)
			return
		}
		slug := strings.TrimSpace(chi.URLParam(r, "slug"))
		if slug == "" {
			jsonError(w, "slug required", http.StatusBadRequest)
			return
		}
		out, err := loadKnowledgeBySlug(projectID, mod, slug)
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, "query failed", http.StatusInternalServerError)
			return
		}
		rev := knowledgeRevForOutput(out)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(rev + "\n"))
	}
}

// loadKnowledgeBySlug reads a single live knowledge entry. Duplicates
// knowledge.loadOneBySlug's SQL because the function is package-private
// in the sub-package and exporting it just for the .rev handler felt
// like a bigger surface change than the small SQL repeat.
func loadKnowledgeBySlug(projectID int64, mod knowledge.Module, slug string) (knowledge.Output, error) {
	row := db.DB.QueryRow(`
		SELECT id, project_id, type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at
		  FROM issues
		 WHERE project_id = ?
		   AND type       = ?
		   AND slug       = ?
		   AND deleted_at IS NULL
	`, projectID, mod.Type(), slug)
	var (
		o       knowledge.Output
		metaRaw string
	)
	if err := row.Scan(
		&o.ID, &o.ProjectID, &o.Type, &o.Slug, &o.Title, &o.Body,
		&o.Status, &metaRaw, &o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		return o, err
	}
	meta, err := mod.UnmarshalMeta(metaRaw)
	if err != nil {
		meta = map[string]any{}
	}
	o.Metadata = meta
	return o, nil
}
