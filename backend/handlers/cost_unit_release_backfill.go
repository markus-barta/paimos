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
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/markus-barta/paimos/backend/db"
)

// EnsureCostUnitReleaseEdges backfills the PAI-599 cost_unit/release edges:
// for every distinct non-empty cost_unit/release label on a ticket, ensure a
// matching container issue of that type exists (created with a properly
// reserved issue number so it never collides with the API path), then edge
// every ticket carrying that label to the container.
//
// Idempotent and cheap once converged — safe to run at every boot (mirrors
// EnsureAtRiskTag). Migration 121 already folded existing groups + title-match
// edges in SQL; this handles the label-only strings that have no container yet,
// which require per-project numbering that can't be done safely in raw SQL.
func EnsureCostUnitReleaseEdges() {
	ensureLabelEdges("cost_unit")
	ensureLabelEdges("release")
}

// ensureLabelEdges processes one dimension. The name doubles as the issues
// column, the container issue type, and the relation edge type. Only the two
// hardcoded callers above pass it, so the value is never user-controlled.
func ensureLabelEdges(dimension string) {
	if dimension != "cost_unit" && dimension != "release" {
		return
	}
	ctx := context.Background()

	// Collect distinct (project, label) pairs that still need a container —
	// fully drained before any writes so we never hold a cursor across the
	// per-label transactions below.
	type pl struct {
		projectID int64
		label     string
	}
	rows, err := db.DB.Query(`
		SELECT DISTINCT i.project_id, i.`+dimension+`
		FROM issues i
		WHERE i.`+dimension+` != '' AND i.deleted_at IS NULL
		  AND i.type NOT IN ('cost_unit','release')
		  AND NOT EXISTS (
		      SELECT 1 FROM issues c
		      WHERE c.project_id = i.project_id AND c.type = ?
		        AND c.title = i.`+dimension+` AND c.deleted_at IS NULL)`, dimension)
	if err != nil {
		log.Printf("EnsureCostUnitReleaseEdges(%s): scan labels: %v", dimension, err)
		return
	}
	var pending []pl
	for rows.Next() {
		var p pl
		if err := rows.Scan(&p.projectID, &p.label); err != nil {
			continue
		}
		pending = append(pending, p)
	}
	rows.Close()

	for _, p := range pending {
		if err := ensureContainerAndEdges(ctx, dimension, p.projectID, p.label); err != nil {
			log.Printf("EnsureCostUnitReleaseEdges(%s): project=%d label=%q: %v", dimension, p.projectID, p.label, err)
		}
	}

	// Edge tickets to containers that now exist (covers both the just-created
	// containers and any pre-existing ones the migration didn't catch). Cheap
	// INSERT OR IGNORE; no-op once converged.
	if _, err := db.DB.Exec(`
		INSERT OR IGNORE INTO issue_relations(source_id, target_id, type)
		SELECT c.id, i.id, ?
		FROM issues i
		JOIN issues c ON c.project_id = i.project_id AND c.type = ?
		             AND c.title = i.`+dimension+` AND c.deleted_at IS NULL
		WHERE i.`+dimension+` != '' AND i.deleted_at IS NULL
		  AND i.type NOT IN ('cost_unit','release')`, dimension, dimension); err != nil {
		log.Printf("EnsureCostUnitReleaseEdges(%s): edge backfill: %v", dimension, err)
	}
}

// ensureContainerAndEdges creates the container issue for (project,label) if it
// is missing, using a reserved issue number, in a single transaction.
func ensureContainerAndEdges(ctx context.Context, dimension string, projectID int64, label string) error {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existing int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM issues WHERE project_id=? AND type=? AND title=? AND deleted_at IS NULL`,
		projectID, dimension, label).Scan(&existing)
	switch {
	case err == nil:
		return nil // already exists (created concurrently) — nothing to do
	case !errors.Is(err, sql.ErrNoRows):
		return err // a real read error — don't risk creating a duplicate
	}

	num, err := db.NextIssueNumber(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO issues(project_id, issue_number, type, title, status, priority)
		 VALUES(?,?,?,?,'backlog','medium')`, projectID, num, dimension, label); err != nil {
		return err
	}
	return tx.Commit()
}
