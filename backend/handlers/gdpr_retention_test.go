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

package handlers

import (
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// TestRetentionSweepAccessAudit guards the access_audit retention sweep, which
// referenced a non-existent `occurred_at` column (the table has created_at) and
// errored out every run with "no such column: occurred_at" — silently never
// purging old audit rows. Asserts the sweep deletes rows past the policy window
// and keeps recent ones (and, implicitly, that the SQL is valid against the
// schema).
func TestRetentionSweepAccessAudit(t *testing.T) {
	defer withTempDB(t)()
	t.Setenv("PAIMOS_RETENTION_DAYS_ACCESS_AUDIT", "30")

	if _, err := db.DB.Exec(
		`INSERT INTO access_audit(project_id, user_id, actor_id, action, created_at)
		 VALUES(NULL, NULL, NULL, 'grant', datetime('now','-90 days'))`); err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	if _, err := db.DB.Exec(
		`INSERT INTO access_audit(project_id, user_id, actor_id, action)
		 VALUES(NULL, NULL, NULL, 'grant')`); err != nil {
		t.Fatalf("seed recent row: %v", err)
	}

	runRetentionSweep()

	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM access_audit`).Scan(&n); err != nil {
		t.Fatalf("count access_audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("access_audit rows after sweep = %d, want 1 (90-day-old purged, recent kept)", n)
	}
}
