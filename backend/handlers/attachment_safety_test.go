// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-110 — unit tests for the upload + serve safety helpers.
//
// Co-located in package handlers so the unexported helpers can be
// exercised directly. The full HTTP path also runs through these — but
// the upload handler short-circuits on storage.Enabled() in the test
// harness (no MinIO), so a pure-helper test is the only level at which
// the active-content decision can be asserted today.

package handlers

import (
	"strings"
	"testing"
)

func TestRejectActiveContent_BlocksHTML(t *testing.T) {
	// Hostile client lies about the type — declared image/png, payload is HTML.
	body := []byte(`<!DOCTYPE html><html><body><script>alert(1)</script></body></html>`)
	if reason := rejectActiveContent("image/png", body); reason == "" {
		t.Fatalf("HTML payload with declared image/png was not rejected")
	}
}

func TestRejectActiveContent_BlocksDeclaredHTML(t *testing.T) {
	if reason := rejectActiveContent("text/html; charset=utf-8", []byte("hi")); reason != "text/html" {
		t.Fatalf("declared text/html: got %q, want text/html", reason)
	}
}

func TestRejectActiveContent_BlocksSVGWithScript(t *testing.T) {
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if reason := rejectActiveContent("image/svg+xml", svg); reason == "" {
		t.Fatalf("SVG with script was not rejected")
	}
}

func TestRejectActiveContent_BlocksSVGEvenIfDeclaredImage(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if reason := rejectActiveContent("image/png", svg); reason == "" {
		t.Fatalf("SVG payload with declared image/png was not rejected")
	}
}

func TestRejectActiveContent_BlocksJavaScript(t *testing.T) {
	js := []byte("function pwn() { fetch('/api/auth/me') }")
	if reason := rejectActiveContent("application/javascript", js); reason != "application/javascript" {
		t.Fatalf("declared JS: got %q", reason)
	}
}

func TestRejectActiveContent_BlocksScriptTagInPlainText(t *testing.T) {
	// Payload that http.DetectContentType will see as text/plain but
	// which contains a <script> tag the moment a browser is tricked into
	// rendering it as HTML (e.g. via a Content-Type override).
	body := []byte("preface text\n<script>alert(1)</script> more")
	if reason := rejectActiveContent("text/plain", body); reason == "" {
		t.Fatalf("payload containing <script> was not rejected")
	}
}

func TestRejectActiveContent_AllowsPNG(t *testing.T) {
	// PNG magic: 89 50 4E 47 0D 0A 1A 0A
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13}
	if reason := rejectActiveContent("image/png", png); reason != "" {
		t.Fatalf("legitimate PNG rejected with reason %q", reason)
	}
}

func TestRejectActiveContent_AllowsJPEG(t *testing.T) {
	// JPEG magic: FF D8 FF
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F'}
	if reason := rejectActiveContent("image/jpeg", jpeg); reason != "" {
		t.Fatalf("legitimate JPEG rejected with reason %q", reason)
	}
}

func TestRejectActiveContent_AllowsPDF(t *testing.T) {
	pdf := []byte("%PDF-1.5\n%\xE2\xE3\xCF\xD3\n")
	if reason := rejectActiveContent("application/pdf", pdf); reason != "" {
		t.Fatalf("legitimate PDF rejected with reason %q", reason)
	}
}

func TestRejectActiveContent_AllowsPlainText(t *testing.T) {
	if reason := rejectActiveContent("text/plain", []byte("hello world")); reason != "" {
		t.Fatalf("plain text rejected with reason %q", reason)
	}
}

func TestSafeServePolicy_InlineForImages(t *testing.T) {
	for _, ct := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf"} {
		served, disp, csp := safeServePolicy(ct)
		if served != ct {
			t.Errorf("%s: served = %q, want %q", ct, served, ct)
		}
		if disp != "inline" {
			t.Errorf("%s: disposition = %q, want inline", ct, disp)
		}
		if !strings.Contains(csp, "default-src 'none'") {
			t.Errorf("%s: csp missing default-src 'none': %q", ct, csp)
		}
	}
}

func TestSafeServePolicy_ForcesDownloadForSVG(t *testing.T) {
	served, disp, _ := safeServePolicy("image/svg+xml")
	if served != "application/octet-stream" {
		t.Errorf("SVG served as %q, want application/octet-stream", served)
	}
	if disp != "attachment" {
		t.Errorf("SVG disposition = %q, want attachment", disp)
	}
}

func TestSafeServePolicy_ForcesDownloadForHTML(t *testing.T) {
	served, disp, _ := safeServePolicy("text/html")
	if served != "application/octet-stream" {
		t.Errorf("HTML served as %q, want application/octet-stream", served)
	}
	if disp != "attachment" {
		t.Errorf("HTML disposition = %q, want attachment", disp)
	}
}

func TestSafeServePolicy_NormalizesContentTypeWithParams(t *testing.T) {
	served, disp, _ := safeServePolicy("image/png; charset=binary")
	if served != "image/png" {
		t.Errorf("served = %q, want image/png (params should be stripped)", served)
	}
	if disp != "inline" {
		t.Errorf("disposition = %q, want inline", disp)
	}
}

func TestSafeServePolicy_UnknownTypeForcesAttachment(t *testing.T) {
	served, disp, csp := safeServePolicy("application/zip")
	if served != "application/octet-stream" {
		t.Errorf("zip served as %q, want application/octet-stream", served)
	}
	if disp != "attachment" {
		t.Errorf("zip disposition = %q, want attachment", disp)
	}
	if !strings.Contains(csp, "sandbox") {
		t.Errorf("csp missing sandbox: %q", csp)
	}
}

func TestNormalizeContentType(t *testing.T) {
	cases := map[string]string{
		"":                                   "",
		"text/html":                          "text/html",
		"Text/HTML":                          "text/html",
		"text/html; charset=utf-8":           "text/html",
		"  application/PDF  ;  foo=bar  ":    "application/pdf",
		"image/svg+xml; charset=us-ascii":    "image/svg+xml",
	}
	for in, want := range cases {
		if got := normalizeContentType(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
