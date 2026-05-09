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
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// MakeListHandler returns the GET handler for a single knowledge
// type. Routed under /api/projects/:id/{alias}, it returns every
// non-trashed entry of the matching type for the project, ordered
// by slug. Empty array (never null) when the project has no
// entries of this kind — clients can iterate without nil checks.
func MakeListHandler(alias string) http.HandlerFunc {
	mod, err := RouteByPath(alias)
	if err != nil {
		// init-time error — surface as a panic so misconfigured
		// router wiring fails loudly.
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := projectIDFromRequest(r)
		if !ok {
			writeError(w, "invalid project id", http.StatusBadRequest)
			return
		}
		out, err := loadByType(projectID, mod)
		if err != nil {
			writeError(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// MakeGetHandler returns the GET handler for a single entry.
// Routed under /api/projects/:id/{alias}/:slug. Returns 404 when
// no live entry matches (project_id, type, slug).
func MakeGetHandler(alias string) http.HandlerFunc {
	mod, err := RouteByPath(alias)
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := projectIDFromRequest(r)
		if !ok {
			writeError(w, "invalid project id", http.StatusBadRequest)
			return
		}
		slug := strings.TrimSpace(chi.URLParam(r, "slug"))
		if slug == "" {
			writeError(w, "slug required", http.StatusBadRequest)
			return
		}
		out, err := loadOneBySlug(projectID, mod, slug)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			writeError(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// MakeCreateHandler returns the POST handler. The slug is taken
// from the request body (POST is collection-rooted). Validation
// runs slug → title → per-type Module check. UNIQUE constraint
// violations from the partial index map to 409.
func MakeCreateHandler(alias string) http.HandlerFunc {
	mod, err := RouteByPath(alias)
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := projectIDFromRequest(r)
		if !ok {
			writeError(w, "invalid project id", http.StatusBadRequest)
			return
		}
		var in Input
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, "invalid body", http.StatusBadRequest)
			return
		}
		if msg := validateInput(mod, in); msg != "" {
			writeError(w, msg, http.StatusBadRequest)
			return
		}
		// PAI-353 — when the parent `handlers` package has registered
		// a hook, route the insert through it so the new issue picks
		// up history-snapshot + mutation_log + system-tag side-effects.
		// Falls back to the direct-SQL implementation when the hook is
		// nil (sub-package tests, mostly).
		insert := insertEntry
		if CreateEntryHook != nil {
			insert = CreateEntryHook
		}
		out, err := insert(r, projectID, mod, in)
		if errors.Is(err, errSlugTaken) {
			writeError(w, "slug already exists for this type", http.StatusConflict)
			return
		}
		// PAI-349 — typed propose errors carry their own HTTP status
		// (rate limit → 429, opt-out → 503). Surface the message verbatim
		// so operators see the actionable guidance.
		if err != nil {
			if coded, ok := err.(httpCodedError); ok {
				writeError(w, err.Error(), coded.HTTPStatus())
				return
			}
			writeError(w, "insert failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

// MakeUpdateHandler returns the PUT handler. The slug in the URL
// identifies the row; the body's `slug` field, if provided,
// renames it (the partial UNIQUE INDEX still enforces the
// (type, slug, project_id) invariant). 404 when no row matches
// the URL slug.
func MakeUpdateHandler(alias string) http.HandlerFunc {
	mod, err := RouteByPath(alias)
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := projectIDFromRequest(r)
		if !ok {
			writeError(w, "invalid project id", http.StatusBadRequest)
			return
		}
		current := strings.TrimSpace(chi.URLParam(r, "slug"))
		if current == "" {
			writeError(w, "slug required", http.StatusBadRequest)
			return
		}
		var in Input
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, "invalid body", http.StatusBadRequest)
			return
		}
		// PUT semantics: URL slug is canonical when body omits it.
		if strings.TrimSpace(in.Slug) == "" {
			in.Slug = current
		}
		if msg := validateInput(mod, in); msg != "" {
			writeError(w, msg, http.StatusBadRequest)
			return
		}
		// PAI-353 — see the matching note on MakeCreateHandler. When
		// the parent handlers package registers UpdateEntryHook, the
		// knowledge UPDATE path goes through the canonical issue
		// helpers (history snapshot, mutation_log, system tags).
		update := updateEntry
		if UpdateEntryHook != nil {
			update = UpdateEntryHook
		}
		out, err := update(r, projectID, mod, current, in)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, "not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, errSlugTaken) {
			writeError(w, "slug already exists for this type", http.StatusConflict)
			return
		}
		if err != nil {
			writeError(w, "update failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// MakeDeleteHandler returns the DELETE handler. Soft-delete only
// — sets deleted_at + deleted_by, matching the existing
// /api/issues/:id DELETE semantics so the Trash flow recovers
// knowledge entries identically. 204 on hit, 404 on miss.
func MakeDeleteHandler(alias string) http.HandlerFunc {
	mod, err := RouteByPath(alias)
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := projectIDFromRequest(r)
		if !ok {
			writeError(w, "invalid project id", http.StatusBadRequest)
			return
		}
		slug := strings.TrimSpace(chi.URLParam(r, "slug"))
		if slug == "" {
			writeError(w, "slug required", http.StatusBadRequest)
			return
		}
		// PAI-353 — see the matching note on MakeCreateHandler. The
		// hook records the snapshot + mutation_log entry so trashed
		// knowledge entries are first-class on the undo / history
		// surface.
		del := deleteEntry
		if DeleteEntryHook != nil {
			del = DeleteEntryHook
		}
		n, err := del(r, projectID, mod, slug)
		if err != nil {
			writeError(w, "delete failed", http.StatusInternalServerError)
			return
		}
		if n == 0 {
			writeError(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
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
// per-type Module check. Returns "" on success, an actionable
// error string on failure — matches the project_agents.go style
// so the caller can pass the message straight into the JSON
// error response.
func validateInput(mod Module, in Input) string {
	if err := ValidateSlug(in.Slug); err != nil {
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

	var nextNum int
	if err := tx.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?`, projectID).Scan(&nextNum); err != nil {
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
	// PUT for status alone. Title / body / slug / metadata are
	// always required (or defaulted) by validateInput.
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
		       reference_count, COALESCE(last_referenced_at,'')
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
		o, err := scanOutput(rows, mod)
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
		       reference_count, COALESCE(last_referenced_at,'')
		  FROM issues
		 WHERE project_id = ?
		   AND type       = ?
		   AND slug       = ?
		   AND deleted_at IS NULL
	`, projectID, mod.Type(), slug)
	return scanOutput(row, mod)
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
		       reference_count, COALESCE(last_referenced_at,'')
		  FROM issues
		 WHERE id = ?
	`, id)
	return scanOutput(row, mod)
}

// rowScanner abstracts *sql.Row vs *sql.Rows so scanOutput can
// serve both single-row and list paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanOutput(s rowScanner, mod Module) (Output, error) {
	var (
		o       Output
		metaRaw string
	)
	if err := s.Scan(
		&o.ID, &o.ProjectID, &o.Type, &o.Slug, &o.Title, &o.Body,
		&o.Status, &metaRaw, &o.CreatedAt, &o.UpdatedAt,
		&o.ReferenceCount, &o.LastReferencedAt,
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

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
