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

// PAI-345 — user-scoped memory CRUD. Endpoints:
//
//	GET    /api/users/me/memory
//	POST   /api/users/me/memory
//	GET    /api/users/me/memory/:slug
//	PUT    /api/users/me/memory/:slug
//	DELETE /api/users/me/memory/:slug
//
// User memory is "rules learned across all my projects" — the cross-
// project layer of PAI-345's three-tier hierarchy (project > user >
// instance). Storage is the same `issues` table the project-scope
// handlers use, distinguished only by the WHERE clause:
//
//	project_id IS NULL AND user_id = :current_user
//
// (See M99 for the schema additions; PAI-346 for the issue-as-memory
// adoption that makes this layer cheap.) The discriminator is purely
// a WHERE-clause concern; the validation, slug rules, payload shape and
// per-type Module dispatch are reused verbatim from the knowledge
// sub-package. v1 only exposes the `memory` type at this scope — the
// other knowledge categories (runbooks, external_systems, etc.) stay
// project-scoped because their context is project-local by definition.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/handlers/knowledge"
)

// userMemoryModule is the per-type Module the user-scope handlers use.
// Pinned to "memory" — see the file-header note about why other
// knowledge types don't get a user scope in v1. Looked up via
// knowledge.RouteByType so any future Validate / MarshalMeta tweaks
// the memory module ships pick up here without a code change.
func userMemoryModule() knowledge.Module {
	mod, err := knowledge.RouteByType("memory")
	if err != nil {
		// init-time invariant — the dispatcher always registers
		// 'memory'. A panic surfaces a wiring break loud-and-fast.
		panic(fmt.Sprintf("PAI-345: knowledge memory module missing: %v", err))
	}
	return mod
}

// requireAuthedUserID extracts the current user's id, writing a 401
// when none. Centralised so every user-scope handler emits the same
// error shape. Returns 0 + false when no user is attached — the
// caller should bail without further DB work.
func requireAuthedUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return 0, false
	}
	return user.ID, true
}

