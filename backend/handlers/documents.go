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
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
	"github.com/markus-barta/paimos/backend/storage"
)

// PAI-55. Customer- and project-scoped document storage backed by MinIO
// (same storage layer as attachments). Documents and attachments share
// one bucket; key namespacing keeps them separate (documents/…/… vs
// attachments' bare <issueId>/…).
//
// A 20 MB upload cap and a MIME-type allowlist keep the surface narrow —
// PDFs / images / common office docs are the v1 target.

const (
	documentsMaxFileSize = 20 << 20 // 20 MB per PAI-55
)

// allowedDocumentMimes is the v1 MIME allowlist. Word docs render as a
// generic file icon in the frontend (no preview); PDFs and images get
// real previews. Anything not in this list → 415.
var allowedDocumentMimes = map[string]bool{
	"application/pdf":             true,
	"image/png":                   true,
	"image/jpeg":                  true,
	"image/webp":                  true,
	"image/svg+xml":               true,
	"application/msword":          true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"text/plain":                  true,
	"text/markdown":               true,
}

// allowedDocumentStatuses mirrors the DB CHECK constraint so the API
// layer rejects bad values with a clean 400 instead of letting the
// constraint fire a 500.
var allowedDocumentStatuses = map[string]bool{
	"draft":   true,
	"active":  true,
	"expired": true,
}

// ── List ─────────────────────────────────────────────────────────────

func ListCustomerDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	listDocuments(w, "customer", id)
}

func ListProjectDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	listDocuments(w, "project", id)
}

func listDocuments(w http.ResponseWriter, scope string, scopeID int64) {
	col := "customer_id"
	if scope == "project" {
		col = "project_id"
	}
	rows, err := db.DB.Query(`
		SELECT id, scope, customer_id, project_id, filename, mime_type,
		       size_bytes, object_key, label, status, valid_from, valid_until,
		       uploaded_by, uploaded_at, updated_at
		FROM documents
		WHERE scope=? AND `+col+`=?
		ORDER BY uploaded_at DESC
	`, scope, scopeID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []models.Document{}
	for rows.Next() {
		var d models.Document
		if err := rows.Scan(&d.ID, &d.Scope, &d.CustomerID, &d.ProjectID,
			&d.Filename, &d.MimeType, &d.SizeBytes, &d.ObjectKey,
			&d.Label, &d.Status, &d.ValidFrom, &d.ValidUntil,
			&d.UploadedBy, &d.UploadedAt, &d.UpdatedAt); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		out = append(out, d)
	}
	jsonOK(w, out)
}

// ── Upload ───────────────────────────────────────────────────────────

func UploadCustomerDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	uploadDocument(w, r, "customer", id)
}

func UploadProjectDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	uploadDocument(w, r, "project", id)
}

