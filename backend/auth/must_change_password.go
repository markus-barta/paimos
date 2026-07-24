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

// PAI-321 — first-login password-change gate.
//
// New users created with `must_change_password=true` (the default
// from the admin form) are blocked from every authed endpoint except
// the small allowlist below, until they call POST /api/auth/password
// to set a new one. The block is uniform: same code path catches
// session-cookie callers AND API-key callers, so a freshly-minted
// account cannot bypass the gate by jumping straight to a key.
//
// Allowlist:
//   - GET  /api/auth/me        — the SPA needs it to render the
//                                first-login screen with the user's
//                                name/role context.
//   - POST /api/auth/password  — the change-password endpoint itself,
//                                which clears the flag.
//   - POST /api/auth/logout    — escape hatch in case the user
//                                changes their mind.
//   - POST /api/auth/impersonation/end — escape hatch for a
//                                super-admin acting as a gated user.
//
// Anything else returns 403 Problem Details with
// `code:"must_change_password"`. A compatibility `error` alias stays
// in the body while older clients finish migrating.

package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/inspr-at/paimos/backend/db"
)

// mustChangeAllowedPaths are the API paths that remain reachable
// while a user has must_change_password = 1. Compared with
// strings.HasPrefix so a future /api/auth/password/v2 (etc.) keeps
// working without a code change.
var mustChangeAllowedPaths = []string{
	"/api/auth/me",
	"/api/auth/password",
	"/api/auth/logout",
	"/api/auth/impersonation/end",
}

func mustChangePathAllowed(p string) bool {
	for _, allowed := range mustChangeAllowedPaths {
		if p == allowed || strings.HasPrefix(p, allowed+"/") {
			return true
		}
	}
	return false
}

// userMustChangePassword reads the flag for the given user. Returns
// false on any DB error so a backend hiccup never locks an admin out
// of fixing the situation. The flag is rare-write / rare-read; the
// extra query is cheap.
func userMustChangePassword(userID int64) bool {
	if userID <= 0 {
		return false
	}
	var v int
	if err := db.DB.QueryRow(
		"SELECT must_change_password FROM users WHERE id=?", userID,
	).Scan(&v); err != nil {
		return false
	}
	return v != 0
}

// MustChangePasswordGate runs after Middleware (so the user is
// already attached to context) and before any route-specific
// middleware. Mounted on the same group as Middleware in main.go +
// the test harness.
//
// Returning 403 (not 401) is deliberate: the user IS authenticated;
// we are just refusing to let them act until they rotate the
// admin-set password. The SPA distinguishes the two.
func MustChangePasswordGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip allowlist paths early — no DB hit on the hot endpoints.
		if mustChangePathAllowed(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		user := GetUser(r)
		if user == nil {
			// Not authenticated — Middleware already returned 401, so
			// this code path is reached only when the gate is mounted
			// on a public route by mistake. Defensive log.
			log.Printf("MustChangePasswordGate: no user on %s — gate misconfigured", r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}
		if userMustChangePassword(user.ID) {
			writeMustChangePasswordProblem(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeMustChangePasswordProblem(w http.ResponseWriter, r *http.Request) {
	const code = "must_change_password"
	reqID := strings.TrimSpace(w.Header().Get("X-PAIMOS-Request-Id"))
	if reqID == "" {
		reqID = strings.TrimSpace(r.Header.Get("X-PAIMOS-Request-Id"))
	}
	body := map[string]any{
		"type":       "https://paimos.com/errors/" + code,
		"title":      "Password change required",
		"status":     http.StatusForbidden,
		"detail":     "password change required before continuing",
		"instance":   r.URL.RequestURI(),
		"code":       code,
		"error":      code,
		"request_id": reqID,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(body)
}

// ClearMustChangePassword wipes the flag for a user. Called from
// ChangePassword on a successful update. Errors are logged, not
// surfaced — leaving the flag set after a successful change would
// trap the user in the gate forever, but the password update itself
// has already succeeded; the flag will clear on the next attempt
// from the user (since current-password now matches the new one,
// the second call still works).
func ClearMustChangePassword(userID int64) {
	if userID <= 0 {
		return
	}
	if _, err := db.DB.Exec(
		"UPDATE users SET must_change_password = 0 WHERE id = ?", userID,
	); err != nil {
		log.Printf("ClearMustChangePassword: user_id=%d: %v", userID, err)
	}
}
