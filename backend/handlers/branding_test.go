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

package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── PUT /api/branding ──────────────────────────────────────────────

func Test_PutBranding_Admin(t *testing.T) {
	ts := newTestServer(t)

	payload := map[string]any{
		"name":      "Acme PM",
		"company":   "Acme, Inc.",
		"product":   "Acme PM",
		"tagline":   "Project management for Acme",
		"website":   "https://acme.example",
		"logo":      "/brand/logo.svg",
		"favicon":   "/brand/favicon.svg",
		"pageTitle": "Acme PM",
		"colors": map[string]string{
			"primary":     "#1a2b3c",
			"primaryDark": "#000000",
			"accent":      "#ff0066",
		},
	}

	resp := ts.put(t, "/api/branding", ts.adminCookie, payload)
	assertStatus(t, resp, http.StatusOK)

	// Server should echo back the written document; confirm by GET.
	resp = ts.get(t, "/api/branding", "")
	assertStatus(t, resp, http.StatusOK)
	var got map[string]any
	decode(t, resp, &got)
	if got["name"] != "Acme PM" {
		t.Errorf("name = %v, want Acme PM", got["name"])
	}
	if got["website"] != "https://acme.example" {
		t.Errorf("website = %v", got["website"])
	}
}

func Test_PutBranding_Member_Forbidden(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.put(t, "/api/branding", ts.memberCookie, map[string]any{"name": "Nope"})
	assertStatus(t, resp, http.StatusForbidden)
}

func Test_PutBranding_Unauthenticated(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.put(t, "/api/branding", "", map[string]any{"name": "Nope"})
	assertStatus(t, resp, http.StatusUnauthorized)
}

