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
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/brand"
)

// defaultBrandingJSON is served when no branding.json file is found in DATA_DIR.
// Values derive from brand.Default so operators get a sensible branding
// document even without writing their own JSON.
func defaultBrandingJSON() []byte {
	b := brand.Default
	return []byte(fmt.Sprintf(`{
  "name": %q,
  "company": %q,
  "product": %q,
  "tagline": "Your Professional & Personal AI Project OS",
  "website": %q,
  "logo": "/logo.svg",
  "favicon": "/favicon.svg",
  "colors": {
    "primary": "#2e6da4",
    "primaryDark": "#1f4d75",
    "primaryLight": "#4a8fc2",
    "primaryPale": "#dce9f4",
    "accent": "#16a34a",
    "sidebarBg": "#1a2d42",
    "sidebarText": "#c8d5e2",
    "loginBg": "#1a2d42",
    "loginPattern": "#243650"
  },
  "pageTitle": %q
}`, b.ProductName, b.CompanyName, b.ProductName, b.WebsiteURL, b.PageTitle))
}

func brandingDir() string {
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		return dir
	}
	return "/app/data"
}

func brandingAssetsDir() string {
	return filepath.Join(brandingDir(), "branding-assets")
}

// brandingFilePattern: branding.json or branding-<slug>.json where slug is
// [a-z0-9-]+. Anchored to prevent any path-traversal shenanigans.
var brandingFilePattern = regexp.MustCompile(`^branding(-[a-z0-9-]+)?\.json$`)

// resolveBrandingFile picks the branding filename from the ?file= query param
// if it matches the whitelist, otherwise defaults to branding.json. Any
// non-matching input silently falls back — callers don't get to target
// arbitrary files.
func resolveBrandingFile(r *http.Request) string {
	f := r.URL.Query().Get("file")
	if f != "" && brandingFilePattern.MatchString(f) {
		return f
	}
	return "branding.json"
}

// GET /api/branding — returns the active branding config.
// Optionally accepts ?file=branding-acme.json to load a specific file.
func GetBranding(w http.ResponseWriter, r *http.Request) {
	filename := resolveBrandingFile(r)

	path := filepath.Join(brandingDir(), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		// Fall back to default branding
		w.Header().Set("Content-Type", "application/json")
		w.Write(defaultBrandingJSON())
		return
	}

	// Validate it's valid JSON
	var check json.RawMessage
	if json.Unmarshal(data, &check) != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(defaultBrandingJSON())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// GET /api/brandings — lists available branding*.json files with their display names.
func ListBrandings(w http.ResponseWriter, r *http.Request) {
	dir := brandingDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		jsonOK(w, []any{})
		return
	}

	type brandingEntry struct {
		File string `json:"file"`
		Name string `json:"name"`
	}

	var result []brandingEntry
	for _, e := range entries {
		name := e.Name()
		if !brandingFilePattern.MatchString(name) {
			continue
		}
		// Read the file to extract the "name" field
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var parsed struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &parsed) != nil {
			continue
		}
		displayName := parsed.Name
		if displayName == "" {
			displayName = strings.TrimSuffix(strings.TrimPrefix(name, "branding"), ".json")
			if displayName == "" {
				displayName = "Default"
			} else {
				displayName = strings.TrimPrefix(displayName, "-")
			}
		}
		result = append(result, brandingEntry{File: name, Name: displayName})
	}

	if result == nil {
		result = []brandingEntry{}
	}
	jsonOK(w, result)
}

