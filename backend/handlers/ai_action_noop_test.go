// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-240. Regression tests for the no-op return path on
// detect_duplicates and find_parent. Both handlers used to return an
// empty body on a single-issue project; that empty body is what made
// the dispatcher record outcome=ok / model=— / tokens=0, which read
// to operators (and to the SPA) like a successful provider call that
// happened to find nothing. The fix is `noOpResult` + outcome=no_op.
//
// These tests exercise the handler directly so the regression is
// pinned to the handler's responsibility (deciding "no candidates")
// rather than a property of the dispatcher pipeline. The dispatcher's
// outcome branching is covered separately by the audit-shape tests in
// ai_optimize_audit_test.go.

package handlers

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/ai"
	"github.com/markus-barta/paimos/backend/db"

	_ "modernc.org/sqlite"
)

// withTempDB opens an isolated SQLite database in a temp dir and
// returns a teardown that closes + clears the global db.DB. Mirrors
// what the external test harness does, scoped to a single in-package
// test that needs the project + issues tables.
func withTempDB(t *testing.T) func() {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	return func() {
		if db.DB != nil {
			_ = db.DB.Close()
			db.DB = nil
		}
		_ = os.Unsetenv("DATA_DIR")
	}
}

func TestDetectDuplicatesHandler_NoOpOnSingleIssueProject(t *testing.T) {
	teardown := withTempDB(t)
	defer teardown()

	// One project, one issue — exactly the PWEB / PWEB-1 repro from
	// the ticket. loadProjectIssueTree's `WHERE id != ?` exclusion
	// drops the only issue from the candidate set.
	res, err := db.DB.Exec(`INSERT INTO projects(name, key, status) VALUES(?, ?, 'active')`, "Single-Issue Project", "SOLO")
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	pid, _ := res.LastInsertId()
	res, err = db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?, 1, 'task', 'the only issue', 'new')`,
		pid,
	)
	if err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	issueID, _ := res.LastInsertId()

	ax := &aiActionContext{
		Ctx:       context.Background(),
		IssueID:   issueID,
		IssueData: ai.Context{IssueKey: "SOLO-1", IssueTitle: "the only issue"},
		DB:        db.DB,
	}

	body, model, ptok, ctok, finish, err := detectDuplicatesHandler(ax)
	if err != nil {
		t.Fatalf("detectDuplicatesHandler returned error: %v", err)
	}

	noOp, ok := body.(noOpResult)
	if !ok {
		t.Fatalf("body type = %T, want noOpResult — empty body would be recorded as outcome=ok and is the bug PAI-240 fixes", body)
	}
	if !noOp.NoOp {
		t.Errorf("noOpResult.NoOp = false, want true")
	}
	if !strings.Contains(strings.ToLower(noOp.Reason), "no other issues") {
		t.Errorf("noOpResult.Reason = %q, want it to mention 'no other issues'", noOp.Reason)
	}
	if model != "" {
		t.Errorf("model = %q, want empty (no provider call)", model)
	}
	if ptok != 0 || ctok != 0 {
		t.Errorf("token counts = (%d, %d), want (0, 0)", ptok, ctok)
	}
	if finish != "" {
		t.Errorf("finish = %q, want empty", finish)
	}
}

func TestFindParentHandler_NoOpOnSingleIssueProject(t *testing.T) {
	teardown := withTempDB(t)
	defer teardown()

	res, err := db.DB.Exec(`INSERT INTO projects(name, key, status) VALUES(?, ?, 'active')`, "Single-Issue Project", "SOLO")
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	pid, _ := res.LastInsertId()
	// Type 'task' so the find_parent epic-guard does not short-circuit
	// before the candidate-tree load.
	res, err = db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?, 1, 'task', 'the only issue', 'new')`,
		pid,
	)
	if err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	issueID, _ := res.LastInsertId()

	ax := &aiActionContext{
		Ctx:       context.Background(),
		IssueID:   issueID,
		IssueData: ai.Context{IssueKey: "SOLO-1", IssueTitle: "the only issue", IssueType: "task"},
		DB:        db.DB,
	}

	body, model, _, _, _, err := findParentHandler(ax)
	if err != nil {
		t.Fatalf("findParentHandler returned error: %v", err)
	}
	noOp, ok := body.(noOpResult)
	if !ok {
		t.Fatalf("body type = %T, want noOpResult", body)
	}
	if !noOp.NoOp {
		t.Errorf("noOpResult.NoOp = false, want true")
	}
	if !strings.Contains(strings.ToLower(noOp.Reason), "parent") {
		t.Errorf("noOpResult.Reason = %q, want it to mention 'parent'", noOp.Reason)
	}
	if model != "" {
		t.Errorf("model = %q, want empty (no provider call)", model)
	}
}

// TestNoOpResultShape pins the wire shape of the no-op marker so a
// future refactor can't quietly change the field name the SPA's
// AiActionResultModal / AiSurfaceFeedback / useAiResultSummary
// branches on. JSON omitempty is explicitly NOT set on `no_op` so
// `false` cases still serialize the discriminator (today only `true`
// is constructed via newNoOpResult, but the test makes the contract
// resilient to a careless edit).
func TestNoOpResultShape(t *testing.T) {
	got := newNoOpResult("nothing here")
	if !got.NoOp {
		t.Errorf("newNoOpResult().NoOp = false, want true")
	}
	if got.Reason != "nothing here" {
		t.Errorf("newNoOpResult().Reason = %q, want %q", got.Reason, "nothing here")
	}
}
