// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// PAI-335 — regression tests for cross-user time-entry writes.
//
// The default test seed (admin / member / external) does NOT include
// a super-admin. M92 only flips `is_super_admin = 1` for username
// 'mba'. Each test below promotes the relevant fixture inline so the
// test stays self-contained — no shared setup across tests.

package handlers_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// promoteToSuperAdmin sets is_super_admin=1 for the given username.
// Called inline by tests that need the gate to allow.
func promoteToSuperAdmin(t *testing.T, username string) {
	t.Helper()
	if _, err := db.DB.Exec(`UPDATE users SET role='admin', role_key='super_admin', is_super_admin=1 WHERE username=?`, username); err != nil {
		t.Fatalf("promote %s: %v", username, err)
	}
}

func userIDByUsername(t *testing.T, username string) int64 {
	t.Helper()
	var id int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		t.Fatalf("lookup user %s: %v", username, err)
	}
	return id
}

// seedTestProjectAndIssue creates a project + issue an admin owns,
// returns the issue id. Members default to editor on internal
// projects so the member cookie can post time entries against it.
func seedTestProjectAndIssue(t *testing.T, ts *testServer) int64 {
	t.Helper()
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Super-Admin Time Entry Project",
		"key":  "SATE",
	}))
	return responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "Issue for time-entry tests",
		"type":  "task",
	}))
}

// TestRegression_SuperAdmin_001_NonSuperAdminCannotCrossUser asserts
// the gate's default-deny posture: a regular member can't write a
// time entry against another user even by passing the field.
func TestRegression_SuperAdmin_001_NonSuperAdminCannotCrossUser(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)
	adminID := userIDByUsername(t, "admin")

	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie, map[string]any{
		"started_at": "2026-05-01T09:00:00Z",
		"user_id":    adminID, // forge an entry on admin's behalf
	})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("INV-SA-001 violated: member with user_id=admin returned %d, want 403 — body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "super-admin") {
		t.Errorf("INV-SA-001 violated: 403 body should mention super-admin, got: %s", body)
	}
}

// TestRegression_SuperAdmin_002_AdminAloneCannotCrossUser asserts
// that the existing role check (`Role == 'admin'`) does NOT subsume
// super-admin — an admin who isn't promoted is still not allowed to
// create time entries on other users' behalf. Today's admins picked
// up no new powers; super-admin is the strictly narrower gate.
func TestRegression_SuperAdmin_002_AdminAloneCannotCrossUser(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)
	memberID := userIDByUsername(t, "member")

	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-01T09:00:00Z",
		"user_id":    memberID,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("INV-SA-002 violated: admin (not super-admin) cross-user create returned %d, want 403", resp.StatusCode)
	}
}

// TestRegression_SuperAdmin_003_SuperAdminCanCrossUser asserts the
// happy path: promote the admin to super-admin, then they can create
// a time entry on member's behalf and the row's user_id is the
// MEMBER's, not the super-admin's.
func TestRegression_SuperAdmin_003_SuperAdminCanCrossUser(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)
	memberID := userIDByUsername(t, "member")
	promoteToSuperAdmin(t, "admin")

	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-01T09:00:00Z",
		"stopped_at": "2026-05-01T11:00:00Z",
		"user_id":    memberID,
		"comment":    "PAI-335 cross-user create",
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("INV-SA-003 violated: super-admin cross-user create returned %d, want 201 — body: %s", resp.StatusCode, body)
	}
	id := responseID(t, resp)

	// The row's user_id MUST be the member's, not the super-admin's —
	// otherwise the super-admin would silently shadow worker hours.
	var owner int64
	if err := db.DB.QueryRow(`SELECT user_id FROM time_entries WHERE id=?`, id).Scan(&owner); err != nil {
		t.Fatalf("lookup row: %v", err)
	}
	if owner != memberID {
		t.Errorf("INV-SA-003 violated: time_entries.user_id = %d, want member id %d", owner, memberID)
	}
}

