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

// PAI-320 — per-user permissions_epoch counter.
//
// The Go middleware already reads role + status fresh from the users
// table on every request (loadSession's JOIN guarantees that), and
// project_members is read per-request by the access cache, so backend
// permission checks are never stale. The drift that *was* user-visible
// lived entirely in the SPA: auth.user.role and the accessibleProjects
// Map are populated from /auth/me at login and only refreshed on
// explicit triggers (5-min heartbeat, profile edits). An admin
// promoting another user wouldn't be felt until the affected user's
// next heartbeat tick.
//
// permissions_epoch is the cheap signal that fixes that. Every
// authenticated response carries `X-Permissions-Epoch: <int>`; the SPA
// tracks the last value it saw and, on a bump, re-fetches /auth/me to
// re-hydrate its access cache. Soft refresh, no re-login.
//
// Bumping is the responsibility of every endpoint that mutates a
// user's role, status, or project membership. The helper here is the
// single point of change: callers go through it, never write the
// column directly. UPDATEs are idempotent on a per-row basis, so a
// double-bump is harmless.

package auth

import (
	"log"

	"github.com/markus-barta/paimos/backend/db"
)

// BumpPermissionsEpoch increments the user's epoch counter. Failures
// log but never block the calling mutation — a missed bump only delays
// the SPA refresh until the next 5-min heartbeat, while a hard error
// here would prevent legitimate role / membership changes.
func BumpPermissionsEpoch(userID int64) {
	if userID <= 0 {
		return
	}
	if _, err := db.DB.Exec(
		"UPDATE users SET permissions_epoch = permissions_epoch + 1 WHERE id = ?",
		userID,
	); err != nil {
		log.Printf("BumpPermissionsEpoch: user_id=%d: %v", userID, err)
	}
}

// GetPermissionsEpoch reads the current value. Returns 0 on any error
// (or for a missing user) so the middleware emits a stable header even
// in degraded states; the SPA treats a missing/zero value as "no
// change to record" on the first request.
func GetPermissionsEpoch(userID int64) int64 {
	if userID <= 0 {
		return 0
	}
	var v int64
	if err := db.DB.QueryRow(
		"SELECT permissions_epoch FROM users WHERE id = ?", userID,
	).Scan(&v); err != nil {
		return 0
	}
	return v
}
