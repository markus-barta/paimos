// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"bytes"
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