// TestRegression_SuperAdmin_004_RetrospectiveBackdate asserts that a
// super-admin can create a far-back-dated entry on someone else's
// behalf — the central use case of "I forgot to log my hours last
// week, can you add them for me?".
func TestRegression_SuperAdmin_004_RetrospectiveBackdate(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)
	memberID := userIDByUsername(t, "member")
	promoteToSuperAdmin(t, "admin")

	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2025-12-01T09:00:00Z", // five months ago
		"stopped_at": "2025-12-01T17:00:00Z",
		"user_id":    memberID,
		"comment":    "retrospective fill-in",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("INV-SA-004 violated: backdated cross-user create returned %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestRegression_SuperAdmin_005_OmittedUserIDStillSelf asserts that
// a super-admin who omits user_id still creates the entry against
// themselves — the cross-user code path is opt-in, not on by
// default. Otherwise the super-admin's normal click-to-start-timer
// flow would silently start writing as someone else after a stale
// form state.
func TestRegression_SuperAdmin_005_OmittedUserIDStillSelf(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)
	adminID := userIDByUsername(t, "admin")
	promoteToSuperAdmin(t, "admin")

	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-01T09:00:00Z",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("INV-SA-005 violated: super-admin self-create returned %d, want 201", resp.StatusCode)
	}
	id := responseID(t, resp)
	var owner int64
	if err := db.DB.QueryRow(`SELECT user_id FROM time_entries WHERE id=?`, id).Scan(&owner); err != nil {
		t.Fatalf("lookup row: %v", err)
	}
	if owner != adminID {
		t.Errorf("INV-SA-005 violated: omitted user_id should mean self (admin id %d), got %d", adminID, owner)
	}
}

// TestRegression_SuperAdmin_006_AuditMarkerOnMeResponse asserts the
// flag is exposed via /auth/me so the SPA can branch on it (show
// the user picker only for super-admins). Without this, the picker
// either has to be requested separately or always-shown.
func TestRegression_SuperAdmin_006_AuditMarkerOnMeResponse(t *testing.T) {
	ts := newTestServer(t)
	promoteToSuperAdmin(t, "admin")

	resp := ts.get(t, "/api/auth/me", ts.adminCookie)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/auth/me returned %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"is_super_admin":true`) {
		t.Errorf("INV-SA-006 violated: /auth/me missing is_super_admin=true after promote — body: %s", body)
	}

	// And member (unpromoted) should see false.
	resp = ts.get(t, "/api/auth/me", ts.memberCookie)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"is_super_admin":false`) {
		t.Errorf("INV-SA-006 violated: /auth/me unpromoted member missing is_super_admin=false — body: %s", body)
	}
}

func TestRegression_SuperAdmin_007_CrossUserCreateWritesQueryableAudit(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)
	memberID := userIDByUsername(t, "member")
	promoteToSuperAdmin(t, "admin")

	resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]any{
		"started_at": "2026-05-01T09:00:00Z",
		"stopped_at": "2026-05-01T10:00:00Z",
		"user_id":    memberID,
		"comment":    "audited cross-user create",
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("cross-user create returned %d, want 201 — body: %s", resp.StatusCode, body)
	}
	entryID := responseID(t, resp)

	var actorID, targetID int64
	var capability, details string
	if err := db.DB.QueryRow(`
		SELECT actor_user_id, target_user_id, capability, details_json
		FROM super_admin_audit
		WHERE capability='time_entries.write_any_user'
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&actorID, &targetID, &capability, &details); err != nil {
		t.Fatalf("lookup super_admin_audit: %v", err)
	}
	if actorID != userIDByUsername(t, "admin") || targetID != memberID {
		t.Fatalf("audit actor/target = %d/%d, want admin/member %d/%d", actorID, targetID, userIDByUsername(t, "admin"), memberID)
	}
	if capability != "time_entries.write_any_user" || !strings.Contains(details, fmt.Sprintf(`"time_entry_id":%d`, entryID)) {
		t.Fatalf("audit row missing capability/details: capability=%q details=%s", capability, details)
	}

	list := ts.get(t, "/api/super-admin-activity?limit=10", ts.adminCookie)
	defer list.Body.Close()
	body, _ := io.ReadAll(list.Body)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("super-admin activity returned %d, want 200 — body: %s", list.StatusCode, body)
	}
	if !strings.Contains(string(body), `"capability":"time_entries.write_any_user"`) {
		t.Fatalf("activity feed missing audit capability — body: %s", body)
	}
}

func TestRegression_SuperAdmin_008_AdminAloneCannotCrossUserUpdateOrDelete(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)

	create := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie, map[string]any{
		"started_at": "2026-05-01T09:00:00Z",
		"stopped_at": "2026-05-01T10:00:00Z",
		"comment":    "member-owned",
	})
	if create.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(create.Body)
		create.Body.Close()
		t.Fatalf("member create returned %d, want 201 — body: %s", create.StatusCode, body)
	}
	entryID := responseID(t, create)

	update := ts.put(t, fmt.Sprintf("/api/time-entries/%d", entryID), ts.adminCookie, map[string]any{
		"comment": "admin tries to edit another user's entry",
	})
	update.Body.Close()
	if update.StatusCode != http.StatusForbidden {
		t.Fatalf("admin cross-user update returned %d, want 403", update.StatusCode)
	}

	del := ts.del(t, fmt.Sprintf("/api/time-entries/%d", entryID), ts.adminCookie)
	del.Body.Close()
	if del.StatusCode != http.StatusForbidden {
		t.Fatalf("admin cross-user delete returned %d, want 403", del.StatusCode)
	}
}
