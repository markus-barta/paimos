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
)

// Test_PublicEndpointsAuthorization is the ACME-1 guard test:
// confirms which endpoints are reachable without a session (the tiny
// public whitelist) and that every other route shows 401 to an
// unauthenticated request.
//
// If anyone ever moves something back out of the auth group by
// accident, this test fails loudly on the next CI run.
func Test_PublicEndpointsAuthorization(t *testing.T) {
	ts := newTestServer(t)

	// These routes MUST stay public — login + health + branding-for-login.
	publicGETs := []string{
		"/api/health",
		"/api/branding",
	}
	for _, path := range publicGETs {
		resp := ts.get(t, path, "")
		if resp.StatusCode >= 400 {
			t.Errorf("public GET %s: status %d, want <400", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// These routes MUST be behind auth. A raw GET without any cookie
	// should return 401 — the auth middleware rejects before the handler
	// runs, so even for not-found paths (logos/avatars) we expect 401,
	// not 404.
	protectedGETs := []string{
		"/api/instance",
		"/api/brandings",
		"/api/logos/test.jpg",
		"/api/avatars/42.jpg",
		"/api/projects",
		"/api/issues",
		"/api/users",
		"/api/tags",
	}
	for _, path := range protectedGETs {
		resp := ts.get(t, path, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("protected GET %s: status %d, want 401", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// Sanity: the same protected endpoints respond normally for an
	// authed admin. 200 for data endpoints, 404 for missing files is
	// fine — the point is "not 401".
	authedProbes := []string{
		"/api/instance",
		"/api/brandings",
		"/api/projects",
	}
	for _, path := range authedProbes {
		resp := ts.get(t, path, ts.adminCookie)
		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("authed GET %s: still 401 with admin cookie — auth wiring is wrong", path)
		}
		_ = resp.Body.Close()
	}
}
