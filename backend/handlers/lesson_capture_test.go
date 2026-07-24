// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/handlers"
)

// PAI-343 — coverage for the lesson-capture trigger detection
// endpoint. Three groups:
//
//   1. Trigger fires for incident / bug-class tickets (tag rule).
//   2. Trigger fires when ancestor epic title matches.
//   3. Trigger fires on heuristic comment phrases.
//   4. Trigger does NOT fire on plain feature-work tickets.
//   5. SuggestMemorySlug helper builds the right shape.

func decodeLessonCapture(t *testing.T, resp *http.Response) handlers.LessonCapturePromptResponse {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /lesson-capture-prompt: status=%d body=%s", resp.StatusCode, b)
	}
	var out handlers.LessonCapturePromptResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestLessonCapturePrompt_TriggerOnTag(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	ticketID := seedTicketRow(t, projID, 1, "Crash on signup", nil, "")

	// Tag ticket with "bug".
	var tagID int64
	_ = db.DB.QueryRow(`SELECT id FROM tags WHERE name='bug'`).Scan(&tagID)
	if tagID == 0 {
		res, _ := db.DB.Exec(`INSERT INTO tags(name,color,description) VALUES('bug','red','')`)
		tagID, _ = res.LastInsertId()
	}
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)`, ticketID, tagID); err != nil {
		t.Fatalf("tag ticket: %v", err)
	}

	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/lesson-capture-prompt", ts.adminCookie)
	out := decodeLessonCapture(t, resp)
	if !out.ShouldPrompt {
		t.Fatalf("expected ShouldPrompt=true, got %+v", out)
	}
	if !strings.Contains(out.Reason, "tag:") {
		t.Errorf("Reason=%q, want tag: prefix", out.Reason)
	}
	if !strings.HasPrefix(out.SuggestedName, "feedback_") {
		t.Errorf("SuggestedName=%q, want feedback_ prefix", out.SuggestedName)
	}
}

func TestLessonCapturePrompt_TriggerOnEpicTitle(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	// Parent epic with [POST-LAUNCH] in title.
	res, _ := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, 100, 'epic', '[POST-LAUNCH] Hardening', 'backlog', 'medium')
	`, projID)
	epicID, _ := res.LastInsertId()

	ticketID := seedTicketRow(t, projID, 1, "Refactor signup", &epicID, "")

	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/lesson-capture-prompt", ts.adminCookie)
	out := decodeLessonCapture(t, resp)
	if !out.ShouldPrompt {
		t.Fatalf("expected ShouldPrompt=true, got %+v", out)
	}
	if !strings.Contains(out.Reason, "epic:") {
		t.Errorf("Reason=%q, want epic: prefix", out.Reason)
	}
}

func TestLessonCapturePrompt_TriggerOnCommentPhrase(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	ticketID := seedTicketRow(t, projID, 1, "Slow query", nil, "")

	// Comment containing "lesson".
	if _, err := db.DB.Exec(`
		INSERT INTO comments(issue_id, author_id, body) VALUES(?, NULL, 'Big lesson learned today: always EXPLAIN.')
	`, ticketID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/lesson-capture-prompt", ts.adminCookie)
	out := decodeLessonCapture(t, resp)
	if !out.ShouldPrompt {
		t.Fatalf("expected ShouldPrompt=true, got %+v", out)
	}
	if !strings.Contains(out.Reason, "comment:") {
		t.Errorf("Reason=%q, want comment: prefix", out.Reason)
	}
}

func TestLessonCapturePrompt_NoTrigger(t *testing.T) {
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")
	ticketID := seedTicketRow(t, projID, 1, "Add feature X", nil, "")

	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/lesson-capture-prompt", ts.adminCookie)
	out := decodeLessonCapture(t, resp)
	if out.ShouldPrompt {
		t.Fatalf("expected ShouldPrompt=false on plain feature ticket, got %+v", out)
	}
}

func TestLessonCapturePrompt_AncestorEpicChain(t *testing.T) {
	// Walks up multiple hops — a child ticket whose grand-parent is
	// a hardening epic should still trigger.
	ts := newTestServer(t)
	projID := seedBatchProject(t, "PAI", "PAI")

	res, _ := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, 100, 'epic', 'Hardening Q3', 'backlog', 'medium')
	`, projID)
	epicID, _ := res.LastInsertId()

	res, _ = db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		VALUES(?, 101, 'epic', 'Sub-epic', 'backlog', 'medium')
	`, projID)
	subEpicID, _ := res.LastInsertId()
	// PAI-584 P6: parent_id column dropped — chain via the parent edge.
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_relations(source_id,target_id,type) VALUES(?,?,'parent')`, epicID, subEpicID); err != nil {
		t.Fatalf("seed sub-epic parent edge: %v", err)
	}

	ticketID := seedTicketRow(t, projID, 1, "Patch leak", &subEpicID, "")

	resp := ts.get(t, "/api/issues/"+itoa(ticketID)+"/lesson-capture-prompt", ts.adminCookie)
	out := decodeLessonCapture(t, resp)
	if !out.ShouldPrompt {
		t.Fatalf("expected ShouldPrompt=true (ancestor epic match), got %+v", out)
	}
}

func TestSuggestMemorySlug(t *testing.T) {
	tests := []struct {
		typ, rule, want string
	}{
		{"feedback", "Use --line-buffered in pipes", "feedback_use_line_buffered_in_pipes"},
		{"feedback", "Always check for nil before deref", "feedback_always_check_for_nil_before_deref"},
		// Truncates to first 6 words.
		{"feedback", "One two three four five six seven eight", "feedback_one_two_three_four_five_six"},
		// Empty rule → fallback.
		{"feedback", "", "feedback_lesson"},
		// Empty type defaults to feedback.
		{"", "Quick win", "feedback_quick_win"},
		// Punctuation squeezed.
		{"reference", "PAI-342: Don't break the link!", "reference_pai_342_don_t_break_the"},
	}
	for _, tt := range tests {
		got := handlers.SuggestMemorySlug(tt.typ, tt.rule)
		if got != tt.want {
			t.Errorf("SuggestMemorySlug(%q, %q) = %q, want %q", tt.typ, tt.rule, got, tt.want)
		}
	}
}
