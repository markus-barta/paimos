// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-159. Tests for the whole-word OK / FAIL matcher used by the
// test-connection endpoint to grade an LLM smoke-test reply.
//
// The bug we're guarding against: a naive `strings.Contains(s, "OK")`
// would wrongly accept "OKAY" / "BLOCK" / "took" — every one of which
// can plausibly appear in a model's witty reply. The matcher requires
// non-letter boundaries on both sides.

package handlers

import "testing"

func TestContainsWholeWord_PositiveMatches(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want string // marker we expect to match
	}{
		{"trailing punctuation", "All systems are OK.", "OK"},
		{"surrounded by spaces", "I think this is OK to ship", "OK"},
		{"start of string", "OK, here's the joke", "OK"},
		{"end of string", "All clear: OK", "OK"},
		{"between quotes", `the duck said "OK" and left`, "OK"},
		{"FAIL with comma", "FAIL, the model is asleep.", "FAIL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !containsWholeWord(c.s, c.want) {
				t.Errorf("containsWholeWord(%q, %q) = false; want true", c.s, c.want)
			}
		})
	}
}

func TestContainsWholeWord_NegativeMatches(t *testing.T) {
	cases := []struct {
		name string
		s    string
	}{
		// Every classic substring trap that "Contains" would mis-fire on.
		{"OKAY substring", "Everything is OKAY"},
		{"BLOCK substring", "Code BLOCKED on review"},
		{"took substring", "took two espressos to wake up"},
		{"lowercase ok", "ok here we go"},        // case-sensitive marker
		{"book substring", "the rubber duck reads bOOK club"},
		{"empty string", ""},
		{"empty marker", "OK"}, // marker arg is "" → false
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			marker := "OK"
			if c.name == "empty marker" {
				marker = ""
			}
			if containsWholeWord(c.s, marker) {
				t.Errorf("containsWholeWord(%q, %q) = true; want false", c.s, marker)
			}
		})
	}
}

func TestContainsWholeWord_RepeatedMarker(t *testing.T) {
	// Two adjacent OKs separated by a comma; the matcher should find
	// at least one — proving the loop advances past a no-match index
	// rather than returning false on the first "near-miss".
	if !containsWholeWord("OKAYish but OK overall", "OK") {
		t.Errorf("expected match for OK after rejecting OKAY substring")
	}
}
