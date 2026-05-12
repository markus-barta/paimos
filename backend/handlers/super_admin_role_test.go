// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func TestRegression_SuperAdminRole_001_AdminCannotGrantSuperAdmin(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.post(t, "/api/users", ts.adminCookie, map[string]any{
		"username": "plain-admin-grant",
		"password": "secret123",
		"role":     "super_admin",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("regular admin super_admin create returned %d, want 403 — body: %s", resp.StatusCode, body)
	}
}

func TestRegression_SuperAdminRole_002_GrantRevokeIsCanonicalAndAudited(t *testing.T) {
	ts := newTestServer(t)
	memberID := userIDByUsername(t, "member")
	promoteToSuperAdmin(t, "admin")

	grant := ts.put(t, fmt.Sprintf("/api/users/%d", memberID), ts.adminCookie, map[string]any{
		"role": "super_admin",
	})
	body, _ := io.ReadAll(grant.Body)
	grant.Body.Close()
	if grant.StatusCode != http.StatusOK {
		t.Fatalf("super-admin grant returned %d, want 200 — body: %s", grant.StatusCode, body)
	}
	if !strings.Contains(string(body), `"role":"super_admin"`) || !strings.Contains(string(body), `"is_super_admin":true`) {
		t.Fatalf("grant response did not expose canonical super_admin role — body: %s", body)
	}

	var legacyRole, roleKey string
	var flag int
	if err := db.DB.QueryRow(`SELECT role, role_key, is_super_admin FROM users WHERE id=?`, memberID).Scan(&legacyRole, &roleKey, &flag); err != nil {
		t.Fatalf("lookup member role: %v", err)
	}
	if legacyRole != "admin" || roleKey != "super_admin" || flag != 1 {
		t.Fatalf("stored grant role/role_key/flag = %q/%q/%d, want admin/super_admin/1", legacyRole, roleKey, flag)
	}

	revoke := ts.put(t, fmt.Sprintf("/api/users/%d", memberID), ts.adminCookie, map[string]any{
		"role": "member",
	})
	body, _ = io.ReadAll(revoke.Body)
	revoke.Body.Close()
	if revoke.StatusCode != http.StatusOK {
		t.Fatalf("super-admin revoke returned %d, want 200 — body: %s", revoke.StatusCode, body)
	}
	if !strings.Contains(string(body), `"role":"member"`) || !strings.Contains(string(body), `"is_super_admin":false`) {
		t.Fatalf("revoke response did not expose canonical member role — body: %s", body)
	}

	var auditCount int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*) FROM super_admin_audit
		WHERE target_user_id=? AND capability='users.grant_super_admin'
	`, memberID).Scan(&auditCount); err != nil {
		t.Fatalf("count role audit: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("role grant/revoke audit count = %d, want 2", auditCount)
	}
}
