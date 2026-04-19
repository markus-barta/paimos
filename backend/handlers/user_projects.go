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
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// ListUserProjects returns project IDs assigned to an external user.
// GET /api/users/{id}/projects
func ListUserProjects(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(`
		SELECT upa.project_id, p.name, p.key
		FROM user_project_access upa
		JOIN projects p ON p.id = upa.project_id
		WHERE upa.user_id = ?
		ORDER BY p.name
	`, userID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type item struct {
		ProjectID int64  `json:"project_id"`
		Name      string `json:"name"`
		Key       string `json:"key"`
	}
	items := []item{}
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.ProjectID, &i.Name, &i.Key); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		items = append(items, i)
	}
	jsonOK(w, items)
}

// AddUserProject assigns a project to an external user.
// POST /api/users/{id}/projects  { "project_id": 123 }
func AddUserProject(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var body struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ProjectID == 0 {
		jsonError(w, "project_id required", http.StatusBadRequest)
		return
	}

	_, err = db.DB.Exec(
		"INSERT OR IGNORE INTO user_project_access(user_id, project_id) VALUES(?, ?)",
		userID, body.ProjectID,
	)
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]bool{"ok": true})
}

// RemoveUserProject removes a project assignment from an external user.
// DELETE /api/users/{id}/projects/{projectId}
func RemoveUserProject(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid user id", http.StatusBadRequest)
		return
	}
	projectID, err := strconv.ParseInt(chi.URLParam(r, "projectId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	res, err := db.DB.Exec(
		"DELETE FROM user_project_access WHERE user_id=? AND project_id=?",
		userID, projectID,
	)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
