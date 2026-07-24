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
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/models"
)

type impersonationMeResponse struct {
	User          models.User `json:"user"`
	Impersonation *struct {
		Active bool        `json:"active"`
		Actor  models.User `json:"actor"`
		Target models.User `json:"target"`
	} `json:"impersonation"`
}

func decodeImpersonationMe(t *testing.T, resp *http.Response) impersonationMeResponse {
	t.Helper()
	defer resp.Body.Close()
	var body impersonationMeResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /auth/me: %v", err)
	}
	return body
}

func TestRegression_Impersonation_001_SessionFramingAndAudit(t *testing.T) {
	ts := newTestServer(t)
	memberID := userIDByUsername(t, "member")
	promoteToSuperAdmin(t, "admin")

	resp := ts.post(t, "/api/auth/impersonation/start", ts.memberCookie, map[string]any{"user_id": memberID})
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("member start impersonation returned %d, want 403 — body: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = ts.post(t, "/api/auth/impersonation/start", ts.adminCookie, map[string]any{"user_id": memberID})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("super-admin start impersonation returned %d, want 200 — body: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	me := decodeImpersonationMe(t, ts.get(t, "/api/auth/me", ts.adminCookie))
	if me.User.Username != "member" || me.User.Role != auth.RoleMember {
		t.Fatalf("effective user = %s/%s, want member/member", me.User.Username, me.User.Role)
	}
	if me.Impersonation == nil || !me.Impersonation.Active {
		t.Fatalf("/auth/me missing active impersonation frame: %+v", me.Impersonation)
	}
	if me.Impersonation.Actor.Username != "admin" || me.Impersonation.Target.Username != "member" {
		t.Fatalf("impersonation frame actor/target = %s/%s, want admin/member", me.Impersonation.Actor.Username, me.Impersonation.Target.Username)
	}

	forbidden := ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Should not be created while effective member",
		"key":  "IMP",
	})
	if forbidden.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(forbidden.Body)
		forbidden.Body.Close()
		t.Fatalf("impersonated member project create returned %d, want 403 — body: %s", forbidden.StatusCode, body)
	}
	forbidden.Body.Close()

	resp = ts.post(t, "/api/auth/impersonation/end", ts.adminCookie, map[string]any{})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("end impersonation returned %d, want 200 — body: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	me = decodeImpersonationMe(t, ts.get(t, "/api/auth/me", ts.adminCookie))
	if me.User.Username != "admin" || me.User.Role != auth.RoleSuperAdmin {
		t.Fatalf("restored user = %s/%s, want admin/super_admin", me.User.Username, me.User.Role)
	}
	if me.Impersonation != nil {
		t.Fatalf("impersonation frame should clear after end: %+v", me.Impersonation)
	}

	assertAuditCount(t, auth.CapabilityImpersonationStart, memberID, 1)
	assertAuditCount(t, auth.CapabilityImpersonationEnd, memberID, 1)
	assertAuditCount(t, auth.CapabilityImpersonationAction, memberID, 1)
}

func assertAuditCount(t *testing.T, capability string, targetID int64, want int) {
	t.Helper()
	var got int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*)
		FROM super_admin_audit a
		JOIN users actor ON actor.id = a.actor_user_id
		WHERE actor.username = 'admin'
		  AND a.target_user_id = ?
		  AND a.capability = ?
	`, targetID, capability).Scan(&got); err != nil {
		t.Fatalf("count audit capability=%s: %v", capability, err)
	}
	if got != want {
		t.Fatalf("audit count capability=%s target_id=%d = %d, want %d", capability, targetID, got, want)
	}
}
