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

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// MaxBatchSize is the hard cap for bulk endpoints. Above this, 413 with
// a clear message. The ceiling is low on purpose — bulk ops lock the
// SQLite writer for the whole transaction, so 100-item batches are
// plenty for agent scripting without blocking the UI.
const MaxBatchSize = 100

// projectKeyPattern matches strings shaped like a project key — one
// uppercase letter, up to 15 more uppercase alphanumerics. Used on the
// /projects/{key}/... batch route to distinguish keys from numeric ids.
var projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}$`)

// resolveProjectRef accepts a project key ("PAI") or numeric id ("6") and
// returns the project's numeric id. Returns (0, false) if the input is
// neither a known project key nor a positive integer.
func resolveProjectRef(raw string) (int64, bool) {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
		return n, true
	}
	if !projectKeyPattern.MatchString(raw) {
		return 0, false
	}
	var id int64
	err := db.DB.QueryRow("SELECT id FROM projects WHERE key=? AND status != 'deleted'", raw).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// BatchCreateItem is one row of POST /api/projects/{key}/issues/batch.
// Mirrors the CreateIssue body, plus parent_ref for same-batch cross-
// references (e.g. "#0" refers to the first item in the same array —
// used for the "create an epic and its child tickets in one call" flow).
type BatchCreateItem struct {
	Title              string   `json:"title"`
	Type               string   `json:"type"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Notes              string   `json:"notes"`
	Status             string   `json:"status"`
	Priority           string   `json:"priority"`
	ParentID           *int64   `json:"parent_id"`
	ParentRef          *string  `json:"parent_ref"`
	AssigneeID         *int64   `json:"assignee_id"`
	CostUnit           string   `json:"cost_unit"`
	Release            string   `json:"release"`
	StartDate          string   `json:"start_date"`
	EndDate            string   `json:"end_date"`
	EstimateHours      *float64 `json:"estimate_hours"`
	EstimateLp         *float64 `json:"estimate_lp"`
}

// BatchUpdateItem is one row of PATCH /api/issues. `ref` is either a
// numeric id or an issue key ("PAI-83"); `fields` carries the partial
// update payload (same allowed fields as PUT /api/issues/{id}).
type BatchUpdateItem struct {
	Ref    string          `json:"ref"`
	Fields json.RawMessage `json:"fields"`
}

// BatchError is the per-row status returned when a batch is rejected.
// The whole batch rolls back; this list tells the caller which rows
// caused the failure so they can fix and re-send.
type BatchError struct {
	Index int    `json:"index"`
	Ref   string `json:"ref,omitempty"`
	Error string `json:"error"`
}

// writeBatchError replies with 400 + the per-row error list and a
// rolled_back=true marker so agents can tell this apart from
// per-item-success responses.
func writeBatchError(w http.ResponseWriter, errs []BatchError, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors":      errs,
		"rolled_back": true,
	})
}

