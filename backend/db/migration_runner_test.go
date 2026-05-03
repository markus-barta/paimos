package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func openMigrationRunnerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration-runner.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if _, err := db.Exec(`CREATE TABLE schema_versions (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create schema_versions: %v", err)
	}
	return db
}

func migrationRecorded(t *testing.T, db *sql.DB, version int) bool {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_versions WHERE version=?", version).Scan(&count); err != nil {
		t.Fatalf("count schema version: %v", err)
	}
	return count > 0
}

func TestApplyMigrationAtomicCommitsStepsAndVersionTogether(t *testing.T) {
	db := openMigrationRunnerTestDB(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()

	m := migration{version: 1, steps: []string{
		`CREATE TABLE atomic_ok (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`INSERT INTO atomic_ok (name) VALUES ('kept')`,
	}}
	if err := applyMigration(context.Background(), conn, m); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if !migrationRecorded(t, db, 1) {
		t.Fatal("expected schema version to be recorded")
	}
	var name string
	if err := db.QueryRow("SELECT name FROM atomic_ok WHERE id=1").Scan(&name); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if name != "kept" {
		t.Fatalf("name=%q want kept", name)
	}
}

func TestApplyMigrationAtomicRollsBackStepsAndVersionOnFailure(t *testing.T) {
	db := openMigrationRunnerTestDB(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()

	m := migration{version: 2, steps: []string{
		`CREATE TABLE atomic_rollback (id INTEGER PRIMARY KEY)`,
		`INSERT INTO missing_table (id) VALUES (1)`,
	}}
	if err := applyMigration(context.Background(), conn, m); err == nil {
		t.Fatal("expected migration failure")
	}
	if migrationRecorded(t, db, 2) {
		t.Fatal("failed migration should not record schema version")
	}
	if tableExists(t, db, "atomic_rollback") {
		t.Fatal("atomic migration left partial DDL behind")
	}
}

func TestApplyMigrationForeignKeyPragmaExceptionIsNotRecordedOnFailure(t *testing.T) {
	db := openMigrationRunnerTestDB(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()

	m := migration{version: 3, steps: []string{
		`PRAGMA foreign_keys=OFF`,
		`CREATE TABLE non_atomic_probe (id INTEGER PRIMARY KEY)`,
		`INSERT INTO missing_table (id) VALUES (1)`,
		`PRAGMA foreign_keys=ON`,
	}}
	if err := applyMigration(context.Background(), conn, m); err == nil {
		t.Fatal("expected migration failure")
	}
	if migrationRecorded(t, db, 3) {
		t.Fatal("failed non-atomic migration should not record schema version")
	}
	if !tableExists(t, db, "non_atomic_probe") {
		t.Fatal("expected explicit non-atomic exception path to expose partial DDL")
	}
}
