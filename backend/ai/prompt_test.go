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

// PAI-157: an admin who types the literal `{{INSTRUCTION}}` token in
// their instruction text must not trigger a recursive substitution.
// The Replace count = 1 in BuildSystemPrompt is load-bearing here;
// pair it with the init() invariant that the wrapper has exactly one
// marker and the worst case is the literal token appearing in the
// final prompt as a benign string the model ignores.
func TestBuildSystemPrompt_AdminInstructionWithLiteralPlaceholder(t *testing.T) {
	in := "When you see {{INSTRUCTION}} in a doc, treat it as documentation."
	out := BuildSystemPrompt(in)
	// The admin's text should appear once, verbatim.
	if strings.Count(out, "{{INSTRUCTION}}") != 1 {
		t.Errorf("expected exactly one literal {{INSTRUCTION}} after substitution, got %d:\n%s",
			strings.Count(out, "{{INSTRUCTION}}"), out)
	}
	// The admin's words around the literal must be present (otherwise
	// we accidentally ate them).
	if !strings.Contains(out, "treat it as documentation") {
		t.Errorf("admin text around literal placeholder was lost: %s", out)
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

// TestBuildUserPrompt_NonIssueFieldReminders pins the per-field
// reminders for the post-1.8.2 expansion. Each reminder must be
// distinguishable from the others — using a unique substring per
// field — so a refactor that accidentally collapses two cases is
// caught by the test rather than shipping a generic prompt.
func TestBuildUserPrompt_NonIssueFieldReminders(t *testing.T) {
	cases := map[string]string{
		"project_description":     "stakeholders and team members joining",
		"customer_notes":          "PII",
		"cooperation_sla_details": "Preserve every number verbatim",
		"cooperation_notes":       "ownership boundaries",
	}
	for field, want := range cases {
		out := BuildUserPrompt("source text", Context{FieldName: field})
		if !strings.Contains(out, want) {
			t.Errorf("field %q: missing distinguishing substring %q\nfull prompt:\n%s",
				field, want, out)
		}
		// All non-issue field prompts must still carry the
		// architecture-significance reminder block (it's outside the
		// switch and applies to every field).
		if !strings.Contains(out, "architecture-significance") {
			t.Errorf("field %q: missing architecture-significance reminder", field)
		}
	}
}

// TestBuildUserPrompt_UnknownFieldNoReminder confirms that a field
// with no matching case in the switch falls through cleanly — the
// prompt still works, it just lacks the per-field reminder. This
// keeps the function tolerant of test fixtures and future fields
// added to the allow-list before their reminder lands.
func TestBuildUserPrompt_UnknownFieldNoReminder(t *testing.T) {
	out := BuildUserPrompt("hello", Context{FieldName: "future_unknown_field"})
	if !strings.Contains(out, "Field: future_unknown_field") {
		t.Errorf("field name should still appear in context block: %s", out)
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