// CreateIssuesBatch atomically creates N issues under a single project.
//
// POST /api/projects/{key}/issues/batch
//
// Body: JSON array of BatchCreateItem (max MaxBatchSize). parent_ref
// "#N" refers to item N in the same batch (zero-based, must be an
// earlier index). External parent_id (pointing outside the batch)
// is honoured with the same cross-project rules as CreateIssue.
//
// On any validation failure: rollback, 400, per-row errors. On
// success: 201, body {issues: [...]} in request order.
//
// Admin-only. Does NOT run the auto-promote-parent-epic / billing-
// timestamp / cascade-children side effects of single-issue CreateIssue
// — bulk ops are deliberately mechanical; the CLI calls single
// endpoints when it needs the full lifecycle.
func CreateIssuesBatch(w http.ResponseWriter, r *http.Request) {
	projectID, ok := resolveProjectRef(chi.URLParam(r, "key"))
	if !ok {
		jsonError(w, "unknown project", http.StatusNotFound)
		return
	}

	var items []BatchCreateItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		jsonError(w, "invalid body: expected JSON array", http.StatusBadRequest)
		return
	}
	if len(items) == 0 {
		jsonError(w, "batch is empty", http.StatusBadRequest)
		return
	}
	if len(items) > MaxBatchSize {
		jsonError(w,
			fmt.Sprintf("batch size %d exceeds limit %d", len(items), MaxBatchSize),
			http.StatusRequestEntityTooLarge)
		return
	}

	// Phase 1: static validation (shape + same-batch parent_ref).
	// Any error here returns 400 without touching the DB.
	errs := validateBatchCreate(items, projectID)
	if len(errs) > 0 {
		writeBatchError(w, errs, http.StatusBadRequest)
		return
	}

	// Phase 2: execute inside a single transaction.
	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "tx begin failed", http.StatusInternalServerError)
		return
	}
	// Safety net — ensures a rollback on any panic/early return.
	defer func() {
		_ = tx.Rollback()
	}()

	var nextNum int
	if err := tx.QueryRow(
		"SELECT COALESCE(MAX(issue_number),0) FROM issues WHERE project_id=?", projectID,
	).Scan(&nextNum); err != nil {
		jsonError(w, "numbering failed", http.StatusInternalServerError)
		return
	}

	var createdByID *int64
	if user := auth.GetUser(r); user != nil {
		createdByID = &user.ID
	}

	insertedIDs := make([]int64, len(items))
	for i, it := range items {
		nextNum++
		typ := it.Type
		if typ == "" {
			typ = inferType(it.ParentID)
		}
		status := it.Status
		if status == "" {
			status = "new"
		}
		priority := it.Priority
		if priority == "" {
			priority = "medium"
		}

		// Resolve effective parent_id: parent_ref (same-batch) takes
		// precedence over parent_id. Validation already ensured at most
		// one is set AND that parent_ref points at an earlier index.
		var parentID *int64
		if it.ParentRef != nil {
			idx, _ := parseParentRef(*it.ParentRef)
			pid := insertedIDs[idx]
			parentID = &pid
		} else if it.ParentID != nil {
			parentID = it.ParentID
		}

		res, err := tx.Exec(`
			INSERT INTO issues(project_id,issue_number,type,parent_id,title,description,
			                   acceptance_criteria,notes,status,priority,cost_unit,release,
			                   start_date,end_date,estimate_hours,estimate_lp,
			                   assignee_id,created_by)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		`, projectID, nextNum, typ, parentID, it.Title, it.Description,
			it.AcceptanceCriteria, it.Notes, status, priority,
			it.CostUnit, it.Release,
			it.StartDate, it.EndDate,
			it.EstimateHours, it.EstimateLp,
			it.AssigneeID, createdByID)
		if err != nil {
			// DB-level rejection (e.g. CHECK constraint). Surface
			// per-row and roll back.
			writeBatchError(w, []BatchError{{
				Index: i, Error: cleanDBError(err),
			}}, http.StatusBadRequest)
			return
		}
		id, _ := res.LastInsertId()
		insertedIDs[i] = id
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}

	// Hydrate for response. Outside the tx since the data is now
	// committed and visible to the default connection.
	out := make([]*models.Issue, len(insertedIDs))
	for i, id := range insertedIDs {
		issue := getIssueByID(id)
		if issue != nil {
			saveSnapshot(issue, auth.GetUser(r))
			out[i] = issue
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"issues": out})
}

