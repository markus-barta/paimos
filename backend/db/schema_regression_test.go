package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

const latestSchemaVersion = 83

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
	found, err := SchemaHasTable(db, name)
	if err != nil {
		t.Fatalf("schema_has_table %s: %v", name, err)
	}
	return found
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	found, err := SchemaHasColumn(db, table, column)
	if err != nil {
		t.Fatalf("schema_has_column %s.%s: %v", table, column, err)
	}
	return found
}

func TestSchemaMigrationsReachLatestVersion(t *testing.T) {
	db := openTestDB(t)
	maxVersion, err := CurrentSchemaVersion(db)
	if err != nil {
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
		"project_context_index",
		"entity_relations",
		"entity_embeddings",
		"ai_prompts",
		"ai_calls",
		"mutation_log",
		"app_settings",
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

func TestSchemaEnablesForeignKeysAndPassesIntegrityCheck(t *testing.T) {
	db := openTestDB(t)
	enabled, err := ForeignKeysEnabled(db)
	if err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if !enabled {
		t.Fatal("expected PRAGMA foreign_keys=ON")
	}
	ok, err := IntegrityCheckOK(db)
	if err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if !ok {
		t.Fatal("expected integrity_check=ok")
	}
}

func TestSchemaContainsCriticalIndexes(t *testing.T) {
	db := openTestDB(t)
	for _, index := range []string{
		"idx_issues_number",
		"idx_issues_deleted_at",
		"idx_project_members_project",
		"idx_project_repos_project",
		"idx_issue_anchors_issue",
		"idx_entity_relations_project_src",
		"idx_entity_relations_project_tgt",
		"idx_ai_prompts_key_enabled",
		"idx_ai_calls_time",
		"idx_ai_calls_issue_time",
		"idx_mutation_log_user_stack",
		"idx_mutation_log_request",
		"idx_documents_project",
		"idx_time_entries_mite_id",
	} {
		found, err := SchemaHasIndex(db, index)
		if err != nil {
			t.Fatalf("schema_has_index %s: %v", index, err)
		}
		if !found {
			t.Fatalf("expected index %s to exist", index)
		}
	}
}
