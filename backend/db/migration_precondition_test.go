package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// openIssuesPreconditionDB creates a minimal issues table sufficient for the
// (project_id, issue_number) duplicate precondition (PAI-576). It deliberately
// omits the unique index so the test can seed the very duplicates migration
// 113 would reject.
func openIssuesPreconditionDB(t *testing.T) *sql.Conn {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "precondition.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE issues (
		id           INTEGER PRIMARY KEY,
		project_id   INTEGER,
		issue_number INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create issues: %v", err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func seedIssue(t *testing.T, conn *sql.Conn, id int, projectID *int, number int) {
	t.Helper()
	var pid any
	if projectID != nil {
		pid = *projectID
	}
	if _, err := conn.ExecContext(context.Background(),
		`INSERT INTO issues (id, project_id, issue_number) VALUES (?, ?, ?)`, id, pid, number); err != nil {
		t.Fatalf("seed issue %d: %v", id, err)
	}
}

func intptr(v int) *int { return &v }

func TestCheckNoDuplicateIssueNumbersPassesWhenUnique(t *testing.T) {
	conn := openIssuesPreconditionDB(t)
	seedIssue(t, conn, 1, intptr(7), 717)
	seedIssue(t, conn, 2, intptr(7), 718)
	seedIssue(t, conn, 3, intptr(8), 717) // same number, different project — fine

	if err := checkNoDuplicateIssueNumbers(context.Background(), conn); err != nil {
		t.Fatalf("expected no error for unique data, got: %v", err)
	}
}

func TestCheckNoDuplicateIssueNumbersIgnoresNullProjectMarkers(t *testing.T) {
	conn := openIssuesPreconditionDB(t)
	// Sprint markers: project_id NULL, issue_number 0. SQLite treats NULLs as
	// distinct in the partial unique index, so these are NOT collisions even
	// though a naive GROUP BY would flag them.
	seedIssue(t, conn, 1, nil, 0)
	seedIssue(t, conn, 2, nil, 0)
	seedIssue(t, conn, 3, nil, 0)

	if err := checkNoDuplicateIssueNumbers(context.Background(), conn); err != nil {
		t.Fatalf("NULL project_id markers must not trip the check, got: %v", err)
	}
}

func TestCheckNoDuplicateIssueNumbersFailsWithOffendingRows(t *testing.T) {
	conn := openIssuesPreconditionDB(t)
	// The genuine legacy-instance collision: project 7 had two issues both numbered #717.
	seedIssue(t, conn, 3055, intptr(7), 717)
	seedIssue(t, conn, 3056, intptr(7), 717)
	seedIssue(t, conn, 9, nil, 0) // NULL marker present too — must be excluded

	err := checkNoDuplicateIssueNumbers(context.Background(), conn)
	if err == nil {
		t.Fatal("expected duplicate (project_id, issue_number) to be rejected")
	}
	msg := err.Error()
	for _, want := range []string{"project_id=7", "issue_number=717", "3055", "3056"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should name the offending row %q; got: %s", want, msg)
		}
	}
}
