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
	pdf := renderLieferberichtPDF(report, lbRenderOpts{Lang: "en", Cols: defaultLBColSet()})
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
	pdf := renderLieferberichtPDF(report, lbRenderOpts{Lang: "en", Cols: defaultLBColSet()})
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("pdf output: %v", err)
	}
}

// PAI-402: lang=de renders the German message catalog (status labels +
// header title + footer); unknown lang falls back to English.
func TestLieferberichtPDF_LocaleSwitch(t *testing.T) {
	report := &lbReport{
		ProjectKey: "X",
		Groups: []lbGroup{{
			EpicKey: "E",
			Issues:  []lbIssue{{IssueKey: "X-1", Title: "t", Status: "delivered"}},
		}},
	}
	for _, lang := range []string{"en", "de", "fr"} {
		var buf bytes.Buffer
		if err := renderLieferberichtPDF(report, lbRenderOpts{Lang: lang, Cols: defaultLBColSet()}).Output(&buf); err != nil {
			t.Fatalf("lang=%s output: %v", lang, err)
		}
		if buf.Len() < 1000 {
			t.Fatalf("lang=%s suspiciously small PDF: %d bytes", lang, buf.Len())
		}
	}
	// Spot-check the catalog has the expected entries; the PDF bytestream is
	// compressed so we can't grep it for strings.
	if resolveLBLang("de").StatusDelivered != "Geliefert" {
		t.Fatalf("de StatusDelivered = %q", resolveLBLang("de").StatusDelivered)
	}
	if resolveLBLang("en").StatusDelivered != "Delivered" {
		t.Fatalf("en StatusDelivered = %q", resolveLBLang("en").StatusDelivered)
	}
	if resolveLBLang("fr").HeaderTitle != "Delivery report" {
		t.Fatalf("fr should fall back to en HeaderTitle")
	}
}

// PAI-400 + PAI-401: an explicit empty `cols=` query value resolves to the
// zero lbColSet, not the default-all-on. The handler distinguishes "absent"
// (back-compat → all visible) from "present but empty" (no numeric cols).
func TestParseLBColSet_EmptyIsZeroSet(t *testing.T) {
	got := parseLBColSet("")
	if got.AnyVisible() {
		t.Fatalf("expected empty input to yield zero set; got %+v", got)
	}
	if got := parseLBColSet("sp,ar_eur"); !(got.SP && got.AREUR && !got.H && !got.ARSP && !got.ARH) {
		t.Fatalf("parseLBColSet(\"sp,ar_eur\") = %+v", got)
	}
}

// PAI-400 + PAI-401: empty column set must still render (no panic) and the
// subtotal/grand-total rows fall back to the "{N} issues" presentation when
// no numeric columns are visible.
func TestLieferberichtPDF_NoNumericColumnsRenders(t *testing.T) {
	report := &lbReport{
		ProjectKey: "X",
		Groups: []lbGroup{
			{EpicKey: "E1", Issues: []lbIssue{{IssueKey: "X-1", Title: "a"}, {IssueKey: "X-2", Title: "b"}}},
			{EpicKey: "E2", Issues: []lbIssue{{IssueKey: "X-3", Title: "c"}}},
		},
	}
	for _, tc := range []struct{ name string; cols lbColSet }{
		{"all-hidden", lbColSet{}},
		{"only-eur", lbColSet{AREUR: true}},
		{"all-visible", defaultLBColSet()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderLieferberichtPDF(report, lbRenderOpts{Lang: "en", Cols: tc.cols}).Output(&buf); err != nil {
				t.Fatalf("output: %v", err)
			}
			if buf.Len() < 1000 {
				t.Fatalf("suspiciously small PDF: %d bytes", buf.Len())
			}
		})
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