// brandingPayload is the shape PUT /api/branding accepts. All fields optional;
// missing fields are filled from the defaults. Colors is a map rather than a
// fixed struct so operators can add new palette keys without a backend bump.
type brandingPayload struct {
	Name      string            `json:"name"`
	Company   string            `json:"company"`
	Product   string            `json:"product"`
	Tagline   string            `json:"tagline"`
	Website   string            `json:"website"`
	Logo      string            `json:"logo"`
	Favicon   string            `json:"favicon"`
	Colors    map[string]string `json:"colors"`
	PageTitle string            `json:"pageTitle"`
}

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// validateBrandingPayload returns a non-empty error message if the payload
// has obviously bad values. Keeps validation light — the UI is admin-only,
// so we're guarding against typos, not attackers.
func validateBrandingPayload(p *brandingPayload) string {
	for k, v := range p.Colors {
		if v == "" {
			continue
		}
		if !hexColorPattern.MatchString(v) {
			return fmt.Sprintf("color %q: must be #rrggbb, got %q", k, v)
		}
	}
	if p.Website != "" && !(strings.HasPrefix(p.Website, "http://") || strings.HasPrefix(p.Website, "https://")) {
		return "website: must start with http:// or https://"
	}
	// Logo/favicon URLs may be absolute (/foo.svg), /brand/foo.svg, or /api/... —
	// don't over-validate. Just reject embedded control chars.
	for _, v := range []string{p.Logo, p.Favicon} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return "logo/favicon: invalid characters"
		}
	}
	return ""
}

// PUT /api/branding — admin-only. Validates + writes branding.json (or
// ?file=branding-<slug>.json if that matches the whitelist). Returns the
// written document so the client can re-apply without a second fetch.
func PutBranding(w http.ResponseWriter, r *http.Request) {
	var p brandingPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if msg := validateBrandingPayload(&p); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}

	filename := resolveBrandingFile(r)
	dir := brandingDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonError(w, "storage error", http.StatusInternalServerError)
		return
	}

	// Marshal with stable indentation so the on-disk file stays diff-friendly
	// for ops who want to hand-edit or version-control their branding.json.
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		jsonError(w, "encode failed", http.StatusInternalServerError)
		return
	}
	// Atomic write: tmp file + rename, so a crash mid-write can't leave a
	// truncated branding.json that would make GetBranding fall back to
	// defaults on next boot.
	path := filepath.Join(dir, filename)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		jsonError(w, "write failed", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		jsonError(w, "write failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// Accepted MIME types for branding asset uploads. Kept as explicit maps (not
// slices) so callers get O(1) lookups — these are checked on every upload.
var logoMimeTypes = map[string]string{
	"image/svg+xml": "svg",
	"image/png":     "png",
	"image/jpeg":    "jpg",
}

var faviconMimeTypes = map[string]string{
	"image/svg+xml":      "svg",
	"image/png":          "png",
	"image/x-icon":       "ico",
	"image/vnd.microsoft.icon": "ico",
}

const (
	logoMaxBytes    = 1 << 20       // 1 MiB
	faviconMaxBytes = 256 * 1024    // 256 KiB
)

// uploadBrandingAsset is the shared body of the logo + favicon upload
// handlers. `baseName` is the on-disk name sans extension ("logo" or
// "favicon"); the caller provides the MIME whitelist and size cap.
func uploadBrandingAsset(w http.ResponseWriter, r *http.Request, baseName string, accept map[string]string, maxBytes int64) {
	// +1KB for multipart envelope overhead
	if err := r.ParseMultipartForm(maxBytes + 1024); err != nil {
		jsonError(w, fmt.Sprintf("file too large (max %d KB)", maxBytes/1024), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "file field required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Size > maxBytes {
		jsonError(w, fmt.Sprintf("file too large (max %d KB)", maxBytes/1024), http.StatusBadRequest)
		return
	}

	// Read full file so we can both detect the content type and write it.
	// Uploads are small (<=1 MiB) — no streaming needed.
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		jsonError(w, "read failed", http.StatusInternalServerError)
		return
	}
	if int64(len(data)) > maxBytes {
		jsonError(w, fmt.Sprintf("file too large (max %d KB)", maxBytes/1024), http.StatusBadRequest)
		return
	}

	// Prefer client-declared content-type, fall back to sniffing. http.DetectContentType
	// can't recognise SVG reliably (it returns text/xml), so we cross-check
	// against the filename extension for SVGs.
	ct := header.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = http.DetectContentType(data)
	}
	// SVG fallback: the filename says .svg and the content begins with
	// <svg or <?xml. Covers the common case where clients send a generic type.
	if _, ok := accept[ct]; !ok {
		if strings.EqualFold(filepath.Ext(header.Filename), ".svg") && looksLikeSVG(data) {
			ct = "image/svg+xml"
		}
	}
	ext, ok := accept[ct]
	if !ok {
		jsonError(w, "unsupported file type: "+ct, http.StatusBadRequest)
		return
	}

	assetsDir := brandingAssetsDir()
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		jsonError(w, "storage error", http.StatusInternalServerError)
		return
	}

	// Clean up older extensions of the same asset so the previous logo
	// doesn't linger under a different extension (e.g. old logo.png when
	// we just wrote logo.svg).
	for _, oldExt := range []string{"svg", "png", "jpg", "ico"} {
		if oldExt == ext {
			continue
		}
		_ = os.Remove(filepath.Join(assetsDir, baseName+"."+oldExt))
	}

	outPath := filepath.Join(assetsDir, baseName+"."+ext)
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		jsonError(w, "write failed", http.StatusInternalServerError)
		return
	}

	// Public URL the frontend should reference. Served by ServeBrandingAsset
	// (public, no auth) mounted at /brand/{filename} in main.go.
	jsonOK(w, map[string]string{"path": "/brand/" + baseName + "." + ext})
}

