package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

const latestSchemaVersion = 79

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	prevDataDir := os.Getenv("DATA_DIR")
	prevTestMode := os.Getenv("PAIMOS_TEST_MODE")
	t.Cleanup(func() {
		_ = DB.Close()
		DB = nil
		_ = os.Setenv("DATA_DIR", prevDataDir)
		_ = os.Setenv("PAIMOS_TEST_MODE", prevTestMode)
	})

	dataDir := t.TempDir()
	if err := os.Setenv("DATA_DIR", dataDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("PAIMOS_TEST_MODE", "1"); err != nil {
		t.Fatal(err)
	}
	if err := Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	return DB
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	return err == nil && found == name
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info %s: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	return false
}

func TestSchemaMigrationsReachLatestVersion(t *testing.T) {
	db := openTestDB(t)
	var maxVersion int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_versions`).Scan(&maxVersion); err != nil {
		t.Fatalf("max schema version: %v", err)
	}
	if maxVersion != latestSchemaVersion {
		t.Fatalf("max schema version=%d want %d", maxVersion, latestSchemaVersion)
	}
}

func TestSchemaContainsCurrentProjectContextAndAIRelationsTables(t *testing.T) {
	db := openTestDB(t)
	for _, table := range []string{
		"project_repos",
		"issue_anchors",
		"project_manifests",
		"entity_relations",
		"entity_embeddings",
		"ai_prompts",
		"project_members",
	} {
		if !tableExists(t, db, table) {
			t.Fatalf("expected table %s to exist", table)
		}
	}
	if tableExists(t, db, "user_project_access") {
		t.Fatal("user_project_access should be removed after migration 65")
	}
	if !columnExists(t, db, "ai_prompts", "placement") {
		t.Fatal("expected ai_prompts.placement to exist")
	}
	if !columnExists(t, db, "sessions", "csrf_token") {
		t.Fatal("expected sessions.csrf_token to exist")
	}
}

func TestSchemaCreatesDatabaseFileInConfiguredDataDir(t *testing.T) {
	db := openTestDB(t)
	if db == nil {
		t.Fatal("db is nil")
	}
	dbPath := filepath.Join(os.Getenv("DATA_DIR"), "paimos.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected sqlite file at %s: %v", dbPath, err)
	}
}
