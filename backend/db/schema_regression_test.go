package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const latestSchemaVersion = 129

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

func TestSchemaAgentRunsClaimedByColumn(t *testing.T) {
	db := openTestDB(t)
	if !columnExists(t, db, "agent_runs", "claimed_by") {
		t.Fatal("expected agent_runs.claimed_by to exist (PAI-624 / M128)")
	}
}

func TestSchemaAgentRunsProviderColumns(t *testing.T) {
	db := openTestDB(t)
	for _, col := range []string{"action_key", "provider_kind", "provider_id", "provider_label", "model", "run_mode"} {
		if !columnExists(t, db, "agent_runs", col) {
			t.Fatalf("expected agent_runs.%s to exist (PAI-629 / M129)", col)
		}
	}
	if !columnExists(t, db, "auto_watch_subscriptions", "actions_json") {
		t.Fatal("expected auto_watch_subscriptions.actions_json to exist (PAI-629 / M129)")
	}
}

func TestPerConnectionPragmasDoNotTouchJournalMode(t *testing.T) {
	for _, pragma := range perConnectionPragmas {
		if strings.Contains(strings.ToLower(pragma), "journal_mode") {
			t.Fatalf("per-connection pragma %q touches journal_mode; WAL must be set once during Open", pragma)
		}
	}
}

func TestEnableWALModePersistsFileJournalMode(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "wal-mode.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := enableWALMode(db); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode=%q, want wal", mode)
	}
}

