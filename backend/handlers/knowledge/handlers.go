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

package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/contracts"
	"github.com/markus-barta/paimos/backend/db"
)

// ── HTTP handlers ───────────────────────────────────────────────
//
// PAI-394 collapsed the original five-alias surface
// (`/memory`, `/runbooks`, `/external-systems`, `/related-projects`,
// `/guidelines`) into one resource:
//
//     /api/projects/{id}/knowledge                # cross-type list
//     /api/projects/{id}/knowledge/{type}/{slug}  # single entry
//
// {type} is the kebab URL form of the SQL discriminator (mechanical
// underscore → hyphen). The handlers below read it from the chi
// URL param at request time, so a new Module costs zero new routes.

// ListAllHandler is the GET /api/projects/{id}/knowledge endpoint.
// Returns every non-trashed knowledge entry across all five types
// for the project, ordered by (type ASC, slug ASC). An optional
// `?type=<kebab>` filter narrows to a single discriminator and
// gives the convenience-endpoint feel that the old per-alias
// surface used to provide.
//
// Empty array (never null) when there are no entries — clients can
// iterate without a nil check.
func ListAllHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	if typeFilter != "" {
		typ, err := TypeFromURLSegment(typeFilter)
		if err != nil {
			writeError(w, r, err.Error(), http.StatusBadRequest)
			return
		}
		mod, _ := RouteByType(typ)
		out, err := loadByType(projectID, mod)
		if err != nil {
			writeError(w, r, "query failed", http.StatusInternalServerError)
			return
		}
		computeNeedsReview(out)
		writeJSON(w, http.StatusOK, out)
		return
	}
	out, err := loadAllTypes(projectID)
	if err != nil {
		writeError(w, r, "query failed", http.StatusInternalServerError)
		return
	}
	computeNeedsReview(out)
	writeJSON(w, http.StatusOK, out)
}

