// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-pdf/fpdf"
)

// Keep these in sync with frontend/src/composables/useIssueDisplay.ts TYPE_SVGS.
const (
	lbIssueTypeTicketSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
    <line x1="9" y1="9" x2="15" y2="9"/>
    <line x1="9" y1="12" x2="15" y2="12"/>
    <line x1="9" y1="15" x2="13" y2="15"/>
  </svg>`
	lbIssueTypeTaskSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <polyline points="9 11 12 14 22 4"/>
    <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/>
  </svg>`
)

func drawLBTypeSVGIcon(pdf *fpdf.Fpdf, issueType string, palette lbPDFPalette, x, y, size float64) {
	color := palette.TypeTicket
	svg := lbIssueTypeTicketSVG
	if issueType == "task" {
		color = palette.TypeTask
		svg = lbIssueTypeTaskSVG
	}
	if err := drawLBStrokeSVG(pdf, svg, color, x, y, size); err != nil {
		// The constants above are intentionally tiny, known-good SVGs. If they ever
		// drift beyond this renderer's subset, leave the key readable and continue.
		return
	}
}

type lbSVGShape struct {
	name  string
	attr  map[string]string
	chars string
}

func drawLBStrokeSVG(pdf *fpdf.Fpdf, svg string, color rgbColor, x, y, size float64) error {
	dec := xml.NewDecoder(strings.NewReader(svg))
	viewX, viewY, viewW, viewH := 0.0, 0.0, 24.0, 24.0
	var shapes []lbSVGShape

	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]string{}
		for _, a := range start.Attr {
			attrs[a.Name.Local] = a.Value
		}
		switch start.Name.Local {
		case "svg":
			if vb := parseSVGNumberList(attrs["viewBox"]); len(vb) == 4 && vb[2] > 0 && vb[3] > 0 {
				viewX, viewY, viewW, viewH = vb[0], vb[1], vb[2], vb[3]
			}
		case "rect", "line", "polyline", "path":
			shapes = append(shapes, lbSVGShape{name: start.Name.Local, attr: attrs})
		}
	}
	if viewW <= 0 || viewH <= 0 {
		return fmt.Errorf("invalid svg viewBox")
	}

	scale := size / viewW
	if hScale := size / viewH; hScale < scale {
		scale = hScale
	}
	offX := x + (size-viewW*scale)/2 - viewX*scale
	offY := y + (size-viewH*scale)/2 - viewY*scale
	tx := func(v float64) float64 { return offX + v*scale }
	ty := func(v float64) float64 { return offY + v*scale }

	setDrawRGB(pdf, color)
	pdf.SetLineWidth(2.0 * scale)
	pdf.SetLineCapStyle("round")
	pdf.SetLineJoinStyle("round")
	defer func() {
		pdf.SetLineCapStyle("butt")
		pdf.SetLineJoinStyle("miter")
	}()

	for _, shape := range shapes {
		switch shape.name {
		case "rect":
			rx := svgFloat(shape.attr, "rx", 0)
			if rx == 0 {
				rx = svgFloat(shape.attr, "ry", 0)
			}
			rectX := svgFloat(shape.attr, "x", 0)
			rectY := svgFloat(shape.attr, "y", 0)
			rectW := svgFloat(shape.attr, "width", 0)
			rectH := svgFloat(shape.attr, "height", 0)
			if rectW <= 0 || rectH <= 0 {
				continue
			}
			if rx > 0 {
				pdf.RoundedRect(tx(rectX), ty(rectY), rectW*scale, rectH*scale, rx*scale, "1234", "D")
			} else {
				pdf.Rect(tx(rectX), ty(rectY), rectW*scale, rectH*scale, "D")
			}
		case "line":
			pdf.Line(
				tx(svgFloat(shape.attr, "x1", 0)),
				ty(svgFloat(shape.attr, "y1", 0)),
				tx(svgFloat(shape.attr, "x2", 0)),
				ty(svgFloat(shape.attr, "y2", 0)),
			)
		case "polyline":
			pts := parseSVGNumberList(shape.attr["points"])
			for i := 0; i+3 < len(pts); i += 2 {
				pdf.Line(tx(pts[i]), ty(pts[i+1]), tx(pts[i+2]), ty(pts[i+3]))
			}
		case "path":
			if err := drawLBPathData(pdf, shape.attr["d"], tx, ty); err != nil {
				return err
			}
		}
	}
	return nil
}

func svgFloat(attrs map[string]string, key string, fallback float64) float64 {
	if raw := strings.TrimSpace(attrs[key]); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v
		}
	}
	return fallback
}

func parseSVGNumberList(s string) []float64 {
	fields := strings.Fields(strings.ReplaceAll(s, ",", " "))
	vals := make([]float64, 0, len(fields))
	for _, f := range fields {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			vals = append(vals, v)
		}
	}
	return vals
}

