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

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

type Comment struct {
	ID         int64   `json:"id"`
	IssueID    int64   `json:"issue_id"`
	AuthorID   *int64  `json:"author_id"`
	Author     *string `json:"author"`
	AvatarPath *string `json:"avatar_path"`
	Body       string  `json:"body"`
	CreatedAt  string  `json:"created_at"`
}

// GET /api/issues/{id}/comments
func ListComments(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid issue id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(`
		SELECT c.id, c.issue_id, c.author_id, COALESCE(NULLIF(u.nickname,''), u.username), u.avatar_path, c.body, c.created_at
		FROM comments c
		LEFT JOIN users u ON u.id = c.author_id
		WHERE c.issue_id = ?
		ORDER BY c.created_at ASC
	`, issueID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	comments := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorID, &c.Author, &c.AvatarPath, &c.Body, &c.CreatedAt); err == nil {
			comments = append(comments, c)
		}
	}
	jsonOK(w, comments)
}

// POST /api/issues/{id}/comments  { "body": "..." }
func CreateComment(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid issue id", http.StatusBadRequest)
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Body == "" {
		jsonError(w, "body required", http.StatusBadRequest)
		return
	}

	user := auth.GetUser(r)
	var authorID *int64
	if user != nil {
		authorID = &user.ID
	}

	res, err := db.DB.Exec(`
		INSERT INTO comments(issue_id, author_id, body) VALUES(?, ?, ?)
	`, issueID, authorID, body.Body)
	if err != nil {
		log.Printf("CreateComment: issue_id=%d author_id=%v err=%v", issueID, authorID, err)
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	var c Comment
	db.DB.QueryRow(`
		SELECT c.id, c.issue_id, c.author_id, COALESCE(NULLIF(u.nickname,''), u.username), u.avatar_path, c.body, c.created_at
		FROM comments c LEFT JOIN users u ON u.id = c.author_id
		WHERE c.id = ?
	`, id).Scan(&c.ID, &c.IssueID, &c.AuthorID, &c.Author, &c.AvatarPath, &c.Body, &c.CreatedAt)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}

// DELETE /api/comments/{id}  — own comment or admin
func DeleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	user := auth.GetUser(r)

	// Check ownership
	var authorID *int64
	err = db.DB.QueryRow("SELECT author_id FROM comments WHERE id=?", id).Scan(&authorID)
	if err != nil {
		jsonError(w, "comment not found", http.StatusNotFound)
		return
	}

	isOwner := authorID != nil && *authorID == user.ID
	if !isOwner && user.Role != "admin" {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := db.DB.Exec("DELETE FROM comments WHERE id=?", id); err != nil {
		log.Printf("DeleteComment: id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