// GetHandler is the GET /api/projects/{id}/knowledge/{type}/{slug}
// endpoint. Resolves the Module from the URL segment per request,
// then returns the single matching entry. 404 when no live row
// matches (project_id, type, slug).
func GetHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}
	mod, ok := moduleFromURL(w, r)
	if !ok {
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, r, "slug required", http.StatusBadRequest)
		return
	}
	out, err := loadOneBySlug(projectID, mod, slug)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, r, "query failed", http.StatusInternalServerError)
		return
	}
	// PAI-351 slice 2 — needs_review is cross-entry derived, so a single load
	// can't carry it. For memory, recompute against the project's memory set
	// and copy the flag onto this entry so the detail read agrees with the
	// list / graph / dependents surfaces (and with any API/MCP consumer).
	if out.Type == memoryModuleInstance.Type() {
		if siblings, sErr := loadByType(projectID, memoryModuleInstance); sErr == nil {
			computeNeedsReview(siblings)
			for _, e := range siblings {
				if e.Slug == out.Slug {
					out.NeedsReview = e.NeedsReview
					out.ReviewReason = e.ReviewReason
					break
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateHandler is the POST /api/projects/{id}/knowledge endpoint.
// Unlike the legacy per-alias endpoints, type lives in the request
// body so a single route serves every Module. Body shape:
//
//	{
//	  "type":     "memory",          // required, kebab-or-snake — both accepted
//	  "slug":     "feedback_alpha",  // required
//	  "title":    "...",             // required
//	  "body":     "...",             // optional
//	  "status":   "backlog",         // optional, defaults to mod.DefaultStatus()
//	  "metadata": {...}              // optional, per-Module shape
//	}
//
// UNIQUE constraint violations on (project_id, type, slug) map to
// 409. Reserved memory subroute slugs (PAI-394) are rejected at
// 400 so they can't shadow `/knowledge/memory/references` etc.
func CreateHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}
	var in struct {
		Type     string         `json:"type"`
		Slug     string         `json:"slug"`
		Title    string         `json:"title"`
		Body     string         `json:"body"`
		Status   string         `json:"status"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, "invalid body", http.StatusBadRequest)
		return
	}
	// Type may travel in the body (canonical) or as a `?type=...`
	// query parameter (convenience for URL-driven clients like
	// curl one-liners and the `paimos knowledge create --type X`
	// CLI). Body wins when both are present and they disagree.
	rawType := strings.TrimSpace(in.Type)
	if rawType == "" {
		rawType = strings.TrimSpace(r.URL.Query().Get("type"))
	}
	mod, err := resolveBodyType(rawType)
	if err != nil {
		if strings.TrimSpace(rawType) != "" {
			writeEnumViolation(w, r, "type", rawType, AllTypes())
		} else {
			writeError(w, r, err.Error(), http.StatusBadRequest)
		}
		return
	}
	payload := Input{
		Slug:     in.Slug,
		Title:    in.Title,
		Body:     in.Body,
		Status:   in.Status,
		Metadata: in.Metadata,
	}
	if problem := validateInput(mod, payload); problem != nil {
		writeValidationProblem(w, r, problem, http.StatusBadRequest)
		return
	}
	// PAI-353 — when the parent `handlers` package has registered
	// a hook, route the insert through it so the new issue picks
	// up history-snapshot + mutation_log + system-tag side-effects.
	insert := insertEntry
	if CreateEntryHook != nil {
		insert = CreateEntryHook
	}
	out, err := insert(r, projectID, mod, payload)
	if errors.Is(err, errSlugTaken) {
		writeError(w, r, "slug already exists for this type", http.StatusConflict)
		return
	}
	// PAI-349 — typed propose errors carry their own HTTP status
	// (rate limit → 429, opt-out → 503). Surface verbatim.
	if err != nil {
		if coded, ok := err.(httpCodedError); ok {
			writeError(w, r, err.Error(), coded.HTTPStatus())
			return
		}
		writeError(w, r, "insert failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// UpdateHandler is the PUT /api/projects/{id}/knowledge/{type}/{slug}
// endpoint. The {slug} in the URL identifies the row; the body's
// `slug` field, if provided, renames it (the partial UNIQUE INDEX
// still enforces (project_id, type, slug)).
//
// 404 when no row matches the URL pair; 409 on rename collision.
// `type` in the body is allowed but must equal the URL type — we
// reject cross-type updates so a `PUT /runbook/X` body can't
// secretly promote the entry to a memory.
func UpdateHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}
	mod, ok := moduleFromURL(w, r)
	if !ok {
		return
	}
	current := strings.TrimSpace(chi.URLParam(r, "slug"))
	if current == "" {
		writeError(w, r, "slug required", http.StatusBadRequest)
		return
	}
	var in struct {
		Type     string         `json:"type"`
		Slug     string         `json:"slug"`
		Title    string         `json:"title"`
		Body     string         `json:"body"`
		Status   string         `json:"status"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Type) != "" {
		bodyMod, err := resolveBodyType(in.Type)
		if err != nil {
			writeEnumViolation(w, r, "type", in.Type, AllTypes())
			return
		}
		if bodyMod.Type() != mod.Type() {
			writeError(w, r, "body type does not match URL type", http.StatusBadRequest)
			return
		}
	}
	payload := Input{
		Slug:     in.Slug,
		Title:    in.Title,
		Body:     in.Body,
		Status:   in.Status,
		Metadata: in.Metadata,
	}
	// PUT semantics: URL slug is canonical when body omits it.
	if strings.TrimSpace(payload.Slug) == "" {
		payload.Slug = current
	}
	if problem := validateInput(mod, payload); problem != nil {
		writeValidationProblem(w, r, problem, http.StatusBadRequest)
		return
	}
	update := updateEntry
	if UpdateEntryHook != nil {
		update = UpdateEntryHook
	}
	out, err := update(r, projectID, mod, current, payload)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, "not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, errSlugTaken) {
		writeError(w, r, "slug already exists for this type", http.StatusConflict)
		return
	}
	if err != nil {
		writeError(w, r, "update failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteHandler is the DELETE /api/projects/{id}/knowledge/{type}/{slug}
// endpoint. Soft-delete only — sets deleted_at + deleted_by, matching
// the existing /api/issues/{id} DELETE semantics so the Trash flow
// recovers knowledge entries identically. 204 on hit, 404 on miss.
func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		writeError(w, r, "invalid project id", http.StatusBadRequest)
		return
	}
	mod, ok := moduleFromURL(w, r)
	if !ok {
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, r, "slug required", http.StatusBadRequest)
		return
	}
	del := deleteEntry
	if DeleteEntryHook != nil {
		del = DeleteEntryHook
	}
	n, err := del(r, projectID, mod, slug)
	if err != nil {
		writeError(w, r, "delete failed", http.StatusInternalServerError)
		return
	}
	if n == 0 {
		writeError(w, r, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── shared helpers ──────────────────────────────────────────────

// moduleFromURL resolves the {type} chi URL param to a Module,
// writing the appropriate 400 / 404 error response to w on failure.
// The bool return tells the caller whether to continue.
func moduleFromURL(w http.ResponseWriter, r *http.Request) (Module, bool) {
	seg := strings.TrimSpace(chi.URLParam(r, "type"))
	if seg == "" {
		writeError(w, r, "type required", http.StatusBadRequest)
		return nil, false
	}
	typ, err := TypeFromURLSegment(seg)
	if err != nil {
		// Unknown type — 404, since the URL space for this type
		// genuinely doesn't exist. Distinguishes from a 400 on
		// malformed input.
		writeError(w, r, err.Error(), http.StatusNotFound)
		return nil, false
	}
	mod, _ := RouteByType(typ)
	return mod, true
}

// resolveBodyType accepts either the SQL discriminator
// (`external_system`) or the kebab URL form (`external-system`)
// in a request body's `type` field. We accept both because the
// CLI and the SPA emit different shapes — the CLI thinks in URL
// segments, the schema/SQL stores discriminators. Either is
// unambiguously resolvable; we normalise here so handlers see
// one type.
func resolveBodyType(raw string) (Module, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("type required")
	}
	// Try direct discriminator first (no '-' present).
	if mod, err := RouteByType(raw); err == nil {
		return mod, nil
	}
	// Fall back to URL-segment interpretation. This rejects
	// non-canonical mixes like `external_System` or
	// `external--system` because TypeFromURLSegment requires a
	// round-trip equality.
	typ, err := TypeFromURLSegment(raw)
	if err != nil {
		return nil, errors.New("unknown knowledge type: " + raw)
	}
	mod, _ := RouteByType(typ)
	return mod, nil
}

// deleteEntry is the direct-SQL fallback used when no hook is
// registered. Soft-deletes the row matching (project_id, type, slug)
// and returns the affected-row count so the caller can map 0 to 404.
func deleteEntry(r *http.Request, projectID int64, mod Module, slug string) (int64, error) {
	var deletedBy *int64
	if user := auth.GetUser(r); user != nil {
		deletedBy = &user.ID
	}
	res, err := db.DB.Exec(`
		UPDATE issues
		   SET deleted_at = datetime('now'),
		       deleted_by = ?
		 WHERE project_id = ?
		   AND type       = ?
		   AND slug       = ?
		   AND deleted_at IS NULL
	`, deletedBy, projectID, mod.Type(), slug)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ── internals ───────────────────────────────────────────────────

// errSlugTaken is the sentinel returned when a UNIQUE constraint
// violation was detected on the (type, slug, project_id) index.
var errSlugTaken = errors.New("slug already exists for this type")

// validateInput bundles the shared (slug + title) checks with the
// per-type Module check and the PAI-394 reserved-slug guard.
// Returns "" on success, an actionable error string on failure.
type validationProblem struct {
	Detail      string
	Code        string
	Field       string
	ValidValues []string
}

func validateInput(mod Module, in Input) *validationProblem {
	if err := ValidateSlug(in.Slug); err != nil {
		return &validationProblem{Detail: err.Error(), Code: "bad_request", Field: "slug"}
	}
	if IsReservedSlug(mod.Type(), in.Slug) {
		return &validationProblem{Detail: "slug is reserved for the unified subroute (rename it)", Code: "bad_request", Field: "slug"}
	}
	if strings.TrimSpace(in.Title) == "" {
		return &validationProblem{Detail: "title required", Code: "bad_request", Field: "title"}
	}
	if status := strings.TrimSpace(in.Status); status != "" && !contracts.Contains(contracts.KnowledgeStatuses, status) {
		return &validationProblem{
			Detail:      fmt.Sprintf("status %q is not valid; expected one of: %s", status, strings.Join(contracts.KnowledgeStatuses, ", ")),
			Code:        "enum_violation",
			Field:       "status",
			ValidValues: append([]string(nil), contracts.KnowledgeStatuses...),
		}
	}
	if err := mod.ValidateInput(in); err != nil {
		return &validationProblem{Detail: err.Error(), Code: "bad_request"}
	}
	return nil
}

// insertEntry writes a fresh knowledge entry. Issue numbering is
// assigned the same way CreateIssue does — MAX(issue_number) + 1
// per project — so knowledge entries share the project's
// numbering namespace with regular issues. That keeps issue_key
// resolution consistent (e.g. "PAI-339" can refer to a memory
// entry too).
func insertEntry(r *http.Request, projectID int64, mod Module, in Input) (Output, error) {
	metaJSON, err := mod.MarshalMeta(in.Metadata)
	if err != nil {
		return Output{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = mod.DefaultStatus()
	}
	var createdBy *int64
	if user := auth.GetUser(r); user != nil {
		createdBy = &user.ID
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return Output{}, err
	}
	defer tx.Rollback()

	nextNum, err := db.NextIssueNumber(r.Context(), tx, projectID)
	if err != nil {
		return Output{}, err
	}

	res, err := tx.ExecContext(r.Context(), `
		INSERT INTO issues(project_id, issue_number, type, title, description, status, priority,
		                   created_by, slug, category_metadata)
		VALUES(?,?,?,?,?,?,?,?,?,?)
	`, projectID, nextNum, mod.Type(), strings.TrimSpace(in.Title), in.Body, status, "medium",
		createdBy, in.Slug, metaJSON)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Output{}, errSlugTaken
		}
		return Output{}, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return Output{}, err
	}
	out, err := loadOneByID(id, mod)
	if err != nil {
		return Output{}, err
	}
	return out, nil
}

// updateEntry mutates an existing knowledge entry in place. Both
// the URL slug and (project_id, type) scope the WHERE so cross-
// type / cross-project slug collisions stay impossible. Rename
// goes through the same UNIQUE constraint via the partial index.
func updateEntry(r *http.Request, projectID int64, mod Module, currentSlug string, in Input) (Output, error) {
	metaJSON, err := mod.MarshalMeta(in.Metadata)
	if err != nil {
		return Output{}, err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	// Use a status only when the caller supplied one — otherwise
	// preserve the current value so the field acts like a partial
	// PUT for status alone.
	statusUpdate := strings.TrimSpace(in.Status)

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return Output{}, err
	}
	defer tx.Rollback()

	var existingID int64
	if err := tx.QueryRowContext(r.Context(),
		`SELECT id FROM issues WHERE project_id=? AND type=? AND slug=? AND deleted_at IS NULL`,
		projectID, mod.Type(), currentSlug).Scan(&existingID); err != nil {
		return Output{}, err
	}

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
			return Output{}, errSlugTaken
		}
		return Output{}, err
	}
	if err := tx.Commit(); err != nil {
		return Output{}, err
	}
	return loadOneByID(existingID, mod)
}

// loadByType returns the array of live entries of the given type
// for the project, ordered by slug. Sorted output gives stable
// pagination regardless of insertion order.
func loadByType(projectID int64, mod Module) ([]Output, error) {
	rows, err := db.DB.Query(`
		SELECT id, project_id, type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at,
		       reference_count, COALESCE(last_referenced_at,''),
		       COALESCE(content_revised_at,''), COALESCE(deps_reviewed_at,'')
		  FROM issues
		 WHERE project_id = ?
		   AND type       = ?
		   AND deleted_at IS NULL
		   AND slug       IS NOT NULL
	  ORDER BY slug ASC
	`, projectID, mod.Type())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Output{}
	for rows.Next() {
		o, err := scanRowByType(rows, mod)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// loadAllTypes returns every live knowledge entry for the project,
// across all five types, ordered by (type ASC, slug ASC). PAI-394
// added this path so the unified GET /knowledge endpoint can serve
// cross-type queries with one round-trip — clients no longer have
// to issue five parallel requests to assemble a project's
// knowledge view.
//
// Each row is scanned with the correct Module so per-type metadata
// unmarshals into the canonical shape — mixing in one slice is
// safe because Output carries the discriminator.
func loadAllTypes(projectID int64) ([]Output, error) {
	rows, err := db.DB.Query(`
		SELECT id, project_id, type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at,
		       reference_count, COALESCE(last_referenced_at,''),
		       COALESCE(content_revised_at,''), COALESCE(deps_reviewed_at,'')
		  FROM issues
		 WHERE project_id = ?
		   AND type IN ('memory','runbook','external_system','related_project','guideline')
		   AND deleted_at IS NULL
		   AND slug       IS NOT NULL
	  ORDER BY type ASC, slug ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Output{}
	for rows.Next() {
		o, err := scanRowAcrossTypes(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// loadOneBySlug returns the single live entry matching
// (project_id, type, slug). sql.ErrNoRows propagates so the
// handler can map it to 404.
func loadOneBySlug(projectID int64, mod Module, slug string) (Output, error) {
	row := db.DB.QueryRow(`
		SELECT id, project_id, type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at,
		       reference_count, COALESCE(last_referenced_at,''),
		       COALESCE(content_revised_at,''), COALESCE(deps_reviewed_at,'')
		  FROM issues
		 WHERE project_id = ?
		   AND type       = ?
		   AND slug       = ?
		   AND deleted_at IS NULL
	`, projectID, mod.Type(), slug)
	return scanRowByType(row, mod)
}

// LoadOneByID is the exported sibling of loadOneByID — same behavior,
// usable from the parent handlers package's PAI-353 write hooks so
// they can build the canonical Output payload after a write.
func LoadOneByID(id int64, mod Module) (Output, error) {
	return loadOneByID(id, mod)
}

func loadOneByID(id int64, mod Module) (Output, error) {
	row := db.DB.QueryRow(`
		SELECT id, project_id, type, COALESCE(slug,''), title, description,
		       status, COALESCE(category_metadata,''), created_at, updated_at,
		       reference_count, COALESCE(last_referenced_at,''),
		       COALESCE(content_revised_at,''), COALESCE(deps_reviewed_at,'')
		  FROM issues
		 WHERE id = ?
	`, id)
	return scanRowByType(row, mod)
}

// rowScanner abstracts *sql.Row vs *sql.Rows so the scan helpers
// can serve both single-row and list paths.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRowByType decodes a row into Output using a known Module.
// Used by the type-filtered list and single-entry paths.
func scanRowByType(s rowScanner, mod Module) (Output, error) {
	var (
		o       Output
		metaRaw string
	)
	if err := s.Scan(
		&o.ID, &o.ProjectID, &o.Type, &o.Slug, &o.Title, &o.Body,
		&o.Status, &metaRaw, &o.CreatedAt, &o.UpdatedAt,
		&o.ReferenceCount, &o.LastReferencedAt,
		&o.ContentRevisedAt, &o.DepsReviewedAt,
	); err != nil {
		return o, err
	}
	meta, err := mod.UnmarshalMeta(metaRaw)
	if err != nil {
		// On corrupt JSON in the column, fall back to an empty
		// map so the API stays usable. The corrupt bytes are
		// preserved at the DB layer for an operator to inspect.
		meta = map[string]any{}
	}
	o.Metadata = meta
	return o, nil
}

// scanRowAcrossTypes is the cross-type list path. It first reads
// the discriminator off the row, then looks up the matching Module
// to unmarshal the per-type tail metadata.
func scanRowAcrossTypes(s rowScanner) (Output, error) {
	var (
		o       Output
		metaRaw string
	)
	if err := s.Scan(
		&o.ID, &o.ProjectID, &o.Type, &o.Slug, &o.Title, &o.Body,
		&o.Status, &metaRaw, &o.CreatedAt, &o.UpdatedAt,
		&o.ReferenceCount, &o.LastReferencedAt,
		&o.ContentRevisedAt, &o.DepsReviewedAt,
	); err != nil {
		return o, err
	}
	mod, err := RouteByType(o.Type)
	if err != nil {
		// Shouldn't happen — the SQL WHERE clause guards the
		// type column. Defensive fallback to an empty map.
		o.Metadata = map[string]any{}
		return o, nil
	}
	meta, err := mod.UnmarshalMeta(metaRaw)
	if err != nil {
		meta = map[string]any{}
	}
	o.Metadata = meta
	return o, nil
}

// projectIDFromRequest mirrors the helper in the parent handlers
// package — duplicated here to keep this sub-package free of
// import cycles back to handlers.
func projectIDFromRequest(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, msg string, code int) {
	writeProblem(w, r, validationProblem{Detail: msg, Code: codeForStatus(code)}, code)
}

func writeValidationProblem(w http.ResponseWriter, r *http.Request, problem *validationProblem, code int) {
	if problem == nil {
		return
	}
	writeProblem(w, r, *problem, code)
}

func writeEnumViolation(w http.ResponseWriter, r *http.Request, field, value string, validValues []string) {
	writeProblem(w, r, validationProblem{
		Detail:      fmt.Sprintf("%s %q is not valid; expected one of: %s", field, strings.TrimSpace(value), strings.Join(validValues, ", ")),
		Code:        "enum_violation",
		Field:       field,
		ValidValues: append([]string(nil), validValues...),
	}, http.StatusBadRequest)
}

func writeProblem(w http.ResponseWriter, r *http.Request, problem validationProblem, status int) {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	code := strings.TrimSpace(problem.Code)
	if code == "" {
		code = codeForStatus(status)
	}
	detail := strings.TrimSpace(problem.Detail)
	if detail == "" {
		detail = http.StatusText(status)
	}
	payload := map[string]any{
		"type":   "https://paimos.com/errors/" + code,
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
		"code":   code,
		"error":  detail,
	}
	if r != nil {
		payload["instance"] = r.URL.RequestURI()
		if reqID := strings.TrimSpace(r.Header.Get("X-PAIMOS-Request-Id")); reqID != "" {
			payload["request_id"] = reqID
		}
	}
	if _, ok := payload["request_id"]; !ok {
		if reqID := strings.TrimSpace(w.Header().Get("X-PAIMOS-Request-Id")); reqID != "" {
			payload["request_id"] = reqID
		}
	}
	if problem.Field != "" {
		payload["field"] = problem.Field
	}
	if len(problem.ValidValues) > 0 {
		payload["valid_values"] = problem.ValidValues
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "http_error"
	}
}
