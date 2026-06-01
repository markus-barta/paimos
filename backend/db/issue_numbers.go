// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package db

import (
	"context"
	"database/sql"
	"fmt"
)

// NextIssueNumber atomically reserves the next project-scoped issue
// number inside tx. The counter row is the serialization point; callers
// must use the returned number for an INSERT in the same transaction.
func NextIssueNumber(ctx context.Context, tx *sql.Tx, projectID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		INSERT INTO project_issue_counters(project_id, next_number)
		VALUES(?, (SELECT COALESCE(MAX(issue_number), 0) + 2 FROM issues WHERE project_id = ?))
		ON CONFLICT(project_id) DO UPDATE
			SET next_number = project_issue_counters.next_number + 1
		RETURNING next_number - 1
	`, projectID, projectID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("reserve issue number: %w", err)
	}
	return n, nil
}
