// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

func userID(t *testing.T, username string) int64 {
	t.Helper()
	var id int64
	if err := db.DB.QueryRow("SELECT id FROM users WHERE username=?", username).Scan(&id); err != nil {
		t.Fatalf("lookup %s: %v", username, err)
	}
	return id
}

func mustChangeFlag(t *testing.T, id int64) int {
	t.Helper()
	var v int
	db.DB.QueryRow("SELECT must_change_password FROM users WHERE id=?", id).Scan(&v)
	return v
}

func sessionCount(t *testing.T, id int64) int {
	t.Helper()
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id=?", id).Scan(&n)
	return n
}

// Admin resets another user's password via the edit dialog: the new
// password must authenticate, the old one must not, the target's live
// sessions must be dropped, and must_change_password must be set so the
// user rotates the admin-known value on next login.
func TestAdminPasswordReset_TakesEffect_KillsSessions_ForcesChange(t *testing.T) {
	ts := newTestServer(t)
	memberID := userID(t, "member")

	// Plant a second live session for the member (a "second device").
	otherSID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := time.Now().UTC()
	if _, err := db.DB.Exec(
		`INSERT INTO sessions(id, user_id, expires_at, csrf_token, via_dev_login, created_at)
		 VALUES (?, ?, ?, '', 0, ?)`,
		otherSID, memberID,
		now.Add(7*24*time.Hour).Format("2006-01-02 15:04:05"),
		now.Format("2006-01-02 15:04:05"),
	); err != nil {
		t.Fatal(err)
	}
	if sessionCount(t, memberID) == 0 {
		t.Fatal("setup: expected at least one member session")
	}

	resp := ts.put(t, "/api/users/"+itoa(memberID), ts.adminCookie, map[string]any{
		"password": "brandnewpass123",
	})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// New password authenticates.
	rNew := ts.post(t, "/api/auth/login", "", map[string]string{"username": "member", "password": "brandnewpass123"})
	assertStatus(t, rNew, http.StatusOK)
	rNew.Body.Close()

	// Old password is rejected.
	rOld := ts.post(t, "/api/auth/login", "", map[string]string{"username": "member", "password": "memberpass"})
	assertStatus(t, rOld, http.StatusUnauthorized)
	rOld.Body.Close()

	// The pre-existing "second device" session was dropped by the reset.
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id=?", otherSID).Scan(&n)
	if n != 0 {
		t.Errorf("admin reset did not invalidate the target's existing session")
	}

	// must_change_password is now set for the member.
	if mustChangeFlag(t, memberID) != 1 {
		t.Errorf("admin reset did not force a password change for the target user")
	}
}

// A new password shorter than the shared minimum is rejected — the admin
// path used to silently accept any length.
func TestAdminPasswordReset_RejectsShortPassword(t *testing.T) {
	ts := newTestServer(t)
	memberID := userID(t, "member")
	resp := ts.put(t, "/api/users/"+itoa(memberID), ts.adminCookie, map[string]any{
		"password": "short", // 5 chars < 8
	})
	assertStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()

	// The old password still works — nothing was changed.
	r := ts.post(t, "/api/auth/login", "", map[string]string{"username": "member", "password": "memberpass"})
	assertStatus(t, r, http.StatusOK)
	r.Body.Close()
}

// must_change_password=false lets an admin reset a service account's
// password without forcing the gate.
func TestAdminPasswordReset_OptOutSkipsForcedChange(t *testing.T) {
	ts := newTestServer(t)
	memberID := userID(t, "member")
	resp := ts.put(t, "/api/users/"+itoa(memberID), ts.adminCookie, map[string]any{
		"password":             "servicepass123",
		"must_change_password": false,
	})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	if mustChangeFlag(t, memberID) != 0 {
		t.Errorf("must_change_password=false should not force a change")
	}
}

// When an admin edits their OWN password, the session they are acting from
// stays alive and the must-change gate is not turned on against them.
func TestAdminPasswordReset_SelfEditKeepsCurrentSession(t *testing.T) {
	ts := newTestServer(t)
	adminID := userID(t, "admin")
	adminSID := ts.adminCookie[len("session="):]

	resp := ts.put(t, "/api/users/"+itoa(adminID), ts.adminCookie, map[string]any{
		"password": "newadminpass123",
	})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Current session survives → the admin can still call a protected route.
	r := ts.get(t, "/api/users", ts.adminCookie)
	assertStatus(t, r, http.StatusOK)
	r.Body.Close()

	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id=?", adminSID).Scan(&n)
	if n != 1 {
		t.Errorf("self password reset deleted the admin's current session")
	}
	if mustChangeFlag(t, adminID) != 0 {
		t.Errorf("self password reset should not force the admin to change again")
	}

	// New password authenticates on a fresh login.
	rNew := ts.post(t, "/api/auth/login", "", map[string]string{"username": "admin", "password": "newadminpass123"})
	assertStatus(t, rNew, http.StatusOK)
	rNew.Body.Close()
}
