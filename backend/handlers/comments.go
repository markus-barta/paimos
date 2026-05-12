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

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("CreateComment: begin tx: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if !issueExistsActiveTx(tx, issueID) {
		jsonError(w, "issue not found", http.StatusNotFound)
		return
	}

	res, err := tx.ExecContext(r.Context(), `
		INSERT INTO comments(issue_id, author_id, body) VALUES(?, ?, ?)
	`, issueID, authorID, body.Body)
	if err != nil {
		log.Printf("CreateComment: issue_id=%d author_id=%v err=%v", issueID, authorID, err)
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	before := commentMutationSnapshot{ID: id}
	after, err := fetchCommentMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("CreateComment: snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       authorID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.comment.create"),
		SubjectType:  "comment",
		SubjectID:    id,
		InverseOp: InverseOp{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/comments/%d", id),
		},
		BeforeState: before,
		AfterState:  after,
		Undoable:    true,
	}); err != nil {
		log.Printf("CreateComment: recordMutation: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("CreateComment: commit: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

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
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("DeleteComment: begin tx: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	before, err := fetchCommentMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("DeleteComment: snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !before.Exists {
		jsonError(w, "comment not found", http.StatusNotFound)
		return
	}

	isOwner := before.AuthorID != nil && *before.AuthorID == user.ID
	if !isOwner && user.Role != "admin" {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := tx.ExecContext(r.Context(), "DELETE FROM comments WHERE id=?", id); err != nil {
		log.Printf("DeleteComment: id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	after, err := fetchCommentMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("DeleteComment: after snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	userID := user.ID
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       &userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.comment.delete"),
		SubjectType:  "comment",
		SubjectID:    id,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/comments/%d", id),
			Body:   before,
		},
		BeforeState: before,
		AfterState:  after,
		Undoable:    true,
	}); err != nil {
		log.Printf("DeleteComment: recordMutation: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("DeleteComment: commit: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
