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
	"net/http"
	"strconv"
	"strings"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/go-chi/chi/v5"
)

type purgeFilter struct {
	FromDate        *string `json:"from_date"`
	ToDate          *string `json:"to_date"`
	Source          string  `json:"source"`          // "all", "mite", "manual"
	UserID          *int64  `json:"user_id"`         // nil = all users
	ConfirmationKey string  `json:"confirmation_key"` // only for purge
}

type purgeResult struct {
	Count      int64   `json:"count"`
	TotalHours float64 `json:"total_hours"`
}

// buildPurgeWhere returns the WHERE clause and args for purge queries.
func buildPurgeWhere(projectID int64, f purgeFilter) (string, []any) {
	where := []string{"issue_id IN (SELECT id FROM issues WHERE project_id = ?)"}
	args := []any{projectID}

	if f.FromDate != nil && *f.FromDate != "" {
		where = append(where, "started_at >= ?")
		args = append(args, *f.FromDate+" 00:00:00")
	}
	if f.ToDate != nil && *f.ToDate != "" {
		where = append(where, "started_at <= ?")
		args = append(args, *f.ToDate+" 23:59:59")
	}

	switch f.Source {
	case "mite":
		where = append(where, "mite_id IS NOT NULL")
	case "manual":
		where = append(where, "mite_id IS NULL")
	}

	if f.UserID != nil {
		where = append(where, "user_id = ?")
		args = append(args, *f.UserID)
	}

	return strings.Join(where, " AND "), args
}

// PurgePreview returns the count and total hours of matching entries without deleting.
// POST /api/projects/{id}/time-entries/purge-preview
func PurgePreview(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	var f purgeFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	where, args := buildPurgeWhere(projectID, f)
	var res purgeResult
	err = db.DB.QueryRow(
		fmt.Sprintf("SELECT COUNT(*), COALESCE(SUM(CASE WHEN override IS NOT NULL THEN override ELSE 0 END), 0) FROM time_entries WHERE %s", where),
		args...,
	).Scan(&res.Count, &res.TotalHours)
	if err != nil {
		jsonError(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, res)
}

// PurgeTimeEntries deletes matching time entries after confirmation.
// POST /api/projects/{id}/time-entries/purge
func PurgeTimeEntries(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	var f purgeFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Verify project exists and confirmation key matches
	var projectKey string
	err = db.DB.QueryRow("SELECT key FROM projects WHERE id = ?", projectID).Scan(&projectKey)
	if err != nil {
		jsonError(w, "project not found", http.StatusNotFound)
		return
	}
	if !strings.EqualFold(f.ConfirmationKey, projectKey) {
		jsonError(w, "confirmation key does not match project key", http.StatusBadRequest)
		return
	}

	where, args := buildPurgeWhere(projectID, f)

	// Collect affected issue IDs before deletion (for system tag re-evaluation)
	rows, err := db.DB.Query(
		fmt.Sprintf("SELECT DISTINCT issue_id FROM time_entries WHERE %s", where),
		args...,
	)
	if err != nil {
		jsonError(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var affectedIssueIDs []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			affectedIssueIDs = append(affectedIssueIDs, id)
		}
	}
	rows.Close()

	// Get count and hours before deleting
	var res purgeResult
	db.DB.QueryRow(
		fmt.Sprintf("SELECT COUNT(*), COALESCE(SUM(CASE WHEN override IS NOT NULL THEN override ELSE 0 END), 0) FROM time_entries WHERE %s", where),
		args...,
	).Scan(&res.Count, &res.TotalHours)

	// Delete
	result, err := db.DB.Exec(
		fmt.Sprintf("DELETE FROM time_entries WHERE %s", where),
		args...,
	)
	if err != nil {
		jsonError(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	deleted, _ := result.RowsAffected()
	res.Count = deleted

	// Log the purge action
	user := auth.GetUser(r)
	username := "unknown"
	if user != nil {
		username = user.Username
	}
	fmt.Printf("audit: purge_time_entries user=%q project_id=%d project_key=%q deleted=%d hours=%.1f source=%q from=%v to=%v user_filter=%v\n",
		username, projectID, projectKey, deleted, res.TotalHours, f.Source, f.FromDate, f.ToDate, f.UserID)

	// Re-evaluate system tags for affected issues
	for _, issueID := range affectedIssueIDs {
		EvaluateSystemTags(issueID)
	}

	jsonOK(w, res)
}

// PurgeUsers returns users who have time entries in a given project.
// GET /api/projects/{id}/time-entries/users
func PurgeUsers(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(`
		SELECT DISTINCT u.id, u.username
		FROM time_entries te
		JOIN users u ON u.id = te.user_id
		WHERE te.issue_id IN (SELECT id FROM issues WHERE project_id = ?)
		ORDER BY u.username
	`, projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type userEntry struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	var users []userEntry
	for rows.Next() {
		var u userEntry
		if rows.Scan(&u.ID, &u.Username) == nil {
			users = append(users, u)
		}
	}
	if users == nil {
		users = []userEntry{}
	}
	jsonOK(w, users)
}