func Test_PutBranding_BadColor(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.put(t, "/api/branding", ts.adminCookie, map[string]any{
		"colors": map[string]string{"primary": "not-a-hex"},
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func Test_PutBranding_BadWebsite(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.put(t, "/api/branding", ts.adminCookie, map[string]any{
		"website": "ftp://acme.example",
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func Test_PutBranding_InvalidJSON(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, ts.srv.URL+"/api/branding", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", ts.adminCookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	assertStatus(t, resp, http.StatusBadRequest)
}

func Test_PutBranding_WritesToDisk(t *testing.T) {
	ts := newTestServer(t)
	dataDir := os.Getenv("DATA_DIR")

	resp := ts.put(t, "/api/branding", ts.adminCookie, map[string]any{
		"name":      "DiskTest",
		"pageTitle": "DiskTest",
	})
	assertStatus(t, resp, http.StatusOK)

	data, err := os.ReadFile(filepath.Join(dataDir, "branding.json"))
	if err != nil {
		t.Fatalf("branding.json not written: %v", err)
	}
	var on map[string]any
	if err := json.Unmarshal(data, &on); err != nil {
		t.Fatalf("unreadable branding.json: %v", err)
	}
	if on["name"] != "DiskTest" {
		t.Errorf("on-disk name = %v", on["name"])
	}
}

func Test_PutBranding_MultiFile(t *testing.T) {
	ts := newTestServer(t)
	dataDir := os.Getenv("DATA_DIR")

	resp := ts.put(t, "/api/branding?file=branding-acme.json", ts.adminCookie, map[string]any{
		"name": "Acme",
	})
	assertStatus(t, resp, http.StatusOK)

	if _, err := os.Stat(filepath.Join(dataDir, "branding-acme.json")); err != nil {
		t.Errorf("branding-acme.json not written: %v", err)
	}
	// branding.json should NOT have been created by this request.
	if _, err := os.Stat(filepath.Join(dataDir, "branding.json")); err == nil {
		t.Errorf("branding.json unexpectedly written for ?file=branding-acme.json")
	}
}

func Test_PutBranding_RejectsPathTraversal(t *testing.T) {
	ts := newTestServer(t)
	dataDir := os.Getenv("DATA_DIR")

	// ?file=../evil.json should silently fall back to branding.json — never
	// escape the data dir.
	resp := ts.put(t, "/api/branding?file=../evil.json", ts.adminCookie, map[string]any{
		"name": "NotEvil",
	})
	assertStatus(t, resp, http.StatusOK)

	// Walk dataDir's parent — evil.json must not exist anywhere.
	parent := filepath.Dir(dataDir)
	if _, err := os.Stat(filepath.Join(parent, "evil.json")); err == nil {
		t.Errorf("path traversal: evil.json escaped data dir")
	}
}

// ── POST /api/branding/logo ────────────────────────────────────────

func Test_UploadBrandingLogo_SVG(t *testing.T) {
	ts := newTestServer(t)
	dataDir := os.Getenv("DATA_DIR")

	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="red"/></svg>`)

	resp := ts.postBrandingAsset(t, "/api/branding/logo", ts.adminCookie, "logo.svg", "image/svg+xml", svg)
	assertStatus(t, resp, http.StatusOK)

	var body struct {
		Path string `json:"path"`
	}
	decode(t, resp, &body)
	if body.Path != "/brand/logo.svg" {
		t.Errorf("path = %q, want /brand/logo.svg", body.Path)
	}

	got, err := os.ReadFile(filepath.Join(dataDir, "branding-assets", "logo.svg"))
	if err != nil {
		t.Fatalf("logo.svg not written: %v", err)
	}
	if !bytes.Equal(got, svg) {
		t.Errorf("on-disk SVG differs from uploaded bytes")
	}
}

func Test_UploadBrandingLogo_PNG(t *testing.T) {
	ts := newTestServer(t)
	dataDir := os.Getenv("DATA_DIR")

	pngBytes := mustMakePNG(t, 8, 8)
	resp := ts.postBrandingAsset(t, "/api/branding/logo", ts.adminCookie, "logo.png", "image/png", pngBytes)
	assertStatus(t, resp, http.StatusOK)

	if _, err := os.Stat(filepath.Join(dataDir, "branding-assets", "logo.png")); err != nil {
		t.Errorf("logo.png not written: %v", err)
	}
}

func Test_UploadBrandingLogo_MemberForbidden(t *testing.T) {
	ts := newTestServer(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	resp := ts.postBrandingAsset(t, "/api/branding/logo", ts.memberCookie, "logo.svg", "image/svg+xml", svg)
	assertStatus(t, resp, http.StatusForbidden)
}

func Test_UploadBrandingLogo_BadMIME(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.postBrandingAsset(t, "/api/branding/logo", ts.adminCookie, "evil.exe", "application/x-msdownload", []byte("MZ\x00\x00"))
	assertStatus(t, resp, http.StatusBadRequest)
}

func Test_UploadBrandingLogo_Oversize(t *testing.T) {
	ts := newTestServer(t)
	// 2 MiB of valid PNG-looking junk. We don't need a real PNG here —
	// the size check fires before any content inspection.
	big := make([]byte, 2<<20)
	for i := range big {
		big[i] = 'a'
	}
	resp := ts.postBrandingAsset(t, "/api/branding/logo", ts.adminCookie, "big.png", "image/png", big)
	assertStatus(t, resp, http.StatusBadRequest)
}

func Test_UploadBrandingLogo_ExtensionSwitchCleansOldFile(t *testing.T) {
	ts := newTestServer(t)
	dataDir := os.Getenv("DATA_DIR")
	assetsDir := filepath.Join(dataDir, "branding-assets")

	// First upload as PNG
	pngBytes := mustMakePNG(t, 4, 4)
	resp := ts.postBrandingAsset(t, "/api/branding/logo", ts.adminCookie, "logo.png", "image/png", pngBytes)
	assertStatus(t, resp, http.StatusOK)
	if _, err := os.Stat(filepath.Join(assetsDir, "logo.png")); err != nil {
		t.Fatalf("logo.png missing after first upload: %v", err)
	}

	// Then overwrite with SVG — logo.png must disappear.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	resp = ts.postBrandingAsset(t, "/api/branding/logo", ts.adminCookie, "logo.svg", "image/svg+xml", svg)
	assertStatus(t, resp, http.StatusOK)
	if _, err := os.Stat(filepath.Join(assetsDir, "logo.png")); err == nil {
		t.Errorf("logo.png still present after switching to SVG — stale asset leak")
	}
	if _, err := os.Stat(filepath.Join(assetsDir, "logo.svg")); err != nil {
		t.Errorf("logo.svg not written after switch: %v", err)
	}
}

// ── POST /api/branding/favicon ─────────────────────────────────────

func Test_UploadBrandingFavicon_SVG(t *testing.T) {
	ts := newTestServer(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	resp := ts.postBrandingAsset(t, "/api/branding/favicon", ts.adminCookie, "favicon.svg", "image/svg+xml", svg)
	assertStatus(t, resp, http.StatusOK)
}

func Test_UploadBrandingFavicon_MemberForbidden(t *testing.T) {
	ts := newTestServer(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	resp := ts.postBrandingAsset(t, "/api/branding/favicon", ts.memberCookie, "favicon.svg", "image/svg+xml", svg)
	assertStatus(t, resp, http.StatusForbidden)
}

func Test_UploadBrandingFavicon_RejectsJPEG(t *testing.T) {
	ts := newTestServer(t)
	// JPEG not in favicon whitelist — should be rejected even though it's
	// accepted on the logo endpoint.
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0}
	resp := ts.postBrandingAsset(t, "/api/branding/favicon", ts.adminCookie, "favicon.jpg", "image/jpeg", jpeg)
	assertStatus(t, resp, http.StatusBadRequest)
}

// ── GET /brand/{filename} ──────────────────────────────────────────

func Test_ServeBrandingAsset_Public(t *testing.T) {
	ts := newTestServer(t)

	// Upload a logo, then fetch it WITHOUT auth — the login page needs this.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><title>Test</title></svg>`)
	resp := ts.postBrandingAsset(t, "/api/branding/logo", ts.adminCookie, "logo.svg", "image/svg+xml", svg)
	assertStatus(t, resp, http.StatusOK)

	resp = ts.get(t, "/brand/logo.svg", "") // no cookie
	assertStatus(t, resp, http.StatusOK)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, svg) {
		t.Errorf("served bytes differ from uploaded SVG")
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("SVG missing restrictive CSP header: %q", csp)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing X-Content-Type-Options: nosniff")
	}
}

func Test_ServeBrandingAsset_NotFound(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/brand/nonexistent.svg", "")
	assertStatus(t, resp, http.StatusNotFound)
}

func Test_ServeBrandingAsset_RejectsBadFilename(t *testing.T) {
	ts := newTestServer(t)
	// Chi decodes URL-encoded segments, so a %2F would be a real slash —
	// chi's route matcher rejects that at the route level. Test what chi
	// *does* hand us: anything with uppercase or special chars.
	resp := ts.get(t, "/brand/LOGO.SVG", "")
	assertStatus(t, resp, http.StatusNotFound)
}

// ── helpers ─────────────────────────────────────────────────────────

// postBrandingAsset is postMultipart's cousin — it sets an explicit
// Content-Type header for the form field so the server can distinguish
// SVG (which http.DetectContentType can't reliably identify) from PNG/JPEG.
func (ts *testServer) postBrandingAsset(t *testing.T, path, cookie, fileName, mimeType string, fileContent []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="` + fileName + `"`}
	h["Content-Type"] = []string{mimeType}
	fw, err := w.CreatePart(h)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	fw.Write(fileContent)
	w.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.srv.URL+path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST multipart %s: %v", path, err)
	}
	return resp
}

// mustMakePNG builds a minimal valid PNG so upload tests have real bytes
// the server can accept via http.DetectContentType.
func mustMakePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}
