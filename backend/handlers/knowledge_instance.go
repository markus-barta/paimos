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

package handlers

// PAI-345 — instance-scoped memory CRUD. Endpoints:
//
//	GET    /api/instance/memory       (any authenticated user)
//	POST   /api/instance/memory       (admin only)
//	GET    /api/instance/memory/:slug (any authenticated user)
//	PUT    /api/instance/memory/:slug (admin only)
//	DELETE /api/instance/memory/:slug (admin only)
//
// Instance memory is "rules every agent on this server should know"
// — the broadest layer of PAI-345's three-tier hierarchy. The WHERE
// clause is the simplest of the three:
//
//	project_id IS NULL AND user_id IS NULL
//
// Reads are wide open (any authed user can pull the universal corpus
// into their bundle); writes are admin-gated server-side. The
// admin-gating is enforced by the route middleware in main.go and
// the test harness — we double-check here defensively so a call that
// somehow lands without RequireAdmin still 403s instead of mutating.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/handlers/knowledge"
)

// sql.ErrNoRows is referenced via errors.Is below; keep the import
// explicit so the sentinel comparison stays readable.
var _ = sql.ErrNoRows

// requireAdminUser returns true when the current request is from an
// admin. Defence-in-depth — the route is already wrapped with
// auth.RequireAdmin, but checking again means a misconfigured router
// can never silently leak instance memory writes to non-admins.
func requireAdminUser(w http.ResponseWriter, r *http.Request) bool {
	user := auth.GetUser(r)
	if !auth.IsAdmin(user) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// instanceMemoryModule pins the Module to "memory". Same rationale
// as userMemoryModule — see knowledge_user.go's file-level note about
// why other knowledge categories don't get a v1 instance scope.
func instanceMemoryModule() knowledge.Module {
	return userMemoryModule()
}

// ListInstanceMemory returns every live instance-scope memory entry,
// ordered by slug. Any authenticated user may read — instance memory
// is meant to be visible everywhere, that's the whole point.
func ListInstanceMemory(w http.ResponseWriter, r *http.Request) {
	if auth.GetUser(r) == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	mod := instanceMemoryModule()
	out, err := loadInstanceMemoryByType(mod)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// GetInstanceMemory returns a single instance-scope memory entry by
// slug. Read access is granted to any authenticated user.
func GetInstanceMemory(w http.ResponseWriter, r *http.Request) {
	if auth.GetUser(r) == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		jsonError(w, "slug required", http.StatusBadRequest)
		return
	}
	mod := instanceMemoryModule()
	out, err := loadOneInstanceMemoryBySlug(mod, slug)
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// CreateInstanceMemory writes a new instance-scope memory entry.
// Admin-only — see requireAdminUser for the defence-in-depth note.
func CreateInstanceMemory(w http.ResponseWriter, r *http.Request) {
	if !requireAdminUser(w, r) {
		return
	}
	mod := instanceMemoryModule()
	var in knowledge.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if msg := validateUserOrInstanceInput(mod, in); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	out, err := createUserOrInstanceMemory(r, mod, nil, in)
	if errors.Is(err, knowledge.ErrSlugTaken) {
		jsonError(w, "slug already exists for this scope", http.StatusConflict)
		return
	}
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(out)
}

// UpdateInstanceMemory mutates an existing instance-scope memory
// entry. Admin-only. URL slug locates the row; body slug renames it
// (subject to the partial UNIQUE INDEX over (type, slug, project_id)
// — instance rows all share project_id NULL so the constraint is
// effectively "unique slug per type within instance scope").
func UpdateInstanceMemory(w http.ResponseWriter, r *http.Request) {
	if !requireAdminUser(w, r) {
		return
	}
	mod := instanceMemoryModule()
	current := strings.TrimSpace(chi.URLParam(r, "slug"))
	if current == "" {
		jsonError(w, "slug required", http.StatusBadRequest)
		return
	}
	var in knowledge.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Slug) == "" {
		in.Slug = current
	}
	if msg := validateUserOrInstanceInput(mod, in); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	out, err := updateUserOrInstanceMemory(r, mod, nil, current, in)
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, knowledge.ErrSlugTaken) {
		jsonError(w, "slug already exists for this scope", http.StatusConflict)
		return
	}
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// DeleteInstanceMemory soft-deletes an instance-scope memory entry.
// Admin-only. Trash flow + undo work the same way they do for
// project-scope memory.
func DeleteInstanceMemory(w http.ResponseWriter, r *http.Request) {
	if !requireAdminUser(w, r) {
		return
	}
	mod := instanceMemoryModule()
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		jsonError(w, "slug required", http.StatusBadRequest)
		return
	}
	n, err := deleteUserOrInstanceMemory(r, mod, nil, slug)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── instance load helpers ───────────────────────────────────────────

// loadInstanceMemoryByType returns the live instance-scope entries.
// Sorted by slug for stable pagination, same as the project / user
// list paths.
func loadInstanceMemoryByType(mod knowledge.Module) ([]knowledge.Output, error) {
	rows, err := db.DB.Query(`
		SELECT id, COALESCE(project_id,0), type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at
		  FROM issues
		 WHERE project_id IS NULL
		   AND user_id    IS NULL
		   AND type       = ?
		   AND deleted_at IS NULL
		   AND slug       IS NOT NULL
	  ORDER BY slug ASC
	`, mod.Type())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []knowledge.Output{}
	for rows.Next() {
		o, err := scanUserOrInstanceOutput(rows, mod)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// loadOneInstanceMemoryBySlug is the single-row variant. sql.ErrNoRows
// propagates up so the handler can map it onto a 404.
func loadOneInstanceMemoryBySlug(mod knowledge.Module, slug string) (knowledge.Output, error) {
	row := db.DB.QueryRow(`
		SELECT id, COALESCE(project_id,0), type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at
		  FROM issues
		 WHERE project_id IS NULL
		   AND user_id    IS NULL
		   AND type       = ?
		   AND slug       = ?
		   AND deleted_at IS NULL
	`, mod.Type(), slug)
	return scanUserOrInstanceOutput(row, mod)
}
