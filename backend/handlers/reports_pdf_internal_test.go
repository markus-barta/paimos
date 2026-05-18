// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Regression: fpdf's character-width table has 65536 entries, so any rune outside
// the Basic Multilingual Plane (emojis, etc.) used to crash SplitText/MultiCell
// with "index out of range" and surface as HTTP 500 on the PDF download.
// See: reports_pdf.go stripNonBMP + smartTruncate.
func TestLieferberichtPDF_NonBMPRunesDoNotPanic(t *testing.T) {
	lp := 1.0
	report := &lbReport{
		ProjectKey:  "BON26",
		ProjectName: "Bonelio",
		Groups: []lbGroup{{
			EpicKey:   "BON26-1",
			EpicTitle: "Epic with rocket \U0001F680",
			Issues: []lbIssue{{
				IssueKey:    "BON26-2",
				Type:        "ticket",
				Title:       "Rotating light \U0001F6A8 and clock \U0001F552 in title",
				Description: "Description with \U0001F6A8 \U0001F552 \U0001F389 \U0001F4A1 emojis interleaved with regular text.",
				Status:      "in-progress",
				EstimateLp:  &lp,
			}},
		}},
	}
	pdf := renderLieferberichtPDF(report)
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("output: %v", err)
	}
	if buf.Len() < 1000 {
		t.Fatalf("suspiciously small PDF: %d bytes", buf.Len())
	}
}

// PAI-399: branding logo is read from $DATA_DIR/branding.json + branding-assets/.
// Test verifies the resolver picks up an SVG upload, rasterizes it, and the
// resulting PDF embeds the rendered bytes without panicking.
func TestResolveBrandingLogoForPDF_SVG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)

	cfg := `{"logo":"/brand/logo.svg"}`
	if err := os.WriteFile(filepath.Join(dir, "branding.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write branding.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "branding-assets"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="#1f4d75"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, "branding-assets", "logo.svg"), []byte(svg), 0o644); err != nil {
		t.Fatalf("write svg: %v", err)
	}

	data, imgType := resolveBrandingLogoForPDF()
	if imgType != "PNG" {
		t.Fatalf("expected SVG to rasterize to PNG, got %s", imgType)
	}
	// PNG magic is "\x89PNG\r\n\x1a\n"
	if len(data) < 8 || string(data[1:4]) != "PNG" {
		t.Fatalf("expected PNG bytes, got %q...", data[:min(8, len(data))])
	}

	// End-to-end: render a minimal report with the SVG-derived logo bytes.
	report := &lbReport{ProjectKey: "X", Groups: []lbGroup{{EpicKey: "E", Issues: []lbIssue{{IssueKey: "X-1", Title: "t"}}}}}
	pdf := renderLieferberichtPDF(report)
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("pdf output: %v", err)
	}
}

// Defense: a missing branding config falls back to the embedded logo so PDF
// rendering keeps working out of the box.
func TestResolveBrandingLogoForPDF_FallbackOnMissing(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir()) // no branding.json inside

	data, imgType := resolveBrandingLogoForPDF()
	if imgType != "PNG" {
		t.Fatalf("expected fallback PNG, got %s", imgType)
	}
	if !bytes.Equal(data, logoPNG) {
		t.Fatalf("expected embedded fallback bytes")
	}
}