// validateBatchCreate runs static checks that don't need DB access:
// required fields, enum shape, parent_ref syntax + forward references,
// mutually exclusive parent_id/parent_ref, etc. Returns empty slice on
// clean input.
func validateBatchCreate(items []BatchCreateItem, projectID int64) []BatchError {
	var errs []BatchError
	for i, it := range items {
		if strings.TrimSpace(it.Title) == "" {
			errs = append(errs, BatchError{Index: i, Error: "title is required"})
		}
		if it.ParentID != nil && it.ParentRef != nil {
			errs = append(errs, BatchError{Index: i, Error: "parent_id and parent_ref are mutually exclusive"})
		}
		if it.ParentRef != nil {
			idx, ok := parseParentRef(*it.ParentRef)
			if !ok {
				errs = append(errs, BatchError{
					Index: i, Error: fmt.Sprintf("parent_ref %q must match shape \"#N\" with N≥0", *it.ParentRef),
				})
				continue
			}
			if idx >= i {
				errs = append(errs, BatchError{
					Index: i, Error: fmt.Sprintf("parent_ref %q must point to an earlier item (this is index %d)", *it.ParentRef, i),
				})
			}
		}
		// Cross-project parent_id — catch now; tx-time would catch too
		// but an early error gives a friendlier payload.
		if it.ParentID != nil {
			var pp sql.NullInt64
			err := db.DB.QueryRow("SELECT project_id FROM issues WHERE id=? AND deleted_at IS NULL", *it.ParentID).Scan(&pp)
			if errors.Is(err, sql.ErrNoRows) {
				errs = append(errs, BatchError{Index: i, Error: "parent_id not found"})
			} else if err == nil && pp.Valid && pp.Int64 != projectID {
				errs = append(errs, BatchError{Index: i, Error: "parent must be in the same project"})
			}
		}
	}
	return errs
}

// parseParentRef parses "#N" → (N, true) for N ≥ 0, else (0, false).
func parseParentRef(s string) (int, bool) {
	if !strings.HasPrefix(s, "#") {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// cleanDBError trims the "UNIQUE constraint failed: ..." noise from
// low-level SQLite errors into something agent-friendly.
func cleanDBError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "CHECK constraint") {
		return "value violates CHECK constraint (e.g. invalid enum) — see /api/schema"
	}
	if strings.Contains(msg, "FOREIGN KEY") {
		return "foreign-key violation (e.g. unknown parent_id / assignee_id)"
	}
	if strings.Contains(msg, "UNIQUE constraint") {
		return "row already exists (duplicate issue_number or similar)"
	}
	return msg
}

// UpdateIssuesBatch atomically applies partial updates to N issues.
//
// PATCH /api/issues
// Body: JSON array [{ref: "PAI-83"|123, fields: {...}}, ...]
//
// Allowed fields mirror PUT /api/issues/{id}. ANY row failing validation
// (unknown ref, invalid enum, mutually-exclusive type+parent…) rolls
// back the whole transaction and returns 400 with per-row errors.
//
// Admin-only. Does NOT run the auto-promote / cascade / billing-
// timestamp side effects of single-issue UpdateIssue — see
// CreateIssuesBatch for the same reasoning.
func UpdateIssuesBatch(w http.ResponseWriter, r *http.Request) {
	var items []BatchUpdateItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		jsonError(w, "invalid body: expected JSON array", http.StatusBadRequest)
		return
	}
	if len(items) == 0 {
		jsonError(w, "batch is empty", http.StatusBadRequest)
		return
	}
	if len(items) > MaxBatchSize {
		jsonError(w,
			fmt.Sprintf("batch size %d exceeds limit %d", len(items), MaxBatchSize),
			http.StatusRequestEntityTooLarge)
		return
	}

	// Phase 1: resolve refs + static validation.
	type row struct {
		index    int
		ref      string
		id       int64
		existing *models.Issue
		update   partialIssueUpdate
	}
	rows := make([]row, 0, len(items))
	var errs []BatchError
	for i, it := range items {
		if it.Ref == "" {
			errs = append(errs, BatchError{Index: i, Error: "ref is required"})
			continue
		}
		id, ok := auth.ResolveIssueRef(it.Ref)
		if !ok {
			errs = append(errs, BatchError{Index: i, Ref: it.Ref, Error: "not found"})
			continue
		}
		existing := getIssueByID(id)
		if existing == nil || existing.DeletedAt != nil {
			errs = append(errs, BatchError{Index: i, Ref: it.Ref, Error: "not found"})
			continue
		}
		upd, validationErr := parsePartialIssueUpdate(it.Fields, existing)
		if validationErr != "" {
			errs = append(errs, BatchError{Index: i, Ref: it.Ref, Error: validationErr})
			continue
		}
		rows = append(rows, row{i, it.Ref, id, existing, upd})
	}
	if len(errs) > 0 {
		writeBatchError(w, errs, http.StatusBadRequest)
		return
	}

	// Phase 2: transaction.
	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "tx begin failed", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for _, rw := range rows {
		if err := applyPartialIssueUpdate(tx, rw.id, rw.update, now); err != nil {
			writeBatchError(w, []BatchError{{
				Index: rw.index, Ref: rw.ref, Error: cleanDBError(err),
			}}, http.StatusBadRequest)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}

	// Hydrate + snapshot.
	out := make([]*models.Issue, len(rows))
	for i, rw := range rows {
		issue := getIssueByID(rw.id)
		if issue != nil {
			saveSnapshot(issue, auth.GetUser(r))
			out[i] = issue
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"issues": out})
}

