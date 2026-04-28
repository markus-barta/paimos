// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build dev_login

package devseed_test

import (
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/devseed"
)

// TestRun_Idempotency pins the PAI-267 contract: re-running dev-seed
// is safe and never grows the row counts past the initial set. This
// is the property the `just dev-up` recipe relies on — boot of an
// existing dev environment must not mint duplicate fixtures.
func TestRun_Idempotency(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			db.DB.Close()
			db.DB = nil
		}
	})

	// First seed
	if err := devseed.Run(); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	users1 := count(t, "SELECT COUNT(*) FROM users WHERE username LIKE 'dev_%'")
	projects1 := count(t, "SELECT COUNT(*) FROM projects WHERE key IN ('PAIT','ACME','BUGZ','LOGS')")
	memberships1 := count(t, "SELECT COUNT(*) FROM project_members WHERE user_id IN (9001,9002,9003,9004)")
	issues1 := count(t, "SELECT COUNT(*) FROM issues WHERE project_id IN (SELECT id FROM projects WHERE key IN ('PAIT','ACME','BUGZ','LOGS'))")

	if users1 != 4 {
		t.Errorf("first run: users count = %d, want 4", users1)
	}
	if projects1 != 4 {
		t.Errorf("first run: projects count = %d, want 4", projects1)
	}
	// PAI-269: phase-1 + phase-2 totals.
	//   PAIT  =   5  (phase-1 only — no rich seed)
	//   ACME  =  33  (phase-1's 5 + 3 sprints + 25 rich tickets)
	//   BUGZ  = 100  (phase-2 fills to 100 regardless of phase-1 floor)
	//   LOGS  =  10  (phase-2 fills to 10)
	const wantIssues = 148
	if issues1 != wantIssues {
		t.Errorf("first run: issues count = %d, want %d (PAIT 5 + ACME 33 + BUGZ 100 + LOGS 10)", issues1, wantIssues)
	}

	// Second seed — must be a no-op
	if err := devseed.Run(); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	users2 := count(t, "SELECT COUNT(*) FROM users WHERE username LIKE 'dev_%'")
	projects2 := count(t, "SELECT COUNT(*) FROM projects WHERE key IN ('PAIT','ACME','BUGZ','LOGS')")
	memberships2 := count(t, "SELECT COUNT(*) FROM project_members WHERE user_id IN (9001,9002,9003,9004)")
	issues2 := count(t, "SELECT COUNT(*) FROM issues WHERE project_id IN (SELECT id FROM projects WHERE key IN ('PAIT','ACME','BUGZ','LOGS'))")

	if users2 != users1 {
		t.Errorf("re-run grew users: %d → %d", users1, users2)
	}
	if projects2 != projects1 {
		t.Errorf("re-run grew projects: %d → %d", projects1, projects2)
	}
	if memberships2 != memberships1 {
		t.Errorf("re-run grew memberships: %d → %d", memberships1, memberships2)
	}
	if issues2 != issues1 {
		t.Errorf("re-run grew issues: %d → %d", issues1, issues2)
	}
}

// TestRun_PinnedUserIDs pins the playwright contract: dev_admin /
// dev_editor / dev_viewer / dev_outsider get ids 9001–9004 in that
// order so test selectors can refer to them stably across machines.
func TestRun_PinnedUserIDs(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			db.DB.Close()
			db.DB = nil
		}
	})
	if err := devseed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cases := []struct {
		username string
		wantID   int64
		wantRole string
	}{
		{"dev_admin", 9001, "admin"},
		{"dev_editor", 9002, "member"},
		{"dev_viewer", 9003, "member"},
		{"dev_outsider", 9004, "external"},
	}
	for _, c := range cases {
		var id int64
		var role string
		if err := db.DB.QueryRow("SELECT id, role FROM users WHERE username=?", c.username).Scan(&id, &role); err != nil {
			t.Errorf("%s: %v", c.username, err)
			continue
		}
		if id != c.wantID {
			t.Errorf("%s: id = %d, want %d", c.username, id, c.wantID)
		}
		if role != c.wantRole {
			t.Errorf("%s: role = %q, want %q", c.username, role, c.wantRole)
		}
	}
}

