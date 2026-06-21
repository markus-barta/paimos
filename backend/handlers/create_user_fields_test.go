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

	"github.com/markus-barta/paimos/backend/models"
)

// CreateUser must accept the same profile fields as UpdateUser so the
// create and edit forms are symmetric — no "create then immediately edit
// to set email/rate/locale" two-step.
func TestCreateUser_AcceptsProfileFields(t *testing.T) {
	ts := newTestServer(t)

	rate := 95.5
	resp := ts.post(t, "/api/users", ts.adminCookie, map[string]any{
		"username":             "harmonized",
		"password":             "initialpass123",
		"role":                 "member",
		"must_change_password": false,
		"nickname":             "har",
		"email":                "harmonized@example.com",
		"internal_rate_hourly": rate,
		"locale":               "de",
	})
	assertStatus(t, resp, http.StatusCreated)
	var u models.User
	decode(t, resp, &u)

	if u.Email != "harmonized@example.com" {
		t.Errorf("email not persisted at create: got %q", u.Email)
	}
	if u.Nickname != "har" {
		t.Errorf("nickname not persisted at create: got %q", u.Nickname)
	}
	if u.Locale != "de" {
		t.Errorf("locale not persisted at create: got %q", u.Locale)
	}
	if u.InternalRateHourly == nil || *u.InternalRateHourly != rate {
		t.Errorf("internal_rate_hourly not persisted at create: got %v", u.InternalRateHourly)
	}

	// must_change_password=false → the user can hit protected routes
	// immediately (no first-login gate).
	cookie := ts.login(t, "harmonized", "initialpass123")
	r := ts.get(t, "/api/projects", cookie)
	assertStatus(t, r, http.StatusOK)
	r.Body.Close()
}

// Nickname is capped at 3 runes on create, same as on update.
func TestCreateUser_RejectsLongNickname(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/users", ts.adminCookie, map[string]any{
		"username": "longnick",
		"password": "initialpass123",
		"nickname": "abcd", // 4 chars > 3
	})
	assertStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

// Default locale falls back to "en" when omitted.
func TestCreateUser_DefaultsLocale(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/users", ts.adminCookie, map[string]any{
		"username": "nolocale",
		"password": "initialpass123",
	})
	assertStatus(t, resp, http.StatusCreated)
	var u models.User
	decode(t, resp, &u)
	if u.Locale != "en" {
		t.Errorf("default locale: got %q, want en", u.Locale)
	}
}
