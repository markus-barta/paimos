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
	"net/http"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// POST /api/users/me/recent-projects — record a project visit for the current user.
// Body: { "project_id": 123 }
// Upserts visited_at; trims the list to the user's recent_projects_limit after insertion.
func UpsertRecentProject(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	var body struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ProjectID == 0 {
		jsonError(w, "project_id required", http.StatusBadRequest)
		return
	}

	_, err := db.DB.Exec(`
		INSERT INTO user_recent_projects (user_id, project_id, visited_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(user_id, project_id) DO UPDATE SET visited_at = datetime('now')
	`, user.ID, body.ProjectID)
	if handleDBError(w, err, "recent project") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/users/me/recent-projects — return the N most recently visited projects.
// N is the user's recent_projects_limit (default 3).
func GetRecentProjects(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	var limit int
	if err := db.DB.QueryRow("SELECT recent_projects_limit FROM users WHERE id=?", user.ID).Scan(&limit); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if limit <= 0 {
		limit = 3
	}

	rows, err := db.DB.Query(`
		SELECT p.id, p.name, p.key, p.description, p.status,
		       p.product_owner, p.customer_id,
		       p.created_at, p.updated_at,
		       COALESCE(p.logo_path, '')
		FROM user_recent_projects urp
		JOIN projects p ON p.id = urp.project_id
		WHERE urp.user_id = ? AND p.status != 'deleted'
		ORDER BY urp.visited_at DESC
		LIMIT ?
	`, user.ID, limit)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := []models.Project{}
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Key, &p.Description, &p.Status,
			&p.ProductOwner, &p.CustomerID,
			&p.CreatedAt, &p.UpdatedAt, &p.LogoPath); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		projects = append(projects, p)
	}
	jsonOK(w, projects)
}
