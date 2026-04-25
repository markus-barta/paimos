// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// PAI-150 wrapper-and-context unit tests.

package ai

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_LayersAdminInstruction(t *testing.T) {
	out := BuildSystemPrompt("Use UK English. Keep acronyms in CAPS.")
	if !strings.Contains(out, "Use UK English") {
		t.Errorf("admin instruction not layered in: %q", out)
	}
	if !strings.Contains(out, "Hard invariants you MUST follow") {
		t.Errorf("fixed wrapper not present: %q", out)
	}
	if strings.Contains(out, "{{INSTRUCTION}}") {
		t.Errorf("placeholder not replaced")
	}
}

func TestBuildSystemPrompt_EmptyInstructionFallsBack(t *testing.T) {
	out := BuildSystemPrompt("")
	if strings.Contains(out, "{{INSTRUCTION}}") {
		t.Errorf("placeholder leaked through with empty input")
	}
	if !strings.Contains(out, "Hard invariants") {
		t.Errorf("wrapper missing")
	}
}

func TestBuildUserPrompt_OmitsEmptyContextLines(t *testing.T) {
	out := BuildUserPrompt("hello", Context{})
	for _, banned := range []string{
		"Project: \n", "Issue: \n", "Parent epic: \n", "Field: \n",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("empty context leaked: %q", banned)
		}
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("source text missing")
	}
}

func TestBuildUserPrompt_FieldSpecificReminder(t *testing.T) {
	out := BuildUserPrompt("- [ ] do thing", Context{FieldName: "acceptance_criteria"})
	if !strings.Contains(out, "Keep checklist items") {
		t.Errorf("acceptance_criteria reminder missing: %q", out)
	}
	out2 := BuildUserPrompt("...", Context{FieldName: "notes"})
	if !strings.Contains(out2, "informal") {
		t.Errorf("notes reminder missing")
	}
}

func TestBuildUserPrompt_ArchitectureWarning(t *testing.T) {
	out := BuildUserPrompt("we are introducing a schema change here", Context{})
	if !strings.Contains(out, "architecture-significance") {
		t.Errorf("architecture-warning block missing")
	}
}

func TestStripFenceEcho(t *testing.T) {
	cases := map[string]string{
		"plain text":                       "plain text",
		"```\nfenced\n```":                 "fenced",
		"```markdown\nfenced\nbody\n```":   "fenced\nbody",
		// Legit inner code block: do NOT strip.
		"prefix\n```\ncode\n```\nsuffix":   "prefix\n```\ncode\n```\nsuffix",
	}
	for in, want := range cases {
		if got := StripFenceEcho(in); got != want {
			t.Errorf("StripFenceEcho(%q) = %q, want %q", in, got, want)
		}
	}
}
