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
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/storage"
)

const (
	defaultMaxFileSize     = 10 << 20 // 10 MB
	maxAttachmentsPerIssue = 20
)

// Attachment is the JSON-serialised attachment record.
type Attachment struct {
	ID          int64  `json:"id"`
	IssueID     int64  `json:"issue_id"`
	ObjectKey   string `json:"object_key"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	UploadedBy  int64  `json:"uploaded_by"`
	Uploader    string `json:"uploader,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// ListAttachments — GET /api/issues/{id}/attachments
func ListAttachments(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(`
		SELECT a.id, a.issue_id, a.object_key, a.filename, a.content_type,
		       a.size_bytes, a.uploaded_by, COALESCE(u.username,''), a.created_at
		FROM attachments a
		LEFT JOIN users u ON u.id = a.uploaded_by
		WHERE a.issue_id = ?
		ORDER BY a.created_at ASC
	`, issueID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	list := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.IssueID, &a.ObjectKey, &a.Filename,
			&a.ContentType, &a.SizeBytes, &a.UploadedBy, &a.Uploader, &a.CreatedAt); err != nil {
			continue
		}
		list = append(list, a)
	}
	jsonOK(w, list)
}

// UploadAttachment — POST /api/issues/{id}/attachments (multipart form, field "file")
func UploadAttachment(w http.ResponseWriter, r *http.Request) {
	if !storage.Enabled() {
		jsonError(w, "file storage not configured", http.StatusServiceUnavailable)
		return
	}

	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Verify issue exists
	var exists int
	if err := db.DB.QueryRow("SELECT 1 FROM issues WHERE id=?", issueID).Scan(&exists); err != nil {
		jsonError(w, "issue not found", http.StatusNotFound)
		return
	}

	// Check attachment count cap
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM attachments WHERE issue_id=?", issueID).Scan(&count); err != nil {
		log.Printf("scan error: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if count >= maxAttachmentsPerIssue {
		jsonError(w, fmt.Sprintf("maximum %d attachments per issue", maxAttachmentsPerIssue), http.StatusUnprocessableEntity)
		return
	}

	maxSize := int64(defaultMaxFileSize)
	if err := r.ParseMultipartForm(maxSize + 1024); err != nil {
		jsonError(w, "file too large (max 10 MB)", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if header.Size > maxSize {
		jsonError(w, "file too large (max 10 MB)", http.StatusRequestEntityTooLarge)
		return
	}

	ct := header.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		ct = http.DetectContentType(buf[:n])
		file.Seek(0, 0)
	}

	filename := header.Filename
	objectKey := fmt.Sprintf("%d/%s_%s", issueID, randHex(12), sanitiseFilename(filename))

	// Server-side image processing: resize large images to max 2000px, strip EXIF
	var uploadReader io.Reader = file
	uploadSize := header.Size
	if strings.HasPrefix(ct, "image/") && ct != "image/gif" && ct != "image/svg+xml" {
		imgData, err := io.ReadAll(file)
		if err != nil {
			jsonError(w, "read failed", http.StatusInternalServerError)
			return
		}
		processed, outCT, pErr := processImage(bytes.NewReader(imgData), 2000, 2000, 85, true)
		if pErr == nil {
			uploadReader = bytes.NewReader(processed)
			uploadSize = int64(len(processed))
			ct = outCT
		} else {
			// Processing failed — upload original unchanged
			uploadReader = bytes.NewReader(imgData)
		}
	}

	if err := storage.Put(r.Context(), objectKey, ct, uploadReader, uploadSize); err != nil {
		jsonError(w, "upload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	user := auth.GetUser(r)
	var uploaderID int64
	if user != nil {
		uploaderID = user.ID
	}

	// Persist the post-processing byte count (what's actually in MinIO) —
	// keeps DB `size_bytes` consistent with the served `Content-Length`.
	res, err := db.DB.Exec(`
		INSERT INTO attachments (issue_id, object_key, filename, content_type, size_bytes, uploaded_by)
		VALUES (?, ?, ?, ?, ?, ?)
	`, issueID, objectKey, filename, ct, uploadSize, uploaderID)
	if err != nil {
		storage.Delete(r.Context(), objectKey)
		jsonError(w, "db insert failed", http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	var a Attachment
	db.DB.QueryRow(`
		SELECT a.id, a.issue_id, a.object_key, a.filename, a.content_type,
		       a.size_bytes, a.uploaded_by, COALESCE(u.username,''), a.created_at
		FROM attachments a
		LEFT JOIN users u ON u.id = a.uploaded_by
		WHERE a.id = ?
	`, id).Scan(&a.ID, &a.IssueID, &a.ObjectKey, &a.Filename,
		&a.ContentType, &a.SizeBytes, &a.UploadedBy, &a.Uploader, &a.CreatedAt)

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, a)
}

// GetAttachmentFile — GET /api/attachments/{id} — proxies file content from MinIO
func GetAttachmentFile(w http.ResponseWriter, r *http.Request) {
	if !storage.Enabled() {
		jsonError(w, "file storage not configured", http.StatusServiceUnavailable)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var a Attachment
	err = db.DB.QueryRow(`
		SELECT id, issue_id, object_key, filename, content_type, size_bytes
		FROM attachments WHERE id = ?
	`, id).Scan(&a.ID, &a.IssueID, &a.ObjectKey, &a.Filename, &a.ContentType, &a.SizeBytes)
	if err == sql.ErrNoRows {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}

	reader, ct, objSize, err := storage.Get(r.Context(), a.ObjectKey)
	if err != nil {
		jsonError(w, "file not found in storage", http.StatusNotFound)
		return
	}
	defer reader.Close()

	if ct == "" {
		ct = a.ContentType
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, a.Filename))
	// Use the actual stored object size, not a.SizeBytes — image processing
	// (resize / re-encode) can shrink the file after upload, and the DB
	// column records the pre-processing upload size.
	w.Header().Set("Content-Length", strconv.FormatInt(objSize, 10))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	io.Copy(w, reader)
}

// DeleteAttachment — DELETE /api/attachments/{id}
func DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var objectKey string
	var uploadedBy int64
	err = db.DB.QueryRow("SELECT object_key, uploaded_by FROM attachments WHERE id=?", id).Scan(&objectKey, &uploadedBy)
	if err == sql.ErrNoRows {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}

	// own-or-admin check
	user := auth.GetUser(r)
	if uploadedBy != user.ID && user.Role != "admin" {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := db.DB.Exec("DELETE FROM attachments WHERE id=?", id); err != nil {
		log.Printf("DeleteAttachment: id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if storage.Enabled() && objectKey != "" {
		storage.Delete(r.Context(), objectKey)
	}

	w.WriteHeader(http.StatusNoContent)
}

// UploadPendingAttachment — POST /api/attachments (no issue_id).
// For inline image paste/drop in create mode before the issue exists.
func UploadPendingAttachment(w http.ResponseWriter, r *http.Request) {
	if !storage.Enabled() {
		jsonError(w, "file storage not configured", http.StatusServiceUnavailable)
		return
	}

	maxSize := int64(defaultMaxFileSize)
	if err := r.ParseMultipartForm(maxSize + 1024); err != nil {
		jsonError(w, "file too large (max 10 MB)", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if header.Size > maxSize {
		jsonError(w, "file too large (max 10 MB)", http.StatusRequestEntityTooLarge)
		return
	}

	ct := header.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		ct = http.DetectContentType(buf[:n])
		file.Seek(0, 0)
	}

	filename := header.Filename
	objectKey := fmt.Sprintf("pending/%s_%s", randHex(12), sanitiseFilename(filename))

	// Server-side image processing
	var uploadReader io.Reader = file
	uploadSize := header.Size
	if strings.HasPrefix(ct, "image/") && ct != "image/gif" && ct != "image/svg+xml" {
		imgData, err := io.ReadAll(file)
		if err != nil {
			jsonError(w, "read failed", http.StatusInternalServerError)
			return
		}
		processed, outCT, pErr := processImage(bytes.NewReader(imgData), 2000, 2000, 85, true)
		if pErr == nil {
			uploadReader = bytes.NewReader(processed)
			uploadSize = int64(len(processed))
			ct = outCT
		} else {
			uploadReader = bytes.NewReader(imgData)
		}
	}

	if err := storage.Put(r.Context(), objectKey, ct, uploadReader, uploadSize); err != nil {
		jsonError(w, "upload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	user := auth.GetUser(r)
	var uploaderID int64
	if user != nil {
		uploaderID = user.ID
	}

	// issue_id is NULL for pending attachments
	res, err := db.DB.Exec(`
		INSERT INTO attachments (issue_id, object_key, filename, content_type, size_bytes, uploaded_by)
		VALUES (NULL, ?, ?, ?, ?, ?)
	`, objectKey, filename, ct, uploadSize, uploaderID)
	if err != nil {
		storage.Delete(r.Context(), objectKey)
		jsonError(w, "db insert failed", http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	var a Attachment
	db.DB.QueryRow(`
		SELECT a.id, COALESCE(a.issue_id,0), a.object_key, a.filename, a.content_type,
		       a.size_bytes, a.uploaded_by, COALESCE(u.username,''), a.created_at
		FROM attachments a LEFT JOIN users u ON u.id = a.uploaded_by
		WHERE a.id = ?
	`, id).Scan(&a.ID, &a.IssueID, &a.ObjectKey, &a.Filename,
		&a.ContentType, &a.SizeBytes, &a.UploadedBy, &a.Uploader, &a.CreatedAt)

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, a)
}

// LinkAttachments — PATCH /api/attachments/link
// Associates pending (issue_id=NULL) attachments with an issue.
// Body: { "issue_id": 123, "attachment_ids": [1, 2, 3] }
func LinkAttachments(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IssueID       int64   `json:"issue_id"`
		AttachmentIDs []int64 `json:"attachment_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IssueID == 0 || len(body.AttachmentIDs) == 0 {
		jsonError(w, "issue_id and attachment_ids required", http.StatusBadRequest)
		return
	}

	// Resolve the owning project and gate on edit access. Orphan sprint
	// issues have no project — linking attachments to an orphan makes no
	// sense (orphans don't support attachments), so reject that case too.
	pid, found, orphan := auth.ProjectIDForIssue(body.IssueID)
	if !found || orphan {
		jsonError(w, "issue not found", http.StatusNotFound)
		return
	}
	if !auth.CanEditProject(r, pid) {
		if auth.CanViewProject(r, pid) {
			jsonError(w, "forbidden", http.StatusForbidden)
		} else {
			jsonError(w, "issue not found", http.StatusNotFound)
		}
		return
	}

	// PAI-112: a pending attachment can only be linked by its uploader,
	// or by an admin. Without this check, any editor on the target project
	// who guessed a pending attachment id could hijack a paste/drop the
	// uploader had not yet committed to an issue.
	user := auth.GetUser(r)
	isAdmin := user != nil && user.Role == "admin"
	var callerID int64
	if user != nil {
		callerID = user.ID
	}

	linked := 0
	for _, aid := range body.AttachmentIDs {
		// Only link attachments that are currently unlinked AND owned by
		// the caller (or any unlinked attachment, for admins). Combining
		// the ownership check into the WHERE clause makes the lookup and
		// the gate one atomic decision.
		var oldKey string
		var err error
		if isAdmin {
			err = db.DB.QueryRow(
				"SELECT object_key FROM attachments WHERE id=? AND issue_id IS NULL",
				aid,
			).Scan(&oldKey)
		} else {
			err = db.DB.QueryRow(
				"SELECT object_key FROM attachments WHERE id=? AND issue_id IS NULL AND uploaded_by=?",
				aid, callerID,
			).Scan(&oldKey)
		}
		if err != nil {
			continue // already linked, doesn't exist, or not owned by caller
		}

		// Move object from pending/ to {issue_id}/ namespace
		newKey := fmt.Sprintf("%d/%s", body.IssueID, strings.TrimPrefix(oldKey, "pending/"))
		if storage.Enabled() {
			// Copy then delete (MinIO has no rename)
			reader, ct, _, err := storage.Get(r.Context(), oldKey)
			if err == nil {
				data, _ := io.ReadAll(reader)
				reader.Close()
				if err := storage.Put(r.Context(), newKey, ct, bytes.NewReader(data), int64(len(data))); err == nil {
					storage.Delete(r.Context(), oldKey)
				}
			}
		}

		if _, err := db.DB.Exec("UPDATE attachments SET issue_id=?, object_key=? WHERE id=?", body.IssueID, newKey, aid); err != nil {
			log.Printf("LinkAttachments: update attachment=%d: %v", aid, err)
			continue
		}
		linked++
	}

	jsonOK(w, map[string]int{"linked": linked})
}

func sanitiseFilename(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" {
		name = "attachment"
	}
	return name
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
