// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"

	"github.com/markus-barta/paimos/backend/models"
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

func TestResolveBrandingLogoBasicSVGForPDF_PathOnlyStroke(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "branding.json"), []byte(`{"logo":"/brand/logo.svg"}`), 0o644); err != nil {
		t.Fatalf("write branding.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "branding-assets"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" fill="none" stroke="currentColor"><path d="M4 12L10 18L20 6"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, "branding-assets", "logo.svg"), []byte(svg), 0o644); err != nil {
		t.Fatalf("write svg: %v", err)
	}
	if sig, ok := resolveBrandingLogoBasicSVGForPDF(); !ok || sig.Wd != 24 || sig.Ht != 24 {
		t.Fatalf("expected direct basic SVG logo, got ok=%v sig=%+v", ok, sig)
	}
}

func TestDrawLBTypeSVGIcons(t *testing.T) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	palette := lbPDFPalette{
		TypeTicket: mustRGB("#1f4d75"),
		TypeTask:   mustRGB("#2e7d32"),
	}
	drawLBTypeSVGIcon(pdf, "ticket", palette, 10, 10, 4)
	drawLBTypeSVGIcon(pdf, "task", palette, 16, 10, 4)
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("pdf output: %v", err)
	}
	if buf.Len() < 800 {
		t.Fatalf("suspiciously small PDF: %d bytes", buf.Len())
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
	if resolveLBLang("fr").HeaderTitle != "Project report" {
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

// PAI-405: IssueList export passes active chip negation through as !value.
// The Lieferbericht endpoint must preserve those as exclusion filters instead
// of treating them as literal status/tag values.
func TestParseLBFilters_SignedFilters(t *testing.T) {
	r := httptest.NewRequest("GET", "/?tag_ids=12,!34,bad,!0,!&statuses=qa,!done,%20!delivered,%20!", nil)
	got := parseLBFilters(r)

	if !reflect.DeepEqual(got.TagIDs, []int64{12}) {
		t.Fatalf("TagIDs = %+v, want [12]", got.TagIDs)
	}
	if !reflect.DeepEqual(got.ExcludeTagIDs, []int64{34}) {
		t.Fatalf("ExcludeTagIDs = %+v, want [34]", got.ExcludeTagIDs)
	}
	if !reflect.DeepEqual(got.Statuses, []string{"qa"}) {
		t.Fatalf("Statuses = %+v, want [qa]", got.Statuses)
	}
	if !reflect.DeepEqual(got.ExcludeStatuses, []string{"done", "delivered"}) {
		t.Fatalf("ExcludeStatuses = %+v, want [done delivered]", got.ExcludeStatuses)
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
	for _, tc := range []struct {
		name string
		cols lbColSet
	}{
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

// BPOPS26-108: long reports must not produce runaway blank pages from fpdf's
// automatic page breaker fighting the renderer's manual row/page accounting.
func TestLieferberichtPDF_LongReportPageCountStaysBounded(t *testing.T) {
	report := &lbReport{
		ProjectKey: "X",
		Groups: []lbGroup{{
			EpicKey:   "E",
			EpicTitle: "Long delivered export",
		}},
	}
	desc := strings.Repeat("A delivered issue with enough prose to wrap in the description column. ", 4)
	for i := 1; i <= 140; i++ {
		report.Groups[0].Issues = append(report.Groups[0].Issues, lbIssue{
			IssueKey:    "X-" + strconv.Itoa(i),
			Type:        "ticket",
			Title:       "Delivered issue " + strconv.Itoa(i),
			Description: desc,
			Status:      "delivered",
		})
	}

	pdf := renderLieferberichtPDF(report, lbRenderOpts{Lang: "de", Cols: defaultLBColSet()})
	if pages := pdf.PageNo(); pages > 20 {
		t.Fatalf("long report rendered too many pages, likely blank-page churn: %d", pages)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("output: %v", err)
	}
}

// PAI-406: corrupt or misclassified raster bytes on disk must not 500 the
// PDF. The resolver sniffs the magic bytes and falls back to the embedded
// logo when they don't match a format we can hand to fpdf.
func TestResolveBrandingLogoForPDF_CorruptBytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		bytes    []byte
	}{
		{"png ext but garbage bytes", "logo.png", []byte("not a real PNG, just text")},
		{"svg ext but truncated", "logo.svg", []byte{0x00, 0x00, 0xff}},
		{"jpg ext but tiny", "logo.jpg", []byte{0x00}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("DATA_DIR", dir)
			cfg := `{"logo":"/brand/` + tc.filename + `"}`
			if err := os.WriteFile(filepath.Join(dir, "branding.json"), []byte(cfg), 0o644); err != nil {
				t.Fatalf("write cfg: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(dir, "branding-assets"), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "branding-assets", tc.filename), tc.bytes, 0o644); err != nil {
				t.Fatalf("write asset: %v", err)
			}
			data, imgType := resolveBrandingLogoForPDF()
			if imgType != "PNG" || !bytes.Equal(data, logoPNG) {
				t.Fatalf("expected embedded fallback for corrupt %s; got imgType=%s len(data)=%d", tc.name, imgType, len(data))
			}
		})
	}
}

func TestSniffImageFormat(t *testing.T) {
	cases := map[string][]byte{
		"png": {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00},
		"jpg": {0xff, 0xd8, 0xff, 0xe0, 0x00},
		"ico": {0x00, 0x00, 0x01, 0x00, 0x01},
		"svg": []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`),
		"":    []byte("not a real image"),
	}
	for want, in := range cases {
		if got := sniffImageFormat(in); got != want {
			t.Errorf("sniffImageFormat(%q…) = %q, want %q", in[:min(len(in), 8)], got, want)
		}
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

func TestProjektberichtCustomerPartyHelpersIncludePostalAndLegalDetails(t *testing.T) {
	customer := &models.Customer{
		Name:                  "ACME GmbH",
		BillingAddressStreet:  "Hauptplatz 1",
		BillingAddressZip:     "8010",
		BillingAddressCity:    "Graz",
		BillingAddressCountry: "Austria",
		TaxID:                 "ATU12345678",
		CompanyRegisterNumber: "FN 123456x",
		ContactName:           "Jane Doe",
		ContactEmail:          "jane@example.com",
	}

	lines := []string{customer.Name}
	lines = append(lines, projectReportCustomerAddressLines(customer)...)
	if taxID := firstNonEmpty(customer.TaxID, customer.VATID); taxID != "" {
		lines = append(lines, "UID: "+taxID)
	}
	if fn := strings.TrimSpace(customer.CompanyRegisterNumber); fn != "" {
		lines = append(lines, "FN: "+fn)
	}
	if contact := projectReportCustomerContact(customer); contact != "" {
		lines = append(lines, "Kontakt: "+contact)
	}

	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"ACME GmbH",
		"Hauptplatz 1",
		"8010, Graz",
		"Austria",
		"UID: ATU12345678",
		"FN: FN 123456x",
		"Kontakt: Jane Doe <jane@example.com>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("customer party lines missing %q:\n%s", want, got)
		}
	}
	if projektberichtPaperSignatureTopPaddingMM < 40 {
		t.Fatalf("paper signature top padding=%vmm, want at least 40mm", projektberichtPaperSignatureTopPaddingMM)
	}
}

// PAI-557 regression: a customer whose postal address lives only in the
// free-form Address field (no structured billing/visit address, country set)
// must still have its full address printed. The previous fallback returned the
// bare country and never reached the free-form Address.
func TestProjektberichtCustomerAddressLines_FreeFormFallback(t *testing.T) {
	customer := &models.Customer{
		Name:    "AVL List GmbH",
		Address: "Hans-List-Platz 1, 8020 Graz, Austria",
		Country: "Austria",
	}

	lines := projectReportCustomerAddressLines(customer)
	got := strings.Join(lines, "\n")

	if !strings.Contains(got, "Hans-List-Platz 1, 8020 Graz, Austria") {
		t.Fatalf("free-form address not printed; got lines:\n%s", got)
	}
	// Country is already part of the free-form text — it must not be duplicated.
	if strings.Count(got, "Austria") != 1 {
		t.Fatalf("country duplicated; got lines:\n%s", got)
	}
}

// A bare country with no street/zip/city anywhere must not masquerade as a
// usable address block.
func TestProjektberichtCustomerAddressLines_CountryOnly(t *testing.T) {
	lines := projectReportCustomerAddressLines(&models.Customer{Name: "X", Country: "Austria"})
	if want := []string{"Austria"}; len(lines) != 1 || lines[0] != want[0] {
		t.Fatalf("country-only fallback = %v, want %v", lines, want)
	}
}
