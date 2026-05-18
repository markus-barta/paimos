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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"
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

	report, err := buildLieferbericht(projectID, scope, sprintIDs, fromDate, toDate)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// PAI-400: distinguish "param absent" (back-compat → all visible) from
	// "param present but empty" (PAI-401 → zero numeric columns). url.Values.Get
	// returns "" for both, so check the underlying slice directly.
	var colSet lbColSet
	if _, present := r.URL.Query()["cols"]; present {
		colSet = parseLBColSet(r.URL.Query().Get("cols"))
	} else {
		colSet = defaultLBColSet()
	}
	pdf := renderLieferberichtPDF(report, lbRenderOpts{Lang: lang, Cols: colSet})

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		jsonError(w, "pdf generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("LB-%s %s.pdf", report.ProjectKey, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())
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

func renderLieferberichtPDF(report *lbReport, opts lbRenderOpts) *fpdf.Fpdf {
	lang := opts.Lang
	msgs := resolveLBLang(lang)
	visible := opts.Cols
	anyNumeric := visible.AnyVisible()
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 12)
	pdf.SetMargins(10, 10, 10)

	// Register UTF-8 fonts for umlaut support
	pdf.AddUTF8FontFromBytes("DejaVu", "", dejaVuSansTTF)
	pdf.AddUTF8FontFromBytes("DejaVu", "B", dejaVuSansBoldTTF)

	// Register logo — prefers the active instance branding (PAI-399). On any
	// resolution failure, resolveBrandingLogoForPDF returns the embedded
	// PAIMOS mark so the PDF never breaks.
	logoBytes, logoImgType := resolveBrandingLogoForPDF()
	pdf.RegisterImageOptionsReader("logo", fpdf.ImageOptions{ImageType: logoImgType}, bytes.NewReader(logoBytes))

	// A4 landscape = 297mm wide. Margins: 10mm each side → usable 277mm.
	const pageW = 297.0
	const marginL = 10.0
	const lineH = 3.0 // line height for MultiCell rows

	// Build report key for header (e.g. "LB-ASC2501-02")
	lbKey := fmt.Sprintf("LB-%s", report.ProjectKey)

	// Header — logo left, "<HeaderTitle> LB-XXX" center-left, locale-aware date+time right.
	pdf.SetHeaderFuncMode(func() {
		pdf.ImageOptions("logo", marginL, 5, 8, 0, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		pdf.SetFont("DejaVu", "", 8)
		pdf.SetTextColor(0, 0, 0)
		pdf.SetXY(marginL+10, 6)
		pdf.CellFormat(120, 4, fmt.Sprintf("%s %s", msgs.HeaderTitle, lbKey), "", 0, "L", false, 0, "")
		// Date + time right-aligned
		pdf.SetFont("DejaVu", "", 7)
		pdf.SetTextColor(80, 80, 80)
		pdf.SetXY(pageW-10-60, 6)
		pdf.CellFormat(60, 4, formatLBTimestamp(time.Now(), lang), "", 0, "R", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetY(13)
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
		{msgs.ColKey, 20, "L"},         // 0
		{msgs.ColType, 12, "L"},        // 1
		{msgs.ColSummary, 68, "L"},     // 2 — multiline
		{msgs.ColDescription, 80, "L"}, // 3 — multiline
		{msgs.ColStatus, 18, "L"},      // 4
		{msgs.ColSP, 10, "R"},          // 5 — visible.SP
		{msgs.ColHours, 10, "R"},       // 6 — visible.H
		{msgs.ColARSP, 14, "R"},        // 7 — visible.ARSP
		{msgs.ColARHours, 14, "R"},     // 8 — visible.ARH
		{msgs.ColAREUR, 18, "R"},       // 9 — visible.AREUR
	}

	// PAI-400: hidden numeric columns release their width to the Description
	// column (index 3) so the page still fills horizontally. The 5 identity
	// columns (Key/Type/Summary/Description/Status) are always visible.
	numericVisible := [5]bool{visible.SP, visible.H, visible.ARSP, visible.ARH, visible.AREUR}
	for i, vis := range numericVisible {
		idx := 5 + i
		if !vis {
			cols[3].w += cols[idx].w
			cols[idx].w = 0
		}
	}

	totalW := 0.0
	for _, c := range cols {
		totalW += c.w
	}

	const tblHeaderH = 6.0
	const minRowH = 5.0

	// Grid border color — light gray, thin
	gridColor := [3]int{200, 200, 200}
	// Header background — blue matching reference
	headerBgColor := [3]int{68, 114, 196} // #4472C4
	// Alternating row color
	altRowBg := [3]int{245, 247, 250}
	// Epic header background — light blue-gray
	epicBg := [3]int{220, 228, 240}

	// Set thin grid line style
	setGridStyle := func() {
		pdf.SetDrawColor(gridColor[0], gridColor[1], gridColor[2])
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
		pdf.SetFillColor(headerBgColor[0], headerBgColor[1], headerBgColor[2])
		pdf.SetTextColor(255, 255, 255)
		y := pdf.GetY()
		x := marginL
		for _, c := range cols {
			pdf.SetXY(x, y)
			pdf.CellFormat(c.w, tblHeaderH, " "+c.header, "", 0, "L", true, 0, "")
			x += c.w
		}
		pdf.SetXY(marginL, y+tblHeaderH)
		pdf.SetTextColor(0, 0, 0)
	}

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

	// Pre-calculate multiline row height using SplitText
	calcRowH := func(summary, desc string) float64 {
		pdf.SetFont("DejaVu", "", 6.5)
		summaryLines := pdf.SplitText(summary, cols[2].w-2)
		descLines := pdf.SplitText(desc, cols[3].w-2)
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
		_, pageH := pdf.GetPageSize()
		if pdf.GetY() > pageH-35 {
			pdf.AddPage()
			drawHeader()
		}

		// Epic group header row — light background, bold, spans full width
		epicY := pdf.GetY()
		pdf.SetFont("DejaVu", "B", 6.5)
		pdf.SetFillColor(epicBg[0], epicBg[1], epicBg[2])
		pdf.SetTextColor(30, 40, 60)
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
		pdf.SetTextColor(0, 0, 0)

		pdf.SetFont("DejaVu", "", 6.5)

		for _, issue := range g.Issues {
			summary := smartTruncate(issue.Title, 200)
			desc := descriptionText(issue.Description, issue.Title)
			if desc != "" {
				desc = smartTruncate(desc, 200)
			}

			rh := calcRowH(summary, desc)

			// Page break check
			_, pageH := pdf.GetPageSize()
			if pdf.GetY()+rh > pageH-12 {
				pdf.AddPage()
				drawHeader()
			}

			rowY := pdf.GetY()

			// Alternating row shading
			if rowIdx%2 == 1 {
				pdf.SetFillColor(altRowBg[0], altRowBg[1], altRowBg[2])
				pdf.Rect(marginL, rowY, totalW, rh, "F")
			}

			pdf.SetFont("DejaVu", "", 6.5)
			x := marginL

			// Col 0: Key
			drawCellBorder(x, rowY, cols[0].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.CellFormat(cols[0].w-1, lineH, issue.IssueKey, "", 0, "L", false, 0, "")
			x += cols[0].w

			// Col 1: Type
			drawCellBorder(x, rowY, cols[1].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.CellFormat(cols[1].w-1, lineH, issue.Type, "", 0, "L", false, 0, "")
			x += cols[1].w

			// Col 2: Summary — multiline
			drawCellBorder(x, rowY, cols[2].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.MultiCell(cols[2].w-1.5, lineH, summary, "", "L", false)
			x += cols[2].w

			// Col 3: Description — multiline
			drawCellBorder(x, rowY, cols[3].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.MultiCell(cols[3].w-1.5, lineH, desc, "", "L", false)
			x += cols[3].w

			// Col 4: Status
			drawCellBorder(x, rowY, cols[4].w, rh)
			pdf.SetXY(x+0.5, rowY+0.8)
			pdf.CellFormat(cols[4].w-1, lineH, statusLabel(issue.Status), "", 0, "L", false, 0, "")
			x += cols[4].w

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
			drawNumeric(5, fmtOptDE(issue.EstimateLp))
			drawNumeric(6, fmtOptDE(issue.EstimateHours))
			drawNumeric(7, fmtOptDE(issue.ArLp))
			drawNumeric(8, fmtOptDE(issue.ArHours))
			drawNumeric(9, fmtDE(issue.ArEur))

			// Advance Y
			pdf.SetXY(marginL, rowY+rh)
			rowIdx++
		}

		// Subtotal row
		subY := pdf.GetY()
		subH := 5.0
		pdf.SetFont("DejaVu", "B", 6.5)
		pdf.SetFillColor(240, 242, 246)
		pdf.Rect(marginL, subY, totalW, subH, "F")
		setGridStyle()
		pdf.Rect(marginL, subY, totalW, subH, "D")

		subLabelW := cols[0].w + cols[1].w + cols[2].w + cols[3].w + cols[4].w
		pdf.SetXY(marginL, subY+0.8)
		pdf.CellFormat(subLabelW-0.5, lineH, msgs.Subtotal, "", 0, "R", false, 0, "")
		if anyNumeric {
			x := marginL + subLabelW
			drawSubCell := func(idx int, text string) {
				if cols[idx].w <= 0 {
					return
				}
				pdf.SetXY(x, subY+0.8)
				pdf.CellFormat(cols[idx].w-0.5, lineH, text, "", 0, "R", false, 0, "")
				x += cols[idx].w
			}
			drawSubCell(5, fmtDE(g.Subtotal.EstimateLp))
			drawSubCell(6, fmtDE(g.Subtotal.EstimateHours))
			drawSubCell(7, fmtDE(g.Subtotal.ArLp))
			drawSubCell(8, fmtDE(g.Subtotal.ArHours))
			drawSubCell(9, fmtDE(g.Subtotal.ArEur))
		} else {
			// PAI-401: no numeric columns visible → show "{N} <issuesUnit>" in
			// the freed-up area to the right of the label.
			countW := totalW - subLabelW
			pdf.SetXY(marginL+subLabelW, subY+0.8)
			pdf.CellFormat(countW-0.5, lineH, lbIssueCountLabel(len(g.Issues), lang), "", 0, "R", false, 0, "")
		}
		pdf.SetXY(marginL, subY+subH)
		pdf.SetFont("DejaVu", "", 6.5)
	}

	// Grand total row
	gtY := pdf.GetY() + 0.5
	gtH := 6.0
	pdf.SetFont("DejaVu", "B", 7)
	pdf.SetFillColor(220, 228, 240)
	pdf.Rect(marginL, gtY, totalW, gtH, "F")
	setGridStyle()
	pdf.Rect(marginL, gtY, totalW, gtH, "D")

	subLabelW := cols[0].w + cols[1].w + cols[2].w + cols[3].w + cols[4].w
	pdf.SetXY(marginL, gtY+1)
	pdf.CellFormat(subLabelW-0.5, lineH+0.5, msgs.GrandTotal, "", 0, "R", false, 0, "")
	if anyNumeric {
		x := marginL + subLabelW
		drawGtCell := func(idx int, text string) {
			if cols[idx].w <= 0 {
				return
			}
			pdf.SetXY(x, gtY+1)
			pdf.CellFormat(cols[idx].w-0.5, lineH+0.5, text, "", 0, "R", false, 0, "")
			x += cols[idx].w
		}
		drawGtCell(5, fmtDE(report.GrandTotal.EstimateLp))
		drawGtCell(6, fmtDE(report.GrandTotal.EstimateHours))
		drawGtCell(7, fmtDE(report.GrandTotal.ArLp))
		drawGtCell(8, fmtDE(report.GrandTotal.ArHours))
		drawGtCell(9, fmtDE(report.GrandTotal.ArEur))
	} else {
		// PAI-401: count total issues across all groups.
		var total int
		for _, g := range report.Groups {
			total += len(g.Issues)
		}
		countW := totalW - subLabelW
		pdf.SetXY(marginL+subLabelW, gtY+1)
		pdf.CellFormat(countW-0.5, lineH+0.5, lbIssueCountLabel(total, lang), "", 0, "R", false, 0, "")
	}

	return pdf
}
