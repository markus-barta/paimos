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
	"fmt"
	"log"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// POST /api/projects/:id/logo — upload project logo (multipart/form-data, field "logo").
// Accepts JPG/PNG, max 3MB. Resizes to max 400×400px, preserves PNG alpha.
// Returns updated Project.
func UploadProjectLogo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := r.ParseMultipartForm(3 << 20); err != nil {
		jsonError(w, "file too large (max 3MB)", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("logo")
	if err != nil {
		jsonError(w, "logo field required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, file); err != nil {
		jsonError(w, "read failed", http.StatusInternalServerError)
		return
	}

	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(buf.Bytes())
	}
	if ct != "image/jpeg" && ct != "image/png" {
		jsonError(w, "only JPEG and PNG images are accepted", http.StatusBadRequest)
		return
	}

	// Process: resize to max 400×400, preserve PNG alpha, strip EXIF
	processed, outCT, err := processImage(bytes.NewReader(buf.Bytes()), 400, 400, 85, true)
	if err != nil {
		jsonError(w, "invalid image", http.StatusBadRequest)
		return
	}

	logosDir := filepath.Join(getDataDir(), "logos")
	if err := os.MkdirAll(logosDir, 0755); err != nil {
		jsonError(w, "storage error", http.StatusInternalServerError)
		return
	}
	ext := "jpg"
	if outCT == "image/png" {
		ext = "png"
	}
	filename := fmt.Sprintf("%d.%s", id, ext)
	destPath := filepath.Join(logosDir, filename)
	if err := os.WriteFile(destPath, processed, 0644); err != nil {
		jsonError(w, "write error", http.StatusInternalServerError)
		return
	}

	// Remove old logo if extension changed (e.g. was .jpg, now .png)
	oldExt := "png"
	if ext == "png" {
		oldExt = "jpg"
	}
	_ = os.Remove(filepath.Join(logosDir, fmt.Sprintf("%d.%s", id, oldExt)))

	relPath := "/api/logos/" + filename
	if _, err := db.DB.Exec("UPDATE projects SET logo_path=?, updated_at=datetime('now') WHERE id=?", relPath, id); err != nil {
		jsonError(w, "db update failed", http.StatusInternalServerError)
		return
	}

	p := getProjectByID(id)
	if p == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, p)
}

// DELETE /api/projects/:id/logo — remove project logo, clear logo_path.
func DeleteProjectLogo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var old string
	if err := db.DB.QueryRow("SELECT logo_path FROM projects WHERE id=?", id).Scan(&old); err != nil {
		log.Printf("scan error: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if old != "" {
		filename := filepath.Base(old)
		_ = os.Remove(filepath.Join(getDataDir(), "logos", filename))
	}

	if _, err := db.DB.Exec("UPDATE projects SET logo_path='', updated_at=datetime('now') WHERE id=?", id); err != nil {
		log.Printf("DeleteProjectLogo: id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	p := getProjectByID(id)
	if p == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, p)
}
