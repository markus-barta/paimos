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
	if issues1 != 20 {
		t.Errorf("first run: issues count = %d, want 20 (5 per project × 4 projects)", issues1)
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