func drawLBPathData(pdf *fpdf.Fpdf, d string, tx, ty func(float64) float64) error {
	tokens := tokenizeSVGPath(d)
	i := 0
	cmd := byte(0)
	curX, curY := 0.0, 0.0
	startX, startY := 0.0, 0.0
	hasPoint := false

	read := func() (float64, error) {
		if i >= len(tokens) || isSVGPathCommand(tokens[i]) {
			return 0, fmt.Errorf("missing path number")
		}
		v, err := strconv.ParseFloat(tokens[i], 64)
		i++
		return v, err
	}
	lineTo := func(x, y float64) {
		if hasPoint {
			pdf.Line(tx(curX), ty(curY), tx(x), ty(y))
		}
		curX, curY = x, y
		hasPoint = true
	}

	for i < len(tokens) {
		if isSVGPathCommand(tokens[i]) {
			cmd = tokens[i][0]
			i++
		}
		if cmd == 0 {
			return fmt.Errorf("path missing command")
		}
		rel := cmd >= 'a' && cmd <= 'z'
		switch cmd {
		case 'M', 'm':
			xv, err := read()
			if err != nil {
				return err
			}
			yv, err := read()
			if err != nil {
				return err
			}
			if rel {
				xv += curX
				yv += curY
			}
			curX, curY, startX, startY = xv, yv, xv, yv
			hasPoint = true
			cmd = 'L'
			if rel {
				cmd = 'l'
			}
		case 'L', 'l':
			xv, err := read()
			if err != nil {
				return err
			}
			yv, err := read()
			if err != nil {
				return err
			}
			if rel {
				xv += curX
				yv += curY
			}
			lineTo(xv, yv)
		case 'H', 'h':
			xv, err := read()
			if err != nil {
				return err
			}
			if rel {
				xv += curX
			}
			lineTo(xv, curY)
		case 'V', 'v':
			yv, err := read()
			if err != nil {
				return err
			}
			if rel {
				yv += curY
			}
			lineTo(curX, yv)
		case 'C', 'c':
			vals := make([]float64, 6)
			for n := range vals {
				v, err := read()
				if err != nil {
					return err
				}
				vals[n] = v
			}
			if rel {
				vals[0] += curX
				vals[1] += curY
				vals[2] += curX
				vals[3] += curY
				vals[4] += curX
				vals[5] += curY
			}
			pdf.CurveCubic(tx(curX), ty(curY), tx(vals[0]), ty(vals[1]), tx(vals[4]), ty(vals[5]), tx(vals[2]), ty(vals[3]), "D")
			curX, curY = vals[4], vals[5]
		case 'Q', 'q':
			vals := make([]float64, 4)
			for n := range vals {
				v, err := read()
				if err != nil {
					return err
				}
				vals[n] = v
			}
			if rel {
				vals[0] += curX
				vals[1] += curY
				vals[2] += curX
				vals[3] += curY
			}
			pdf.Curve(tx(curX), ty(curY), tx(vals[0]), ty(vals[1]), tx(vals[2]), ty(vals[3]), "D")
			curX, curY = vals[2], vals[3]
		case 'A', 'a':
			vals := make([]float64, 7)
			for n := range vals {
				v, err := read()
				if err != nil {
					return err
				}
				vals[n] = v
			}
			xv, yv := vals[5], vals[6]
			if rel {
				xv += curX
				yv += curY
			}
			// fpdf has no SVG elliptical-arc primitive. For the tiny 3mm Lucide
			// icons used in the table, connecting to the arc endpoint preserves
			// the recognizable stroke while still reusing the SVG source geometry.
			lineTo(xv, yv)
		case 'Z', 'z':
			lineTo(startX, startY)
		default:
			return fmt.Errorf("unsupported path command %q", string(cmd))
		}
	}
	return nil
}

func tokenizeSVGPath(d string) []string {
	var tokens []string
	for i := 0; i < len(d); {
		r := rune(d[i])
		if unicode.IsSpace(r) || d[i] == ',' {
			i++
			continue
		}
		if unicode.IsLetter(r) {
			tokens = append(tokens, string(d[i]))
			i++
			continue
		}
		start := i
		if d[i] == '+' || d[i] == '-' {
			i++
		}
		for i < len(d) {
			ch := d[i]
			if (ch >= '0' && ch <= '9') || ch == '.' {
				i++
				continue
			}
			if ch == 'e' || ch == 'E' {
				i++
				if i < len(d) && (d[i] == '+' || d[i] == '-') {
					i++
				}
				continue
			}
			break
		}
		if start == i {
			i++
			continue
		}
		tokens = append(tokens, d[start:i])
	}
	return tokens
}

func isSVGPathCommand(tok string) bool {
	return len(tok) == 1 && strings.ContainsRune("MmLlHhVvCcQqAaZz", rune(tok[0]))
}