func uploadDocument(w http.ResponseWriter, r *http.Request, scope string, scopeID int64) {
	if !storage.Enabled() {
		jsonError(w, "file storage not configured", http.StatusServiceUnavailable)
		return
	}
	// Hard-cap the request body so a malicious client can't exhaust
	// memory before we get to the per-file size check below.
	r.Body = http.MaxBytesReader(w, r.Body, documentsMaxFileSize+1<<20)
	if err := r.ParseMultipartForm(documentsMaxFileSize); err != nil {
		jsonError(w, "file too large or malformed multipart", http.StatusRequestEntityTooLarge)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if hdr.Size > documentsMaxFileSize {
		jsonError(w, "file exceeds 20MB limit", http.StatusRequestEntityTooLarge)
		return
	}

	mimeType := hdr.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if !allowedDocumentMimes[mimeType] {
		jsonError(w, "unsupported content type: "+mimeType, http.StatusUnsupportedMediaType)
		return
	}

	cleanName := sanitiseFilename(hdr.Filename)
	if cleanName == "" {
		cleanName = "upload"
	}

	// Optional metadata in form fields. All free to omit at upload time;
	// the user can edit via PUT after the row exists.
	label := r.FormValue("label")
	status := r.FormValue("status")
	if status == "" {
		status = "active"
	}
	if !allowedDocumentStatuses[status] {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}
	validFrom := nullableStrDoc(r.FormValue("valid_from"))
	validUntil := nullableStrDoc(r.FormValue("valid_until"))

	// Object-key namespace: documents/<scope>/<scopeID>/<rand>-<name>
	// Different from the bare "<issueID>/…" attachments use, so a single
	// bucket can hold both without collision.
	objectKey := fmt.Sprintf("documents/%s/%d/%s_%s",
		scope, scopeID, randHex(12), cleanName)

	if err := storage.Put(r.Context(), objectKey, mimeType, file, hdr.Size); err != nil {
		jsonError(w, "upload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	user := auth.GetUser(r)
	var uploaderID *int64
	if user != nil {
		uploaderID = &user.ID
	}

	var custID, projID *int64
	if scope == "customer" {
		custID = &scopeID
	} else {
		projID = &scopeID
	}

	res, err := db.DB.Exec(`
		INSERT INTO documents(
			scope, customer_id, project_id, filename, mime_type, size_bytes,
			object_key, label, status, valid_from, valid_until, uploaded_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, scope, custID, projID, cleanName, mimeType, hdr.Size,
		objectKey, label, status, validFrom, validUntil, uploaderID)
	if err != nil {
		// Best-effort orphan cleanup so a DB failure doesn't strand the
		// upload in MinIO forever.
		storage.Delete(r.Context(), objectKey)
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, getDocumentByID(id))
}

// ── Update metadata ─────────────────────────────────────────────────

func UpdateDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Label      *string `json:"label"`
		Status     *string `json:"status"`
		ValidFrom  *string `json:"valid_from"`
		ValidUntil *string `json:"valid_until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Status != nil && !allowedDocumentStatuses[*body.Status] {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = db.DB.Exec(`
		UPDATE documents SET
			label       = COALESCE(?, label),
			status      = COALESCE(?, status),
			valid_from  = CASE WHEN ? IS NOT NULL THEN ? ELSE valid_from END,
			valid_until = CASE WHEN ? IS NOT NULL THEN ? ELSE valid_until END,
			updated_at  = ?
		WHERE id=?
	`, body.Label, body.Status,
		body.ValidFrom, body.ValidFrom,
		body.ValidUntil, body.ValidUntil,
		now, id)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	d := getDocumentByID(id)
	if d == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, d)
}

// ── Download ─────────────────────────────────────────────────────────

func DownloadDocument(w http.ResponseWriter, r *http.Request) {
	if !storage.Enabled() {
		jsonError(w, "file storage not configured", http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	d := getDocumentByID(id)
	if d == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	reader, ct, objSize, err := storage.Get(r.Context(), d.ObjectKey)
	if err != nil {
		jsonError(w, "file not found in storage", http.StatusNotFound)
		return
	}
	defer reader.Close()
	if ct == "" {
		ct = d.MimeType
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(objSize, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, d.Filename))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	io.Copy(w, reader)
}

// ── Delete ───────────────────────────────────────────────────────────

func DeleteDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	d := getDocumentByID(id)
	if d == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if _, err := db.DB.Exec("DELETE FROM documents WHERE id=?", id); err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if storage.Enabled() && d.ObjectKey != "" {
		storage.Delete(r.Context(), d.ObjectKey)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ─────────────────────────────────────────────────────────

func getDocumentByID(id int64) *models.Document {
	var d models.Document
	err := db.DB.QueryRow(`
		SELECT id, scope, customer_id, project_id, filename, mime_type,
		       size_bytes, object_key, label, status, valid_from, valid_until,
		       uploaded_by, uploaded_at, updated_at
		FROM documents WHERE id=?
	`, id).Scan(&d.ID, &d.Scope, &d.CustomerID, &d.ProjectID,
		&d.Filename, &d.MimeType, &d.SizeBytes, &d.ObjectKey,
		&d.Label, &d.Status, &d.ValidFrom, &d.ValidUntil,
		&d.UploadedBy, &d.UploadedAt, &d.UpdatedAt)
	if err != nil {
		return nil
	}
	return &d
}

func nullableStrDoc(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
