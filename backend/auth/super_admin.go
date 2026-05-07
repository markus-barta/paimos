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

// PAI-335 — super-admin gate.
//
// Orthogonal to the role enum (admin / member / external). Currently
// gates one capability: "write a time entry on behalf of another
// user". Every code path that wants to ask the question goes through
// IsSuperAdmin so a future swap to a proper role + capability table
// (PAI-336) only changes one place.
//
// The flag is set at the database level (M92 backfills `mba` to 1).
// There is intentionally no admin UI to grant / revoke today —
// keeping the surface tiny is the whole point of shipping this as a
// boolean rather than a fourth role.

package auth

import (
	"net/http"

	"github.com/markus-barta/paimos/backend/models"
)

// IsSuperAdmin reports whether the given user is a super-admin.
// Returns false for nil users so callers can safely pass GetUser(r)
// without an explicit nil check.
func IsSuperAdmin(u *models.User) bool {
	if u == nil {
		return false
	}
	return u.IsSuperAdmin
}

// IsSuperAdminRequest is the convenience form for handlers that hold
// the *http.Request rather than the resolved user. Same nil-safety.
func IsSuperAdminRequest(r *http.Request) bool {
	return IsSuperAdmin(GetUser(r))
}

// RequireSuperAdmin gates a route to super-admin callers only. Any
// non-super-admin (including regular admins) gets 403. Today no route
// uses this directly — time-entry handlers do their own per-call
// branch because the gate is conditional on `body.user_id != caller`,
// not on the route itself. The middleware exists so the next
// super-admin-only endpoint doesn't have to reinvent the check.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsSuperAdminRequest(r) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
