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
	_ "embed"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"
	"github.com/markus-barta/paimos/backend/models"
)

//go:embed assets/logo.png
var logoPNG []byte

//go:embed assets/DejaVuSans.ttf
var dejaVuSansTTF []byte

//go:embed assets/DejaVuSans-Bold.ttf
var dejaVuSansBoldTTF []byte

// GET /api/projects/{id}/reports/lieferbericht/pdf
func GetLieferberichtPDF(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}

	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "all_open"
	}
	sprintIDs := r.URL.Query().Get("sprint_ids")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")
	lang := r.URL.Query().Get("lang")

	report, err := buildLieferbericht(projectID, scope, sprintIDs, fromDate, toDate, parseLBFilters(r))
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	colSet := requestLBColSet(r)
	var reportCode string
	var acceptURL string
	if r.URL.Query().Get("snapshot") == "1" {
		reportCode = randHex(5)
		acceptURL = acceptanceURLForCode(r, reportCode)
	}
	// PAI-418 / PAI-425. text_source ∈ {"tech", "report"}; unknown values
	// fall back to "tech" so a typo or a stale client never silently
	// switches to the customer-facing variant.
	textSource := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("text_source")))
	if textSource != "report" {
		textSource = "tech"
	}
	pdf := renderLieferberichtPDF(report, lbRenderOpts{Lang: lang, Cols: colSet, BaseURL: reportRequestBaseURL(r), ReportCode: reportCode, AcceptanceURL: acceptURL, TextSource: textSource})

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		jsonError(w, "pdf generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if reportCode != "" {
		if err := createProjectReportSnapshot(r, report, lang, r.URL.RawQuery, reportCode, buf.Bytes()); err != nil {
			jsonError(w, "report snapshot failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	filename := fmt.Sprintf("PB-%s %s.pdf", report.ProjectKey, time.Now().Format("2006-01-02"))
	writePDFBytesResponse(w, buf.Bytes(), filename)
}

func writePDFResponse(w http.ResponseWriter, pdf *fpdf.Fpdf, filename string) {
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		jsonError(w, "pdf generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writePDFBytesResponse(w, buf.Bytes(), filename)
}

func writePDFBytesResponse(w http.ResponseWriter, body []byte, filename string) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
}

// fmtDE formats a float with German decimal separator (comma).
func fmtDE(v float64) string {
	if v == 0 {
		return ""
	}
	if v == float64(int(v)) {
		return strconv.Itoa(int(v))
	}
	return strings.Replace(strconv.FormatFloat(v, 'f', 2, 64), ".", ",", 1)
}

func fmtOptDE(v *float64) string {
	if v == nil {
		return ""
	}
	return fmtDE(*v)
}

// truncRunes safely truncates a string by rune count, not byte count.
func truncRunes(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "..."
}

// stripNonBMP replaces runes outside the Basic Multilingual Plane (codepoint > 0xFFFF)
// with '?'. fpdf's character-width table has 65536 entries (splittext.go:31, MultiCell),
// so emojis and other supplementary-plane runes cause runtime index-out-of-range panics.
func stripNonBMP(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r > 0xFFFF }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r > 0xFFFF {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// smartTruncate truncates text at a word boundary, appending ellipsis.
func smartTruncate(s string, maxRunes int) string {
	s = stripNonBMP(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	truncated := string(runes[:maxRunes])
	// find last space to avoid mid-word cut
	if idx := strings.LastIndex(truncated, " "); idx > maxRunes/2 {
		truncated = truncated[:idx]
	}
	return truncated + "…"
}

// descriptionText returns empty if description is redundant with summary.
func descriptionText(desc, summary string) string {
	d := strings.TrimSpace(desc)
	s := strings.TrimSpace(summary)
	if d == "" || d == s || strings.HasPrefix(d, s) {
		return ""
	}
	return d
}

// bodyTextForRow picks which text variant the Projektbericht row body
// cell should render, based on the export's text_source choice
// (PAI-418 / PAI-425). For text_source="report" the customer-facing
// report_summary takes precedence; when an issue has no summary yet
// we fall back to the technical description with a visible
// "[keine Kundenfassung]" prefix so the reader sees the gap instead
// of being silently served the technical body.
//
// PAI-432. The fallback prefix is ALWAYS rendered for missing
// summaries — even when the description is empty too. Otherwise a
// ticket with no summary and an empty body would silently render
// nothing, defeating the "visible gap" contract.
func bodyTextForRow(issue lbIssue, textSource string) string {
	if textSource == "report" {
		if s := strings.TrimSpace(issue.ReportSummary); s != "" {
			return s
		}
		fallback := descriptionText(issue.Description, issue.Title)
		if fallback == "" {
			return "[keine Kundenfassung]"
		}
		return "[keine Kundenfassung] " + fallback
	}
	return descriptionText(issue.Description, issue.Title)
}

func reportRequestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xf := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); xf == "http" || xf == "https" {
		scheme = xf
	}
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

type rgbColor struct{ r, g, b int }

type lbPDFPalette struct {
	Primary        rgbColor
	PrimaryDark    rgbColor
	PrimaryPale    rgbColor
	TableRowBorder rgbColor
	TableRowAlt    rgbColor
	TypeTicket     rgbColor
	TypeTask       rgbColor
}

func mustRGB(hex string) rgbColor {
	c, ok := parseHexRGB(hex)
	if !ok {
		return rgbColor{}
	}
	return c
}

func parseHexRGB(hex string) (rgbColor, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return rgbColor{}, false
	}
	n, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return rgbColor{}, false
	}
	return rgbColor{r: int(n >> 16 & 0xff), g: int(n >> 8 & 0xff), b: int(n & 0xff)}, true
}

func contrastTextColor(bg rgbColor) rgbColor {
	// YIQ is sufficient for black/white table-header contrast in print/PDF.
	if (bg.r*299+bg.g*587+bg.b*114)/1000 >= 140 {
		return rgbColor{r: 0, g: 0, b: 0}
	}
	return rgbColor{r: 255, g: 255, b: 255}
}

func setTextRGB(pdf *fpdf.Fpdf, c rgbColor) { pdf.SetTextColor(c.r, c.g, c.b) }
func setDrawRGB(pdf *fpdf.Fpdf, c rgbColor) { pdf.SetDrawColor(c.r, c.g, c.b) }
func setFillRGB(pdf *fpdf.Fpdf, c rgbColor) { pdf.SetFillColor(c.r, c.g, c.b) }

func resolveLBPDFPalette() lbPDFPalette {
	colors := map[string]string{
		"primary":        "#2e6da4",
		"primaryDark":    "#1f4d75",
		"primaryPale":    "#dce9f4",
		"tableRowBorder": "#e8eaed",
		"tableRowAlt":    "#f8f9fa",
		"typeTicket":     "#1f4d75",
		"typeTask":       "#2e7d32",
	}
	if data, err := os.ReadFile(filepath.Join(brandingDir(), "branding.json")); err == nil {
		var parsed struct {
			Colors map[string]string `json:"colors"`
		}
		if json.Unmarshal(data, &parsed) == nil {
			for k, v := range parsed.Colors {
				if _, ok := colors[k]; ok {
					if _, valid := parseHexRGB(v); valid {
						colors[k] = v
					}
				}
			}
		}
	}
	return lbPDFPalette{
		Primary:        mustRGB(colors["primary"]),
		PrimaryDark:    mustRGB(colors["primaryDark"]),
		PrimaryPale:    mustRGB(colors["primaryPale"]),
		TableRowBorder: mustRGB(colors["tableRowBorder"]),
		TableRowAlt:    mustRGB(colors["tableRowAlt"]),
		TypeTicket:     mustRGB(colors["typeTicket"]),
		TypeTask:       mustRGB(colors["typeTask"]),
	}
}

func renderLieferberichtPDF(report *lbReport, opts lbRenderOpts) *fpdf.Fpdf {
	lang := opts.Lang
	msgs := resolveLBLang(lang)
	visible := opts.Cols
	anyNumeric := visible.AnyVisible()
	palette := resolveLBPDFPalette()
	pdf := fpdf.New("L", "mm", "A4", "")
	// The table renderer performs its own page-break checks. Leaving fpdf's
	// automatic page breaks enabled lets MultiCell/CellFormat add pages after
	// borders/backgrounds have already been painted, which can leave blank
	// header/footer-only pages in long reports.
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetMargins(10, 10, 10)

	// Register UTF-8 fonts for umlaut support
	pdf.AddUTF8FontFromBytes("DejaVu", "", dejaVuSansTTF)
	pdf.AddUTF8FontFromBytes("DejaVu", "B", dejaVuSansBoldTTF)

	// Register logo — prefers the active instance branding (PAI-399), with
	// magic-byte sniffing inside the resolver to reject corrupt assets
	// (PAI-406). Belt-and-suspenders: if fpdf still fails to register the
	// bytes (e.g. valid magic but truncated payload), drop the stale error
	// and re-register the embedded fallback so the PDF never 500s.
	logoSVG, useLogoSVG := resolveBrandingLogoBasicSVGForPDF()
	if !useLogoSVG {
		logoBytes, logoImgType := resolveBrandingLogoForPDF()
		pdf.RegisterImageOptionsReader("logo", fpdf.ImageOptions{ImageType: logoImgType}, bytes.NewReader(logoBytes))
		if pdf.Error() != nil {
			pdf.ClearError()
			pdf.RegisterImageOptionsReader("logo", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(logoPNG))
		}
	}

	// A4 landscape = 297mm wide. Margins: 10mm each side → usable 277mm.
	const pageW = 297.0
	const marginL = 10.0
	const marginR = 10.0
	const usableW = pageW - marginL - marginR
	const lineH = 3.0 // line height for MultiCell rows

	// Build report key for header (e.g. "PB-ASC2501-02").
	reportKey := fmt.Sprintf("PB-%s", report.ProjectKey)

	// Header — logo left, title centered horizontally, date right.
	// All three pieces share the same vertical mid-line so the eye
	// reads them as a single row instead of staggered baselines.
	//
	// Layout math (mm, A4 landscape, pageW=297, marginL=10):
	//   logo cap        h=4.8  at y=4.2  → midpoint 6.6
	//   title cell      h=4    at y=4.6  → midpoint 6.6
	//   date cell       h=4    at y=4.6  → midpoint 6.6
	//   title cell width = 120; centered on the page:
	//     x = (pageW - 120) / 2 = 88.5
	//     align="C" so the text itself is centered in that cell
	pdf.SetHeaderFuncMode(func() {
		const logoH = 4.8
		const logoY = 4.2
		const textY = 4.6
		const textH = 4.0
		const titleW = 120.0
		const dateW = 60.0
		if useLogoSVG {
			setDrawRGB(pdf, palette.Primary)
			pdf.SetLineWidth(0.25)
			pdf.SetLineCapStyle("round")
			pdf.SetLineJoinStyle("round")
			pdf.SetXY(marginL, logoY)
			pdf.SVGBasicWrite(logoSVG, logoH/logoSVG.Ht)
			pdf.SetLineCapStyle("butt")
			pdf.SetLineJoinStyle("miter")
		} else {
			pdf.ImageOptions("logo", marginL, logoY, 0, logoH, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		}
		pdf.SetFont("DejaVu", "", 8)
		pdf.SetTextColor(0, 0, 0)
		pdf.SetXY((pageW-titleW)/2, textY)
		pdf.CellFormat(titleW, textH, fmt.Sprintf("%s %s", msgs.HeaderTitle, reportKey), "", 0, "C", false, 0, "")
		pdf.SetFont("DejaVu", "", 7)
		pdf.SetTextColor(80, 80, 80)
		pdf.SetXY(pageW-marginR-dateW, textY)
		pdf.CellFormat(dateW, textH, formatLBTimestamp(time.Now(), lang), "", 0, "R", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetY(16)
	}, true)

	// Footer — localized page number ("Seite N/M" / "Page N of M").
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont("DejaVu", "", 6)
		pdf.SetTextColor(150, 150, 150)
		// fpdf substitutes {nb} (kept literal in the format string) with the
		// total page count at output time.
		pdf.CellFormat(0, 4, fmt.Sprintf(msgs.PageNOfM, pdf.PageNo()), "", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})
	pdf.AliasNbPages("")

	pdf.AddPage()

	// Column definitions — matches reference layout
	type col struct {
		header string
		w      float64
		align  string
	}
	cols := []col{
		{msgs.ColKey, 28, "L"},         // 0 — type icon + linked issue key
		{msgs.ColSummary, 68, "L"},     // 1 — multiline
		{msgs.ColDescription, 80, "L"}, // 2 — multiline
		{msgs.ColStatus, 18, "L"},      // 3
		{msgs.ColSP, 10, "R"},          // 4 — visible.SP
		{msgs.ColHours, 10, "R"},       // 5 — visible.H
		{msgs.ColARSP, 14, "R"},        // 6 — visible.ARSP
		{msgs.ColARHours, 14, "R"},     // 7 — visible.ARH
		{msgs.ColAREUR, 18, "R"},       // 8 — visible.AREUR
	}

	// PAI-400: hidden numeric columns release their width to the Description
	// column (index 3). Any remaining printable width is assigned there too so
	// the table reaches the landscape page's right margin.
	numericVisible := [5]bool{visible.SP, visible.H, visible.ARSP, visible.ARH, visible.AREUR}
	for i, vis := range numericVisible {
		idx := 4 + i
		if !vis {
			cols[2].w += cols[idx].w
			cols[idx].w = 0
		}
	}
	totalW := 0.0
	for _, c := range cols {
		totalW += c.w
	}
	if extra := usableW - totalW; extra > 0 {
		cols[2].w += extra
		totalW += extra
	}

	// PAI-403: in count-only subtotal/grand-total rows we paint a fixed
	// 38mm "{N} Tickets" cell at the right edge of the row over the filled
	// background. The label sits left of it. Both fit inside the full-width
	// row Rect so no overdraw.
	const countCellW = 38.0

	const tblHeaderH = 6.0
	const minRowH = 5.0

	headerTextColor := contrastTextColor(palette.Primary)

	// Set thin grid line style
	setGridStyle := func() {
		setDrawRGB(pdf, palette.TableRowBorder)
		pdf.SetLineWidth(0.2)
	}

	// Draw full grid rect for a cell
	drawCellBorder := func(x, y, w, h float64) {
		setGridStyle()
		pdf.Rect(x, y, w, h, "D")
	}

	// Table header — blue background, white text
	drawHeader := func() {
		pdf.SetFont("DejaVu", "B", 6.5)
		setFillRGB(pdf, palette.Primary)
		setTextRGB(pdf, headerTextColor)
		y := pdf.GetY()
		x := marginL
		for _, c := range cols {
			// PAI-403: skip hidden columns. fpdf treats CellFormat(w==0, …) as
			// "fill to right margin" — drawing the header would ghost-extend
			// across the page.
			if c.w <= 0 {
				continue
			}
			pdf.SetXY(x, y)
			pdf.CellFormat(c.w, tblHeaderH, " "+c.header, "", 0, "L", true, 0, "")
			x += c.w
		}
		pdf.SetXY(marginL, y+tblHeaderH)
		setTextRGB(pdf, rgbColor{})
	}

	ensureSpace := func(h float64) {
		_, pageH := pdf.GetPageSize()
		if pdf.GetY()+h > pageH-12 {
			pdf.AddPage()
			drawHeader()
		}
	}

	drawProjektberichtIntro(pdf, report, palette, marginL, usableW)
	drawHeader()

	statusLabel := func(status string) string {
		switch status {
		case "done", "delivered", "accepted", "invoiced":
			return msgs.StatusDelivered
		case "in-progress", "qa":
			return msgs.StatusInProgress
		default:
			return msgs.StatusPlanned
		}
	}

	issueURL := func(issue lbIssue) string {
		if issue.ID <= 0 || opts.BaseURL == "" {
			return ""
		}
		return strings.TrimRight(opts.BaseURL, "/") + fmt.Sprintf("/projects/%d/issues/%d", report.ProjectID, issue.ID)
	}

	// Pre-calculate multiline row height using SplitText
	calcRowH := func(summary, desc string) float64 {
		pdf.SetFont("DejaVu", "", 6.5)
		summaryLines := pdf.SplitText(summary, cols[1].w-2)
		descLines := pdf.SplitText(desc, cols[2].w-2)
		nLines := len(summaryLines)
		if len(descLines) > nLines {
			nLines = len(descLines)
		}
		if nLines < 1 {
			nLines = 1
		}
		h := float64(nLines)*lineH + 1.6
		if h < minRowH {
			return minRowH
		}
		return h
	}

	rowIdx := 0

	for _, g := range report.Groups {
		// Page break check for epic header + at least 2 rows
		ensureSpace(35)

		// Epic group header row — light background, bold, spans full width
		epicY := pdf.GetY()
		pdf.SetFont("DejaVu", "B", 6.5)
		setFillRGB(pdf, palette.PrimaryPale)
		setTextRGB(pdf, palette.PrimaryDark)
		epicLabel := g.EpicKey
		if g.EpicTitle != "" && g.EpicTitle != g.EpicKey {
			epicLabel += " — " + g.EpicTitle
		}
		epicH := 5.0
		pdf.Rect(marginL, epicY, totalW, epicH, "F")
		setGridStyle()
		pdf.Rect(marginL, epicY, totalW, epicH, "D")
		pdf.SetXY(marginL+1, epicY+0.5)
		pdf.CellFormat(totalW-2, epicH-1, smartTruncate(epicLabel, 160), "", 0, "L", false, 0, "")
		pdf.SetXY(marginL, epicY+epicH)
		setTextRGB(pdf, rgbColor{})

		pdf.SetFont("DejaVu", "", 6.5)

		for _, issue := range g.Issues {
			summary := smartTruncate(issue.Title, 200)
			desc := bodyTextForRow(issue, opts.TextSource)
			if desc != "" {
				desc = smartTruncate(desc, 200)
			}

			rh := calcRowH(summary, desc)

			// Page break check
			ensureSpace(rh)

			rowY := pdf.GetY()

			// Alternating row shading
			if rowIdx%2 == 1 {
				setFillRGB(pdf, palette.TableRowAlt)
				pdf.Rect(marginL, rowY, totalW, rh, "F")
			}

			pdf.SetFont("DejaVu", "", 6.5)
			x := marginL

			// Col 0: Type icon + linked issue key
			drawCellBorder(x, rowY, cols[0].w, rh)
			link := issueURL(issue)
			if link != "" {
				pdf.LinkString(x+0.5, rowY, cols[0].w-1, rh, link)
			}
			const iconSize = 3.0
			const iconGap = 1.2
			iconX := x + 1.0
			iconY := rowY + (rh-iconSize)/2
			drawLBTypeSVGIcon(pdf, issue.Type, palette, iconX, iconY, iconSize)
			setGridStyle()
			keyX := iconX + iconSize + iconGap
			keyY := rowY + (rh-lineH)/2
			setTextRGB(pdf, palette.Primary)
			pdf.SetXY(keyX, keyY)
			pdf.CellFormat(cols[0].w-(keyX-x)-0.8, lineH, issue.IssueKey, "", 0, "L", false, 0, link)
			setTextRGB(pdf, rgbColor{})
			x += cols[0].w

			// Col 1: Summary — multiline
			drawCellBorder(x, rowY, cols[1].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.MultiCell(cols[1].w-1.5, lineH, summary, "", "L", false)
			x += cols[1].w

			// Col 2: Description — multiline
			drawCellBorder(x, rowY, cols[2].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.MultiCell(cols[2].w-1.5, lineH, desc, "", "L", false)
			x += cols[2].w

			// Col 3: Status
			drawCellBorder(x, rowY, cols[3].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.CellFormat(cols[3].w-1, lineH, statusLabel(issue.Status), "", 0, "L", false, 0, "")
			x += cols[3].w

			// Numeric columns (PAI-400) — drawn only when visible.
			drawNumeric := func(idx int, text string) {
				if cols[idx].w <= 0 {
					return
				}
				drawCellBorder(x, rowY, cols[idx].w, rh)
				pdf.SetXY(x, rowY+0.8)
				pdf.CellFormat(cols[idx].w-0.5, lineH, text, "", 0, "R", false, 0, "")
				x += cols[idx].w
			}
			drawNumeric(4, fmtOptDE(issue.EstimateLp))
			drawNumeric(5, fmtOptDE(issue.EstimateHours))
			drawNumeric(6, fmtOptDE(issue.ArLp))
			drawNumeric(7, fmtOptDE(issue.ArHours))
			drawNumeric(8, fmtDE(issue.ArEur))

			// Advance Y
			pdf.SetXY(marginL, rowY+rh)
			rowIdx++
		}

		// Subtotal row
		ensureSpace(5.0)
		subY := pdf.GetY()
		subH := 5.0
		pdf.SetFont("DejaVu", "B", 6.5)
		setFillRGB(pdf, palette.TableRowAlt)
		pdf.Rect(marginL, subY, totalW, subH, "F")
		setGridStyle()
		pdf.Rect(marginL, subY, totalW, subH, "D")

		if anyNumeric {
			subLabelW := cols[0].w + cols[1].w + cols[2].w + cols[3].w
			pdf.SetXY(marginL, subY+0.8)
			pdf.CellFormat(subLabelW-0.5, lineH, msgs.Subtotal, "", 0, "R", false, 0, "")
			x := marginL + subLabelW
			drawSubCell := func(idx int, text string) {
				if cols[idx].w <= 0 {
					return
				}
				pdf.SetXY(x, subY+0.8)
				pdf.CellFormat(cols[idx].w-0.5, lineH, text, "", 0, "R", false, 0, "")
				x += cols[idx].w
			}
			drawSubCell(4, fmtDE(g.Subtotal.EstimateLp))
			drawSubCell(5, fmtDE(g.Subtotal.EstimateHours))
			drawSubCell(6, fmtDE(g.Subtotal.ArLp))
			drawSubCell(7, fmtDE(g.Subtotal.ArHours))
			drawSubCell(8, fmtDE(g.Subtotal.ArEur))
		} else {
			// PAI-401 + PAI-403: count-only mode. The numeric cols are width 0
			// (their budget went to Description), so reusing sum(cols[0..4]) as
			// the label width leaves zero room for the count. Instead, carve a
			// fixed 38mm count cell off the right of totalW and let the label
			// take the rest. Both right-aligned within their cell so the
			// alignment matches the numeric-mode row above.
			labelW := totalW - countCellW
			pdf.SetXY(marginL, subY+0.8)
			pdf.CellFormat(labelW-0.5, lineH, msgs.Subtotal, "", 0, "R", false, 0, "")
			pdf.SetXY(marginL+labelW, subY+0.8)
			pdf.CellFormat(countCellW-0.5, lineH, lbIssueCountLabel(len(g.Issues), lang), "", 0, "R", false, 0, "")
		}
		pdf.SetXY(marginL, subY+subH)
		pdf.SetFont("DejaVu", "", 6.5)
	}

	// Grand total row
	ensureSpace(6.5)
	gtY := pdf.GetY() + 0.5
	gtH := 6.0
	pdf.SetFont("DejaVu", "B", 7)
	setFillRGB(pdf, palette.PrimaryPale)
	pdf.Rect(marginL, gtY, totalW, gtH, "F")
	setGridStyle()
	pdf.Rect(marginL, gtY, totalW, gtH, "D")

	if anyNumeric {
		subLabelW := cols[0].w + cols[1].w + cols[2].w + cols[3].w
		pdf.SetXY(marginL, gtY+1)
		pdf.CellFormat(subLabelW-0.5, lineH+0.5, msgs.GrandTotal, "", 0, "R", false, 0, "")
		x := marginL + subLabelW
		drawGtCell := func(idx int, text string) {
			if cols[idx].w <= 0 {
				return
			}
			pdf.SetXY(x, gtY+1)
			pdf.CellFormat(cols[idx].w-0.5, lineH+0.5, text, "", 0, "R", false, 0, "")
			x += cols[idx].w
		}
		drawGtCell(4, fmtDE(report.GrandTotal.EstimateLp))
		drawGtCell(5, fmtDE(report.GrandTotal.EstimateHours))
		drawGtCell(6, fmtDE(report.GrandTotal.ArLp))
		drawGtCell(7, fmtDE(report.GrandTotal.ArHours))
		drawGtCell(8, fmtDE(report.GrandTotal.ArEur))
	} else {
		// PAI-401 + PAI-403: count-only mode. See subtotal block above for
		// why we don't reuse sum(cols[0..4]) as the label width.
		var total int
		for _, g := range report.Groups {
			total += len(g.Issues)
		}
		labelW := totalW - countCellW
		pdf.SetXY(marginL, gtY+1)
		pdf.CellFormat(labelW-0.5, lineH+0.5, msgs.GrandTotal, "", 0, "R", false, 0, "")
		pdf.SetXY(marginL+labelW, gtY+1)
		pdf.CellFormat(countCellW-0.5, lineH+0.5, lbIssueCountLabel(total, lang), "", 0, "R", false, 0, "")
	}

	pdf.SetY(gtY + gtH + 5)
	drawProjektberichtConfirmation(pdf, report, opts, palette, marginL, usableW)

	return pdf
}

type projectReportParty struct {
	Title string
	Lines []string
}

const projektberichtPaperSignatureTopPaddingMM = 40.0

func drawProjektberichtConfirmation(pdf *fpdf.Fpdf, report *lbReport, opts lbRenderOpts, palette lbPDFPalette, marginL, usableW float64) {
	_, pageH := pdf.GetPageSize()
	if pdf.GetY()+142 > pageH-12 {
		pdf.AddPage()
	}
	y := pdf.GetY()

	drawProjektberichtParties(pdf, report, palette, marginL, usableW)
	y = pdf.GetY() + 5

	pdf.SetFont("DejaVu", "B", 8)
	setTextRGB(pdf, palette.PrimaryDark)
	pdf.SetXY(marginL, y)
	pdf.CellFormat(usableW, 5, "Bestätigung und Abnahme", "", 0, "L", false, 0, "")
	y += 6

	pdf.SetFont("DejaVu", "", 6.8)
	setTextRGB(pdf, rgbColor{})
	text := "Der Kunde bestätigt hiermit, dass die in diesem Projektbericht angeführten Leistungen vereinbarungsgemäß erbracht bzw. bereitgestellt wurden, und nimmt diese – mit Ausnahme etwaiger in der Anlage dokumentierter offener Punkte – hiermit ab und übernimmt sie. Darüber hinausgehende Mängel sind dem Kunden nach derzeitigem Kenntnisstand nicht bekannt. Etwaige in der Anlage angeführte bekannte offene Punkte und allfällige Mängel sind dem Kunden bereits vor Abgabe dieser Bestätigung im Projekt nachvollziehbar dokumentiert oder per E-Mail zur Verfügung gestellt worden."
	pdf.SetXY(marginL, y)
	pdf.MultiCell(usableW, 3.4, text, "", "L", false)

	y = pdf.GetY() + projektberichtPaperSignatureTopPaddingMM
	pdf.SetFont("DejaVu", "", 6.5)
	lineY := y
	colW := (usableW - 16) / 3
	labels := []string{"Ort, Datum", "Name und Funktion/Rolle in BLOCKSCHRIFT", "Firmenmäßige Unterschrift oder digitale Signatur"}
	if opts.Lang == "en" {
		labels = []string{"Place, date", "Name and role in block letters", "Company signature or digital signature"}
	}
	for i, label := range labels {
		x := marginL + float64(i)*(colW+8)
		setDrawRGB(pdf, palette.TableRowBorder)
		pdf.Line(x, lineY, x+colW, lineY)
		pdf.SetXY(x, lineY+1.5)
		pdf.CellFormat(colW, 3, label, "", 0, "L", false, 0, "")
	}

	if opts.AcceptanceURL != "" {
		qrY := lineY + 12
		const qrSize = 24.0
		qrX := marginL + usableW - qrSize
		if qrBytes, err := projektberichtQRPNG(opts.AcceptanceURL, 160); err == nil {
			name := "projektbericht-qr-" + opts.ReportCode
			pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(qrBytes))
			if pdf.Error() == nil {
				pdf.ImageOptions(name, qrX, qrY, qrSize, qrSize, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, opts.AcceptanceURL)
			} else {
				pdf.ClearError()
			}
		}
		pdf.SetFont("DejaVu", "", 5.8)
		setTextRGB(pdf, palette.Primary)
		pdf.SetXY(marginL+usableW-56, qrY+qrSize+2)
		pdf.MultiCell(56, 3, opts.AcceptanceURL, "", "R", false)
		setTextRGB(pdf, rgbColor{})
		pdf.SetY(qrY + qrSize + 8)
	}
}

func drawProjektberichtParties(pdf *fpdf.Fpdf, report *lbReport, palette lbPDFPalette, marginL, usableW float64) {
	customer := projectReportCustomerParty(report)
	contractor := projectReportContractorParty()

	pdf.SetFont("DejaVu", "B", 8)
	setTextRGB(pdf, palette.PrimaryDark)
	pdf.SetX(marginL)
	pdf.CellFormat(usableW, 5, "Vertragspartner", "", 0, "L", false, 0, "")
	pdf.SetY(pdf.GetY() + 5)

	colGap := 8.0
	colW := (usableW - colGap) / 2
	y := pdf.GetY()
	drawProjectReportPartyBox(pdf, customer, palette, marginL, y, colW)
	drawProjectReportPartyBox(pdf, contractor, palette, marginL+colW+colGap, y, colW)
	pdf.SetY(y + projectReportPartyBoxHeight(customer, contractor) + 1)
	setTextRGB(pdf, rgbColor{})
}

func drawProjectReportPartyBox(pdf *fpdf.Fpdf, party projectReportParty, palette lbPDFPalette, x, y, w float64) {
	h := projectReportPartyBoxHeight(party)
	setDrawRGB(pdf, palette.TableRowBorder)
	pdf.Rect(x, y, w, h, "D")
	setFillRGB(pdf, palette.PrimaryPale)
	pdf.Rect(x, y, w, 5, "F")

	pdf.SetFont("DejaVu", "B", 6.5)
	setTextRGB(pdf, palette.PrimaryDark)
	pdf.SetXY(x+1.2, y+0.9)
	pdf.CellFormat(w-2.4, 3, party.Title, "", 0, "L", false, 0, "")

	pdf.SetFont("DejaVu", "", 6.2)
	setTextRGB(pdf, rgbColor{})
	lineY := y + 6.2
	for _, line := range party.Lines {
		pdf.SetXY(x+1.2, lineY)
		pdf.CellFormat(w-2.4, 3, smartTruncate(line, 86), "", 0, "L", false, 0, "")
		lineY += 3.4
	}
}

func projectReportPartyBoxHeight(parties ...projectReportParty) float64 {
	maxLines := 1
	for _, p := range parties {
		if len(p.Lines) > maxLines {
			maxLines = len(p.Lines)
		}
	}
	return 6.2 + float64(maxLines)*3.4 + 1.5
}

func projectReportCustomerParty(report *lbReport) projectReportParty {
	party := projectReportParty{Title: "Auftraggeber / Kunde"}
	var project *models.Project
	var customer *models.Customer
	if report != nil && report.ProjectID > 0 {
		project = getProjectByID(report.ProjectID)
		if project != nil && project.CustomerID != nil {
			customer = getCustomerByID(*project.CustomerID)
		}
	}

	name := ""
	if customer != nil {
		name = strings.TrimSpace(customer.Name)
	}
	if name == "" && project != nil {
		name = firstNonEmpty(project.CustomerName, project.CustomerLabel)
	}
	if name == "" && report != nil {
		name = firstNonEmpty(report.ProjectName, report.ProjectKey)
	}
	if name == "" {
		name = "Kunde"
	}
	party.Lines = append(party.Lines, name)

	if customer != nil {
		party.Lines = append(party.Lines, projectReportCustomerAddressLines(customer)...)
		if taxID := firstNonEmpty(customer.TaxID, customer.VATID); taxID != "" {
			party.Lines = append(party.Lines, "UID: "+taxID)
		}
		if fn := strings.TrimSpace(customer.CompanyRegisterNumber); fn != "" {
			party.Lines = append(party.Lines, "FN: "+fn)
		}
		if contact := projectReportCustomerContact(customer); contact != "" {
			party.Lines = append(party.Lines, "Kontakt: "+contact)
		}
	}
	if report != nil {
		projectName := firstNonEmpty(report.ProjectName, report.ProjectKey)
		if projectName != "" {
			if strings.TrimSpace(report.ProjectKey) != "" && projectName != report.ProjectKey {
				party.Lines = append(party.Lines, "Projekt: "+report.ProjectKey+" - "+projectName)
			} else {
				party.Lines = append(party.Lines, "Projekt: "+projectName)
			}
		}
	}
	return party
}

func projectReportContractorParty() projectReportParty {
	return projectReportParty{
		Title: "Auftragnehmer",
		Lines: []string{
			"BYTEPOETS GmbH",
			"Gadollaplatz 1, 8010 Graz, Austria",
			"UID: ATU65885358, FN: 349730i",
			"FB Gericht: Landesgericht für ZRS Graz",
			"Geschäftsführer: Ing. Markus Barta",
			"office@bytepoets.com, +43 664 606 97 100",
		},
	}
}

func projectReportCustomerAddressLines(c *models.Customer) []string {
	if c == nil {
		return nil
	}
	// Prefer a structured billing address, then a visit address, then the
	// free-form Address field — the only place some customers (e.g. those
	// imported or only lightly edited) keep their postal address. Each
	// structured branch is only taken when it actually carries a street/zip/
	// city; a bare country must not short-circuit the richer free-form field
	// (PAI-557: print the postal address whenever it is available).
	if hasPostalDetail(c.BillingAddressStreet, c.BillingAddressZip, c.BillingAddressCity) {
		country := firstNonEmpty(c.BillingAddressCountry, c.Country)
		return compactPostalAddressLines(c.BillingAddressStreet, c.BillingAddressZip, c.BillingAddressCity, country)
	}
	if hasPostalDetail(c.VisitAddressStreet, c.VisitAddressZip, "") {
		return compactPostalAddressLines(c.VisitAddressStreet, c.VisitAddressZip, "", c.Country)
	}
	if addr := strings.TrimSpace(c.Address); addr != "" {
		// Free-form addresses usually already include the city and country, so
		// emit the text as-is and only append the country when it is missing.
		lines := []string{addr}
		if country := strings.TrimSpace(c.Country); country != "" && !strings.Contains(strings.ToLower(addr), strings.ToLower(country)) {
			lines = append(lines, country)
		}
		return lines
	}
	return compactPostalAddressLines("", "", "", c.Country)
}

// hasPostalDetail reports whether a structured address carries a real
// street/zip/city — not just a country, which alone is not worth printing as a
// standalone address block.
func hasPostalDetail(street, zip, city string) bool {
	return strings.TrimSpace(street) != "" ||
		strings.TrimSpace(zip) != "" ||
		strings.TrimSpace(city) != ""
}

func projectReportCustomerContact(c *models.Customer) string {
	if c == nil {
		return ""
	}
	contact := strings.TrimSpace(c.ContactName)
	if email := strings.TrimSpace(c.ContactEmail); email != "" {
		if contact != "" {
			contact += " <" + email + ">"
		} else {
			contact = email
		}
	}
	return contact
}

func compactAddress(street, zip, city, country string) string {
	street = strings.TrimSpace(street)
	zip = strings.TrimSpace(zip)
	city = strings.TrimSpace(city)
	country = strings.TrimSpace(country)
	zipCity := joinNonEmpty(zip, city)
	return joinNonEmpty(street, zipCity, country)
}

func compactPostalAddressLines(street, zip, city, country string) []string {
	street = strings.TrimSpace(street)
	zip = strings.TrimSpace(zip)
	city = strings.TrimSpace(city)
	country = strings.TrimSpace(country)
	zipCity := joinNonEmpty(zip, city)
	lines := make([]string, 0, 3)
	if street != "" {
		lines = append(lines, street)
	}
	if zipCity != "" {
		lines = append(lines, zipCity)
	}
	if country != "" {
		lines = append(lines, country)
	}
	return lines
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, ", ")
}

func firstNonEmpty(parts ...string) string {
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			return part
		}
	}
	return ""
}

func drawProjektberichtIntro(pdf *fpdf.Fpdf, report *lbReport, palette lbPDFPalette, marginL, usableW float64) {
	var coop *models.CooperationMetadata
	var perms []projectReportPermission
	if report.ProjectID > 0 {
		coop, _ = loadCooperation(report.ProjectID)
		perms, _ = loadProjectReportPermissions(report.ProjectID)
	}
	pdf.SetFont("DejaVu", "B", 8)
	setTextRGB(pdf, palette.PrimaryDark)
	pdf.SetXY(marginL, pdf.GetY())
	pdf.CellFormat(usableW, 5, "1. Grundlagen", "", 0, "L", false, 0, "")
	pdf.SetY(pdf.GetY() + 5)

	pdf.SetFont("DejaVu", "", 6.5)
	setTextRGB(pdf, rgbColor{})
	basis := ""
	terms := ""
	if coop != nil {
		basis = strings.TrimSpace(coop.ReportContractBasis)
		terms = strings.TrimSpace(coop.ReportTermsURL)
	}
	text := fmt.Sprintf("Dieser Projektbericht dokumentiert den Stand und die Lieferungen des Projekts %s. Die angeführten Lieferungen beziehen sich auf den ausgewählten Berichtsumfang.", report.ProjectKey)
	if basis != "" {
		text += " Grundlage: " + basis + "."
	}
	if terms != "" {
		text += " AGB: " + terms + "."
	}
	text += " Für das Projekt wurde eine schriftliche Rückmeldung innerhalb von 21 Werktagen vereinbart. Sollte innerhalb dieser Frist weder eine schriftliche Abnahme erfolgen noch schriftlich Mängel geltend gemacht werden, gilt die gelieferte Funktionalität als mangelfrei abgenommen."
	pdf.SetX(marginL)
	pdf.MultiCell(usableW, 3.2, text, "", "L", false)
	pdf.SetY(pdf.GetY() + 2)

	pdf.SetFont("DejaVu", "B", 8)
	setTextRGB(pdf, palette.PrimaryDark)
	pdf.SetX(marginL)
	pdf.CellFormat(usableW, 5, "2. Berechtigungen", "", 0, "L", false, 0, "")
	pdf.SetY(pdf.GetY() + 5)

	if len(perms) == 0 {
		pdf.SetFont("DejaVu", "", 6.5)
		setTextRGB(pdf, rgbColor{r: 120, g: 120, b: 120})
		pdf.SetX(marginL)
		pdf.CellFormat(usableW, 4, "Keine projektspezifischen Bericht-Berechtigungen hinterlegt.", "", 0, "L", false, 0, "")
		pdf.SetY(pdf.GetY() + 5)
		setTextRGB(pdf, rgbColor{})
		return
	}

	headers := []string{"Person", "Unternehmen", "Funktion", "Freigabe", "Lieferung", "Abnahme"}
	widths := []float64{42, 45, 58, 22, 22, 22}
	extra := usableW
	for _, w := range widths {
		extra -= w
	}
	if extra > 0 {
		widths[2] += extra
	}
	pdf.SetFont("DejaVu", "B", 6.2)
	setFillRGB(pdf, palette.PrimaryPale)
	setTextRGB(pdf, palette.PrimaryDark)
	y := pdf.GetY()
	x := marginL
	for i, h := range headers {
		pdf.SetXY(x, y)
		pdf.CellFormat(widths[i], 4.5, " "+h, "", 0, "L", true, 0, "")
		x += widths[i]
	}
	pdf.SetY(y + 4.5)
	pdf.SetFont("DejaVu", "", 6.2)
	setTextRGB(pdf, rgbColor{})
	for _, p := range perms {
		y = pdf.GetY()
		x = marginL
		values := []string{p.PersonName, p.Company, p.RoleLabel, yesNoDE(p.MayApprove), yesNoDE(p.MayDeliver), yesNoDE(p.MayAccept)}
		for i, v := range values {
			setDrawRGB(pdf, palette.TableRowBorder)
			pdf.Rect(x, y, widths[i], 4.2, "D")
			pdf.SetXY(x+0.5, y+0.6)
			pdf.CellFormat(widths[i]-1, 3, smartTruncate(v, 42), "", 0, "L", false, 0, "")
			x += widths[i]
		}
		pdf.SetY(y + 4.2)
	}
	pdf.SetY(pdf.GetY() + 4)
}

func yesNoDE(v bool) string {
	if v {
		return "JA"
	}
	return "NEIN"
}

func projektberichtQRPNG(value string, size int) ([]byte, error) {
	code, err := qr.Encode(value, qr.M, qr.Auto)
	if err != nil {
		return nil, err
	}
	scaled, err := barcode.Scale(code, size, size)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
