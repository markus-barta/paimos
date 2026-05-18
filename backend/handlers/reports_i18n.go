// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"fmt"
	"strings"
	"time"
)

// lbLang is the in-memory message catalog used by the Lieferbericht PDF
// renderer (PAI-402). Frontend chrome lives in /frontend/src/i18n/*.ts; this
// catalog only covers strings baked into the PDF.
type lbLang struct {
	HeaderTitle      string
	StatusDelivered  string
	StatusInProgress string
	StatusPlanned    string
	ColKey           string
	ColType          string
	ColSummary       string
	ColDescription   string
	ColStatus        string
	ColSP            string
	ColHours         string
	ColARSP          string
	ColARHours       string
	ColAREUR         string
	Subtotal         string
	GrandTotal       string
	PageNOfM         string // printf with one %d for current page; literal {nb} expands at PDF output time
	IssuesUnit       string // "{N} {IssuesUnit}" — e.g. "issues" / "Tickets"
}

var lbMessages = map[string]lbLang{
	"en": {
		HeaderTitle:      "Delivery report",
		StatusDelivered:  "Delivered",
		StatusInProgress: "In progress",
		StatusPlanned:    "Planned",
		ColKey:           "Key",
		ColType:          "Type",
		ColSummary:       "Summary",
		ColDescription:   "Description",
		ColStatus:        "Status",
		ColSP:            "SP",
		ColHours:         "h",
		ColARSP:          "AR SP",
		ColARHours:       "AR h",
		ColAREUR:         "AR EUR",
		Subtotal:         "Subtotal",
		GrandTotal:       "Grand Total",
		PageNOfM:         "Page %d of {nb}",
		IssuesUnit:       "issues",
	},
	"de": {
		HeaderTitle:      "Lieferbericht",
		StatusDelivered:  "Geliefert",
		StatusInProgress: "Umsetzung",
		StatusPlanned:    "Geplant",
		ColKey:           "Key",
		ColType:          "Typ",
		ColSummary:       "Zusammenfassung",
		ColDescription:   "Beschreibung",
		ColStatus:        "Status",
		ColSP:            "SP",
		ColHours:         "h",
		ColARSP:          "AR SP",
		ColARHours:       "AR h",
		ColAREUR:         "AR EUR",
		Subtotal:         "Zwischensumme",
		GrandTotal:       "Gesamtsumme",
		PageNOfM:         "Seite %d/{nb}",
		IssuesUnit:       "Tickets",
	},
}

// resolveLBLang picks the message catalog for a given lang code. Falls back
// to English for unknown / empty values so PDF rendering never breaks.
func resolveLBLang(lang string) lbLang {
	if msgs, ok := lbMessages[strings.ToLower(lang)]; ok {
		return msgs
	}
	return lbMessages["en"]
}

// lbMonthsDE maps Go's English month names to their German equivalents. Go's
// time.Format only emits English; for the German PDF header we substitute
// after formatting.
var lbMonthsDE = map[string]string{
	"January":   "Januar",
	"February":  "Februar",
	"March":     "März",
	"April":     "April",
	"May":       "Mai",
	"June":      "Juni",
	"July":      "Juli",
	"August":    "August",
	"September": "September",
	"October":   "Oktober",
	"November":  "November",
	"December":  "Dezember",
}

// formatLBTimestamp renders the current time for the PDF header in the
// caller's locale. German uses "2. Januar 2026 um 15:04:05"; English uses
// "January 2, 2026 at 15:04:05".
func formatLBTimestamp(t time.Time, lang string) string {
	switch strings.ToLower(lang) {
	case "de":
		s := t.Format("2. January 2006 um 15:04:05")
		for en, de := range lbMonthsDE {
			if strings.Contains(s, en) {
				return strings.Replace(s, en, de, 1)
			}
		}
		return s
	default:
		return t.Format("January 2, 2006 at 15:04:05")
	}
}

// lbIssueCountLabel formats "{N} {unit}" for the count-only subtotal/grand
// total rows (PAI-401). Used when no numeric columns are visible.
func lbIssueCountLabel(n int, lang string) string {
	return fmt.Sprintf("%d %s", n, resolveLBLang(lang).IssuesUnit)
}

// lbColSet controls which numeric columns the Lieferbericht PDF renders
// (PAI-400). The five Key/Type/Summary/Description/Status identity columns
// are always present; this set governs only the numeric tail.
type lbColSet struct {
	SP    bool
	H     bool
	ARSP  bool
	ARH   bool
	AREUR bool
}

// AnyVisible reports whether any numeric column is visible.
func (s lbColSet) AnyVisible() bool { return s.SP || s.H || s.ARSP || s.ARH || s.AREUR }

// defaultLBColSet returns the back-compat "show everything" set used when the
// request has no `?cols=` query param.
func defaultLBColSet() lbColSet { return lbColSet{SP: true, H: true, ARSP: true, ARH: true, AREUR: true} }

// parseLBColSet reads a comma-separated `?cols=` query value into an lbColSet.
// Accepted tokens (case-insensitive): sp, h, ar_sp, ar_h, ar_eur. Unknown
// tokens are silently ignored. An empty input yields the zero set (nothing
// visible).
func parseLBColSet(s string) lbColSet {
	var set lbColSet
	for _, tok := range strings.Split(s, ",") {
		switch strings.ToLower(strings.TrimSpace(tok)) {
		case "sp":
			set.SP = true
		case "h":
			set.H = true
		case "ar_sp":
			set.ARSP = true
		case "ar_h":
			set.ARH = true
		case "ar_eur":
			set.AREUR = true
		}
	}
	return set
}

// lbRenderOpts bundles per-request rendering options so the renderer signature
// stays stable as new toggles are added.
type lbRenderOpts struct {
	Lang string
	Cols lbColSet
}