// partialIssueUpdate carries the parsed allowed fields for a bulk update.
// Using typed pointers (nil = no change) matches how UpdateIssue encodes
// partiality, so the SQL shape stays identical.
type partialIssueUpdate struct {
	Title              *string
	Description        *string
	AcceptanceCriteria *string
	Notes              *string
	Type               *string
	ParentID           *int64
	Status             *string
	Priority           *string
	CostUnit           *string
	Release            *string
	AssigneeID         *int64
	StartDate          *string
	EndDate            *string
	EstimateHours      *float64
	EstimateLp         *float64
}

// parsePartialIssueUpdate decodes a generic fields map into the typed
// partial-update struct + runs validation that doesn't need the DB
// (type+parent consistency is checked against `existing`).
//
// Returns a user-facing error string when invalid; empty means OK.
func parsePartialIssueUpdate(raw json.RawMessage, existing *models.Issue) (partialIssueUpdate, string) {
	var upd partialIssueUpdate
	if len(raw) == 0 {
		return upd, "fields is required"
	}
	var body struct {
		Title              *string  `json:"title"`
		Description        *string  `json:"description"`
		AcceptanceCriteria *string  `json:"acceptance_criteria"`
		Notes              *string  `json:"notes"`
		Type               *string  `json:"type"`
		ParentID           *int64   `json:"parent_id"`
		Status             *string  `json:"status"`
		Priority           *string  `json:"priority"`
		CostUnit           *string  `json:"cost_unit"`
		Release            *string  `json:"release"`
		AssigneeID         *int64   `json:"assignee_id"`
		StartDate          *string  `json:"start_date"`
		EndDate            *string  `json:"end_date"`
		EstimateHours      *float64 `json:"estimate_hours"`
		EstimateLp         *float64 `json:"estimate_lp"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return upd, "invalid fields: " + err.Error()
	}
	upd.Title = body.Title
	upd.Description = body.Description
	upd.AcceptanceCriteria = body.AcceptanceCriteria
	upd.Notes = body.Notes
	upd.Type = body.Type
	upd.ParentID = body.ParentID
	upd.Status = body.Status
	upd.Priority = body.Priority
	upd.CostUnit = body.CostUnit
	upd.Release = body.Release
	upd.AssigneeID = body.AssigneeID
	upd.StartDate = body.StartDate
	upd.EndDate = body.EndDate
	upd.EstimateHours = body.EstimateHours
	upd.EstimateLp = body.EstimateLp

	// Hierarchy validation — mirror UpdateIssue's check.
	if body.Type != nil || body.ParentID != nil {
		newType := existing.Type
		if body.Type != nil {
			newType = *body.Type
		}
		newParent := existing.ParentID
		if body.ParentID != nil {
			newParent = body.ParentID
		}
		if err := validateParent(newType, newParent, existing.ProjectID); err != nil {
			return upd, err.Error()
		}
	}
	return upd, ""
}

// applyPartialIssueUpdate issues the UPDATE statement inside an open tx.
// Column list intentionally narrower than single-issue UpdateIssue —
// batch ops stay mechanical.
func applyPartialIssueUpdate(tx *sql.Tx, id int64, upd partialIssueUpdate, now string) error {
	_, err := tx.Exec(`
		UPDATE issues SET
			title               = COALESCE(?, title),
			description         = COALESCE(?, description),
			acceptance_criteria = COALESCE(?, acceptance_criteria),
			notes               = COALESCE(?, notes),
			type                = COALESCE(?, type),
			parent_id           = CASE WHEN ? IS NOT NULL THEN ? ELSE parent_id END,
			status              = COALESCE(?, status),
			priority            = COALESCE(?, priority),
			cost_unit           = COALESCE(?, cost_unit),
			release             = COALESCE(?, release),
			assignee_id         = CASE WHEN ? IS NOT NULL THEN ? ELSE assignee_id END,
			start_date          = COALESCE(?, start_date),
			end_date            = COALESCE(?, end_date),
			estimate_hours      = COALESCE(?, estimate_hours),
			estimate_lp         = COALESCE(?, estimate_lp),
			updated_at          = ?
		WHERE id=?
	`,
		upd.Title, upd.Description, upd.AcceptanceCriteria, upd.Notes,
		upd.Type,
		upd.ParentID, upd.ParentID,
		upd.Status, upd.Priority, upd.CostUnit, upd.Release,
		upd.AssigneeID, upd.AssigneeID,
		upd.StartDate, upd.EndDate,
		upd.EstimateHours, upd.EstimateLp,
		now, id)
	return err
}

// LookupIssuesByKeys implements GET /api/issues?keys=PAI-1,PAI-2,...
// Returns one entry per requested key IN ORDER. Missing or inaccessible
// refs appear as {ref, error: "not found"} — never silently dropped, so
// agents can reconcile exactly which keys failed.
//
// Dispatched from ListAllIssues (see issues.go) when the `keys` query
// param is present so the canonical /api/issues URL stays the entry
// point. Response shape on that code path is {issues: [...]} where any
// list element may be the error-shape above.
func LookupIssuesByKeys(w http.ResponseWriter, r *http.Request) {
	keysParam := strings.TrimSpace(r.URL.Query().Get("keys"))
	if keysParam == "" {
		jsonError(w, "keys query param required", http.StatusBadRequest)
		return
	}
	refs := splitCSV(keysParam)
	if len(refs) > MaxBatchSize {
		jsonError(w,
			fmt.Sprintf("too many keys: %d exceeds limit %d", len(refs), MaxBatchSize),
			http.StatusRequestEntityTooLarge)
		return
	}

	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		id, ok := auth.ResolveIssueRef(ref)
		if !ok {
			out = append(out, map[string]any{"ref": ref, "error": "not found"})
			continue
		}
		issue := getIssueByID(id)
		if issue == nil || issue.DeletedAt != nil {
			out = append(out, map[string]any{"ref": ref, "error": "not found"})
			continue
		}
		// Per-item access — hide inaccessible issues as not-found so the
		// endpoint doesn't leak existence to callers who can't see them.
		if issue.ProjectID != nil && !auth.CanViewProject(r, *issue.ProjectID) {
			out = append(out, map[string]any{"ref": ref, "error": "not found"})
			continue
		}
		out = append(out, issue)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"issues": out})
}

// ListOrLookupIssues is the /api/issues entry point: when `?keys=…` is
// present it dispatches to LookupIssuesByKeys (pick list semantics),
// otherwise falls through to ListAllIssues (the filtered pager).
func ListOrLookupIssues(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("keys") != "" {
		LookupIssuesByKeys(w, r)
		return
	}
	ListAllIssues(w, r)
}

