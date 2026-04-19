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
	"github.com/markus-barta/paimos/backend/models"
)

// ValidColors is the allowed palette — enforced server-side.
var ValidColors = map[string]bool{
	"gray": true, "slate": true, "blue": true, "indigo": true,
	"purple": true, "pink": true, "red": true, "orange": true,
	"yellow": true, "green": true, "teal": true, "cyan": true,
}

// ── Tag CRUD ────────────────────────────────────────────────────────────────

func ListTags(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(
		`SELECT id, name, color, description, system, created_at FROM tags ORDER BY name`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	tags := []models.Tag{}
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Description, &t.System, &t.CreatedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		tags = append(tags, t)
	}
	jsonOK(w, tags)
}

func CreateTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}
	if body.Color == "" {
		body.Color = "gray"
	}
	if !ValidColors[body.Color] {
		jsonError(w, "invalid color", http.StatusBadRequest)
		return
	}

	res, err := db.DB.Exec(
		`INSERT INTO tags(name,color,description) VALUES(?,?,?)`,
		body.Name, body.Color, body.Description,
	)
	if handleDBError(w, err, "tag") {
		return
	}
	id, _ := res.LastInsertId()
	var t models.Tag
	db.DB.QueryRow(`SELECT id,name,color,description,system,created_at FROM tags WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.Color, &t.Description, &t.System, &t.CreatedAt)
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, t)
}

func UpdateTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Color       *string `json:"color"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Color != nil && !ValidColors[*body.Color] {
		jsonError(w, "invalid color", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec(`
		UPDATE tags SET
			name        = COALESCE(?, name),
			color       = COALESCE(?, color),
			description = COALESCE(?, description)
		WHERE id=?
	`, body.Name, body.Color, body.Description, id); err != nil {
		handleDBError(w, err, "tag")
		return
	}
	var t models.Tag
	if err := db.DB.QueryRow(`SELECT id,name,color,description,system,created_at FROM tags WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.Color, &t.Description, &t.System, &t.CreatedAt); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, t)
}

func DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if isSystemTag(id) {
		jsonError(w, "system tags cannot be deleted", http.StatusForbidden)
		return
	}
	// Explicitly remove associations first — belt-and-suspenders in case
	// ON DELETE CASCADE doesn't fire (e.g. FK pragma not active on this conn).
	if _, err := db.DB.Exec(`DELETE FROM issue_tags WHERE tag_id=?`, id); err != nil {
		log.Printf("DeleteTag issue_tags id=%d: %v", id, err)
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if _, err := db.DB.Exec(`DELETE FROM project_tags WHERE tag_id=?`, id); err != nil {
		log.Printf("DeleteTag project_tags id=%d: %v", id, err)
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	res, err := db.DB.Exec(`DELETE FROM tags WHERE id=?`, id)
	if err != nil {
		log.Printf("DeleteTag id=%d: %v", id, err)
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Tag associations ─────────────────────────────────────────────────────────

func AddTagToIssue(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid issue id", http.StatusBadRequest)
		return
	}
	var body struct {
		TagID int64 `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TagID == 0 {
		jsonError(w, "tag_id required", http.StatusBadRequest)
		return
	}
	// Block manual attachment of system tags
	if isSystemTag(body.TagID) {
		jsonError(w, "system tags cannot be added manually", http.StatusForbidden)
		return
	}
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`,
		issueID, body.TagID); err != nil {
		log.Printf("AddTagToIssue: issue_id=%d tag_id=%d err=%v", issueID, body.TagID, err)
		jsonError(w, "failed to attach tag", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func RemoveTagFromIssue(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid issue id", http.StatusBadRequest)
		return
	}
	tagID, err := strconv.ParseInt(chi.URLParam(r, "tag_id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid tag id", http.StatusBadRequest)
		return
	}
	// Block manual removal of system tags
	if isSystemTag(tagID) {
		jsonError(w, "system tags cannot be removed manually", http.StatusForbidden)
		return
	}
	if _, err := db.DB.Exec(`DELETE FROM issue_tags WHERE issue_id=? AND tag_id=?`, issueID, tagID); err != nil {
		log.Printf("RemoveTagFromIssue: issue=%d tag=%d: %v", issueID, tagID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func AddTagToProject(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body struct {
		TagID int64 `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TagID == 0 {
		jsonError(w, "tag_id required", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO project_tags(project_id,tag_id) VALUES(?,?)`,
		projectID, body.TagID); err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func RemoveTagFromProject(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	tagID, err := strconv.ParseInt(chi.URLParam(r, "tag_id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid tag id", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec(`DELETE FROM project_tags WHERE project_id=? AND tag_id=?`, projectID, tagID); err != nil {
		log.Printf("RemoveTagFromProject: project=%d tag=%d: %v", projectID, tagID, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Tag loading helpers (used by other handlers) ──────────────────────────────

// LoadTagsForIssues loads tags for a slice of issues in a single query.
func LoadTagsForIssues(issues []models.Issue) []models.Issue {
	if len(issues) == 0 {
		return issues
	}
	// Collect IDs
	ids := make([]any, len(issues))
	idxByID := make(map[int64]int, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		idxByID[iss.ID] = i
		issues[i].Tags = []models.Tag{}
	}

	placeholders := buildPlaceholders(len(ids))
	rows, err := db.DB.Query(`
		SELECT it.issue_id, t.id, t.name, t.color, t.description, t.system, t.created_at
		FROM issue_tags it
		JOIN tags t ON t.id = it.tag_id
		WHERE it.issue_id IN (`+placeholders+`)
		ORDER BY t.name
	`, ids...)
	if err != nil {
		return issues
	}
	defer rows.Close()
	for rows.Next() {
		var issueID int64
		var t models.Tag
		if err := rows.Scan(&issueID, &t.ID, &t.Name, &t.Color, &t.Description, &t.System, &t.CreatedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		if idx, ok := idxByID[issueID]; ok {
			issues[idx].Tags = append(issues[idx].Tags, t)
		}
	}
	return issues
}

// LoadTagsForProjects loads tags for a slice of projects in a single query.
func LoadTagsForProjects(projects []models.Project) []models.Project {
	if len(projects) == 0 {
		return projects
	}
	ids := make([]any, len(projects))
	idxByID := make(map[int64]int, len(projects))
	for i, p := range projects {
		ids[i] = p.ID
		idxByID[p.ID] = i
		projects[i].Tags = []models.Tag{}
	}
	placeholders := buildPlaceholders(len(ids))
	rows, err := db.DB.Query(`
		SELECT pt.project_id, t.id, t.name, t.color, t.description, t.system, t.created_at
		FROM project_tags pt
		JOIN tags t ON t.id = pt.tag_id
		WHERE pt.project_id IN (`+placeholders+`)
		ORDER BY t.name
	`, ids...)
	if err != nil {
		return projects
	}
	defer rows.Close()
	for rows.Next() {
		var projID int64
		var t models.Tag
		if err := rows.Scan(&projID, &t.ID, &t.Name, &t.Color, &t.Description, &t.System, &t.CreatedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		if idx, ok := idxByID[projID]; ok {
			projects[idx].Tags = append(projects[idx].Tags, t)
		}
	}
	return projects
}

// LoadTagsForIssue loads tags for a single issue.
func LoadTagsForIssue(issue *models.Issue) {
	issue.Tags = []models.Tag{}
	rows, err := db.DB.Query(`
		SELECT t.id, t.name, t.color, t.description, t.system, t.created_at
		FROM issue_tags it
		JOIN tags t ON t.id = it.tag_id
		WHERE it.issue_id = ?
		ORDER BY t.name
	`, issue.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Description, &t.System, &t.CreatedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		issue.Tags = append(issue.Tags, t)
	}
}

// LoadTagsForProject loads tags for a single project.
func LoadTagsForProject(project *models.Project) {
	project.Tags = []models.Tag{}
	rows, err := db.DB.Query(`
		SELECT t.id, t.name, t.color, t.description, t.system, t.created_at
		FROM project_tags pt
		JOIN tags t ON t.id = pt.tag_id
		WHERE pt.project_id = ?
		ORDER BY t.name
	`, project.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Description, &t.System, &t.CreatedAt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		project.Tags = append(project.Tags, t)
	}
}

// isSystemTag checks if a tag has the system flag set.
func isSystemTag(tagID int64) bool {
	var sys int
	if err := db.DB.QueryRow(`SELECT system FROM tags WHERE id=?`, tagID).Scan(&sys); err != nil {
		return false
	}
	return sys == 1
}

func buildPlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	b := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