func TestSchemaContainsCurrentProjectContextAndAIRelationsTables(t *testing.T) {
	db := openTestDB(t)
	for _, table := range []string{
		"project_repos",
		"issue_anchors",
		// PAI-358: project_manifests dropped in M102 — the manifest
		// editor surface was retired with the v3.0 footer bar redesign.
		"project_context_index",
		"entity_relations",
		"entity_embeddings",
		"ai_prompts",
		"ai_calls",
		"mutation_log",
		"app_settings",
		"project_members",
		"project_agents",
		"project_environments",
		"project_deploy_recipes",
		"project_report_permissions",
		"project_report_snapshots",
		"role_permissions",
		"super_admin_audit",
		"project_issue_counters",
	} {
		if !tableExists(t, db, table) {
			t.Fatalf("expected table %s to exist", table)
		}
	}
	if tableExists(t, db, "user_project_access") {
		t.Fatal("user_project_access should be removed after migration 65")
	}
	if tableExists(t, db, "project_manifests") {
		t.Fatal("project_manifests should be removed after migration 102 (PAI-358)")
	}
	if !columnExists(t, db, "ai_prompts", "placement") {
		t.Fatal("expected ai_prompts.placement to exist")
	}
	if !columnExists(t, db, "mutation_log", "after_state") {
		t.Fatal("expected mutation_log.after_state to exist")
	}
	if !columnExists(t, db, "mutation_log", "redoable") {
		t.Fatal("expected mutation_log.redoable to exist")
	}
	if !columnExists(t, db, "sessions", "csrf_token") {
		t.Fatal("expected sessions.csrf_token to exist")
	}
	if !columnExists(t, db, "sessions", "actor_user_id") {
		t.Fatal("expected sessions.actor_user_id to exist (PAI-389 / M106)")
	}
	if !columnExists(t, db, "sessions", "acting_as_user_id") {
		t.Fatal("expected sessions.acting_as_user_id to exist (PAI-389 / M106)")
	}
	if !columnExists(t, db, "users", "issue_auto_refresh_enabled") {
		t.Fatal("expected users.issue_auto_refresh_enabled to exist")
	}
	if !columnExists(t, db, "users", "issue_auto_refresh_interval_seconds") {
		t.Fatal("expected users.issue_auto_refresh_interval_seconds to exist")
	}
	if !columnExists(t, db, "users", "role_key") {
		t.Fatal("expected users.role_key to exist (PAI-336 / M105)")
	}
	if !columnExists(t, db, "project_cooperation", "report_contract_basis") {
		t.Fatal("expected project_cooperation.report_contract_basis to exist (PAI-407 / M107)")
	}
	if !columnExists(t, db, "project_cooperation", "report_customer_responsibilities") {
		t.Fatal("expected project_cooperation.report_customer_responsibilities to exist (PAI-407 / M107)")
	}
	if !columnExists(t, db, "customers", "tax_id") {
		t.Fatal("expected customers.tax_id to exist (PAI-558 / M114)")
	}
	if !columnExists(t, db, "customers", "company_register_number") {
		t.Fatal("expected customers.company_register_number to exist (PAI-558 / M114)")
	}
	// PAI-324 / M93 — agent + session attribution on history snapshots.
	if !columnExists(t, db, "issue_history", "agent_name") {
		t.Fatal("expected issue_history.agent_name to exist")
	}
	if !columnExists(t, db, "issue_history", "session_id") {
		t.Fatal("expected issue_history.session_id to exist")
	}
	// PAI-329 / M95 — agent rendering shape extensions.
	if !columnExists(t, db, "project_agents", "body") {
		t.Fatal("expected project_agents.body to exist")
	}
	if !columnExists(t, db, "project_agents", "bootstrap_steps") {
		t.Fatal("expected project_agents.bootstrap_steps to exist")
	}
	if !columnExists(t, db, "project_agents", "non_negotiable_rules") {
		t.Fatal("expected project_agents.non_negotiable_rules to exist")
	}
	// PAI-338 / M96 — knowledge-plane columns on issues.
	if !columnExists(t, db, "issues", "slug") {
		t.Fatal("expected issues.slug to exist (PAI-338 / M96)")
	}
	if !columnExists(t, db, "issues", "category_metadata") {
		t.Fatal("expected issues.category_metadata to exist (PAI-338 / M96)")
	}
	// PAI-345 / M99 — user_id column for cross-scope memory.
	if !columnExists(t, db, "issues", "user_id") {
		t.Fatal("expected issues.user_id to exist (PAI-345 / M99)")
	}
	// PAI-347 / M100 — memory reference-count tracking.
	if !columnExists(t, db, "issues", "reference_count") {
		t.Fatal("expected issues.reference_count to exist (PAI-347 / M100)")
	}
	if !columnExists(t, db, "issues", "last_referenced_at") {
		t.Fatal("expected issues.last_referenced_at to exist (PAI-347 / M100)")
	}
	// PAI-577 / M115 — issue-list freshness marker for the conditional-GET ETag.
	if !columnExists(t, db, "issues", "content_rev") {
		t.Fatal("expected issues.content_rev to exist (PAI-577 / M115)")
	}
	// PAI-354 / M101 — agent attribution on mutation_log rows.
	// session_id has lived here since M83; agent_name is the new arrival.
	if !columnExists(t, db, "mutation_log", "agent_name") {
		t.Fatal("expected mutation_log.agent_name to exist (PAI-354 / M101)")
	}
	if !columnExists(t, db, "mutation_log", "session_id") {
		t.Fatal("expected mutation_log.session_id to exist (PAI-354 / M101)")
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
		// PAI-338 / M96 — slug uniqueness for the knowledge plane.
		"idx_issues_type_slug_project",
		// PAI-345 / M99 — user-scoped knowledge lookups.
		"idx_issues_user_type",
		// PAI-336 / M105 — queryable privileged-action audit feed.
		"idx_super_admin_audit_created_at",
		"idx_super_admin_audit_actor",
		"idx_super_admin_audit_target",
		"idx_super_admin_audit_capability",
		"idx_sessions_actor_user",
		"idx_sessions_acting_as_user",
		"idx_project_report_permissions_project",
		"idx_project_report_snapshots_project",
		"idx_project_report_snapshots_code",
		"idx_issues_project_number_unique",
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

func TestSchemaSeedsSuperAdminCapabilities(t *testing.T) {
	db := openTestDB(t)
	for _, row := range []struct {
		role       string
		capability string
	}{
		{"admin", "security.super_admin_audit.read"},
		{"super_admin", "security.super_admin_audit.read"},
		{"super_admin", "time_entries.write_any_user"},
		{"super_admin", "users.grant_super_admin"},
		{"super_admin", "auth.impersonation.start"},
		{"super_admin", "auth.impersonation.end"},
		{"super_admin", "auth.impersonation.action"},
	} {
		var found int
		if err := db.QueryRow(
			"SELECT 1 FROM role_permissions WHERE role=? AND capability=?",
			row.role, row.capability,
		).Scan(&found); err != nil {
			t.Fatalf("expected role_permissions row (%s, %s): %v", row.role, row.capability, err)
		}
	}
}
