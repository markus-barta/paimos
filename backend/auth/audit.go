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

package auth

import (
	"context"
	"database/sql"
	"log"

	"github.com/markus-barta/paimos/backend/db"
)

// Access-audit action names. Kept in one place so callers cannot typo them.
const (
	AuditActionGrant  = "grant"
	AuditActionUpdate = "update"
	AuditActionRevoke = "revoke"
)

// RecordAccessChange appends a row to access_audit. When tx is non-nil the
// insert runs inside it (caller controls commit); otherwise it runs
// directly against db.DB. Failures are logged but not propagated — the
// calling permission change is the authoritative operation and must not
// fail just because audit logging did.
func RecordAccessChange(
	ctx context.Context,
	tx *sql.Tx,
	projectID, userID int64,
	action string,
	oldLevel, newLevel AccessLevel,
	actorID int64,
) {
	const stmt = `
		INSERT INTO access_audit
			(project_id, user_id, actor_id, action, old_level, new_level)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	var err error
	if tx != nil {
		_, err = tx.ExecContext(ctx, stmt, projectID, userID, actorID, action, string(oldLevel), string(newLevel))
	} else {
		_, err = db.DB.ExecContext(ctx, stmt, projectID, userID, actorID, action, string(oldLevel), string(newLevel))
	}
	if err != nil {
		log.Printf("access audit: record %s project=%d user=%d actor=%d: %v", action, projectID, userID, actorID, err)
	}
}