// ListUserMemory returns the current user's memory entries. Mirrors
// knowledge.MakeListHandler but with the user-scope WHERE.
func ListUserMemory(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireAuthedUserID(w, r)
	if !ok {
		return
	}
	mod := userMemoryModule()
	out, err := loadUserMemoryByType(uid, mod)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// GetUserMemory returns one memory entry owned by the current user.
// 404 when no live entry matches (user_id, type, slug).
func GetUserMemory(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireAuthedUserID(w, r)
	if !ok {
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		jsonError(w, "slug required", http.StatusBadRequest)
		return
	}
	mod := userMemoryModule()
	out, err := loadOneUserMemoryBySlug(uid, mod, slug)
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

// CreateUserMemory inserts a new memory entry owned by the current
// user. Slug validation + per-type Module checks are reused from the
// knowledge package so error shapes match the project-scope endpoints.
func CreateUserMemory(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireAuthedUserID(w, r)
	if !ok {
		return
	}
	mod := userMemoryModule()
	var in knowledge.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if msg := validateUserOrInstanceInput(mod, in); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	out, err := createUserOrInstanceMemory(r, mod, &uid, in)
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

// UpdateUserMemory mutates an existing memory entry owned by the
// current user. URL slug locates the row; body slug, when present,
// renames it (subject to the partial UNIQUE INDEX over (type, slug,
// project_id) — note that user-scope rows all share project_id NULL,
// so the constraint collapses to "unique slug per type across all
// users". To dodge cross-user collisions we extend the WHERE with
// user_id and accept that two users can never pick the same slug for
// a memory entry — acceptable for v1, the slug name space is large
// enough; PAI-339's editor warns on common ones.
func UpdateUserMemory(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireAuthedUserID(w, r)
	if !ok {
		return
	}
	mod := userMemoryModule()
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
	// PUT semantics: URL slug is canonical when body omits it.
	if strings.TrimSpace(in.Slug) == "" {
		in.Slug = current
	}
	if msg := validateUserOrInstanceInput(mod, in); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	out, err := updateUserOrInstanceMemory(r, mod, &uid, current, in)
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

// DeleteUserMemory soft-deletes a memory entry owned by the current
// user. Mirrors the project-scope DELETE: 204 on hit, 404 on miss,
// the row remains recoverable through the existing trash flow.
func DeleteUserMemory(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireAuthedUserID(w, r)
	if !ok {
		return
	}
	mod := userMemoryModule()
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		jsonError(w, "slug required", http.StatusBadRequest)
		return
	}
	n, err := deleteUserOrInstanceMemory(r, mod, &uid, slug)
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

// ── shared helpers (used by knowledge_instance.go too) ──────────────

// validateUserOrInstanceInput bundles the slug + title + per-type
// validation. Mirrors the package-private validateInput inside the
// knowledge sub-package; duplicated here so the user / instance paths
// don't have to import a private symbol.
func validateUserOrInstanceInput(mod knowledge.Module, in knowledge.Input) string {
	if err := knowledge.ValidateSlug(in.Slug); err != nil {
		return err.Error()
	}
	if strings.TrimSpace(in.Title) == "" {
		return "title required"
	}
	if err := mod.ValidateInput(in); err != nil {
		return err.Error()
	}
	return ""
}

// createUserOrInstanceMemory inserts a memory entry at user or
// instance scope. ownerUserID == nil → instance scope; non-nil → user
// scope. The full canonical-issue side-effects (history snapshot,
// mutation_log row, system tags, SSE broadcast) ride along so the
// promoted entry is indistinguishable from a freshly-created project
// memory in audit logs.
func createUserOrInstanceMemory(r *http.Request, mod knowledge.Module, ownerUserID *int64, in knowledge.Input) (knowledge.Output, error) {
	metaJSON, err := mod.MarshalMeta(in.Metadata)
	if err != nil {
		return knowledge.Output{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = mod.DefaultStatus()
	}
	var createdBy *int64
	if u := auth.GetUser(r); u != nil {
		createdBy = &u.ID
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return knowledge.Output{}, err
	}
	defer tx.Rollback()

	// Issue numbering for non-project rows: scoped to (project_id IS NULL,
	// user_id IS NULL) for instance, or (user_id = ?) for user. Using a
	// shared counter per scope keeps the issue_key resolution path
	// reasonable (PAI-339: knowledge entries can carry issue_key aliases).
	var nextNum int
	if ownerUserID == nil {
		err = tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues
			 WHERE project_id IS NULL AND user_id IS NULL`).Scan(&nextNum)
	} else {
		err = tx.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues
			 WHERE project_id IS NULL AND user_id = ?`, *ownerUserID).Scan(&nextNum)
	}
	if err != nil {
		return knowledge.Output{}, err
	}

	res, err := tx.ExecContext(r.Context(), `
		INSERT INTO issues(project_id, user_id, issue_number, type, title, description, status, priority,
		                   created_by, slug, category_metadata)
		VALUES(NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, ownerUserID, nextNum, mod.Type(), strings.TrimSpace(in.Title), in.Body, status, "medium",
		createdBy, in.Slug, metaJSON)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return knowledge.Output{}, knowledge.ErrSlugTaken
		}
		return knowledge.Output{}, err
	}
	id, _ := res.LastInsertId()

	afterSnap, err := fetchIssueMutationSnapshotTx(tx, id)
	if err != nil {
		return knowledge.Output{}, err
	}
	var mutUserID *int64
	if u := auth.GetUser(r); u != nil {
		mutUserID = &u.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       mutUserID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.create"),
		SubjectType:  "issue",
		SubjectID:    id,
		InverseOp: InverseOp{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/issues/%d", id),
		},
		BeforeState: nil,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		return knowledge.Output{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Output{}, err
	}

	if issue := getIssueByID(id); issue != nil {
		saveSnapshot(issue, auth.GetUser(r), r)
	}
	EvaluateSystemTags(id)

	out, err := loadOneUserOrInstanceMemoryByID(id, mod)
	return out, err
}

// updateUserOrInstanceMemory mutates an existing user / instance
// memory entry. Mirrors the project-scope updateKnowledgeEntry but the
// WHERE clause uses (user_id IS NULL or =) instead of (project_id =).
func updateUserOrInstanceMemory(r *http.Request, mod knowledge.Module, ownerUserID *int64, currentSlug string, in knowledge.Input) (knowledge.Output, error) {
	metaJSON, err := mod.MarshalMeta(in.Metadata)
	if err != nil {
		return knowledge.Output{}, err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return knowledge.Output{}, err
	}
	defer tx.Rollback()

	var existingID int64
	if ownerUserID == nil {
		err = tx.QueryRowContext(r.Context(),
			`SELECT id FROM issues
			 WHERE project_id IS NULL AND user_id IS NULL
			   AND type = ? AND slug = ? AND deleted_at IS NULL`,
			mod.Type(), currentSlug).Scan(&existingID)
	} else {
		err = tx.QueryRowContext(r.Context(),
			`SELECT id FROM issues
			 WHERE project_id IS NULL AND user_id = ?
			   AND type = ? AND slug = ? AND deleted_at IS NULL`,
			*ownerUserID, mod.Type(), currentSlug).Scan(&existingID)
	}
	if err != nil {
		return knowledge.Output{}, err
	}

	beforeSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return knowledge.Output{}, err
	}

	statusUpdate := strings.TrimSpace(in.Status)
	args := []any{
		strings.TrimSpace(in.Title), in.Body, in.Slug, metaJSON, now, existingID,
	}
	updateSQL := `UPDATE issues
		   SET title             = ?,
		       description       = ?,
		       slug              = ?,
		       category_metadata = ?,
		       updated_at        = ?`
	if statusUpdate != "" {
		updateSQL += `, status = ?`
		args = []any{
			strings.TrimSpace(in.Title), in.Body, in.Slug, metaJSON, now, statusUpdate, existingID,
		}
	}
	updateSQL += ` WHERE id = ?`

	if _, err := tx.ExecContext(r.Context(), updateSQL, args...); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return knowledge.Output{}, knowledge.ErrSlugTaken
		}
		return knowledge.Output{}, err
	}

	afterSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return knowledge.Output{}, err
	}
	var mutUserID *int64
	if u := auth.GetUser(r); u != nil {
		mutUserID = &u.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       mutUserID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.update"),
		SubjectType:  "issue",
		SubjectID:    existingID,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/issues/%d", existingID),
			Body:   beforeSnap,
		},
		BeforeState: beforeSnap,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		return knowledge.Output{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Output{}, err
	}

	if issue := getIssueByID(existingID); issue != nil {
		saveSnapshot(issue, auth.GetUser(r), r)
	}
	EvaluateSystemTags(existingID)

	return loadOneUserOrInstanceMemoryByID(existingID, mod)
}

// deleteUserOrInstanceMemory soft-deletes a user / instance memory
// entry. Same shape as deleteKnowledgeEntry — record mutation_log,
// snapshot history, fan out the SSE event — just with the user-scope
// WHERE clause.
func deleteUserOrInstanceMemory(r *http.Request, mod knowledge.Module, ownerUserID *int64, slug string) (int64, error) {
	user := auth.GetUser(r)
	var deletedBy *int64
	if user != nil {
		deletedBy = &user.ID
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var existingID int64
	if ownerUserID == nil {
		err = tx.QueryRowContext(r.Context(),
			`SELECT id FROM issues
			 WHERE project_id IS NULL AND user_id IS NULL
			   AND type = ? AND slug = ? AND deleted_at IS NULL`,
			mod.Type(), slug).Scan(&existingID)
	} else {
		err = tx.QueryRowContext(r.Context(),
			`SELECT id FROM issues
			 WHERE project_id IS NULL AND user_id = ?
			   AND type = ? AND slug = ? AND deleted_at IS NULL`,
			*ownerUserID, mod.Type(), slug).Scan(&existingID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	beforeSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(r.Context(), `
		UPDATE issues
		   SET deleted_at = datetime('now'),
		       deleted_by = ?
		 WHERE id = ?
		   AND deleted_at IS NULL
	`, deletedBy, existingID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, nil
	}

	afterSnap, err := fetchIssueMutationSnapshotTx(tx, existingID)
	if err != nil {
		return 0, err
	}
	var mutUserID *int64
	if user != nil {
		mutUserID = &user.ID
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       mutUserID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.delete"),
		SubjectType:  "issue",
		SubjectID:    existingID,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/issues/%d", existingID),
			Body:   beforeSnap,
		},
		BeforeState: beforeSnap,
		AfterState:  afterSnap,
		Undoable:    true,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	if snap := getIssueByID(existingID); snap != nil {
		saveSnapshot(snap, user, r)
	}
	return n, nil
}

// loadUserMemoryByType returns the live entries of `mod.Type()` owned
// by the given user. Sort by slug for stable pagination.
func loadUserMemoryByType(userID int64, mod knowledge.Module) ([]knowledge.Output, error) {
	rows, err := db.DB.Query(`
		SELECT id, COALESCE(project_id,0), type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at
		  FROM issues
		 WHERE project_id IS NULL
		   AND user_id    = ?
		   AND type       = ?
		   AND deleted_at IS NULL
		   AND slug       IS NOT NULL
	  ORDER BY slug ASC
	`, userID, mod.Type())
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

// loadOneUserMemoryBySlug is the single-row variant. sql.ErrNoRows
// propagates so the handler can return 404.
func loadOneUserMemoryBySlug(userID int64, mod knowledge.Module, slug string) (knowledge.Output, error) {
	row := db.DB.QueryRow(`
		SELECT id, COALESCE(project_id,0), type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at
		  FROM issues
		 WHERE project_id IS NULL
		   AND user_id    = ?
		   AND type       = ?
		   AND slug       = ?
		   AND deleted_at IS NULL
	`, userID, mod.Type(), slug)
	return scanUserOrInstanceOutput(row, mod)
}

// loadOneUserOrInstanceMemoryByID re-reads a freshly-written row by
// primary key. Used by the create / update paths to return the
// canonical Output payload after a write.
func loadOneUserOrInstanceMemoryByID(id int64, mod knowledge.Module) (knowledge.Output, error) {
	row := db.DB.QueryRow(`
		SELECT id, COALESCE(project_id,0), type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at
		  FROM issues
		 WHERE id = ?
	`, id)
	return scanUserOrInstanceOutput(row, mod)
}

// rowScannerLite is duplicated from the knowledge sub-package so this
// file can scan from either a *sql.Row or *sql.Rows. Keeping it
// package-private avoids an unnecessary export from `knowledge`.
type rowScannerLite interface {
	Scan(dest ...any) error
}

// scanUserOrInstanceOutput materialises a row into knowledge.Output.
// project_id is COALESCE'd to 0 (nil-safe via `int64`) — the API
// payload represents user / instance memory with project_id == 0 so
// clients can distinguish the scope without a separate field. Bundle
// resolution (CLI) treats project_id == 0 as "not project-scoped".
func scanUserOrInstanceOutput(s rowScannerLite, mod knowledge.Module) (knowledge.Output, error) {
	var (
		o       knowledge.Output
		metaRaw string
	)
	if err := s.Scan(
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