// looksLikeSVG is a minimal heuristic — we don't parse the XML, just
// check the first few hundred bytes for <svg or <?xml. The processImage
// path rejects anything that's not raster, so this is only reached when
// content-type said "text/xml" or similar.
func looksLikeSVG(data []byte) bool {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "<svg") || strings.HasPrefix(strings.TrimSpace(s), "<?xml")
}

// POST /api/branding/logo — admin-only. Multipart with field "file".
// Accepts SVG, PNG, JPEG up to 1 MiB. Saves to $DATA_DIR/branding-assets/logo.<ext>.
func UploadBrandingLogo(w http.ResponseWriter, r *http.Request) {
	uploadBrandingAsset(w, r, "logo", logoMimeTypes, logoMaxBytes)
}

// POST /api/branding/favicon — admin-only. Multipart with field "file".
// Accepts SVG, PNG, ICO up to 256 KiB. Saves to $DATA_DIR/branding-assets/favicon.<ext>.
func UploadBrandingFavicon(w http.ResponseWriter, r *http.Request) {
	uploadBrandingAsset(w, r, "favicon", faviconMimeTypes, faviconMaxBytes)
}

// brandingAssetFilenamePattern: letters, digits, dash, dot only. Matches
// `logo.svg`, `favicon.ico`, etc. Kept tight because this endpoint is PUBLIC
// and touches the filesystem.
var brandingAssetFilenamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.[a-z0-9]+$`)

// GET /brand/{filename} — public. Serves files from $DATA_DIR/branding-assets/.
// Public because the login page needs the logo before the user can authenticate.
// Filename is strictly whitelisted to prevent path traversal; missing files
// return 404 (not default logos — those are served as /logo.svg from the
// static frontend bundle).
func ServeBrandingAsset(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if !brandingAssetFilenamePattern.MatchString(filename) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(brandingAssetsDir(), filename)
	// Defense-in-depth: resolve and re-check the path is inside assetsDir.
	absAssets, err1 := filepath.Abs(brandingAssetsDir())
	absFile, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil || !strings.HasPrefix(absFile, absAssets+string(filepath.Separator)) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// SVG can contain <script>; serve with a restrictive CSP so even an SVG
	// loaded directly in a browser tab can't run JS. Combined with
	// nosniff, the browser also won't re-interpret it as HTML.
	if strings.HasSuffix(filename, ".svg") {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Short cache: the file can change at any time via the admin UI.
	w.Header().Set("Cache-Control", "public, max-age=60")
	http.ServeFile(w, r, path)
}