// TestRun_PasswordsAreEmpty pins the security invariant: dev users
// MUST have an empty password column so the normal login form's
// bcrypt compare fails. The dev-login route (token-protected) is the
// only valid way in.
func TestRun_PasswordsAreEmpty(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			db.DB.Close()
			db.DB = nil
		}
	})
	if err := devseed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rows, err := db.DB.Query("SELECT username, password FROM users WHERE username LIKE 'dev_%'")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, pw string
		if err := rows.Scan(&name, &pw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if pw != "" {
			t.Errorf("%s has non-empty password (%q) — normal login form must not be able to authenticate dev users", name, pw)
		}
	}
}

func count(t *testing.T, query string) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// TestRun_RichFixtures pins the PAI-269 phase-2 surface assertions:
// ACME has 3 sprints + time entries; BUGZ has soft-deleted rows +
// depends_on / blocks relations; LOGS has comments. These are the
// signature rows the dev-up walkthrough relies on — without them the
// reporting / trash / relation / comment surfaces have nothing to
// render.
func TestRun_RichFixtures(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			db.DB.Close()
			db.DB = nil
		}
	})
	if err := devseed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// ACME — 3 sprint-typed issues.
	acmeSprints := count(t, `
		SELECT COUNT(*) FROM issues
		WHERE type='sprint' AND project_id=(SELECT id FROM projects WHERE key='ACME')
	`)
	if acmeSprints != 3 {
		t.Errorf("ACME sprints: got %d, want 3", acmeSprints)
	}

	// ACME — at least one time entry on a project issue. Reporting +
	// billing surfaces need this to render anything meaningful.
	acmeTimeEntries := count(t, `
		SELECT COUNT(*) FROM time_entries
		WHERE issue_id IN (SELECT id FROM issues WHERE project_id=(SELECT id FROM projects WHERE key='ACME'))
	`)
	if acmeTimeEntries < 15 {
		t.Errorf("ACME time entries: got %d, want at least 15 (2-3 entries × 15 tickets)", acmeTimeEntries)
	}

	// BUGZ — at least 5 soft-deleted issues for the trash + restore flow.
	bugzDeleted := count(t, `
		SELECT COUNT(*) FROM issues
		WHERE deleted_at IS NOT NULL AND project_id=(SELECT id FROM projects WHERE key='BUGZ')
	`)
	if bugzDeleted < 5 {
		t.Errorf("BUGZ soft-deleted: got %d, want at least 5", bugzDeleted)
	}

	// BUGZ — depends_on + blocks relations between project issues.
	bugzRelations := count(t, `
		SELECT COUNT(*) FROM issue_relations
		WHERE type IN ('depends_on','blocks')
		  AND source_id IN (SELECT id FROM issues WHERE project_id=(SELECT id FROM projects WHERE key='BUGZ'))
	`)
	if bugzRelations < 8 {
		t.Errorf("BUGZ depends_on+blocks relations: got %d, want at least 8", bugzRelations)
	}

	// LOGS — at least 5 comments per issue × 5 newly-seeded issues.
	logsComments := count(t, `
		SELECT COUNT(*) FROM comments
		WHERE issue_id IN (SELECT id FROM issues WHERE project_id=(SELECT id FROM projects WHERE key='LOGS'))
	`)
	if logsComments < 25 {
		t.Errorf("LOGS comments: got %d, want at least 25 (5 comments × 5 phase-2 issues)", logsComments)
	}

	// LOGS issues have non-trivial markdown bodies (the seeder feeds a
	// shared multi-paragraph body — at minimum it should be longer
	// than the 5-issue phase-1 floor's empty-string default).
	var bodyMin int
	if err := db.DB.QueryRow(`
		SELECT MIN(LENGTH(description)) FROM issues
		WHERE project_id=(SELECT id FROM projects WHERE key='LOGS')
		  AND type != 'sprint'
		  AND description != ''
	`).Scan(&bodyMin); err == nil && bodyMin < 200 {
		// Phase-1 issues have empty descriptions, so we filter description != ''.
		// Among the rich-seed issues, the shared body is several hundred chars.
		t.Errorf("LOGS rich-seed body: shortest non-empty description is %d chars, want at least 200", bodyMin)
	}
}
