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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// ── Portal types (customer-facing, no internal fields) ──────────────────────

type portalProject struct {
	ID           int64          `json:"id"`
	Key          string         `json:"key"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Status       string         `json:"status"`
	LogoPath     string         `json:"logo_path"`
	IssueCount   int            `json:"issue_count"`
	DoneCount    int            `json:"done_count"`
	LastActivity string         `json:"last_activity,omitempty"`
	ByStatus     map[string]int `json:"by_status,omitempty"`
	// WouldHideCount is the count of non-deleted issues in this project
	// that lack the CUSTOMERPORTAL tag — i.e. the number that would be
	// hidden if enforcement flipped on right now. Only set when
	// PAIMOS_PORTAL_VISIBILITY_DRY_RUN=true (PAI-462), so operators can
	// gauge the blast radius before unsetting the env var.
	WouldHideCount *int `json:"would_hide_count,omitempty"`
}

// portalIssue is the shape sent to the Customer Portal — strictly the
// fields a customer needs to see. PAI-474 removed the internal effort
// and pricing leak that existed before v3.6.0:
//
//   - cost_unit, release       → internal budget / release plumbing
//   - estimate_hours/_lp       → our internal effort
//   - ar_hours/_lp             → our internal effort
//   - estimate_eur, ar_eur     → derived pricing / margin signal
//
// Those fields were emitted in v3.6.0 even though the UI rendered them as
// "—"; anyone with DevTools could read them. They are now never queried
// or serialised on this path. The acceptance report (Projektbericht) is
// a separate endpoint that still includes pricing because it's the
// deliverable contract — not affected here.
type portalIssue struct {
	ID                 int64   `json:"id"`
	IssueKey           string  `json:"issue_key"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	AcceptanceCriteria string  `json:"acceptance_criteria"`
	ReportSummary      string  `json:"report_summary"`
	Status             string  `json:"status"`
	Priority           string  `json:"priority"`
	Type               string  `json:"type"`
	AcceptedAt         *string `json:"accepted_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type portalIssueListEnvelope struct {
	Issues               []portalIssue `json:"issues"`
	Total                int           `json:"total"`
	Returned             int           `json:"returned"`
	Offset               int           `json:"offset"`
	Limit                int           `json:"limit"`
	HasMore              bool          `json:"has_more"`
	Sort                 string        `json:"sort,omitempty"`
	Order                string        `json:"order,omitempty"`
	Query                string        `json:"query,omitempty"`
	Revision             string        `json:"revision,omitempty"`
	Fingerprint          string        `json:"fingerprint,omitempty"`
	SelectionFingerprint string        `json:"selection_fingerprint,omitempty"`
}

// portalSummary is the browse-view KPI payload. Pricing aggregates were
// removed in PAI-474 — customers see prices in the Projektbericht
// (acceptance report) at sign-off time, not while browsing.
type portalSummary struct {
	TotalIssues int            `json:"total_issues"`
	ByStatus    map[string]int `json:"by_status"`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func portalProjectID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

func checkPortalAccess(r *http.Request, projectID int64) bool {
	return auth.HasProjectAccess(r, projectID)
}

// checkPortalEdit gates portal mutations (accept / reject / undo). Viewers
// may browse a project through the portal but cannot mutate issue status —
// that capability requires editor access.
func checkPortalEdit(r *http.Request, projectID int64) bool {
	return auth.CanEditProject(r, projectID)
}

// portalVisibilityEnforced reports whether portal endpoints should
// constrain results to issues carrying the CUSTOMERPORTAL tag. The
// default is true; setting PAIMOS_PORTAL_VISIBILITY_DRY_RUN=true (PAI-462)
// disables enforcement for the rollout grace period while the backfill
// migration runs and operators read `would_hide_count` per project.
func portalVisibilityEnforced() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv("PAIMOS_PORTAL_VISIBILITY_DRY_RUN"))) != "true"
}

// portalVisibilityCondition returns a SQL fragment suitable for either a
// JOIN ON clause or a WHERE clause that constrains the issues row aliased
// as `alias` to those carrying the CUSTOMERPORTAL tag (PAI-460). The
// returned bool reports whether enforcement is on; when off, the
// fragment is "1=1" so the same splice point composes uniformly. The
// returned arg should only be appended to the query when enforced=true.
//
// EXISTS is used (over a JOIN) so the rest of each query's plan stays
// untouched and the visibility filter composes with the existing access
// gate without ambiguity around row duplication.
func portalVisibilityCondition(alias string) (frag string, arg any, enforced bool) {
	if !portalVisibilityEnforced() {
		return "1=1", nil, false
	}
	id, _ := customerPortalTagID()
	return "EXISTS (SELECT 1 FROM issue_tags itt WHERE itt.issue_id = " + alias + ".id AND itt.tag_id = ?)", id, true
}

// portalAllowedTypes is the set of issue types that may appear in the
// customer-portal issues endpoint (PAI-461). Memory, Guideline, Runbook,
// External_system, Related_project — internal knowledge surfaces — are
// excluded by name even before the CUSTOMERPORTAL tag filter as a
// defense-in-depth measure: a buggy or stale tag attached to a memory
// row would still not leak through this list.
var portalAllowedTypes = map[string]struct{}{
	"epic":      {},
	"cost_unit": {},
	"release":   {},
	"sprint":    {},
	"ticket":    {},
	"task":      {},
}

func portalAllowedType(t string) bool {
	_, ok := portalAllowedTypes[t]
	return ok
}

// portalSortColumns maps the user-facing sort key to the SQL column
// expression (PAI-461). The column allowlist is enforced server-side so
// the endpoint never builds an ORDER BY from arbitrary input.
var portalSortColumns = map[string]string{
	"":            "i.updated_at",
	"key":         "i.issue_number",
	"title":       "i.title",
	"status":      "i.status",
	"priority":    "i.priority",
	"updated_at":  "i.updated_at",
	"accepted_at": "i.accepted_at",
}

// portalSortColumn resolves an incoming `sort` query parameter to a
// safe SQL column. Returns ok=false for any value not in the allowlist.
func portalSortColumn(key string) (string, bool) {
	col, ok := portalSortColumns[strings.ToLower(strings.TrimSpace(key))]
	return col, ok
}

// portalCSV splits a comma-separated query parameter into its trimmed
// non-empty values. Used by the multi-select portal filters (PAI-461).
// We deliberately use comma-separated, not repeated params, to match the
// internal IssueList contract that the shared IssueFilterBar will emit.
func portalCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// portalCSVInts is portalCSV's int64 variant for tag_ids. Non-integer
// segments are dropped silently — the caller still validates the
// remaining ids by joining on issue_tags.
func portalCSVInts(raw string) []int64 {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		if v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func appendPortalIssueSearchFilter(where string, args []any, raw string) (string, []any) {
	fts := strings.TrimSpace(raw)
	if fts == "" {
		return where, args
	}
	likePattern := "%" + fts + "%"
	keyOrBodyMatchSQL := `
				SELECT ii.id
				FROM issues ii
				LEFT JOIN projects pp ON pp.id = ii.project_id
				WHERE ii.deleted_at IS NULL AND (
					ii.title LIKE ? OR ii.description LIKE ? OR ii.acceptance_criteria LIKE ?
					OR ii.report_summary LIKE ?
					OR (COALESCE(pp.key,'') || '-' || CAST(ii.issue_number AS TEXT)) LIKE ?
				)`
	if len(fts) >= 2 {
		if ftsToken, useFTS := sanitizeFTS5Token(fts); useFTS {
			where += ` AND i.id IN (
				SELECT CAST(entity_id AS INTEGER) FROM search_index
				WHERE entity_type IN ('issue','comment') AND search_index MATCH ?
				UNION` + keyOrBodyMatchSQL + `
			)`
			args = append(args, ftsToken, likePattern, likePattern, likePattern, likePattern, likePattern)
			return where, args
		}
	}
	where += ` AND i.id IN (` + keyOrBodyMatchSQL + `
			)`
	args = append(args, likePattern, likePattern, likePattern, likePattern, likePattern)
	return where, args
}

// ── GET /api/portal/projects ─────────────────────────────────────────────────

func PortalListProjects(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Visibility filter sits on the LEFT JOIN's ON clause so projects
	// with zero customer-visible issues still appear (with counts at 0).
	// Moving it into WHERE would convert the LEFT JOIN to an effective
	// INNER JOIN and drop those projects entirely.
	visFrag, visArg, visOn := portalVisibilityCondition("i")

	var query string
	var args []any

	if auth.IsAdmin(user) {
		query = `
			SELECT p.id, p.key, p.name, p.description, p.status,
			       COALESCE(p.logo_path, ''),
			       COUNT(i.id) as issue_count,
			       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count
			FROM projects p
			LEFT JOIN issues i ON i.project_id = p.id AND i.deleted_at IS NULL AND ` + visFrag + `
			WHERE p.status = 'active'
			GROUP BY p.id
			ORDER BY p.name`
		if visOn {
			args = append(args, visArg)
		}
	} else {
		query = `
			SELECT p.id, p.key, p.name, p.description, p.status,
			       COALESCE(p.logo_path, ''),
			       COUNT(i.id) as issue_count,
			       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count
			FROM projects p
			JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = ? AND pm.access_level IN ('viewer','editor')
			LEFT JOIN issues i ON i.project_id = p.id AND i.deleted_at IS NULL AND ` + visFrag + `
			WHERE p.status = 'active'
			GROUP BY p.id
			ORDER BY p.name`
		args = append(args, user.ID)
		if visOn {
			args = append(args, visArg)
		}
	}

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := []portalProject{}
	for rows.Next() {
		var p portalProject
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Status,
			&p.LogoPath, &p.IssueCount, &p.DoneCount); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		projects = append(projects, p)
	}
	jsonOK(w, projects)
}

// ── GET /api/portal/projects/{id} ────────────────────────────────────────────

func PortalGetProject(w http.ResponseWriter, r *http.Request) {
	id, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, id) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	visFrag, visArg, visOn := portalVisibilityCondition("i")
	args := []any{}
	if visOn {
		args = append(args, visArg)
	}
	args = append(args, id)

	var p portalProject
	err = db.DB.QueryRow(`
		SELECT p.id, p.key, p.name, p.description, p.status,
		       COALESCE(p.logo_path, ''),
		       COUNT(i.id) as issue_count,
		       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count
		FROM projects p
		LEFT JOIN issues i ON i.project_id = p.id AND i.deleted_at IS NULL AND `+visFrag+`
		WHERE p.id = ?
		GROUP BY p.id
	`, args...).Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Status,
		&p.LogoPath, &p.IssueCount, &p.DoneCount)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, p)
}

// ── GET /api/portal/projects/{id}/issues ─────────────────────────────────────

func PortalListIssues(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	where := "WHERE i.project_id = ? AND i.deleted_at IS NULL"
	args := []any{projectID}

	// PAI-460: gate to issues tagged CUSTOMERPORTAL (no-op when the
	// dry-run env var is set per PAI-462). PAI-461: multi-select filters
	// are applied on top of the visibility gate, never around it.
	if frag, arg, on := portalVisibilityCondition("i"); on {
		where += " AND " + frag
		args = append(args, arg)
	}

	// PAI-461: status[], type[], priority[], tag_ids[] — comma-separated,
	// allowlisted per field. Each filter is a no-op when its param is
	// empty so existing single-value clients keep working.
	if vals := portalCSV(q.Get("status")); len(vals) > 0 {
		where += " AND i.status IN (" + buildPlaceholders(len(vals)) + ")"
		for _, v := range vals {
			args = append(args, v)
		}
	}
	if vals := portalCSV(q.Get("type")); len(vals) > 0 {
		for _, v := range vals {
			if !portalAllowedType(v) {
				jsonError(w, "type not allowed in portal: "+v, http.StatusBadRequest)
				return
			}
		}
		where += " AND i.type IN (" + buildPlaceholders(len(vals)) + ")"
		for _, v := range vals {
			args = append(args, v)
		}
	}
	if vals := portalCSV(q.Get("priority")); len(vals) > 0 {
		where += " AND i.priority IN (" + buildPlaceholders(len(vals)) + ")"
		for _, v := range vals {
			args = append(args, v)
		}
	}
	if tagIDs := portalCSVInts(q.Get("tag_ids")); len(tagIDs) > 0 {
		// Every requested tag must be attached — AND semantics, not OR —
		// which matches the internal IssueList contract for tag_ids.
		for _, tid := range tagIDs {
			where += ` AND EXISTS (SELECT 1 FROM issue_tags itf WHERE itf.issue_id = i.id AND itf.tag_id = ?)`
			args = append(args, tid)
		}
	}
	if v := q.Get("cost_unit"); v != "" {
		where += " AND " + costUnitLabelExpr + " = ?"
		args = append(args, v)
	}
	searchTerm := strings.TrimSpace(q.Get("q"))
	where, args = appendPortalIssueSearchFilter(where, args, searchTerm)

	// PAI-461: sort + order, both allowlisted. Default preserves the
	// pre-PAI-461 behaviour (most-recent first by updated_at).
	sortCol, ok := portalSortColumn(q.Get("sort"))
	if !ok {
		jsonError(w, "sort column not allowed in portal: "+q.Get("sort"), http.StatusBadRequest)
		return
	}
	orderDir := "DESC"
	if d := strings.ToLower(strings.TrimSpace(q.Get("order"))); d == "asc" {
		orderDir = "ASC"
	} else if d != "" && d != "desc" {
		jsonError(w, "order must be asc or desc", http.StatusBadRequest)
		return
	}

	countArgs := append([]any{}, args...)
	var total int
	// #nosec G701 -- where is assembled from fixed SQL fragments; user values are placeholders.
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*)
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		%s
	`, where), countArgs...).Scan(&total); err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}

	limit, offset := parseIssueListWindow(r, 0)
	// #nosec G201 -- where is fixed-fragment assembly; sortCol/orderDir come from server-side allowlists.
	listSQL := fmt.Sprintf(`
		SELECT i.id, COALESCE(p.key || '-' || i.issue_number, ''),
		       i.title, i.description, i.acceptance_criteria,
		       i.report_summary,
		       i.status, i.priority, i.type,
		       i.accepted_at,
		       i.created_at, i.updated_at
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		%s
		ORDER BY %s %s
	`, where, sortCol, orderDir)
	if limit > 0 {
		listSQL += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

	// #nosec G701 -- listSQL uses fixed fragments and allowlisted sort columns; user values are placeholders.
	rows, err := db.DB.Query(listSQL, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	issues := []portalIssue{}
	for rows.Next() {
		var pi portalIssue
		if err := rows.Scan(&pi.ID, &pi.IssueKey,
			&pi.Title, &pi.Description, &pi.AcceptanceCriteria,
			&pi.ReportSummary,
			&pi.Status, &pi.Priority, &pi.Type,
			&pi.AcceptedAt,
			&pi.CreatedAt, &pi.UpdatedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		issues = append(issues, pi)
	}
	if q.Get("envelope") == "1" {
		sortKey := strings.ToLower(strings.TrimSpace(q.Get("sort")))
		order := strings.ToLower(strings.TrimSpace(q.Get("order")))
		fingerprint := issueListFingerprint("portal", projectID, where, countArgs, searchTerm, sortKey, order)
		selectionFingerprint := issueListFingerprint("portal-ids", projectID, where, countArgs, searchTerm)
		jsonOK(w, portalIssueListEnvelope{
			Issues:               issues,
			Total:                total,
			Returned:             len(issues),
			Offset:               offset,
			Limit:                limit,
			HasMore:              issueListHasMore(total, offset, len(issues), limit),
			Sort:                 sortKey,
			Order:                order,
			Query:                searchTerm,
			Revision:             issueListRevision(w),
			Fingerprint:          fingerprint,
			SelectionFingerprint: selectionFingerprint,
		})
		return
	}
	jsonOK(w, issues)
}

// ── GET /api/portal/projects/{id}/issues/{issueId} ──────────────────────────

func PortalGetIssue(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	issueID, err := strconv.ParseInt(chi.URLParam(r, "issueId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid issue id", http.StatusBadRequest)
		return
	}

	// PAI-460: append the visibility constraint to WHERE — a missing
	// CUSTOMERPORTAL tag returns 404 (existing no-rows path) rather than
	// 403, so the endpoint never discloses that an internal issue exists
	// at this id.
	visFrag, visArg, visOn := portalVisibilityCondition("i")
	args := []any{issueID, projectID}
	if visOn {
		args = append(args, visArg)
	}

	var pi portalIssue
	err = db.DB.QueryRow(`
		SELECT i.id, COALESCE(p.key || '-' || i.issue_number, ''),
		       i.title, i.description, i.acceptance_criteria,
		       i.report_summary,
		       i.status, i.priority, i.type,
		       i.accepted_at,
		       i.created_at, i.updated_at
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE i.id = ? AND i.project_id = ? AND i.deleted_at IS NULL AND `+visFrag+`
	`, args...).Scan(&pi.ID, &pi.IssueKey,
		&pi.Title, &pi.Description, &pi.AcceptanceCriteria,
		&pi.ReportSummary,
		&pi.Status, &pi.Priority, &pi.Type,
		&pi.AcceptedAt,
		&pi.CreatedAt, &pi.UpdatedAt)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, pi)
}

// ── POST /api/portal/projects/{id}/requests ──────────────────────────────────

func PortalSubmitRequest(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		jsonError(w, "title required", http.StatusBadRequest)
		return
	}

	// PAI-459: portal submissions are the one path that auto-applies the
	// CUSTOMERPORTAL tag — a customer-submitted request is, by definition,
	// something the customer should see in their own portal. Run the
	// issue insert + tag attach + audit row in a single transaction so a
	// failure leaves nothing behind to clean up.
	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("PortalSubmitRequest: begin tx: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	nextNum, err := db.NextIssueNumber(r.Context(), tx, projectID)
	if err != nil {
		log.Printf("PortalSubmitRequest: next issue_number: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := tx.ExecContext(r.Context(), `
		INSERT INTO issues (project_id, issue_number, type, title, description, status, priority, created_at, updated_at, notes)
		VALUES (?, ?, 'ticket', ?, ?, 'new', 'medium', ?, ?, '[customer request]')
	`, projectID, nextNum, body.Title, body.Description, now, now)
	if err != nil {
		log.Printf("PortalSubmitRequest: insert issue: %v", err)
		jsonError(w, "create failed", http.StatusInternalServerError)
		return
	}
	issueID, _ := res.LastInsertId()

	// PAI-459: auto-attach CUSTOMERPORTAL inside the same tx. Tag id is
	// process-cached; lookup falls back to a direct query if the cache is
	// cold (e.g. first request after restart, before any other portal
	// activity primed it).
	tagID, ok := customerPortalTagIDTx(r.Context(), tx)
	if !ok {
		log.Printf("PortalSubmitRequest: CUSTOMERPORTAL tag missing (migration 109 not applied?)")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO issue_tags(issue_id, tag_id) VALUES(?, ?)`, issueID, tagID); err != nil {
		log.Printf("PortalSubmitRequest: attach CUSTOMERPORTAL issue=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Audit the auto-tag with its own mutation_type so the admin
	// visibility report (PAI-467) can render "auto-tagged on portal
	// submission" distinct from interactive toggles or migration
	// backfills. Not user-undoable: undoing the tag would orphan the
	// portal request from its own visibility marker.
	var userID *int64
	if user := auth.GetUser(r); user != nil {
		userID = &user.ID
	}
	after, err := fetchIssueTagMutationSnapshotTx(tx, issueID, tagID)
	if err != nil {
		log.Printf("PortalSubmitRequest: snapshot issue=%d tag=%d: %v", issueID, tagID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: "portal.submit.auto_tag",
		SubjectType:  "issue_tag",
		SubjectID:    issueID,
		AfterState:   after,
		Undoable:     false,
	}); err != nil {
		log.Printf("PortalSubmitRequest: recordMutation: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var key string
	if err := tx.QueryRowContext(r.Context(), "SELECT COALESCE(p.key || '-' || ?, '') FROM projects p WHERE p.id=?", nextNum, projectID).Scan(&key); err != nil {
		log.Printf("PortalSubmitRequest: resolve issue_key: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("PortalSubmitRequest: commit: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]any{"id": issueID, "issue_key": key, "issue_number": nextNum})
}

// ── POST /api/portal/issues/{id}/accept ──────────────────────────────────────

func PortalAcceptIssue(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Verify issue exists and is done, and user has project access
	var projectID int64
	var status string
	err = db.DB.QueryRow("SELECT project_id, status FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if status == "accepted" {
		// Already accepted — idempotent no-op
		jsonOK(w, map[string]any{"accepted": true, "status": "accepted"})
		return
	}
	if status != "done" && status != "delivered" {
		jsonError(w, "only done or delivered issues can be accepted", http.StatusUnprocessableEntity)
		return
	}

	user := auth.GetUser(r)
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec("UPDATE issues SET status='accepted', accepted_at=?, accepted_by=?, updated_at=? WHERE id=?", now, user.ID, now, issueID); err != nil {
		log.Printf("PortalAcceptIssue: id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"accepted": true, "status": "accepted"})
}

// ── POST /api/portal/issues/{id}/reject ───────────────────────────────────────

func PortalRejectIssue(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		// Legacy field — maps to title if title is empty
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	// Support both new (title+description) and legacy (reason) formats
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = strings.TrimSpace(body.Reason)
	}
	if title == "" {
		jsonError(w, "title or reason required", http.StatusBadRequest)
		return
	}
	description := strings.TrimSpace(body.Description)
	if description == "" {
		description = title
	}

	// Verify issue exists and is done, and user has project access
	var projectID int64
	var status, priority string
	var assigneeID *int64
	err = db.DB.QueryRow("SELECT project_id, status, priority, assignee_id FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status, &priority, &assigneeID)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if status != "done" && status != "delivered" {
		jsonError(w, "only done or delivered issues can be rejected", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	user := auth.GetUser(r)

	// Create child task describing the rejection.
	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	nextNum, err := db.NextIssueNumber(r.Context(), tx, projectID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// PAI-584 P6: parent_id column dropped — the rejection task's parent (the
	// rejected issue) is written as a `parent` edge via setParentEdge below.
	sqlRes, err := tx.ExecContext(r.Context(), `
		INSERT INTO issues (project_id, issue_number, type, title, description,
			status, priority, assignee_id, created_by, created_at, updated_at, notes)
		VALUES (?, ?, 'task', ?, ?, 'backlog', ?, ?, ?, ?, ?, '[portal rejection]')
	`, projectID, nextNum, title, description, priority, assigneeID, user.ID, now, now)
	if handleDBError(w, err, "issue") {
		return
	}
	childID, _ := sqlRes.LastInsertId()

	if err := setParentEdge(r.Context(), tx, childID, &issueID); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reopen parent to in-progress, clear accepted_at/accepted_by
	if _, err := tx.ExecContext(r.Context(), "UPDATE issues SET status='in-progress', accepted_at=NULL, accepted_by=NULL, updated_at=? WHERE id=?", now, issueID); err != nil {
		log.Printf("PortalRejectIssue: reopen parent id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"rejected": true, "status": "in-progress", "child_id": childID})
}

// ── POST /api/portal/issues/{id}/undo-accept ─────────────────────────────────

func PortalUndoAccept(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var projectID int64
	var status string
	var acceptedAt *string
	err = db.DB.QueryRow("SELECT project_id, status, accepted_at FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status, &acceptedAt)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if status != "accepted" || acceptedAt == nil {
		jsonError(w, "issue is not accepted", http.StatusUnprocessableEntity)
		return
	}
	// Same-day check
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.HasPrefix(*acceptedAt, today) {
		jsonError(w, "can only undo today's acceptance", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec("UPDATE issues SET status='done', accepted_at=NULL, accepted_by=NULL, updated_at=? WHERE id=?", now, issueID); err != nil {
		log.Printf("PortalUndoAccept: id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"undone": true, "status": "done"})
}

// ── POST /api/portal/issues/{id}/undo-reject ─────────────────────────────────

func PortalUndoReject(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var projectID int64
	var status string
	err = db.DB.QueryRow("SELECT project_id, status FROM issues WHERE id=? AND deleted_at IS NULL", issueID).Scan(&projectID, &status)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if !checkPortalEdit(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	// Find today's rejection child task
	today := time.Now().UTC().Format("2006-01-02")
	var childID int64
	// PAI-584 P6: children via the `parent` edge, not parent_id.
	err = db.DB.QueryRow(
		`SELECT id FROM issues
		 WHERE id IN (SELECT target_id FROM issue_relations WHERE source_id=? AND type='parent')
		   AND notes='[portal rejection]' AND created_at LIKE ? LIMIT 1`,
		issueID, today+"%",
	).Scan(&childID)
	if err != nil {
		jsonError(w, "no rejection from today found", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	// Cancel the child task
	if _, err := db.DB.Exec("UPDATE issues SET status='cancelled', updated_at=? WHERE id=?", now, childID); err != nil {
		log.Printf("PortalUndoReject: cancel child id=%d: %v", childID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Restore parent to done
	if _, err := db.DB.Exec("UPDATE issues SET status='done', updated_at=? WHERE id=?", now, issueID); err != nil {
		log.Printf("PortalUndoReject: restore parent id=%d: %v", issueID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"undone": true, "status": "done"})
}

// ── GET /api/projects/{id}/acceptance-log ─────────────────────────────────────

func AcceptanceLog(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	type action struct {
		IssueKey string `json:"issue_key"`
		Action   string `json:"action"`
		TaskKey  string `json:"task_key,omitempty"`
		Title    string `json:"title,omitempty"`
		At       string `json:"at"`
	}
	type group struct {
		Date    string   `json:"date"`
		User    string   `json:"user"`
		Actions []action `json:"actions"`
	}

	// Accepted issues
	acceptRows, err := db.DB.Query(`
		SELECT COALESCE(p.key || '-' || i.issue_number, ''),
		       COALESCE(u.username, ''), i.accepted_at
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		LEFT JOIN users u ON u.id = i.accepted_by
		WHERE i.project_id = ? AND i.accepted_at IS NOT NULL AND i.deleted_at IS NULL
		ORDER BY i.accepted_at DESC
	`, projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer acceptRows.Close()

	groupMap := map[string]*group{} // key: date|user
	var groupOrder []string

	for acceptRows.Next() {
		var issueKey, username, acceptedAt string
		if err := acceptRows.Scan(&issueKey, &username, &acceptedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		date := acceptedAt[:10]
		gk := date + "|" + username
		g, ok := groupMap[gk]
		if !ok {
			g = &group{Date: date, User: username}
			groupMap[gk] = g
			groupOrder = append(groupOrder, gk)
		}
		g.Actions = append(g.Actions, action{IssueKey: issueKey, Action: "accepted", At: acceptedAt})
	}

	// Rejected issues (child tasks with [portal rejection] notes)
	rejectRows, err := db.DB.Query(`
		SELECT COALESCE(pp.key || '-' || parent.issue_number, ''),
		       COALESCE(pp.key || '-' || i.issue_number, ''),
		       i.title, COALESCE(u.username, ''), i.created_at
		FROM issues i
		-- PAI-584 P6: parent via the parent edge, not i.parent_id.
		JOIN issue_relations pr ON pr.target_id = i.id AND pr.type='parent'
		JOIN issues parent ON parent.id = pr.source_id
		LEFT JOIN projects pp ON pp.id = i.project_id
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.project_id = ? AND i.notes = '[portal rejection]' AND i.deleted_at IS NULL
		ORDER BY i.created_at DESC
	`, projectID)
	if err == nil {
		defer rejectRows.Close()
		for rejectRows.Next() {
			var parentKey, taskKey, title, username, createdAt string
			if err := rejectRows.Scan(&parentKey, &taskKey, &title, &username, &createdAt); err != nil {
				log.Printf("scan error: %v", err)
				continue
			}
			date := createdAt[:10]
			gk := date + "|" + username
			g, ok := groupMap[gk]
			if !ok {
				g = &group{Date: date, User: username}
				groupMap[gk] = g
				groupOrder = append(groupOrder, gk)
			}
			g.Actions = append(g.Actions, action{IssueKey: parentKey, Action: "rejected", TaskKey: taskKey, Title: title, At: createdAt})
		}
	}

	result := []group{}
	for _, gk := range groupOrder {
		result = append(result, *groupMap[gk])
	}
	if result == nil {
		result = []group{}
	}
	jsonOK(w, result)
}

// ── GET /api/portal/projects/{id}/summary ────────────────────────────────────

func PortalProjectSummary(w http.ResponseWriter, r *http.Request) {
	projectID, err := portalProjectID(r)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !checkPortalAccess(r, projectID) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	// PAI-460: by-status rollup must reflect visible-only counts.
	visFrag, visArg, visOn := portalVisibilityCondition("issues")
	args := []any{projectID}
	if visOn {
		args = append(args, visArg)
	}

	// #nosec G202 -- visFrag is a fixed SQL fragment; its tag id is bound as a placeholder arg.
	rows, err := db.DB.Query(`
		SELECT status, COUNT(*)
		FROM issues
		WHERE project_id = ? AND deleted_at IS NULL AND `+visFrag+`
		GROUP BY status
	`, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	summary := portalSummary{ByStatus: map[string]int{}}
	for rows.Next() {
		var st string
		var cnt int
		if err := rows.Scan(&st, &cnt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		summary.ByStatus[st] = cnt
		summary.TotalIssues += cnt
	}
	jsonOK(w, summary)
}

// ── GET /api/portal/overview ─────────────────────────────────────────────────
//
// Welcome-screen aggregation. One round-trip for the four KPI counters,
// the customer's projects (with per-status counts + last activity for the
// segmented card UI), the across-projects acceptance queue, and the most
// recent Projektbericht snapshots. Access matches PortalListProjects:
// admins see every active project; portal/member users see only projects
// granted via project_members.

type portalOverviewKPIs struct {
	ActiveProjects     int `json:"active_projects"`
	OpenIssues         int `json:"open_issues"`
	AwaitingAcceptance int `json:"awaiting_acceptance"`
	AcceptedThisMonth  int `json:"accepted_this_month"`
}

type portalAwaitingIssue struct {
	ID          int64  `json:"id"`
	IssueKey    string `json:"issue_key"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	ProjectID   int64  `json:"project_id"`
	ProjectKey  string `json:"project_key"`
	ProjectName string `json:"project_name"`
	UpdatedAt   string `json:"updated_at"`
	CanEdit     bool   `json:"can_edit"`
}

type portalReportLink struct {
	Code        string  `json:"code"`
	ProjectID   int64   `json:"project_id"`
	ProjectKey  string  `json:"project_key"`
	ProjectName string  `json:"project_name"`
	Status      string  `json:"status"`
	TotalIssues int     `json:"total_issues"`
	CreatedAt   string  `json:"created_at"`
	AcceptedAt  *string `json:"accepted_at"`
}

type portalOverview struct {
	KPIs                  portalOverviewKPIs    `json:"kpis"`
	Projects              []portalProject       `json:"projects"`
	AwaitingAcceptance    []portalAwaitingIssue `json:"awaiting_acceptance"`
	RecentProjektberichte []portalReportLink    `json:"recent_projektberichte"`
}

func PortalOverview(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Collect accessible project IDs first. The rest of the queries
	// then constrain to that set instead of repeating the membership
	// JOIN N times.
	var (
		projectIDs []int64
		projects   []portalProject
		isAdmin    = auth.IsAdmin(user)
	)

	// PAI-460: per-project KPIs and last_activity reflect visible-only
	// counts. Same JOIN-condition technique as PortalListProjects.
	visFrag, visArg, visOn := portalVisibilityCondition("i")

	{
		var query string
		var args []any
		if isAdmin {
			query = `
				SELECT p.id, p.key, p.name, p.description, p.status,
				       COALESCE(p.logo_path, ''),
				       COUNT(i.id) as issue_count,
				       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count,
				       COALESCE(MAX(i.updated_at), '') as last_activity
				FROM projects p
				LEFT JOIN issues i ON i.project_id = p.id AND i.deleted_at IS NULL AND ` + visFrag + `
				WHERE p.status = 'active'
				GROUP BY p.id
				ORDER BY last_activity DESC, p.name`
			if visOn {
				args = append(args, visArg)
			}
		} else {
			query = `
				SELECT p.id, p.key, p.name, p.description, p.status,
				       COALESCE(p.logo_path, ''),
				       COUNT(i.id) as issue_count,
				       COUNT(CASE WHEN i.status = 'done' THEN 1 END) as done_count,
				       COALESCE(MAX(i.updated_at), '') as last_activity
				FROM projects p
				JOIN project_members pm
				  ON pm.project_id = p.id AND pm.user_id = ?
				 AND pm.access_level IN ('viewer','editor')
				LEFT JOIN issues i ON i.project_id = p.id AND i.deleted_at IS NULL AND ` + visFrag + `
				WHERE p.status = 'active'
				GROUP BY p.id
				ORDER BY last_activity DESC, p.name`
			args = append(args, user.ID)
			if visOn {
				args = append(args, visArg)
			}
		}
		rows, err := db.DB.Query(query, args...)
		if err != nil {
			jsonError(w, "query failed", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p portalProject
			if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Status,
				&p.LogoPath, &p.IssueCount, &p.DoneCount, &p.LastActivity); err != nil {
				log.Printf("portal overview scan project: %v", err)
				continue
			}
			p.ByStatus = map[string]int{}
			projects = append(projects, p)
			projectIDs = append(projectIDs, p.ID)
		}
	}

	out := portalOverview{
		Projects:              []portalProject{},
		AwaitingAcceptance:    []portalAwaitingIssue{},
		RecentProjektberichte: []portalReportLink{},
	}
	if len(projects) > 0 {
		out.Projects = projects
	}
	out.KPIs.ActiveProjects = len(projectIDs)

	// No accessible projects → return the empty shell. Avoids building
	// a WHERE … IN () clause, which SQLite rejects.
	if len(projectIDs) == 0 {
		jsonOK(w, out)
		return
	}

	placeholders, idArgs := intInPlaceholders(projectIDs)

	// Per-project status breakdown + open-issue count + acceptance-queue
	// pre-counts. One query that buckets every relevant status; we then
	// slot the results back into the matching project. PAI-460: filter
	// to CUSTOMERPORTAL-visible issues only — re-resolve the visibility
	// condition for the `issues` table alias used in this block.
	{
		statusVisFrag, statusVisArg, statusVisOn := portalVisibilityCondition("issues")
		statusArgs := append([]any{}, idArgs...)
		if statusVisOn {
			statusArgs = append(statusArgs, statusVisArg)
		}
		// #nosec G202 -- placeholders is ?-only assembly; statusVisFrag is a fixed fragment with a placeholder arg.
		q := `
			SELECT project_id, status, COUNT(*)
			FROM issues
			WHERE deleted_at IS NULL
			  AND project_id IN (` + placeholders + `)
			  AND ` + statusVisFrag + `
			GROUP BY project_id, status`
		rows, err := db.DB.Query(q, statusArgs...)
		if err != nil {
			jsonError(w, "status rollup query failed", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		byProject := map[int64]map[string]int{}
		for rows.Next() {
			var pid int64
			var st string
			var cnt int
			if err := rows.Scan(&pid, &st, &cnt); err != nil {
				continue
			}
			if byProject[pid] == nil {
				byProject[pid] = map[string]int{}
			}
			byProject[pid][st] = cnt
			switch st {
			case "done", "delivered":
				out.KPIs.AwaitingAcceptance += cnt
			}
			if st != "done" && st != "delivered" && st != "cancelled" && st != "accepted" && st != "invoiced" {
				out.KPIs.OpenIssues += cnt
			}
		}
		for i := range out.Projects {
			if m, ok := byProject[out.Projects[i].ID]; ok {
				out.Projects[i].ByStatus = m
			}
		}
	}

	// Awaiting-acceptance issue list across the customer's projects.
	// Cap at 20 — anything more belongs in the per-project view.
	// PAI-460: only the customer's portal-visible items qualify; the rest
	// are internal noise.
	{
		awaitVisFrag, awaitVisArg, awaitVisOn := portalVisibilityCondition("i")
		awaitArgs := append([]any{}, idArgs...)
		if awaitVisOn {
			awaitArgs = append(awaitArgs, awaitVisArg)
		}
		// #nosec G202 -- placeholders is ?-only assembly; awaitVisFrag is a fixed fragment with a placeholder arg.
		q := `
			SELECT i.id, COALESCE(p.key || '-' || i.issue_number, ''),
			       i.title, i.type, i.status, i.priority,
			       i.project_id, p.key, p.name, i.updated_at
			FROM issues i
			JOIN projects p ON p.id = i.project_id
			WHERE i.deleted_at IS NULL
			  AND i.status IN ('done','delivered')
			  AND i.project_id IN (` + placeholders + `)
			  AND ` + awaitVisFrag + `
			ORDER BY i.updated_at DESC
			LIMIT 20`
		rows, err := db.DB.Query(q, awaitArgs...)
		if err != nil {
			jsonError(w, "acceptance queue query failed", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		editCache := map[int64]bool{}
		for rows.Next() {
			var it portalAwaitingIssue
			if err := rows.Scan(&it.ID, &it.IssueKey, &it.Title, &it.Type, &it.Status,
				&it.Priority, &it.ProjectID, &it.ProjectKey, &it.ProjectName, &it.UpdatedAt); err != nil {
				continue
			}
			canEdit, ok := editCache[it.ProjectID]
			if !ok {
				canEdit = isAdmin || auth.CanEditProject(r, it.ProjectID)
				editCache[it.ProjectID] = canEdit
			}
			it.CanEdit = canEdit
			out.AwaitingAcceptance = append(out.AwaitingAcceptance, it)
		}
	}

	// accepted_this_month counter — PAI-460: visible-only.
	{
		acceptVisFrag, acceptVisArg, acceptVisOn := portalVisibilityCondition("issues")
		acceptArgs := append([]any{}, idArgs...)
		if acceptVisOn {
			acceptArgs = append(acceptArgs, acceptVisArg)
		}
		// #nosec G202 -- placeholders is ?-only assembly; acceptVisFrag is a fixed fragment with a placeholder arg.
		q := `
			SELECT COUNT(*)
			FROM issues
			WHERE deleted_at IS NULL
			  AND accepted_at IS NOT NULL
			  AND strftime('%Y-%m', accepted_at) = strftime('%Y-%m', 'now')
			  AND project_id IN (` + placeholders + `)
			  AND ` + acceptVisFrag
		if err := db.DB.QueryRow(q, acceptArgs...).Scan(&out.KPIs.AcceptedThisMonth); err != nil {
			log.Printf("portal overview accepted_this_month: %v", err)
		}
	}

	// PAI-462: when running in dry-run mode (filter off), surface a
	// per-project would_hide_count so operators can verify the blast
	// radius before unsetting PAIMOS_PORTAL_VISIBILITY_DRY_RUN. The query
	// counts non-deleted issues missing the CUSTOMERPORTAL tag — i.e.
	// exactly what the live filter would hide.
	if !portalVisibilityEnforced() {
		tagID, _ := customerPortalTagID()
		whcArgs := append([]any{tagID}, idArgs...)
		// #nosec G202 -- placeholders is ?-only assembly; the rest of the SQL is fixed with placeholder args.
		q := `
			SELECT i.project_id, COUNT(*)
			FROM issues i
			WHERE i.deleted_at IS NULL
			  AND i.project_id IN (` + placeholders + `)
			  AND NOT EXISTS (
			    SELECT 1 FROM issue_tags it
			    WHERE it.issue_id = i.id AND it.tag_id = ?
			  )
			GROUP BY i.project_id`
		// Re-bind: NOT EXISTS uses tagID, IN uses idArgs.
		whcArgs = append([]any{}, idArgs...)
		whcArgs = append(whcArgs, tagID)
		rows, err := db.DB.Query(q, whcArgs...)
		if err == nil {
			defer rows.Close()
			byProject := map[int64]int{}
			for rows.Next() {
				var pid int64
				var cnt int
				if err := rows.Scan(&pid, &cnt); err == nil {
					byProject[pid] = cnt
				}
			}
			for i := range out.Projects {
				v := byProject[out.Projects[i].ID]
				out.Projects[i].WouldHideCount = &v
			}
		} else {
			log.Printf("portal overview would_hide_count: %v", err)
		}
	}

	// Recent Projektbericht snapshots across accessible projects. Top 5.
	{
		// #nosec G202 -- placeholders is ?-only assembly; the rest of the SQL is fixed.
		q := `
			SELECT prs.code, prs.project_id, p.key, p.name,
			       prs.status, prs.total_issues, prs.created_at, prs.accepted_at
			FROM project_report_snapshots prs
			JOIN projects p ON p.id = prs.project_id
			WHERE prs.project_id IN (` + placeholders + `)
			ORDER BY prs.created_at DESC
			LIMIT 5`
		rows, err := db.DB.Query(q, idArgs...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var rep portalReportLink
				if err := rows.Scan(&rep.Code, &rep.ProjectID, &rep.ProjectKey, &rep.ProjectName,
					&rep.Status, &rep.TotalIssues, &rep.CreatedAt, &rep.AcceptedAt); err != nil {
					continue
				}
				out.RecentProjektberichte = append(out.RecentProjektberichte, rep)
			}
		} else {
			log.Printf("portal overview projektberichte: %v", err)
		}
	}

	jsonOK(w, out)
}

// intInPlaceholders renders ?,?,? for a list and returns the args slice
// shaped for sql.DB.Query. Caller must guard against an empty slice —
// SQLite rejects `IN ()`.
func intInPlaceholders(ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	parts := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		parts[i] = "?"
		args[i] = id
	}
	return strings.Join(parts, ","), args
}
