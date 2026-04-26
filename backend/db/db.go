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

package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"modernc.org/sqlite"

	"github.com/markus-barta/paimos/backend/brand"
)

func init() {
	// RegisterConnectionHook fires on every new connection in the pool.
	// This is the correct way to set per-connection SQLite pragmas — a one-shot
	// PRAGMA exec at startup only covers a single connection.
	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, _ string) error {
		ctx := context.Background()
		for _, pragma := range []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA busy_timeout=5000",
			"PRAGMA foreign_keys=ON",
		} {
			if _, err := conn.ExecContext(ctx, pragma, nil); err != nil {
				return fmt.Errorf("pragma %q: %w", pragma, err)
			}
		}
		return nil
	})
}

var DB *sql.DB

func Open() error {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, brand.Default.DBFilename)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}

	// WAL mode allows concurrent readers; writers are serialized by SQLite
	// internally. busy_timeout prevents immediate SQLITE_BUSY errors under
	// write contention — connections wait up to 5s before failing.
	// (These are also set per-connection via the hook above.)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	DB = db
	return migrate(db)
}

func migrate(db *sql.DB) error {
	// In test mode, skip fsync and keep the journal in memory so the ~70
	// migration statements don't each pay a disk-sync cost. Applied here
	// (not after Open) because migrations run inside Open().
	if os.Getenv("PAIMOS_TEST_MODE") == "1" {
		db.Exec("PRAGMA synchronous=OFF")
		db.Exec("PRAGMA journal_mode=MEMORY")
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_versions (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_versions: %w", err)
	}

	type migration struct {
		version int
		steps   []string
	}

	migrations := []migration{
		{1, []string{
			`CREATE TABLE IF NOT EXISTS users (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				username   TEXT NOT NULL UNIQUE,
				password   TEXT NOT NULL,
				role       TEXT NOT NULL DEFAULT 'member' CHECK(role IN ('admin','member')),
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE TABLE IF NOT EXISTS sessions (
				id         TEXT PRIMARY KEY,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				expires_at TEXT NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS projects (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				name        TEXT NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				status      TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','archived')),
				created_at  TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE TABLE IF NOT EXISTS issues (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				title       TEXT NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				status      TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','in-progress','done','closed')),
				priority    TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('low','medium','high')),
				assignee_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
				created_at  TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_project ON issues(project_id)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_user  ON sessions(user_id)`,
		}},

		{2, []string{
			`ALTER TABLE projects ADD COLUMN key TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN issue_number INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE issues ADD COLUMN type TEXT NOT NULL DEFAULT 'ticket'`,
			`ALTER TABLE issues ADD COLUMN parent_id INTEGER REFERENCES issues(id) ON DELETE SET NULL`,
			`ALTER TABLE issues ADD COLUMN acceptance_criteria TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN notes TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN cost_unit TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN release TEXT NOT NULL DEFAULT ''`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_project_number ON issues(project_id, issue_number)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_parent   ON issues(parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_type     ON issues(type)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_costunit ON issues(cost_unit)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_release  ON issues(release)`,
			`UPDATE issues SET issue_number = (
				SELECT COUNT(*) FROM issues i2
				WHERE i2.project_id = issues.project_id AND i2.id <= issues.id
			) WHERE issue_number = 0`,
		}},

		// Migration 3: global tags, join tables, FTS5 search index with triggers
		{3, []string{
			// Tags table
			`CREATE TABLE IF NOT EXISTS tags (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				name        TEXT NOT NULL UNIQUE,
				color       TEXT NOT NULL DEFAULT 'gray',
				description TEXT NOT NULL DEFAULT '',
				created_at  TEXT NOT NULL DEFAULT (datetime('now'))
			)`,

			// Join tables
			`CREATE TABLE IF NOT EXISTS issue_tags (
				issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
				PRIMARY KEY (issue_id, tag_id)
			)`,
			`CREATE TABLE IF NOT EXISTS project_tags (
				project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				tag_id     INTEGER NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
				PRIMARY KEY (project_id, tag_id)
			)`,

			// FTS5 virtual table
			// content: space-separated searchable text for the entity
			`CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
				entity_type,
				entity_id UNINDEXED,
				content,
				tokenize='porter ascii'
			)`,

			// ── Project triggers ──────────────────────────────────────────────
			`CREATE TRIGGER IF NOT EXISTS trg_projects_ai
				AFTER INSERT ON projects BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('project', NEW.id, NEW.name || ' ' || NEW.key || ' ' || NEW.description);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_projects_au
				AFTER UPDATE ON projects BEGIN
					DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('project', NEW.id, NEW.name || ' ' || NEW.key || ' ' || NEW.description);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_projects_ad
				AFTER DELETE ON projects BEGIN
					DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
				END`,

			// ── Issue triggers ────────────────────────────────────────────────
			`CREATE TRIGGER IF NOT EXISTS trg_issues_ai
				AFTER INSERT ON issues BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						NEW.title || ' ' || NEW.description || ' ' ||
						NEW.acceptance_criteria || ' ' || NEW.notes || ' ' ||
						NEW.cost_unit || ' ' || NEW.release || ' ' || NEW.type);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_issues_au
				AFTER UPDATE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						NEW.title || ' ' || NEW.description || ' ' ||
						NEW.acceptance_criteria || ' ' || NEW.notes || ' ' ||
						NEW.cost_unit || ' ' || NEW.release || ' ' || NEW.type);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_issues_ad
				AFTER DELETE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
				END`,

			// ── User triggers ─────────────────────────────────────────────────
			`CREATE TRIGGER IF NOT EXISTS trg_users_ai
				AFTER INSERT ON users BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('user', NEW.id, NEW.username || ' ' || NEW.role);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_users_au
				AFTER UPDATE ON users BEGIN
					DELETE FROM search_index WHERE entity_type='user' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('user', NEW.id, NEW.username || ' ' || NEW.role);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_users_ad
				AFTER DELETE ON users BEGIN
					DELETE FROM search_index WHERE entity_type='user' AND entity_id=OLD.id;
				END`,

			// ── Tag triggers ──────────────────────────────────────────────────
			`CREATE TRIGGER IF NOT EXISTS trg_tags_ai
				AFTER INSERT ON tags BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('tag', NEW.id, NEW.name || ' ' || NEW.description);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_tags_au
				AFTER UPDATE ON tags BEGIN
					DELETE FROM search_index WHERE entity_type='tag' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('tag', NEW.id, NEW.name || ' ' || NEW.description);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_tags_ad
				AFTER DELETE ON tags BEGIN
					DELETE FROM search_index WHERE entity_type='tag' AND entity_id=OLD.id;
				END`,

			// ── Backfill existing data into FTS ───────────────────────────────
			`INSERT INTO search_index(entity_type, entity_id, content)
				SELECT 'project', id, name || ' ' || key || ' ' || description FROM projects`,
			`INSERT INTO search_index(entity_type, entity_id, content)
				SELECT 'issue', id,
					title || ' ' || description || ' ' ||
					acceptance_criteria || ' ' || notes || ' ' ||
					cost_unit || ' ' || release || ' ' || type
				FROM issues`,
			`INSERT INTO search_index(entity_type, entity_id, content)
				SELECT 'user', id, username || ' ' || role FROM users`,
		}},
		// Migration 4: depends_on + impacts (plain-text issue-key references, e.g. "ACME-1, ACME-3")
		{4, []string{
			`ALTER TABLE issues ADD COLUMN depends_on TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN impacts    TEXT NOT NULL DEFAULT ''`,
		}},

		// Migration 6: TOTP 2FA — secret + enabled flag on users, pending token table
		{6, []string{
			`ALTER TABLE users ADD COLUMN totp_secret  TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`,
			`CREATE TABLE IF NOT EXISTS totp_pending (
				token      TEXT PRIMARY KEY,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				expires_at TEXT NOT NULL
			)`,
		}},

		// Migration 9: comments — threaded comments on issues
		{9, []string{
			`CREATE TABLE IF NOT EXISTS comments (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
				body       TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id, created_at)`,
		}},

		// Migration 8: integrations — one row per provider, config stored as JSON
		{8, []string{
			`CREATE TABLE IF NOT EXISTS integrations (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				provider   TEXT NOT NULL UNIQUE,
				config     TEXT NOT NULL DEFAULT '{}',
				updated_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
		}},

		// Migration 7: API keys — named long-lived tokens for programmatic access
		{7, []string{
			`CREATE TABLE IF NOT EXISTS api_keys (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				name       TEXT NOT NULL,
				key_hash   TEXT NOT NULL UNIQUE,
				key_prefix TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now')),
				last_used_at TEXT
			)`,
			`CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id)`,
		}},

		// Migration 10: three-phase soft delete for users and projects.
		//
		// users: add status column (active / inactive / deleted).
		//   active   = normal login
		//   inactive = login blocked, data preserved, shown as "Disabled" in UI
		//   deleted  = login blocked, hidden from UI, restorable via DB
		//
		// projects: the existing status column has CHECK(status IN ('active','archived')).
		// SQLite does not support ALTER TABLE ... MODIFY COLUMN, so we recreate the
		// table without the restrictive CHECK and migrate all data. Application logic
		// enforces valid values (active / archived / deleted).
		//
		// IMPORTANT: We MUST disable foreign_keys before dropping projects_old,
		// otherwise the ON DELETE CASCADE on issues.project_id would wipe all issues.
		// We re-enable foreign_keys after the migration step is complete.
		{10, []string{
			// ── Users: add status column ──────────────────────────────────────
			`ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`,

			// ── Projects: recreate table to drop the restrictive CHECK ─────────
			// Disable FK enforcement for the duration of the table swap
			`PRAGMA foreign_keys=OFF`,
			// Step 1: rename existing table
			`ALTER TABLE projects RENAME TO projects_old`,
			// Step 2: create new table without CHECK constraint on status
			`CREATE TABLE projects (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				name        TEXT NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				status      TEXT NOT NULL DEFAULT 'active',
				created_at  TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
				key         TEXT NOT NULL DEFAULT ''
			)`,
			// Step 3: copy data
			`INSERT INTO projects(id,name,description,status,created_at,updated_at,key)
				SELECT id,name,description,status,created_at,updated_at,key FROM projects_old`,
			// Step 4: drop old table — safe now because FK enforcement is off
			`DROP TABLE projects_old`,
			// Step 5: recreate indexes and triggers
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_key ON projects(key)`,
			`CREATE TRIGGER IF NOT EXISTS trg_projects_ai2
				AFTER INSERT ON projects BEGIN
					DELETE FROM search_index WHERE entity_type='project' AND entity_id=NEW.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('project', NEW.id, NEW.name || ' ' || NEW.key || ' ' || NEW.description);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_projects_au2
				AFTER UPDATE ON projects BEGIN
					DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('project', NEW.id, NEW.name || ' ' || NEW.key || ' ' || NEW.description);
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_projects_ad2
				AFTER DELETE ON projects BEGIN
					DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
				END`,
			// Re-enable FK enforcement
			`PRAGMA foreign_keys=ON`,
		}},

		// Migration 11: fix broken FK references caused by migration 10.
		//
		// When migration 10 renamed projects→projects_old and created a new projects table,
		// SQLite internally rewrote the FK references in `issues` and `project_tags` to
		// point to "projects_old". Now projects_old is gone, so any INSERT/UPDATE on those
		// tables fails with "no such table: main.projects_old".
		//
		// Fix: recreate issues and project_tags with correct FK references to `projects`.
		// Full column lists preserved exactly. FK-off pattern required.
		{11, []string{
			`PRAGMA foreign_keys=OFF`,

			// ── Recreate issues ───────────────────────────────────────────────
			`ALTER TABLE issues RENAME TO issues_old`,
			`CREATE TABLE issues (
				id                  INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id          INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				title               TEXT NOT NULL,
				description         TEXT NOT NULL DEFAULT '',
				status              TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','in-progress','done','closed')),
				priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('low','medium','high')),
				assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
				created_at          TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
				issue_number        INTEGER NOT NULL DEFAULT 0,
				type                TEXT NOT NULL DEFAULT 'ticket',
				parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
				acceptance_criteria TEXT NOT NULL DEFAULT '',
				notes               TEXT NOT NULL DEFAULT '',
				cost_unit           TEXT NOT NULL DEFAULT '',
				release             TEXT NOT NULL DEFAULT '',
				depends_on          TEXT NOT NULL DEFAULT '',
				impacts             TEXT NOT NULL DEFAULT ''
			)`,
			`INSERT INTO issues SELECT * FROM issues_old`,
			`DROP TABLE issues_old`,

			// ── Restore issue indexes ─────────────────────────────────────────
			`CREATE INDEX IF NOT EXISTS idx_issues_project        ON issues(project_id)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_project_number ON issues(project_id, issue_number)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_parent         ON issues(parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_type           ON issues(type)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_costunit       ON issues(cost_unit)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_release        ON issues(release)`,

			// ── Recreate project_tags ─────────────────────────────────────────
			`ALTER TABLE project_tags RENAME TO project_tags_old`,
			`CREATE TABLE project_tags (
				project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				tag_id     INTEGER NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
				PRIMARY KEY (project_id, tag_id)
			)`,
			`INSERT INTO project_tags SELECT * FROM project_tags_old`,
			`DROP TABLE project_tags_old`,

			`PRAGMA foreign_keys=ON`,
		}},

		// Migration 5: issue change history — full JSON snapshot per save
		{5, []string{
			`CREATE TABLE IF NOT EXISTS issue_history (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
				snapshot   TEXT NOT NULL,
				changed_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE INDEX IF NOT EXISTS idx_issue_history_issue ON issue_history(issue_id, changed_at)`,
		}},

		// Migration 12: issue_relations — unified M:N relation table replacing
		// parent_id for group→ticket links and free-text depends_on/impacts fields.
		// Relation types: groups | sprint | depends_on | impacts
		{12, []string{
			`CREATE TABLE IF NOT EXISTS issue_relations (
				source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				type      TEXT NOT NULL,
				PRIMARY KEY (source_id, target_id, type)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_source ON issue_relations(source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_target ON issue_relations(target_id)`,
		}},

		// Migration 13: group-level and sprint-level nullable columns on issues.
		// All additive — safe, no data loss.
		{13, []string{
			// Group (epic, cost_unit) fields
			`ALTER TABLE issues ADD COLUMN billing_type  TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN total_budget  REAL`,
			`ALTER TABLE issues ADD COLUMN rate_hourly   REAL`,
			`ALTER TABLE issues ADD COLUMN rate_package  REAL`,
			// Release fields
			`ALTER TABLE issues ADD COLUMN start_date    TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN end_date      TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN group_state   TEXT NOT NULL DEFAULT ''`,
			// Sprint fields
			`ALTER TABLE issues ADD COLUMN sprint_state  TEXT NOT NULL DEFAULT ''`,
			// Jira mapping fields (shared across group types and sprint)
			`ALTER TABLE issues ADD COLUMN jira_id       TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN jira_version  TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE issues ADD COLUMN jira_text     TEXT NOT NULL DEFAULT ''`,
		}},

		// Migration 14: expand issues.type to allow cost_unit, release, sprint.
		// The current CHECK(type IN ('epic','ticket','task')) must be removed.
		// Also rename status values: open→backlog, done→complete, closed→canceled.
		// Requires table recreate with FK-off pattern; data migration for status.
		{14, []string{
			`PRAGMA foreign_keys=OFF`,

			`ALTER TABLE issues RENAME TO issues_old14`,
			`CREATE TABLE issues (
				id                  INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id          INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				title               TEXT NOT NULL,
				description         TEXT NOT NULL DEFAULT '',
				status              TEXT NOT NULL DEFAULT 'backlog',
				priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('low','medium','high')),
				assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
				created_at          TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
				issue_number        INTEGER NOT NULL DEFAULT 0,
				type                TEXT NOT NULL DEFAULT 'ticket',
				parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
				acceptance_criteria TEXT NOT NULL DEFAULT '',
				notes               TEXT NOT NULL DEFAULT '',
				cost_unit           TEXT NOT NULL DEFAULT '',
				release             TEXT NOT NULL DEFAULT '',
				depends_on          TEXT NOT NULL DEFAULT '',
				impacts             TEXT NOT NULL DEFAULT '',
				billing_type        TEXT NOT NULL DEFAULT '',
				total_budget        REAL,
				rate_hourly         REAL,
				rate_package        REAL,
				start_date          TEXT NOT NULL DEFAULT '',
				end_date            TEXT NOT NULL DEFAULT '',
				group_state         TEXT NOT NULL DEFAULT '',
				sprint_state        TEXT NOT NULL DEFAULT '',
				jira_id             TEXT NOT NULL DEFAULT '',
				jira_version        TEXT NOT NULL DEFAULT '',
				jira_text           TEXT NOT NULL DEFAULT ''
			)`,
			// Copy data with status rename
			`INSERT INTO issues(id,project_id,title,description,status,priority,
			                    assignee_id,created_at,updated_at,issue_number,type,parent_id,
			                    acceptance_criteria,notes,cost_unit,release,depends_on,impacts,
			                    billing_type,total_budget,rate_hourly,rate_package,
			                    start_date,end_date,group_state,sprint_state,jira_id,jira_version,jira_text)
			SELECT id,project_id,title,description,
			       CASE status
			           WHEN 'open'   THEN 'backlog'
			           WHEN 'done'   THEN 'complete'
			           WHEN 'closed' THEN 'canceled'
			           ELSE status
			       END,
			       priority,assignee_id,created_at,updated_at,issue_number,type,parent_id,
			       acceptance_criteria,notes,cost_unit,release,depends_on,impacts,
			       COALESCE(billing_type,''),total_budget,rate_hourly,rate_package,
			       COALESCE(start_date,''),COALESCE(end_date,''),COALESCE(group_state,''),
			       COALESCE(sprint_state,''),COALESCE(jira_id,''),COALESCE(jira_version,''),COALESCE(jira_text,'')
			FROM issues_old14`,
			`DROP TABLE issues_old14`,

			// Restore indexes
			`CREATE INDEX IF NOT EXISTS idx_issues_project        ON issues(project_id)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_project_number ON issues(project_id, issue_number)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_parent         ON issues(parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_type           ON issues(type)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_costunit       ON issues(cost_unit)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_release        ON issues(release)`,

			`PRAGMA foreign_keys=ON`,
		}},

		// Migration 15: add product_owner (FK→users) and customer_id to projects.
		// Additive — safe.
		{15, []string{
			`ALTER TABLE projects ADD COLUMN product_owner INTEGER REFERENCES users(id) ON DELETE SET NULL`,
			`ALTER TABLE projects ADD COLUMN customer_id   TEXT NOT NULL DEFAULT ''`,
		}},

		// Migration 19: fix broken FK in issue_relations.
		// Migration 14 renamed issues→issues_old14, which caused SQLite to rewrite the
		// REFERENCES clause in issue_relations to point at issues_old14. After migration 14
		// dropped issues_old14 and recreated issues, issue_relations was left with a dangling
		// FK reference, making any INSERT fail with "no such table: main.issues_old14".
		// Fix: recreate issue_relations with the correct REFERENCES issues(id).
		// MUST run before migrations 17 and 18 (which INSERT into issue_relations).
		{19, []string{
			`PRAGMA foreign_keys=OFF`,
			`ALTER TABLE issue_relations RENAME TO issue_relations_old19`,
			`CREATE TABLE issue_relations (
				source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				type      TEXT NOT NULL,
				PRIMARY KEY (source_id, target_id, type)
			)`,
			`INSERT OR IGNORE INTO issue_relations SELECT source_id, target_id, type FROM issue_relations_old19`,
			`DROP TABLE issue_relations_old19`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_source ON issue_relations(source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_target ON issue_relations(target_id)`,
			`PRAGMA foreign_keys=ON`,
		}},

		// Migration 17: data migration — wire existing epic→ticket parent_id links into
		// issue_relations(type='groups'). After this, parent_id is only used for task→ticket.
		// Safe: additive insert into issue_relations; parent_id column left intact for now.
		{17, []string{
			`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type)
			 SELECT parent_id, id, 'groups'
			 FROM issues
			 WHERE type = 'ticket'
			   AND parent_id IS NOT NULL
			   AND EXISTS (SELECT 1 FROM issues p WHERE p.id = issues.parent_id AND p.type = 'epic')`,
		}},

		// Migration 18: data migration — parse free-text depends_on/impacts fields and
		// insert resolved issue_relations rows. Rows that cannot be resolved (bad keys,
		// cross-project references) are silently skipped; we preserve the free-text column.
		// issue_key is not stored; reconstruct as projects.key || '-' || issues.issue_number.
		// NOTE: only handles the first comma-separated token per row (covers ~99% of real data).
		// Multi-value rows are rare; a future cleanup migration can handle them if needed.
		{18, []string{
			// depends_on: resolve first token to issue id via reconstructed issue_key
			`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type)
			 SELECT i.id, i2.id, 'depends_on'
			 FROM issues i
			 JOIN issues i2 ON (
			   SELECT p.key || '-' || i2.issue_number FROM projects p WHERE p.id = i2.project_id
			 ) = TRIM(SUBSTR(i.depends_on || ',', 1, INSTR(i.depends_on || ',', ',') - 1))
			 WHERE i.depends_on != ''`,
			// impacts: same pattern
			`INSERT OR IGNORE INTO issue_relations(source_id, target_id, type)
			 SELECT i.id, i2.id, 'impacts'
			 FROM issues i
			 JOIN issues i2 ON (
			   SELECT p.key || '-' || i2.issue_number FROM projects p WHERE p.id = i2.project_id
			 ) = TRIM(SUBSTR(i.impacts || ',', 1, INSTR(i.impacts || ',', ',') - 1))
			 WHERE i.impacts != ''`,
		}},

		// Migration 16: time_entries — ticket-level start/stop time tracking.
		// New table — safe.
		{16, []string{
			`CREATE TABLE IF NOT EXISTS time_entries (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				ticket_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				started_at  TEXT NOT NULL,
				stopped_at  TEXT,
				override    REAL,
				comment     TEXT NOT NULL DEFAULT '',
				created_at  TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE INDEX IF NOT EXISTS idx_time_entries_ticket ON time_entries(ticket_id)`,
			`CREATE INDEX IF NOT EXISTS idx_time_entries_user   ON time_entries(user_id)`,
		}},

		// Migration 20: fix broken FK references in issue_tags, comments, issue_history.
		// Prior migrations renamed issues→issues_old, causing SQLite to silently rewrite
		// REFERENCES to point at "issues_old" instead of "issues". With foreign_keys=ON
		// this causes every DML on those tables to fail with "no such table: main.issues_old".
		// Fix: recreate all three tables with correct REFERENCES issues(id).
		{20, []string{
			`PRAGMA foreign_keys=OFF`,

			// issue_tags
			`ALTER TABLE issue_tags RENAME TO issue_tags_old20`,
			`CREATE TABLE issue_tags (
				issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
				PRIMARY KEY (issue_id, tag_id)
			)`,
			`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old20`,
			`DROP TABLE issue_tags_old20`,

			// comments
			`ALTER TABLE comments RENAME TO comments_old20`,
			`CREATE TABLE comments (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
				body       TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`INSERT OR IGNORE INTO comments SELECT * FROM comments_old20`,
			`DROP TABLE comments_old20`,
			`CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id, created_at)`,

			// issue_history
			`ALTER TABLE issue_history RENAME TO issue_history_old20`,
			`CREATE TABLE issue_history (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
				snapshot   TEXT NOT NULL,
				changed_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`INSERT OR IGNORE INTO issue_history SELECT * FROM issue_history_old20`,
			`DROP TABLE issue_history_old20`,
			`CREATE INDEX IF NOT EXISTS idx_issue_history_issue ON issue_history(issue_id, changed_at)`,

			`PRAGMA foreign_keys=ON`,
		}},

		// Migration 21: views table — saved column+filter sets per user.
		// is_shared=1 → visible to all users; is_admin_default=1 → appears in "Basics" section.
		{21, []string{
			`CREATE TABLE IF NOT EXISTS views (
				id               INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				title            TEXT    NOT NULL,
				description      TEXT    NOT NULL DEFAULT '',
				columns_json     TEXT    NOT NULL DEFAULT '[]',
				filters_json     TEXT    NOT NULL DEFAULT '{}',
				is_shared        INTEGER NOT NULL DEFAULT 0,
				is_admin_default INTEGER NOT NULL DEFAULT 0,
				created_at       TEXT    NOT NULL DEFAULT (datetime('now')),
				updated_at       TEXT    NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE INDEX IF NOT EXISTS idx_views_user ON views(user_id)`,
		}},

		// Migration 22: seed the "Default" admin view.
		// columns_json = hidden keys (cost_unit, release, and all v2 fields).
		// Visible = Key, Type, Title, Status, Priority, Assignee, Tags.
		// Inserts only if no is_admin_default view named "Default" already exists.
		{22, []string{
			`INSERT INTO views (user_id, title, description, columns_json, filters_json, is_shared, is_admin_default)
			 SELECT u.id,
			        'Default',
			        'Standard view: Key, Type, Title, Status, Priority, Assignee, Tags.',
			        '["cost_unit","release","billing_type","total_budget","rate_hourly","rate_package","start_date","end_date","group_state","sprint_state","jira_id","jira_version","jira_text"]',
			        '{}',
			        1, 1
			 FROM users u
			 WHERE u.role = 'admin'
			   AND NOT EXISTS (
			       SELECT 1 FROM views WHERE is_admin_default = 1 AND title = 'Default'
			   )
			 ORDER BY u.id LIMIT 1`,
		}},

		// Migration 23: make project_id nullable on issues to support project-less sprints.
		// Requires table recreate (SQLite can't ALTER NOT NULL → NULL).
		// The (project_id, issue_number) unique index is replaced with a partial one that
		// only applies when project_id IS NOT NULL — orphan sprints get issue_number=0.
		{23, []string{
			`PRAGMA foreign_keys=OFF`,
			`ALTER TABLE issues RENAME TO issues_old23`,
			`CREATE TABLE issues (
				id                  INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
				title               TEXT NOT NULL,
				description         TEXT NOT NULL DEFAULT '',
				status              TEXT NOT NULL DEFAULT 'backlog',
				priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('low','medium','high')),
				assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
				created_at          TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
				issue_number        INTEGER NOT NULL DEFAULT 0,
				type                TEXT NOT NULL DEFAULT 'ticket',
				parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
				acceptance_criteria TEXT NOT NULL DEFAULT '',
				notes               TEXT NOT NULL DEFAULT '',
				cost_unit           TEXT NOT NULL DEFAULT '',
				release             TEXT NOT NULL DEFAULT '',
				depends_on          TEXT NOT NULL DEFAULT '',
				impacts             TEXT NOT NULL DEFAULT '',
				billing_type        TEXT NOT NULL DEFAULT '',
				total_budget        REAL,
				rate_hourly         REAL,
				rate_package        REAL,
				start_date          TEXT NOT NULL DEFAULT '',
				end_date            TEXT NOT NULL DEFAULT '',
				group_state         TEXT NOT NULL DEFAULT '',
				sprint_state        TEXT NOT NULL DEFAULT '',
				jira_id             TEXT NOT NULL DEFAULT '',
				jira_version        TEXT NOT NULL DEFAULT '',
				jira_text           TEXT NOT NULL DEFAULT ''
			)`,
			`INSERT INTO issues SELECT * FROM issues_old23`,
			`DROP TABLE issues_old23`,
			`CREATE INDEX IF NOT EXISTS idx_issues_project        ON issues(project_id)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_project_number ON issues(project_id, issue_number) WHERE project_id IS NOT NULL`,
			`CREATE INDEX IF NOT EXISTS idx_issues_parent         ON issues(parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_type           ON issues(type)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_costunit       ON issues(cost_unit)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_release        ON issues(release)`,
			`PRAGMA foreign_keys=ON`,
		}},

		// Migration 24: add archived flag to issues (for sprints) +
		// index on issue_relations(target_id, type) for sprint_ids subquery performance.
		{24, []string{
			`ALTER TABLE issues ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_target ON issue_relations(target_id, type)`,
		}},

		// Migration 25: enhanced user profiles — nickname (≤3 chars for avatar badge),
		// first/last name, email, and avatar_path (relative path under STATIC_DIR).
		{25, []string{
			`ALTER TABLE users ADD COLUMN nickname   TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE users ADD COLUMN first_name TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE users ADD COLUMN last_name  TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE users ADD COLUMN email      TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE users ADD COLUMN avatar_path TEXT NOT NULL DEFAULT ''`,
		}},

		// Migration 26: rewrite legacy avatar paths from /avatars/{n}.jpg to
		// /api/avatars/{n}.jpg — avatars moved from STATIC_DIR to DATA_DIR
		// (volume-mounted) so they survive container rebuilds.
		{26, []string{
			`UPDATE users SET avatar_path = REPLACE(avatar_path, '/avatars/', '/api/avatars/')
			 WHERE avatar_path LIKE '/avatars/%' AND avatar_path NOT LIKE '/api/%'`,
		}},

		// Migration 27: fix broken FK references caused by migration 23.
		//
		// Migration 23 renamed issues→issues_old23 then recreated issues.
		// SQLite silently rewrote all REFERENCES in child tables (issue_tags,
		// comments, issue_history) to point at issues_old23. After DROP TABLE
		// issues_old23, every DML on those tables failed with:
		//   "no such table: main.issues_old23"
		// This blocked tag attachment and comment creation for all users.
		// Fix: same pattern as migration 20 — recreate all three tables with
		// correct REFERENCES issues(id). FK enforcement off during swap.
		{27, []string{
			`PRAGMA foreign_keys=OFF`,

			// issue_tags
			`ALTER TABLE issue_tags RENAME TO issue_tags_old27`,
			`CREATE TABLE issue_tags (
				issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
				PRIMARY KEY (issue_id, tag_id)
			)`,
			`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old27`,
			`DROP TABLE issue_tags_old27`,

			// comments
			`ALTER TABLE comments RENAME TO comments_old27`,
			`CREATE TABLE comments (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
				body       TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`INSERT OR IGNORE INTO comments SELECT * FROM comments_old27`,
			`DROP TABLE comments_old27`,
			`CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id, created_at)`,

			// issue_history
			`ALTER TABLE issue_history RENAME TO issue_history_old27`,
			`CREATE TABLE issue_history (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
				snapshot   TEXT NOT NULL,
				changed_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`INSERT OR IGNORE INTO issue_history SELECT * FROM issue_history_old27`,
			`DROP TABLE issue_history_old27`,
			`CREATE INDEX IF NOT EXISTS idx_issue_history_issue ON issue_history(issue_id, changed_at)`,

			`PRAGMA foreign_keys=ON`,
		}},

		// Migration 28: fix stale FTS5 triggers left by migration 23.
		//
		// When migration 23 renamed issues→issues_old23 and created a new issues table,
		// SQLite automatically remapped the existing FTS triggers (trg_issues_ai/au/ad)
		// to fire on issues_old23. After DROP TABLE issues_old23 those triggers became
		// orphaned. Migration 27 fixes the FK references; this migration fixes the triggers.
		//
		// Fix: drop all stale issue triggers by name, then recreate them on issues.

		{29, []string{
			// Migration 29: editor preferences per user.
			// markdown_default — render long-text fields in Markdown by default.
			// monospace_fields  — use monospace font for long-text fields.
			`ALTER TABLE users ADD COLUMN markdown_default INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE users ADD COLUMN monospace_fields  INTEGER NOT NULL DEFAULT 0`,
		}},

		{28, []string{
			`DROP TRIGGER IF EXISTS trg_issues_ai`,
			`DROP TRIGGER IF EXISTS trg_issues_au`,
			`DROP TRIGGER IF EXISTS trg_issues_ad`,
			`CREATE TRIGGER trg_issues_ai
				AFTER INSERT ON issues BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						NEW.title || ' ' || NEW.description || ' ' ||
						NEW.acceptance_criteria || ' ' || NEW.notes || ' ' ||
						NEW.cost_unit || ' ' || NEW.release || ' ' || NEW.type);
				END`,
			`CREATE TRIGGER trg_issues_au
				AFTER UPDATE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						NEW.title || ' ' || NEW.description || ' ' ||
						NEW.acceptance_criteria || ' ' || NEW.notes || ' ' ||
						NEW.cost_unit || ' ' || NEW.release || ' ' || NEW.type);
				END`,
			`CREATE TRIGGER trg_issues_ad
				AFTER DELETE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
				END`,
		}},

		// Migration 30: expand issue FTS coverage + add comment FTS entity.
		//
		// Issue triggers (28) only indexed 7 fields. This migration drops and
		// recreates them to also include depends_on, impacts, jira_id,
		// jira_version, jira_text — all added in migrations 4 and 13 but never
		// backfilled into FTS.
		//
		// Also adds a new 'comment' entity type to search_index with
		// INSERT/DELETE triggers on the comments table (UPDATE not needed —
		// comments are immutable after creation in the current UI).
		//
		// Must run AFTER migration 28 (which it supersedes) so the correct
		// triggers are active on first install and on existing DBs.
		// Migration 31: fix broken FK in issue_relations (again).
		// Migration 23 renamed issues→issues_old23, which caused SQLite to
		// silently rewrite REFERENCES in issue_relations to point at issues_old23.
		// Migration 27 fixed issue_tags/comments/issue_history but missed
		// issue_relations. Exact same pattern as migration 19 (issues_old14).
		{31, []string{
			`PRAGMA foreign_keys=OFF`,
			`ALTER TABLE issue_relations RENAME TO issue_relations_old31`,
			`CREATE TABLE issue_relations (
				source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				type      TEXT NOT NULL,
				PRIMARY KEY (source_id, target_id, type)
			)`,
			`INSERT OR IGNORE INTO issue_relations SELECT source_id, target_id, type FROM issue_relations_old31`,
			`DROP TABLE issue_relations_old31`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_source ON issue_relations(source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_target ON issue_relations(target_id, type)`,
			`PRAGMA foreign_keys=ON`,
		}},


		{30, []string{
			// Drop old issue triggers (from migration 28)
			`DROP TRIGGER IF EXISTS trg_issues_ai`,
			`DROP TRIGGER IF EXISTS trg_issues_au`,
			`DROP TRIGGER IF EXISTS trg_issues_ad`,

			// Recreate with expanded content
			`CREATE TRIGGER trg_issues_ai
				AFTER INSERT ON issues BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						COALESCE(NEW.title,'') || ' ' ||
						COALESCE(NEW.description,'') || ' ' ||
						COALESCE(NEW.acceptance_criteria,'') || ' ' ||
						COALESCE(NEW.notes,'') || ' ' ||
						COALESCE(NEW.cost_unit,'') || ' ' ||
						COALESCE(NEW.release,'') || ' ' ||
						COALESCE(NEW.type,'') || ' ' ||
						COALESCE(NEW.depends_on,'') || ' ' ||
						COALESCE(NEW.impacts,'') || ' ' ||
						COALESCE(NEW.jira_id,'') || ' ' ||
						COALESCE(NEW.jira_version,'') || ' ' ||
						COALESCE(NEW.jira_text,''));
				END`,
			`CREATE TRIGGER trg_issues_au
				AFTER UPDATE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						COALESCE(NEW.title,'') || ' ' ||
						COALESCE(NEW.description,'') || ' ' ||
						COALESCE(NEW.acceptance_criteria,'') || ' ' ||
						COALESCE(NEW.notes,'') || ' ' ||
						COALESCE(NEW.cost_unit,'') || ' ' ||
						COALESCE(NEW.release,'') || ' ' ||
						COALESCE(NEW.type,'') || ' ' ||
						COALESCE(NEW.depends_on,'') || ' ' ||
						COALESCE(NEW.impacts,'') || ' ' ||
						COALESCE(NEW.jira_id,'') || ' ' ||
						COALESCE(NEW.jira_version,'') || ' ' ||
						COALESCE(NEW.jira_text,''));
				END`,
			`CREATE TRIGGER trg_issues_ad
				AFTER DELETE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
				END`,

			// Comment triggers (comments are immutable — no UPDATE trigger needed)
			`CREATE TRIGGER IF NOT EXISTS trg_comments_ai
				AFTER INSERT ON comments BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('comment', NEW.id, COALESCE(NEW.body,''));
				END`,
			`CREATE TRIGGER IF NOT EXISTS trg_comments_ad
				AFTER DELETE ON comments BEGIN
					DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.id;
				END`,

			// Backfill issues — delete stale FTS rows and re-insert with full content
			`DELETE FROM search_index WHERE entity_type='issue'`,
			`INSERT INTO search_index(entity_type, entity_id, content)
				SELECT 'issue', id,
					COALESCE(title,'') || ' ' ||
					COALESCE(description,'') || ' ' ||
					COALESCE(acceptance_criteria,'') || ' ' ||
					COALESCE(notes,'') || ' ' ||
					COALESCE(cost_unit,'') || ' ' ||
					COALESCE(release,'') || ' ' ||
					COALESCE(type,'') || ' ' ||
					COALESCE(depends_on,'') || ' ' ||
					COALESCE(impacts,'') || ' ' ||
					COALESCE(jira_id,'') || ' ' ||
					COALESCE(jira_version,'') || ' ' ||
					COALESCE(jira_text,'')
				FROM issues`,

			// Backfill comments
			`DELETE FROM search_index WHERE entity_type='comment'`,
		`INSERT INTO search_index(entity_type, entity_id, content)
			SELECT 'comment', id, COALESCE(body,'') FROM comments`,
		}},

		// ── Migration 32: Schema Normalisation ────────────────────────────────────
		//
		// One authoritative migration that eliminates 31 migrations of accumulated
		// scar tissue. No data is destroyed — all existing rows are preserved.
		//
		// Changes (in order):
		//  1. Normalise status enum:  complete→done, canceled→cancelled  (data UPDATE)
		//  2. Flip sprint relations:  source↔target swapped so source=sprint, target=issue
		//     (consistent with groups convention: source=container, target=member)
		//  3. Recreate issues with CHECK constraints + drop legacy depends_on/impacts columns
		//     + rename time_entries.ticket_id→issue_id in the same sweep
		//  4. Recreate all 5 child tables (issue_tags, comments, issue_history,
		//     issue_relations, time_entries) with correct FKs to new issues table
		//  5. Add missing indexes
		//  6. Drop orphaned project triggers (from original M3, orphaned by M10 recreate)
		//  7. Recreate project triggers with clean names (no "2" suffix)
		//  8. Update user FTS triggers to include profile fields (nickname, first_name,
		//     last_name, email) added in M25 but never indexed
		//  9. Backfill FTS — rebuild issues + users from scratch
		{32, []string{
			// ── Step 1: Normalise status values ───────────────────────────────────
			// Map ALL non-canonical values to the 4 canonical ones so the CHECK
			// constraint in step 3 doesn't reject any existing rows.
			`UPDATE issues SET status = 'backlog'     WHERE status IN ('open')`,
			`UPDATE issues SET status = 'done'        WHERE status IN ('complete', 'closed')`,
			`UPDATE issues SET status = 'cancelled'   WHERE status IN ('canceled')`,

			// ── Step 1b: Safety cleanup (idempotent retry guard) ─────────────────
			// If M32 was partially applied before (e.g. step 3 failed), the RENAME
			// may have already created issues_old32. Drop it so the rename succeeds.
			`DROP TABLE IF EXISTS issues_old32`,
			`DROP TABLE IF EXISTS issue_tags_old32`,
			`DROP TABLE IF EXISTS comments_old32`,
			`DROP TABLE IF EXISTS issue_history_old32`,
			`DROP TABLE IF EXISTS issue_relations_old32`,
			`DROP TABLE IF EXISTS time_entries_old32`,

			// ── Step 2: Flip sprint relations (source=sprint, target=issue) ───────
			// Previously: source=issue, target=sprint.  Convention was inconsistent
			// with groups (source=container).  Swap so source is always the container.
			// A temp column approach isn't needed: we swap the pair atomically via CTE.
			`UPDATE issue_relations
			 SET source_id = target_id,
			     target_id = source_id
			 WHERE type = 'sprint'`,

			// ── Step 3 + 4: Recreate tables with corrected schema ────────────────
			`PRAGMA foreign_keys=OFF`,

			// issues — add CHECK on status + type, drop depends_on/impacts columns
			`ALTER TABLE issues RENAME TO issues_old32`,
			`CREATE TABLE issues (
				id                  INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
				title               TEXT NOT NULL,
				description         TEXT NOT NULL DEFAULT '',
				status              TEXT NOT NULL DEFAULT 'backlog'
				                    CHECK(status IN ('backlog','in-progress','done','cancelled')),
				priority            TEXT NOT NULL DEFAULT 'medium'
				                    CHECK(priority IN ('low','medium','high')),
				assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
				created_at          TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
				issue_number        INTEGER NOT NULL DEFAULT 0,
				type                TEXT NOT NULL DEFAULT 'ticket'
				                    CHECK(type IN ('epic','cost_unit','release','sprint','ticket','task')),
				parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
				acceptance_criteria TEXT NOT NULL DEFAULT '',
				notes               TEXT NOT NULL DEFAULT '',
				cost_unit           TEXT NOT NULL DEFAULT '',
				release             TEXT NOT NULL DEFAULT '',
				billing_type        TEXT NOT NULL DEFAULT '',
				total_budget        REAL,
				rate_hourly         REAL,
				rate_package        REAL,
				start_date          TEXT NOT NULL DEFAULT '',
				end_date            TEXT NOT NULL DEFAULT '',
				group_state         TEXT NOT NULL DEFAULT '',
				sprint_state        TEXT NOT NULL DEFAULT '',
				jira_id             TEXT NOT NULL DEFAULT '',
				jira_version        TEXT NOT NULL DEFAULT '',
				jira_text           TEXT NOT NULL DEFAULT '',
				archived            INTEGER NOT NULL DEFAULT 0
			)`,
			// Copy all columns except depends_on and impacts (dropped)
			`INSERT INTO issues (
				id, project_id, title, description, status, priority, assignee_id,
				created_at, updated_at, issue_number, type, parent_id,
				acceptance_criteria, notes, cost_unit, release,
				billing_type, total_budget, rate_hourly, rate_package,
				start_date, end_date, group_state, sprint_state,
				jira_id, jira_version, jira_text, archived
			) SELECT
				id, project_id, title, description, status, priority, assignee_id,
				created_at, updated_at, issue_number, type, parent_id,
				acceptance_criteria, notes, cost_unit, release,
				billing_type, total_budget, rate_hourly, rate_package,
				start_date, end_date, group_state, sprint_state,
				jira_id, jira_version, jira_text, archived
			FROM issues_old32`,
			`DROP TABLE issues_old32`,

			// Recreate indexes on issues
			`CREATE INDEX IF NOT EXISTS idx_issues_project        ON issues(project_id)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_project_number
			 ON issues(project_id, issue_number) WHERE project_id IS NOT NULL`,
			`CREATE INDEX IF NOT EXISTS idx_issues_parent         ON issues(parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_type           ON issues(type)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_status         ON issues(status)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_assignee       ON issues(assignee_id)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_updated        ON issues(updated_at)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_costunit       ON issues(cost_unit)`,
			`CREATE INDEX IF NOT EXISTS idx_issues_release        ON issues(release)`,

			// issue_tags — recreate with correct FK
			`ALTER TABLE issue_tags RENAME TO issue_tags_old32`,
			`CREATE TABLE issue_tags (
				issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
				PRIMARY KEY (issue_id, tag_id)
			)`,
			`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old32`,
			`DROP TABLE issue_tags_old32`,

			// comments — recreate with correct FK
			`ALTER TABLE comments RENAME TO comments_old32`,
			`CREATE TABLE comments (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
				body       TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`INSERT OR IGNORE INTO comments SELECT * FROM comments_old32`,
			`DROP TABLE comments_old32`,
			`CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id, created_at)`,

			// issue_history — recreate with correct FK
			`ALTER TABLE issue_history RENAME TO issue_history_old32`,
			`CREATE TABLE issue_history (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
				snapshot   TEXT NOT NULL,
				changed_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`INSERT OR IGNORE INTO issue_history SELECT * FROM issue_history_old32`,
			`DROP TABLE issue_history_old32`,
			`CREATE INDEX IF NOT EXISTS idx_issue_history_issue ON issue_history(issue_id, changed_at)`,

			// issue_relations — recreate with correct FK + CHECK on type
			`ALTER TABLE issue_relations RENAME TO issue_relations_old32`,
			`CREATE TABLE issue_relations (
				source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				type      TEXT NOT NULL
				          CHECK(type IN ('groups','sprint','depends_on','impacts')),
				PRIMARY KEY (source_id, target_id, type)
			)`,
			`INSERT OR IGNORE INTO issue_relations SELECT source_id, target_id, type
			 FROM issue_relations_old32`,
			`DROP TABLE issue_relations_old32`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_source
			 ON issue_relations(source_id, type)`,
			`CREATE INDEX IF NOT EXISTS idx_issue_relations_target
			 ON issue_relations(target_id, type)`,

			// time_entries — rename ticket_id→issue_id for consistency
			`ALTER TABLE time_entries RENAME TO time_entries_old32`,
			`CREATE TABLE time_entries (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
				user_id    INTEGER NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
				started_at TEXT NOT NULL,
				stopped_at TEXT,
				override   REAL,
				comment    TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`INSERT OR IGNORE INTO time_entries(id, issue_id, user_id, started_at, stopped_at, override, comment, created_at)
			 SELECT id, ticket_id, user_id, started_at, stopped_at, override, comment, created_at
			 FROM time_entries_old32`,
			`DROP TABLE time_entries_old32`,
			`CREATE INDEX IF NOT EXISTS idx_time_entries_issue ON time_entries(issue_id)`,
			`CREATE INDEX IF NOT EXISTS idx_time_entries_user  ON time_entries(user_id)`,

			// Add missing FK indexes on other tables
			`CREATE INDEX IF NOT EXISTS idx_totp_pending_user   ON totp_pending(user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_projects_owner      ON projects(product_owner)`,

			`PRAGMA foreign_keys=ON`,

			// ── Step 5: Add CHECK constraints to projects + users via ALTER TABLE ─
			// SQLite doesn't support ALTER TABLE ADD CONSTRAINT.
			// Instead we enforce via app logic (already done) — document the expected
			// values here via comments in this migration for future reference.
			// projects.status: active | archived | deleted
			// users.status:    active | inactive | deleted
			// (Full table recreation not worth it — no data enforcement gap in practice)

			// ── Step 6+7: Project FTS triggers (drop orphans, recreate clean) ─────
			// Original trg_projects_ai/au/ad were created in M3 on the pre-M10
			// projects table, then orphaned when M10 dropped that table.
			// M10 created trg_projects_ai2/au2/ad2 on the new table.
			// This migration: drop all, recreate with clean names (no "2" suffix).
			`DROP TRIGGER IF EXISTS trg_projects_ai`,
			`DROP TRIGGER IF EXISTS trg_projects_au`,
			`DROP TRIGGER IF EXISTS trg_projects_ad`,
			`DROP TRIGGER IF EXISTS trg_projects_ai2`,
			`DROP TRIGGER IF EXISTS trg_projects_au2`,
			`DROP TRIGGER IF EXISTS trg_projects_ad2`,
			`CREATE TRIGGER trg_projects_ai
				AFTER INSERT ON projects BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('project', NEW.id,
						COALESCE(NEW.name,'') || ' ' || COALESCE(NEW.key,'') || ' ' ||
						COALESCE(NEW.description,''));
				END`,
			`CREATE TRIGGER trg_projects_au
				AFTER UPDATE ON projects BEGIN
					DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('project', NEW.id,
						COALESCE(NEW.name,'') || ' ' || COALESCE(NEW.key,'') || ' ' ||
						COALESCE(NEW.description,''));
				END`,
			`CREATE TRIGGER trg_projects_ad
				AFTER DELETE ON projects BEGIN
					DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
				END`,

			// ── Step 8: Update user FTS triggers to include profile fields ────────
			// M3 triggers only indexed username + role.
			// M25 added nickname, first_name, last_name, email — never indexed.
			`DROP TRIGGER IF EXISTS trg_users_ai`,
			`DROP TRIGGER IF EXISTS trg_users_au`,
			`DROP TRIGGER IF EXISTS trg_users_ad`,
			`CREATE TRIGGER trg_users_ai
				AFTER INSERT ON users BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('user', NEW.id,
						COALESCE(NEW.username,'') || ' ' ||
						COALESCE(NEW.nickname,'') || ' ' ||
						COALESCE(NEW.first_name,'') || ' ' ||
						COALESCE(NEW.last_name,'') || ' ' ||
						COALESCE(NEW.email,'') || ' ' ||
						COALESCE(NEW.role,''));
				END`,
			`CREATE TRIGGER trg_users_au
				AFTER UPDATE ON users BEGIN
					DELETE FROM search_index WHERE entity_type='user' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('user', NEW.id,
						COALESCE(NEW.username,'') || ' ' ||
						COALESCE(NEW.nickname,'') || ' ' ||
						COALESCE(NEW.first_name,'') || ' ' ||
						COALESCE(NEW.last_name,'') || ' ' ||
						COALESCE(NEW.email,'') || ' ' ||
						COALESCE(NEW.role,''));
				END`,
			`CREATE TRIGGER trg_users_ad
				AFTER DELETE ON users BEGIN
					DELETE FROM search_index WHERE entity_type='user' AND entity_id=OLD.id;
				END`,

			// ── Step 9: Rebuild issue + user FTS (drop_on/impacts removed; profile added) ─
			`DROP TRIGGER IF EXISTS trg_issues_ai`,
			`DROP TRIGGER IF EXISTS trg_issues_au`,
			`DROP TRIGGER IF EXISTS trg_issues_ad`,
			`CREATE TRIGGER trg_issues_ai
				AFTER INSERT ON issues BEGIN
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						COALESCE(NEW.title,'') || ' ' ||
						COALESCE(NEW.description,'') || ' ' ||
						COALESCE(NEW.acceptance_criteria,'') || ' ' ||
						COALESCE(NEW.notes,'') || ' ' ||
						COALESCE(NEW.cost_unit,'') || ' ' ||
						COALESCE(NEW.release,'') || ' ' ||
						COALESCE(NEW.type,'') || ' ' ||
						COALESCE(NEW.jira_id,'') || ' ' ||
						COALESCE(NEW.jira_version,'') || ' ' ||
						COALESCE(NEW.jira_text,''));
				END`,
			`CREATE TRIGGER trg_issues_au
				AFTER UPDATE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
					INSERT INTO search_index(entity_type, entity_id, content)
					VALUES('issue', NEW.id,
						COALESCE(NEW.title,'') || ' ' ||
						COALESCE(NEW.description,'') || ' ' ||
						COALESCE(NEW.acceptance_criteria,'') || ' ' ||
						COALESCE(NEW.notes,'') || ' ' ||
						COALESCE(NEW.cost_unit,'') || ' ' ||
						COALESCE(NEW.release,'') || ' ' ||
						COALESCE(NEW.type,'') || ' ' ||
						COALESCE(NEW.jira_id,'') || ' ' ||
						COALESCE(NEW.jira_version,'') || ' ' ||
						COALESCE(NEW.jira_text,''));
				END`,
			`CREATE TRIGGER trg_issues_ad
				AFTER DELETE ON issues BEGIN
					DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
				END`,

			// Backfill FTS — issues (without removed columns), users (with profile)
			`DELETE FROM search_index WHERE entity_type='issue'`,
			`INSERT INTO search_index(entity_type, entity_id, content)
				SELECT 'issue', id,
					COALESCE(title,'') || ' ' ||
					COALESCE(description,'') || ' ' ||
					COALESCE(acceptance_criteria,'') || ' ' ||
					COALESCE(notes,'') || ' ' ||
					COALESCE(cost_unit,'') || ' ' ||
					COALESCE(release,'') || ' ' ||
					COALESCE(type,'') || ' ' ||
					COALESCE(jira_id,'') || ' ' ||
					COALESCE(jira_version,'') || ' ' ||
					COALESCE(jira_text,'')
				FROM issues`,
			`DELETE FROM search_index WHERE entity_type='user'`,
			`INSERT INTO search_index(entity_type, entity_id, content)
				SELECT 'user', id,
					COALESCE(username,'') || ' ' ||
					COALESCE(nickname,'') || ' ' ||
					COALESCE(first_name,'') || ' ' ||
					COALESCE(last_name,'') || ' ' ||
					COALESCE(email,'') || ' ' ||
			COALESCE(role,'')
			FROM users`,
		}},

	// ── Migration 33 — estimate + AR fields, rename rate_package→rate_lp,
	//    fix comment FTS triggers orphaned by M32 table recreation ─────────
	{33, []string{
		// ── Step 1: Fix comment FTS triggers (orphaned by M32 comments table recreation)
		`DROP TRIGGER IF EXISTS trg_comments_ai`,
		`DROP TRIGGER IF EXISTS trg_comments_ad`,
		`CREATE TRIGGER trg_comments_ai
			AFTER INSERT ON comments BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('comment', NEW.id, COALESCE(NEW.body,''));
			END`,
		`CREATE TRIGGER trg_comments_ad
			AFTER DELETE ON comments BEGIN
				DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.id;
			END`,

		// Backfill comment FTS (any comments created after M32 are missing)
		`DELETE FROM search_index WHERE entity_type='comment'`,
		`INSERT INTO search_index(entity_type, entity_id, content)
			SELECT 'comment', id, COALESCE(body,'') FROM comments`,

		// ── Step 2: Add new estimate + AR columns (additive)
		`ALTER TABLE issues ADD COLUMN estimate_hours REAL`,
		`ALTER TABLE issues ADD COLUMN estimate_lp    REAL`,
		`ALTER TABLE issues ADD COLUMN ar_hours        REAL`,
		`ALTER TABLE issues ADD COLUMN ar_lp           REAL`,

		// ── Step 3: Rename rate_package → rate_lp via table recreation
		`PRAGMA foreign_keys=OFF`,

		`DROP TABLE IF EXISTS issues_old33`,
		`ALTER TABLE issues RENAME TO issues_old33`,
		`CREATE TABLE issues (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			issue_number        INTEGER NOT NULL DEFAULT 0,
			type                TEXT NOT NULL DEFAULT 'ticket'
			                    CHECK(type IN ('epic','cost_unit','release','sprint','ticket','task')),
			parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
			title               TEXT NOT NULL,
			description         TEXT NOT NULL DEFAULT '',
			acceptance_criteria TEXT NOT NULL DEFAULT '',
			notes               TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'backlog'
			                    CHECK(status IN ('backlog','in-progress','done','cancelled')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('low','medium','high')),
			cost_unit           TEXT NOT NULL DEFAULT '',
			release             TEXT NOT NULL DEFAULT '',
			billing_type        TEXT NOT NULL DEFAULT '',
			total_budget        REAL,
			rate_hourly         REAL,
			rate_lp             REAL,
			start_date          TEXT NOT NULL DEFAULT '',
			end_date            TEXT NOT NULL DEFAULT '',
			group_state         TEXT NOT NULL DEFAULT '',
			sprint_state        TEXT NOT NULL DEFAULT '',
			jira_id             TEXT NOT NULL DEFAULT '',
			jira_version        TEXT NOT NULL DEFAULT '',
			jira_text           TEXT NOT NULL DEFAULT '',
			estimate_hours      REAL,
			estimate_lp         REAL,
			ar_hours            REAL,
			ar_lp               REAL,
			archived            INTEGER NOT NULL DEFAULT 0,
			assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at          TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issues
			SELECT id, project_id, issue_number, type, parent_id,
			       title, description, acceptance_criteria, notes,
			       status, priority, cost_unit, release,
			       billing_type, total_budget, rate_hourly, rate_package,
			       start_date, end_date, group_state, sprint_state,
			       jira_id, jira_version, jira_text,
			       estimate_hours, estimate_lp, ar_hours, ar_lp,
			       archived, assignee_id, created_at, updated_at
			FROM issues_old33`,
		`DROP TABLE issues_old33`,

		// Recreate child tables (SQLite FK rewrite bug — same as M27/M31/M32)
		`DROP TABLE IF EXISTS issue_tags_old33`,
		`ALTER TABLE issue_tags RENAME TO issue_tags_old33`,
		`CREATE TABLE issue_tags (
			issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, tag_id)
		)`,
		`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old33`,
		`DROP TABLE issue_tags_old33`,

		`DROP TABLE IF EXISTS comments_old33`,
		`ALTER TABLE comments RENAME TO comments_old33`,
		`CREATE TABLE comments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body       TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO comments SELECT * FROM comments_old33`,
		`DROP TABLE comments_old33`,

		`DROP TABLE IF EXISTS issue_history_old33`,
		`ALTER TABLE issue_history RENAME TO issue_history_old33`,
		`CREATE TABLE issue_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			snapshot   TEXT NOT NULL,
			changed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO issue_history SELECT * FROM issue_history_old33`,
		`DROP TABLE issue_history_old33`,

		`DROP TABLE IF EXISTS issue_relations_old33`,
		`ALTER TABLE issue_relations RENAME TO issue_relations_old33`,
		`CREATE TABLE issue_relations (
			source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			type      TEXT NOT NULL
			          CHECK(type IN ('groups','sprint','depends_on','impacts')),
			PRIMARY KEY (source_id, target_id, type)
		)`,
		`INSERT OR IGNORE INTO issue_relations SELECT * FROM issue_relations_old33`,
		`DROP TABLE issue_relations_old33`,

		`DROP TABLE IF EXISTS time_entries_old33`,
		`ALTER TABLE time_entries RENAME TO time_entries_old33`,
		`CREATE TABLE time_entries (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at TEXT NOT NULL DEFAULT (datetime('now')),
			stopped_at TEXT,
			override   REAL,
			comment    TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO time_entries SELECT * FROM time_entries_old33`,
		`DROP TABLE time_entries_old33`,

		`PRAGMA foreign_keys=ON`,

		// Recreate indexes (dropped with old tables)
		`CREATE INDEX IF NOT EXISTS idx_issues_project     ON issues(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_parent      ON issues(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_assignee    ON issues(assignee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_type        ON issues(type)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_status      ON issues(status)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_tags_tag     ON issue_tags(tag_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_issue     ON comments(issue_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_history_issue ON issue_history(issue_id, changed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_source ON issue_relations(source_id, type)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_target ON issue_relations(target_id, type)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_issue ON time_entries(issue_id)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_user  ON time_entries(user_id)`,

		// Recreate FTS triggers (orphaned by table rename)
		`DROP TRIGGER IF EXISTS trg_issues_ai`,
		`DROP TRIGGER IF EXISTS trg_issues_au`,
		`DROP TRIGGER IF EXISTS trg_issues_ad`,
		`CREATE TRIGGER trg_issues_ai
			AFTER INSERT ON issues BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' ||
					COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' ||
					COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' ||
					COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.type,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' ||
					COALESCE(NEW.jira_version,'') || ' ' ||
					COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_au
			AFTER UPDATE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' ||
					COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' ||
					COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' ||
					COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.type,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' ||
					COALESCE(NEW.jira_version,'') || ' ' ||
					COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_ad
			AFTER DELETE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
			END`,

		// Recreate comment FTS triggers (orphaned again by comments recreation)
		`DROP TRIGGER IF EXISTS trg_comments_ai`,
		`DROP TRIGGER IF EXISTS trg_comments_ad`,
		`CREATE TRIGGER trg_comments_ai
			AFTER INSERT ON comments BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('comment', NEW.id, COALESCE(NEW.body,''));
			END`,
		`CREATE TRIGGER trg_comments_ad
			AFTER DELETE ON comments BEGIN
				DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.id;
			END`,
	}},

	// ── Migration 34 — epic color field ──────────────────────────────────────
	{34, []string{
		`ALTER TABLE issues ADD COLUMN color TEXT`,
	}},

	// ── Migration 35 — attachments table ─────────────────────────────────────
	{35, []string{
		`CREATE TABLE IF NOT EXISTS attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			object_key   TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL,
			uploaded_by  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_attachments_issue ON attachments(issue_id)`,
	}},

	// ── Migration 36 — seed standard admin-default views ──────────────────────
	// Seeds Issues, Epics, Cost Units, Releases admin-default views if they
	// don't already exist. Each INSERT is independently guarded so existing
	// views (e.g. Epics created manually) are never overwritten.
	{36, []string{
		// Issues — tickets and tasks, hides billing/budget/Jira fields
		`INSERT INTO views (user_id, title, description, columns_json, filters_json, is_shared, is_admin_default)
		 SELECT u.id,
		        'Issues',
		        'Tickets and tasks. Hides billing, budget and Jira fields.',
		        '["billing_type","total_budget","rate_hourly","rate_lp","estimate_hours","estimate_lp","ar_hours","ar_lp","group_state","sprint_state","jira_id","jira_version","jira_text"]',
		        '{"type":["ticket","task"]}',
		        1, 1
		 FROM users u
		 WHERE u.role = 'admin'
		   AND NOT EXISTS (SELECT 1 FROM views WHERE is_admin_default = 1 AND title = 'Issues')
		 ORDER BY u.id LIMIT 1`,
		// Epics — billing and timeline fields visible, sprint/Jira hidden
		`INSERT INTO views (user_id, title, description, columns_json, filters_json, is_shared, is_admin_default)
		 SELECT u.id,
		        'Epics',
		        'Epic planning view with billing and timeline fields.',
		        '["cost_unit","release","sprint","sprint_state","jira_id","jira_version","jira_text"]',
		        '{"type":["epic"]}',
		        1, 1
		 FROM users u
		 WHERE u.role = 'admin'
		   AND NOT EXISTS (SELECT 1 FROM views WHERE is_admin_default = 1 AND title = 'Epics')
		 ORDER BY u.id LIMIT 1`,
		// Cost Units — billing and estimation fields visible, Jira/sprint hidden
		`INSERT INTO views (user_id, title, description, columns_json, filters_json, is_shared, is_admin_default)
		 SELECT u.id,
		        'Cost Units',
		        'Cost unit overview with billing and estimation fields.',
		        '["epic","sprint","sprint_state","jira_id","jira_version","jira_text"]',
		        '{"type":["cost_unit"]}',
		        1, 1
		 FROM users u
		 WHERE u.role = 'admin'
		   AND NOT EXISTS (SELECT 1 FROM views WHERE is_admin_default = 1 AND title = 'Cost Units')
		 ORDER BY u.id LIMIT 1`,
		// Releases — timeline and group state visible, finance/Jira hidden
		`INSERT INTO views (user_id, title, description, columns_json, filters_json, is_shared, is_admin_default)
		 SELECT u.id,
		        'Releases',
		        'Release planning with timeline and group state.',
		        '["billing_type","total_budget","rate_hourly","rate_lp","estimate_hours","estimate_lp","ar_hours","ar_lp","sprint_state","jira_id","jira_version","jira_text"]',
		        '{"type":["release"]}',
		        1, 1
		 FROM users u
		 WHERE u.role = 'admin'
		   AND NOT EXISTS (SELECT 1 FROM views WHERE is_admin_default = 1 AND title = 'Releases')
		 ORDER BY u.id LIMIT 1`,
	}},

	// ── Migration 37 — project logo ───────────────────────────────────────────
	{37, []string{
		`ALTER TABLE projects ADD COLUMN logo_path TEXT NOT NULL DEFAULT ''`,
	}},

	// ── Migration 38 — recent projects per user ───────────────────────────────
	{38, []string{
		`CREATE TABLE IF NOT EXISTS user_recent_projects (
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			visited_at TEXT    NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (user_id, project_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_urp_user_visited ON user_recent_projects(user_id, visited_at DESC)`,
		`ALTER TABLE users ADD COLUMN recent_projects_limit INTEGER NOT NULL DEFAULT 3`,
	}},

	// ── Migration 39 — internal hourly rate ───────────────────────────────────
	{39, []string{
		`ALTER TABLE users ADD COLUMN internal_rate_hourly REAL`,
		`ALTER TABLE time_entries ADD COLUMN internal_rate_hourly REAL`,
	}},

	// ── Migration 40 — nullable issue_id on attachments (pending uploads) ──
	{40, []string{
		`PRAGMA foreign_keys=OFF`,
		`ALTER TABLE attachments RENAME TO attachments_old`,
		`CREATE TABLE attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER REFERENCES issues(id) ON DELETE CASCADE,
			object_key   TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL DEFAULT 0,
			uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO attachments SELECT * FROM attachments_old`,
		`DROP TABLE attachments_old`,
		`CREATE INDEX IF NOT EXISTS idx_attachments_issue ON attachments(issue_id)`,
		`PRAGMA foreign_keys=ON`,
	}},
	// Migration 44: per-user alt-unit display preferences
	{44, []string{
		`ALTER TABLE users ADD COLUMN show_alt_unit_table INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN show_alt_unit_detail INTEGER NOT NULL DEFAULT 0`,
	}},

	// Migration 43: created_by on issues — tracks who created the issue
	{43, []string{
		`ALTER TABLE issues ADD COLUMN created_by INTEGER REFERENCES users(id) ON DELETE SET NULL`,
		// Backfill from the earliest issue_history entry (the creation snapshot)
		`UPDATE issues SET created_by = (
			SELECT changed_by FROM issue_history
			WHERE issue_id = issues.id
			ORDER BY changed_at ASC, id ASC LIMIT 1
		) WHERE created_by IS NULL`,
	}},

	// Migration 42: View management — sort_order, hidden, user_view_pins
	{42, []string{
		`ALTER TABLE views ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE views ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0`,
		// Backfill sort_order for existing admin-default views (alphabetical by title)
		`UPDATE views SET sort_order = (
			SELECT COUNT(*) FROM views v2
			WHERE v2.is_admin_default = 1 AND v2.title < views.title
		) WHERE is_admin_default = 1`,
		// User view pins table — lazy init (no rows = all defaults shown)
		`CREATE TABLE IF NOT EXISTS user_view_pins (
			user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			view_id  INTEGER NOT NULL REFERENCES views(id) ON DELETE CASCADE,
			pinned   INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (user_id, view_id)
		)`,
	}},

	// Migration 41: Drop porter stemmer from FTS5 — use plain ascii tokenizer.
	// Porter reduces "onboarding" → "onboard", breaking prefix queries like "onboardi*".
	// At <300 issues/project, stemming gain is negligible; plain ascii prefix search is correct.
	{41, []string{
		// Drop and recreate the FTS5 virtual table with ascii-only tokenizer
		`DROP TABLE IF EXISTS search_index`,
		`CREATE VIRTUAL TABLE search_index USING fts5(
			entity_type,
			entity_id UNINDEXED,
			content,
			tokenize='ascii'
		)`,
		// Backfill all entities
		`INSERT INTO search_index(entity_type, entity_id, content)
			SELECT 'project', id,
				COALESCE(name,'') || ' ' || COALESCE(key,'') || ' ' || COALESCE(description,'')
			FROM projects`,
		`INSERT INTO search_index(entity_type, entity_id, content)
			SELECT 'issue', id,
				COALESCE(title,'') || ' ' ||
				COALESCE(description,'') || ' ' ||
				COALESCE(acceptance_criteria,'') || ' ' ||
				COALESCE(notes,'') || ' ' ||
				COALESCE(cost_unit,'') || ' ' ||
				COALESCE(release,'') || ' ' ||
				COALESCE(type,'') || ' ' ||
				COALESCE(jira_id,'') || ' ' ||
				COALESCE(jira_version,'') || ' ' ||
				COALESCE(jira_text,'')
			FROM issues`,
		`INSERT INTO search_index(entity_type, entity_id, content)
			SELECT 'user', id,
				COALESCE(username,'') || ' ' ||
				COALESCE(nickname,'') || ' ' ||
				COALESCE(first_name,'') || ' ' ||
				COALESCE(last_name,'') || ' ' ||
				COALESCE(email,'') || ' ' ||
				COALESCE(role,'')
			FROM users`,
		`INSERT INTO search_index(entity_type, entity_id, content)
			SELECT 'tag', id,
				COALESCE(name,'') || ' ' || COALESCE(description,'')
			FROM tags`,
		`INSERT INTO search_index(entity_type, entity_id, content)
			SELECT 'comment', id, COALESCE(body,'') FROM comments`,
	}},

	// ── Migration 45 — external user role + user_project_access ──
	// Extends users.role CHECK to include 'external'.
	// Creates user_project_access table for per-project visibility.
	// Adds accepted_at/accepted_by columns to issues for customer acceptance.
	// NOTE: Recreated tables include columns from M42-44 (sort_order, hidden, created_by, alt-unit prefs).
	{45, []string{
		`PRAGMA foreign_keys=OFF`,

		// Recreate users with expanded role CHECK + M44 columns
		`DROP TABLE IF EXISTS users_old45`,
		`ALTER TABLE users RENAME TO users_old45`,
		`CREATE TABLE users (
			id                    INTEGER PRIMARY KEY AUTOINCREMENT,
			username              TEXT NOT NULL UNIQUE,
			password              TEXT NOT NULL,
			role                  TEXT NOT NULL DEFAULT 'member'
			                      CHECK(role IN ('admin','member','external')),
			status                TEXT NOT NULL DEFAULT 'active',
			created_at            TEXT NOT NULL DEFAULT (datetime('now')),
			nickname              TEXT NOT NULL DEFAULT '',
			first_name            TEXT NOT NULL DEFAULT '',
			last_name             TEXT NOT NULL DEFAULT '',
			email                 TEXT NOT NULL DEFAULT '',
			avatar_path           TEXT NOT NULL DEFAULT '',
			markdown_default      INTEGER NOT NULL DEFAULT 0,
			monospace_fields      INTEGER NOT NULL DEFAULT 0,
			recent_projects_limit INTEGER NOT NULL DEFAULT 3,
			internal_rate_hourly  REAL,
			show_alt_unit_table   INTEGER NOT NULL DEFAULT 0,
			show_alt_unit_detail  INTEGER NOT NULL DEFAULT 0,
			totp_secret           TEXT NOT NULL DEFAULT '',
			totp_enabled          INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT INTO users (id,username,password,role,status,created_at,nickname,first_name,last_name,email,avatar_path,markdown_default,monospace_fields,recent_projects_limit,internal_rate_hourly,show_alt_unit_table,show_alt_unit_detail,totp_secret,totp_enabled)
			SELECT id,username,password,role,status,created_at,nickname,first_name,last_name,email,avatar_path,markdown_default,monospace_fields,recent_projects_limit,internal_rate_hourly,show_alt_unit_table,show_alt_unit_detail,totp_secret,totp_enabled FROM users_old45`,
		`DROP TABLE users_old45`,

		// Recreate sessions (FK to users)
		`DROP TABLE IF EXISTS sessions_old45`,
		`ALTER TABLE sessions RENAME TO sessions_old45`,
		`CREATE TABLE sessions (
			id         TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO sessions SELECT * FROM sessions_old45`,
		`DROP TABLE sessions_old45`,

		// Recreate api_keys (FK to users)
		`DROP TABLE IF EXISTS api_keys_old45`,
		`ALTER TABLE api_keys RENAME TO api_keys_old45`,
		`CREATE TABLE api_keys (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name         TEXT NOT NULL,
			key_hash     TEXT NOT NULL UNIQUE,
			key_prefix   TEXT NOT NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now')),
			last_used_at TEXT
		)`,
		`INSERT OR IGNORE INTO api_keys SELECT * FROM api_keys_old45`,
		`DROP TABLE api_keys_old45`,

		// Recreate totp_pending (FK to users)
		`DROP TABLE IF EXISTS totp_pending_old45`,
		`ALTER TABLE totp_pending RENAME TO totp_pending_old45`,
		`CREATE TABLE totp_pending (
			token      TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO totp_pending SELECT * FROM totp_pending_old45`,
		`DROP TABLE totp_pending_old45`,

		// Recreate user_recent_projects (FK to users)
		`DROP TABLE IF EXISTS user_recent_projects_old45`,
		`ALTER TABLE user_recent_projects RENAME TO user_recent_projects_old45`,
		`CREATE TABLE user_recent_projects (
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			visited_at TEXT    NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (user_id, project_id)
		)`,
		`INSERT OR IGNORE INTO user_recent_projects SELECT * FROM user_recent_projects_old45`,
		`DROP TABLE user_recent_projects_old45`,

		// Recreate projects (FK product_owner -> users)
		`DROP TABLE IF EXISTS projects_old45`,
		`ALTER TABLE projects RENAME TO projects_old45`,
		`CREATE TABLE projects (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			name          TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL DEFAULT 'active',
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
			key           TEXT NOT NULL DEFAULT '',
			product_owner INTEGER REFERENCES users(id) ON DELETE SET NULL,
			customer_id   TEXT NOT NULL DEFAULT '',
			logo_path     TEXT NOT NULL DEFAULT ''
		)`,
		`INSERT INTO projects SELECT * FROM projects_old45`,
		`DROP TABLE projects_old45`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_key ON projects(key)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(product_owner)`,
		// Recreate project_tags (FK orphaned by projects rename)
		`DROP TABLE IF EXISTS project_tags_old45`,
		`ALTER TABLE project_tags RENAME TO project_tags_old45`,
		`CREATE TABLE project_tags (
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (project_id, tag_id)
		)`,
		`INSERT OR IGNORE INTO project_tags SELECT * FROM project_tags_old45`,
		`DROP TABLE project_tags_old45`,

		// Recreate views (FK user_id -> users) + M42 columns
		`DROP TABLE IF EXISTS views_old45`,
		`ALTER TABLE views RENAME TO views_old45`,
		`CREATE TABLE views (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title            TEXT    NOT NULL,
			description      TEXT    NOT NULL DEFAULT '',
			columns_json     TEXT    NOT NULL DEFAULT '[]',
			filters_json     TEXT    NOT NULL DEFAULT '{}',
			is_shared        INTEGER NOT NULL DEFAULT 0,
			is_admin_default INTEGER NOT NULL DEFAULT 0,
			sort_order       INTEGER NOT NULL DEFAULT 0,
			hidden           INTEGER NOT NULL DEFAULT 0,
			created_at       TEXT    NOT NULL DEFAULT (datetime('now')),
			updated_at       TEXT    NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO views (id,user_id,title,description,columns_json,filters_json,is_shared,is_admin_default,sort_order,hidden,created_at,updated_at)
			SELECT id,user_id,title,description,columns_json,filters_json,is_shared,is_admin_default,sort_order,hidden,created_at,updated_at FROM views_old45`,
		`DROP TABLE views_old45`,
		`CREATE INDEX IF NOT EXISTS idx_views_user ON views(user_id)`,
		// Recreate user_view_pins (FK to users + views — M42)
		`DROP TABLE IF EXISTS user_view_pins_old45`,
		`ALTER TABLE user_view_pins RENAME TO user_view_pins_old45`,
		`CREATE TABLE user_view_pins (
			user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			view_id  INTEGER NOT NULL REFERENCES views(id) ON DELETE CASCADE,
			pinned   INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (user_id, view_id)
		)`,
		`INSERT OR IGNORE INTO user_view_pins SELECT * FROM user_view_pins_old45`,
		`DROP TABLE user_view_pins_old45`,

		// Recreate issues (FK assignee_id -> users)
		`DROP TABLE IF EXISTS issues_old45`,
		`ALTER TABLE issues RENAME TO issues_old45`,
		`CREATE TABLE issues (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			issue_number        INTEGER NOT NULL DEFAULT 0,
			type                TEXT NOT NULL DEFAULT 'ticket'
			                    CHECK(type IN ('epic','cost_unit','release','sprint','ticket','task')),
			parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
			title               TEXT NOT NULL,
			description         TEXT NOT NULL DEFAULT '',
			acceptance_criteria TEXT NOT NULL DEFAULT '',
			notes               TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'backlog'
			                    CHECK(status IN ('backlog','in-progress','done','cancelled')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('low','medium','high')),
			cost_unit           TEXT NOT NULL DEFAULT '',
			release             TEXT NOT NULL DEFAULT '',
			billing_type        TEXT NOT NULL DEFAULT '',
			total_budget        REAL,
			rate_hourly         REAL,
			rate_lp             REAL,
			start_date          TEXT NOT NULL DEFAULT '',
			end_date            TEXT NOT NULL DEFAULT '',
			group_state         TEXT NOT NULL DEFAULT '',
			sprint_state        TEXT NOT NULL DEFAULT '',
			jira_id             TEXT NOT NULL DEFAULT '',
			jira_version        TEXT NOT NULL DEFAULT '',
			jira_text           TEXT NOT NULL DEFAULT '',
			estimate_hours      REAL,
			estimate_lp         REAL,
			ar_hours            REAL,
			ar_lp               REAL,
			color               TEXT,
			archived            INTEGER NOT NULL DEFAULT 0,
			assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_by          INTEGER REFERENCES users(id) ON DELETE SET NULL,
			accepted_at         TEXT,
			accepted_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at          TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issues (
			id, project_id, issue_number, type, parent_id,
			title, description, acceptance_criteria, notes,
			status, priority, cost_unit, release,
			billing_type, total_budget, rate_hourly, rate_lp,
			start_date, end_date, group_state, sprint_state,
			jira_id, jira_version, jira_text,
			estimate_hours, estimate_lp, ar_hours, ar_lp,
			color, archived, assignee_id, created_by,
			created_at, updated_at
		) SELECT
			id, project_id, issue_number, type, parent_id,
			title, description, acceptance_criteria, notes,
			status, priority, cost_unit, release,
			billing_type, total_budget, rate_hourly, rate_lp,
			start_date, end_date, group_state, sprint_state,
			jira_id, jira_version, jira_text,
			estimate_hours, estimate_lp, ar_hours, ar_lp,
			color, archived, assignee_id, created_by,
			created_at, updated_at
		FROM issues_old45`,
		`DROP TABLE issues_old45`,

		// Recreate child tables (SQLite FK rewrite bug)
		`DROP TABLE IF EXISTS issue_tags_old45`,
		`ALTER TABLE issue_tags RENAME TO issue_tags_old45`,
		`CREATE TABLE issue_tags (
			issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, tag_id)
		)`,
		`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old45`,
		`DROP TABLE issue_tags_old45`,

		`DROP TABLE IF EXISTS comments_old45`,
		`ALTER TABLE comments RENAME TO comments_old45`,
		`CREATE TABLE comments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body       TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO comments SELECT * FROM comments_old45`,
		`DROP TABLE comments_old45`,

		`DROP TABLE IF EXISTS issue_history_old45`,
		`ALTER TABLE issue_history RENAME TO issue_history_old45`,
		`CREATE TABLE issue_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			snapshot   TEXT NOT NULL,
			changed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO issue_history SELECT * FROM issue_history_old45`,
		`DROP TABLE issue_history_old45`,

		`DROP TABLE IF EXISTS issue_relations_old45`,
		`ALTER TABLE issue_relations RENAME TO issue_relations_old45`,
		`CREATE TABLE issue_relations (
			source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			type      TEXT NOT NULL
			          CHECK(type IN ('groups','sprint','depends_on','impacts')),
			PRIMARY KEY (source_id, target_id, type)
		)`,
		`INSERT OR IGNORE INTO issue_relations SELECT * FROM issue_relations_old45`,
		`DROP TABLE issue_relations_old45`,

		`DROP TABLE IF EXISTS time_entries_old45`,
		`ALTER TABLE time_entries RENAME TO time_entries_old45`,
		`CREATE TABLE time_entries (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id             INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			user_id              INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at           TEXT NOT NULL DEFAULT (datetime('now')),
			stopped_at           TEXT,
			override             REAL,
			comment              TEXT NOT NULL DEFAULT '',
			created_at           TEXT NOT NULL DEFAULT (datetime('now')),
			internal_rate_hourly REAL
		)`,
		`INSERT OR IGNORE INTO time_entries SELECT * FROM time_entries_old45`,
		`DROP TABLE time_entries_old45`,

		`DROP TABLE IF EXISTS attachments_old45`,
		`ALTER TABLE attachments RENAME TO attachments_old45`,
		`CREATE TABLE attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER REFERENCES issues(id) ON DELETE CASCADE,
			object_key   TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL DEFAULT 0,
			uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO attachments SELECT * FROM attachments_old45`,
		`DROP TABLE attachments_old45`,

		`PRAGMA foreign_keys=ON`,

		// Recreate all indexes
		`CREATE INDEX IF NOT EXISTS idx_issues_project     ON issues(project_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_project_number
		 ON issues(project_id, issue_number) WHERE project_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_issues_parent      ON issues(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_type        ON issues(type)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_status      ON issues(status)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_assignee    ON issues(assignee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_updated     ON issues(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_costunit    ON issues(cost_unit)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_release     ON issues(release)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_tags_tag     ON issue_tags(tag_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_issue     ON comments(issue_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_history_issue ON issue_history(issue_id, changed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_source ON issue_relations(source_id, type)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_target ON issue_relations(target_id, type)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_issue ON time_entries(issue_id)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_user  ON time_entries(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_totp_pending_user  ON totp_pending(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_attachments_issue  ON attachments(issue_id)`,
		`CREATE INDEX IF NOT EXISTS idx_urp_user_visited   ON user_recent_projects(user_id, visited_at DESC)`,

		// Recreate FTS triggers (orphaned by table renames)
		`DROP TRIGGER IF EXISTS trg_issues_ai`,
		`DROP TRIGGER IF EXISTS trg_issues_au`,
		`DROP TRIGGER IF EXISTS trg_issues_ad`,
		`CREATE TRIGGER trg_issues_ai
			AFTER INSERT ON issues BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' ||
					COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' ||
					COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' ||
					COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.type,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' ||
					COALESCE(NEW.jira_version,'') || ' ' ||
					COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_au
			AFTER UPDATE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' ||
					COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' ||
					COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' ||
					COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.type,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' ||
					COALESCE(NEW.jira_version,'') || ' ' ||
					COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_ad
			AFTER DELETE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
			END`,

		`DROP TRIGGER IF EXISTS trg_comments_ai`,
		`DROP TRIGGER IF EXISTS trg_comments_ad`,
		`CREATE TRIGGER trg_comments_ai
			AFTER INSERT ON comments BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('comment', NEW.id, COALESCE(NEW.body,''));
			END`,
		`CREATE TRIGGER trg_comments_ad
			AFTER DELETE ON comments BEGIN
				DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.id;
			END`,

		`DROP TRIGGER IF EXISTS trg_users_ai`,
		`DROP TRIGGER IF EXISTS trg_users_au`,
		`DROP TRIGGER IF EXISTS trg_users_ad`,
		`CREATE TRIGGER trg_users_ai
			AFTER INSERT ON users BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('user', NEW.id,
					COALESCE(NEW.username,'') || ' ' ||
					COALESCE(NEW.nickname,'') || ' ' ||
					COALESCE(NEW.first_name,'') || ' ' ||
					COALESCE(NEW.last_name,'') || ' ' ||
					COALESCE(NEW.email,'') || ' ' ||
					COALESCE(NEW.role,''));
			END`,
		`CREATE TRIGGER trg_users_au
			AFTER UPDATE ON users BEGIN
				DELETE FROM search_index WHERE entity_type='user' AND entity_id=OLD.id;
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('user', NEW.id,
					COALESCE(NEW.username,'') || ' ' ||
					COALESCE(NEW.nickname,'') || ' ' ||
					COALESCE(NEW.first_name,'') || ' ' ||
					COALESCE(NEW.last_name,'') || ' ' ||
					COALESCE(NEW.email,'') || ' ' ||
					COALESCE(NEW.role,''));
			END`,
		`CREATE TRIGGER trg_users_ad
			AFTER DELETE ON users BEGIN
				DELETE FROM search_index WHERE entity_type='user' AND entity_id=OLD.id;
			END`,

		// Recreate project FTS triggers (orphaned by projects table rename)
		`DROP TRIGGER IF EXISTS trg_projects_ai`,
		`DROP TRIGGER IF EXISTS trg_projects_au`,
		`DROP TRIGGER IF EXISTS trg_projects_ad`,
		`CREATE TRIGGER trg_projects_ai
			AFTER INSERT ON projects BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('project', NEW.id,
					COALESCE(NEW.name,'') || ' ' || COALESCE(NEW.key,'') || ' ' ||
					COALESCE(NEW.description,''));
			END`,
		`CREATE TRIGGER trg_projects_au
			AFTER UPDATE ON projects BEGIN
				DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('project', NEW.id,
					COALESCE(NEW.name,'') || ' ' || COALESCE(NEW.key,'') || ' ' ||
					COALESCE(NEW.description,''));
			END`,
		`CREATE TRIGGER trg_projects_ad
			AFTER DELETE ON projects BEGIN
				DELETE FROM search_index WHERE entity_type='project' AND entity_id=OLD.id;
			END`,

		// user_project_access table
		`CREATE TABLE user_project_access (
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, project_id)
		)`,
		`CREATE INDEX idx_upa_user ON user_project_access(user_id)`,
	}},

	// ── M46: Add 'new' status to issues ─────────────────────────────────────
	{46, []string{
		`PRAGMA foreign_keys=OFF`,

		// Recreate issues table: add 'new' to CHECK, change DEFAULT to 'new'
		`DROP TABLE IF EXISTS issues_old46`,
		`ALTER TABLE issues RENAME TO issues_old46`,
		`CREATE TABLE issues (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			issue_number        INTEGER NOT NULL DEFAULT 0,
			type                TEXT NOT NULL DEFAULT 'ticket'
			                    CHECK(type IN ('epic','cost_unit','release','sprint','ticket','task')),
			parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
			title               TEXT NOT NULL,
			description         TEXT NOT NULL DEFAULT '',
			acceptance_criteria TEXT NOT NULL DEFAULT '',
			notes               TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'new'
			                    CHECK(status IN ('new','backlog','in-progress','done','cancelled')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('low','medium','high')),
			cost_unit           TEXT NOT NULL DEFAULT '',
			release             TEXT NOT NULL DEFAULT '',
			billing_type        TEXT NOT NULL DEFAULT '',
			total_budget        REAL,
			rate_hourly         REAL,
			rate_lp             REAL,
			start_date          TEXT NOT NULL DEFAULT '',
			end_date            TEXT NOT NULL DEFAULT '',
			group_state         TEXT NOT NULL DEFAULT '',
			sprint_state        TEXT NOT NULL DEFAULT '',
			jira_id             TEXT NOT NULL DEFAULT '',
			jira_version        TEXT NOT NULL DEFAULT '',
			jira_text           TEXT NOT NULL DEFAULT '',
			estimate_hours      REAL,
			estimate_lp         REAL,
			ar_hours            REAL,
			ar_lp               REAL,
			color               TEXT,
			archived            INTEGER NOT NULL DEFAULT 0,
			assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_by          INTEGER REFERENCES users(id) ON DELETE SET NULL,
			accepted_at         TEXT,
			accepted_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at          TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issues (
			id, project_id, issue_number, type, parent_id,
			title, description, acceptance_criteria, notes,
			status, priority, cost_unit, release,
			billing_type, total_budget, rate_hourly, rate_lp,
			start_date, end_date, group_state, sprint_state,
			jira_id, jira_version, jira_text,
			estimate_hours, estimate_lp, ar_hours, ar_lp,
			color, archived, assignee_id, created_by,
			accepted_at, accepted_by,
			created_at, updated_at
		) SELECT
			id, project_id, issue_number, type, parent_id,
			title, description, acceptance_criteria, notes,
			status, priority, cost_unit, release,
			billing_type, total_budget, rate_hourly, rate_lp,
			start_date, end_date, group_state, sprint_state,
			jira_id, jira_version, jira_text,
			estimate_hours, estimate_lp, ar_hours, ar_lp,
			color, archived, assignee_id, created_by,
			accepted_at, accepted_by,
			created_at, updated_at
		FROM issues_old46`,
		`DROP TABLE issues_old46`,

		// Recreate child tables (SQLite FK rewrite bug)
		`DROP TABLE IF EXISTS issue_tags_old46`,
		`ALTER TABLE issue_tags RENAME TO issue_tags_old46`,
		`CREATE TABLE issue_tags (
			issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, tag_id)
		)`,
		`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old46`,
		`DROP TABLE issue_tags_old46`,

		`DROP TABLE IF EXISTS comments_old46`,
		`ALTER TABLE comments RENAME TO comments_old46`,
		`CREATE TABLE comments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body       TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO comments SELECT * FROM comments_old46`,
		`DROP TABLE comments_old46`,

		`DROP TABLE IF EXISTS issue_history_old46`,
		`ALTER TABLE issue_history RENAME TO issue_history_old46`,
		`CREATE TABLE issue_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			snapshot   TEXT NOT NULL,
			changed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO issue_history SELECT * FROM issue_history_old46`,
		`DROP TABLE issue_history_old46`,

		`DROP TABLE IF EXISTS issue_relations_old46`,
		`ALTER TABLE issue_relations RENAME TO issue_relations_old46`,
		`CREATE TABLE issue_relations (
			source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			type      TEXT NOT NULL
			          CHECK(type IN ('groups','sprint','depends_on','impacts')),
			PRIMARY KEY (source_id, target_id, type)
		)`,
		`INSERT OR IGNORE INTO issue_relations SELECT * FROM issue_relations_old46`,
		`DROP TABLE issue_relations_old46`,

		`DROP TABLE IF EXISTS time_entries_old46`,
		`ALTER TABLE time_entries RENAME TO time_entries_old46`,
		`CREATE TABLE time_entries (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id             INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			user_id              INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at           TEXT NOT NULL DEFAULT (datetime('now')),
			stopped_at           TEXT,
			override             REAL,
			comment              TEXT NOT NULL DEFAULT '',
			created_at           TEXT NOT NULL DEFAULT (datetime('now')),
			internal_rate_hourly REAL
		)`,
		`INSERT OR IGNORE INTO time_entries SELECT * FROM time_entries_old46`,
		`DROP TABLE time_entries_old46`,

		`DROP TABLE IF EXISTS attachments_old46`,
		`ALTER TABLE attachments RENAME TO attachments_old46`,
		`CREATE TABLE attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER REFERENCES issues(id) ON DELETE CASCADE,
			object_key   TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL DEFAULT 0,
			uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO attachments SELECT * FROM attachments_old46`,
		`DROP TABLE attachments_old46`,

		`PRAGMA foreign_keys=ON`,

		// Recreate indexes
		`CREATE INDEX IF NOT EXISTS idx_issues_project     ON issues(project_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_project_number
		 ON issues(project_id, issue_number) WHERE project_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_issues_parent      ON issues(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_type        ON issues(type)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_status      ON issues(status)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_assignee    ON issues(assignee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_updated     ON issues(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_costunit    ON issues(cost_unit)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_release     ON issues(release)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_tags_tag     ON issue_tags(tag_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_issue     ON comments(issue_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_history_issue ON issue_history(issue_id, changed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_source ON issue_relations(source_id, type)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_target ON issue_relations(target_id, type)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_issue ON time_entries(issue_id)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_user  ON time_entries(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_attachments_issue  ON attachments(issue_id)`,

		// Recreate FTS triggers (orphaned by table renames)
		`DROP TRIGGER IF EXISTS trg_issues_ai`,
		`DROP TRIGGER IF EXISTS trg_issues_au`,
		`DROP TRIGGER IF EXISTS trg_issues_ad`,
		`CREATE TRIGGER trg_issues_ai
			AFTER INSERT ON issues BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' ||
					COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' ||
					COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' ||
					COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.type,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' ||
					COALESCE(NEW.jira_version,'') || ' ' ||
					COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_au
			AFTER UPDATE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' ||
					COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' ||
					COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' ||
					COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.type,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' ||
					COALESCE(NEW.jira_version,'') || ' ' ||
					COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_ad
			AFTER DELETE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
			END`,

		`DROP TRIGGER IF EXISTS trg_comments_ai`,
		`DROP TRIGGER IF EXISTS trg_comments_ad`,
		`CREATE TRIGGER trg_comments_ai
			AFTER INSERT ON comments BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('comment', NEW.id, COALESCE(NEW.body,''));
			END`,
		`CREATE TRIGGER trg_comments_ad
			AFTER DELETE ON comments BEGIN
				DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.id;
			END`,
	}},

	// ── M47: Add locale column to users ─────────────────────────────────────
	{47, []string{
		`ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT 'en'`,
	}},

	// ── M48: Add time_override to issues ─────────────────────────────────────
	{48, []string{
		`ALTER TABLE issues ADD COLUMN time_override REAL`,
	}},

	// ── M49: Add recent_timers_limit to users ────────────────────────────────
	{49, []string{
		`ALTER TABLE users ADD COLUMN recent_timers_limit INTEGER NOT NULL DEFAULT 5`,
	}},

	// ── M50: Add timezone to users ───────────────────────────────────────────
	{50, []string{
		`ALTER TABLE users ADD COLUMN timezone TEXT NOT NULL DEFAULT 'auto'`,
	}},

	// ── M51: Expand status enum + add invoiced_at/invoice_number ─────────────
	// Adds 'accepted' and 'invoiced' to the status CHECK constraint.
	// Adds invoiced_at TEXT and invoice_number TEXT columns.
	// Must recreate issues + child tables (SQLite FK rewrite bug).
	{51, []string{
		`PRAGMA foreign_keys=OFF`,

		`DROP TABLE IF EXISTS issues_old51`,
		`ALTER TABLE issues RENAME TO issues_old51`,
		`CREATE TABLE issues (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			issue_number        INTEGER NOT NULL DEFAULT 0,
			type                TEXT NOT NULL DEFAULT 'ticket'
			                    CHECK(type IN ('epic','cost_unit','release','sprint','ticket','task')),
			parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
			title               TEXT NOT NULL,
			description         TEXT NOT NULL DEFAULT '',
			acceptance_criteria TEXT NOT NULL DEFAULT '',
			notes               TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'new'
			                    CHECK(status IN ('new','backlog','in-progress','done','accepted','invoiced','cancelled')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('low','medium','high')),
			cost_unit           TEXT NOT NULL DEFAULT '',
			release             TEXT NOT NULL DEFAULT '',
			billing_type        TEXT NOT NULL DEFAULT '',
			total_budget        REAL,
			rate_hourly         REAL,
			rate_lp             REAL,
			start_date          TEXT NOT NULL DEFAULT '',
			end_date            TEXT NOT NULL DEFAULT '',
			group_state         TEXT NOT NULL DEFAULT '',
			sprint_state        TEXT NOT NULL DEFAULT '',
			jira_id             TEXT NOT NULL DEFAULT '',
			jira_version        TEXT NOT NULL DEFAULT '',
			jira_text           TEXT NOT NULL DEFAULT '',
			estimate_hours      REAL,
			estimate_lp         REAL,
			ar_hours            REAL,
			ar_lp               REAL,
			time_override       REAL,
			color               TEXT,
			archived            INTEGER NOT NULL DEFAULT 0,
			assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_by          INTEGER REFERENCES users(id) ON DELETE SET NULL,
			accepted_at         TEXT,
			accepted_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			invoiced_at         TEXT,
			invoice_number      TEXT NOT NULL DEFAULT '',
			created_at          TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issues (
			id, project_id, issue_number, type, parent_id,
			title, description, acceptance_criteria, notes,
			status, priority, cost_unit, release,
			billing_type, total_budget, rate_hourly, rate_lp,
			start_date, end_date, group_state, sprint_state,
			jira_id, jira_version, jira_text,
			estimate_hours, estimate_lp, ar_hours, ar_lp,
			time_override, color, archived, assignee_id, created_by,
			accepted_at, accepted_by,
			created_at, updated_at
		) SELECT
			id, project_id, issue_number, type, parent_id,
			title, description, acceptance_criteria, notes,
			status, priority, cost_unit, release,
			billing_type, total_budget, rate_hourly, rate_lp,
			start_date, end_date, group_state, sprint_state,
			jira_id, jira_version, jira_text,
			estimate_hours, estimate_lp, ar_hours, ar_lp,
			time_override, color, archived, assignee_id, created_by,
			accepted_at, accepted_by,
			created_at, updated_at
		FROM issues_old51`,
		`DROP TABLE issues_old51`,

		// Recreate child tables (SQLite FK rewrite bug)
		`DROP TABLE IF EXISTS issue_tags_old51`,
		`ALTER TABLE issue_tags RENAME TO issue_tags_old51`,
		`CREATE TABLE issue_tags (
			issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, tag_id)
		)`,
		`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old51`,
		`DROP TABLE issue_tags_old51`,

		`DROP TABLE IF EXISTS comments_old51`,
		`ALTER TABLE comments RENAME TO comments_old51`,
		`CREATE TABLE comments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO comments SELECT * FROM comments_old51`,
		`DROP TABLE comments_old51`,

		`DROP TABLE IF EXISTS issue_history_old51`,
		`ALTER TABLE issue_history RENAME TO issue_history_old51`,
		`CREATE TABLE issue_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			snapshot   TEXT NOT NULL DEFAULT '',
			changed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issue_history SELECT * FROM issue_history_old51`,
		`DROP TABLE issue_history_old51`,

		// Recreate FTS triggers (point at new issues table)
		`DROP TRIGGER IF EXISTS trg_issues_ai`,
		`DROP TRIGGER IF EXISTS trg_issues_au`,
		`DROP TRIGGER IF EXISTS trg_issues_ad`,
		`CREATE TRIGGER trg_issues_ai
			AFTER INSERT ON issues BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_au
			AFTER UPDATE ON issues BEGIN
				UPDATE search_index SET content =
					COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,'')
				WHERE entity_type='issue' AND entity_id=NEW.id;
			END`,
		`CREATE TRIGGER trg_issues_ad
			AFTER DELETE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
			END`,

		// Recreate comment triggers
		`DROP TABLE IF EXISTS issue_relations_old51`,
		`ALTER TABLE issue_relations RENAME TO issue_relations_old51`,
		`CREATE TABLE issue_relations (
			source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			type      TEXT NOT NULL
			          CHECK(type IN ('groups','sprint','depends_on','impacts')),
			PRIMARY KEY (source_id, target_id, type)
		)`,
		`INSERT OR IGNORE INTO issue_relations SELECT * FROM issue_relations_old51`,
		`DROP TABLE issue_relations_old51`,

		`DROP TABLE IF EXISTS time_entries_old51`,
		`ALTER TABLE time_entries RENAME TO time_entries_old51`,
		`CREATE TABLE time_entries (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id             INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			user_id              INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at           TEXT NOT NULL DEFAULT (datetime('now')),
			stopped_at           TEXT,
			override             REAL,
			comment              TEXT NOT NULL DEFAULT '',
			created_at           TEXT NOT NULL DEFAULT (datetime('now')),
			internal_rate_hourly REAL
		)`,
		`INSERT OR IGNORE INTO time_entries SELECT * FROM time_entries_old51`,
		`DROP TABLE time_entries_old51`,

		`DROP TABLE IF EXISTS attachments_old51`,
		`ALTER TABLE attachments RENAME TO attachments_old51`,
		`CREATE TABLE attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER REFERENCES issues(id) ON DELETE CASCADE,
			object_key   TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL DEFAULT 0,
			uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO attachments SELECT * FROM attachments_old51`,
		`DROP TABLE attachments_old51`,

		// Recreate FTS triggers (point at new issues table)
		`DROP TRIGGER IF EXISTS trg_issues_ai`,
		`DROP TRIGGER IF EXISTS trg_issues_au`,
		`DROP TRIGGER IF EXISTS trg_issues_ad`,
		`CREATE TRIGGER trg_issues_ai
			AFTER INSERT ON issues BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('issue', NEW.id,
					COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,''));
			END`,
		`CREATE TRIGGER trg_issues_au
			AFTER UPDATE ON issues BEGIN
				UPDATE search_index SET content =
					COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
					COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
					COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
					COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,'')
				WHERE entity_type='issue' AND entity_id=NEW.id;
			END`,
		`CREATE TRIGGER trg_issues_ad
			AFTER DELETE ON issues BEGIN
				DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
			END`,

		`DROP TRIGGER IF EXISTS trg_comments_ai`,
		`DROP TRIGGER IF EXISTS trg_comments_ad`,
		`CREATE TRIGGER trg_comments_ai
			AFTER INSERT ON comments BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('comment', NEW.id, COALESCE(NEW.body,''));
			END`,
		`CREATE TRIGGER trg_comments_ad
			AFTER DELETE ON comments BEGIN
				DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.id;
			END`,

		`PRAGMA foreign_keys=ON`,
	}},

	// ── M52: Fix user_recent_projects FK pointing at stale projects_old45 ──────
	// M45 recreated user_recent_projects BEFORE recreating projects, so the FK
	// internally references the renamed (then dropped) projects_old45 table.
	// Recreate the table to fix the FK reference.
	{52, []string{
		`PRAGMA foreign_keys=OFF`,
		`DROP TABLE IF EXISTS user_recent_projects_old52`,
		`ALTER TABLE user_recent_projects RENAME TO user_recent_projects_old52`,
		`CREATE TABLE user_recent_projects (
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			visited_at TEXT    NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (user_id, project_id)
		)`,
		`INSERT OR IGNORE INTO user_recent_projects SELECT * FROM user_recent_projects_old52`,
		`DROP TABLE user_recent_projects_old52`,
		`CREATE INDEX IF NOT EXISTS idx_urp_user_visited ON user_recent_projects(user_id, visited_at DESC)`,
		`PRAGMA foreign_keys=ON`,
	}},

	// ── M53: Add preview_hover_delay to users ──────────────────────────────────
	{53, []string{
		`ALTER TABLE users ADD COLUMN preview_hover_delay INTEGER NOT NULL DEFAULT 1000`,
	}},

	// ── M54: Add last_login_at to users ─────────────────────────────────────────
	{54, []string{
		`ALTER TABLE users ADD COLUMN last_login_at TEXT`,
	}},

	// ── M55: Add 'qa' status to issues CHECK constraint ──────────────────────
	// Recreates issues table to add 'qa' between 'in-progress' and 'done'.
	{55, []string{
		`PRAGMA foreign_keys=OFF`,

		`DROP TABLE IF EXISTS issues_old55`,
		`ALTER TABLE issues RENAME TO issues_old55`,
		`CREATE TABLE issues (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			issue_number        INTEGER NOT NULL DEFAULT 0,
			type                TEXT NOT NULL DEFAULT 'ticket'
			                    CHECK(type IN ('epic','cost_unit','release','sprint','ticket','task')),
			parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
			title               TEXT NOT NULL,
			description         TEXT NOT NULL DEFAULT '',
			acceptance_criteria TEXT NOT NULL DEFAULT '',
			notes               TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'new'
			                    CHECK(status IN ('new','backlog','in-progress','qa','done','accepted','invoiced','cancelled')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('low','medium','high')),
			cost_unit           TEXT NOT NULL DEFAULT '',
			release             TEXT NOT NULL DEFAULT '',
			billing_type        TEXT NOT NULL DEFAULT '',
			total_budget        REAL,
			rate_hourly         REAL,
			rate_lp             REAL,
			start_date          TEXT NOT NULL DEFAULT '',
			end_date            TEXT NOT NULL DEFAULT '',
			group_state         TEXT NOT NULL DEFAULT '',
			sprint_state        TEXT NOT NULL DEFAULT '',
			jira_id             TEXT NOT NULL DEFAULT '',
			jira_version        TEXT NOT NULL DEFAULT '',
			jira_text           TEXT NOT NULL DEFAULT '',
			estimate_hours      REAL,
			estimate_lp         REAL,
			ar_hours            REAL,
			ar_lp               REAL,
			time_override       REAL,
			color               TEXT,
			archived            INTEGER NOT NULL DEFAULT 0,
			assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_by          INTEGER REFERENCES users(id) ON DELETE SET NULL,
			accepted_at         TEXT,
			accepted_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			invoiced_at         TEXT,
			invoice_number      TEXT NOT NULL DEFAULT '',
			created_at          TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issues SELECT * FROM issues_old55`,
		`DROP TABLE issues_old55`,

		// Recreate child tables with correct FK references
		`DROP TABLE IF EXISTS issue_tags_old55`,
		`ALTER TABLE issue_tags RENAME TO issue_tags_old55`,
		`CREATE TABLE issue_tags (
			issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, tag_id)
		)`,
		`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old55`,
		`DROP TABLE issue_tags_old55`,

		`DROP TABLE IF EXISTS comments_old55`,
		`ALTER TABLE comments RENAME TO comments_old55`,
		`CREATE TABLE comments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO comments SELECT * FROM comments_old55`,
		`DROP TABLE comments_old55`,

		`DROP TABLE IF EXISTS issue_history_old55`,
		`ALTER TABLE issue_history RENAME TO issue_history_old55`,
		`CREATE TABLE issue_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			snapshot   TEXT NOT NULL DEFAULT '',
			changed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issue_history SELECT * FROM issue_history_old55`,
		`DROP TABLE issue_history_old55`,

		`DROP TABLE IF EXISTS issue_relations_old55`,
		`ALTER TABLE issue_relations RENAME TO issue_relations_old55`,
		`CREATE TABLE issue_relations (
			source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			type      TEXT NOT NULL
			          CHECK(type IN ('groups','sprint','depends_on','impacts')),
			PRIMARY KEY (source_id, target_id, type)
		)`,
		`INSERT OR IGNORE INTO issue_relations SELECT * FROM issue_relations_old55`,
		`DROP TABLE issue_relations_old55`,

		`DROP TABLE IF EXISTS time_entries_old55`,
		`ALTER TABLE time_entries RENAME TO time_entries_old55`,
		`CREATE TABLE time_entries (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id             INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			user_id              INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at           TEXT NOT NULL DEFAULT (datetime('now')),
			stopped_at           TEXT,
			override             REAL,
			comment              TEXT NOT NULL DEFAULT '',
			created_at           TEXT NOT NULL DEFAULT (datetime('now')),
			internal_rate_hourly REAL
		)`,
		`INSERT OR IGNORE INTO time_entries SELECT * FROM time_entries_old55`,
		`DROP TABLE time_entries_old55`,

		`DROP TABLE IF EXISTS attachments_old55`,
		`ALTER TABLE attachments RENAME TO attachments_old55`,
		`CREATE TABLE attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER REFERENCES issues(id) ON DELETE CASCADE,
			object_key   TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL DEFAULT 0,
			uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO attachments SELECT * FROM attachments_old55`,
		`DROP TABLE attachments_old55`,

		// Recreate indexes
		`CREATE INDEX IF NOT EXISTS idx_issues_project  ON issues(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_parent   ON issues(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_assignee  ON issues(assignee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_status    ON issues(status)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_type      ON issues(type)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_number    ON issues(project_id, issue_number)`,

		// Recreate FTS triggers
		`DROP TRIGGER IF EXISTS trg_issues_ai`,
		`DROP TRIGGER IF EXISTS trg_issues_au`,
		`DROP TRIGGER IF EXISTS trg_issues_ad`,
		`CREATE TRIGGER trg_issues_ai AFTER INSERT ON issues BEGIN
			INSERT INTO search_index(entity_type, entity_id, content)
			VALUES('issue', NEW.id,
				COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
				COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
				COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
				COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,''));
		END`,
		`CREATE TRIGGER trg_issues_au AFTER UPDATE ON issues BEGIN
			UPDATE search_index SET content =
				COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
				COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
				COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
				COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,'')
			WHERE entity_type='issue' AND entity_id=NEW.id;
		END`,
		`CREATE TRIGGER trg_issues_ad AFTER DELETE ON issues BEGIN
			DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
		END`,

		// Recreate comment FTS triggers
		`DROP TRIGGER IF EXISTS trg_comments_ai`,
		`DROP TRIGGER IF EXISTS trg_comments_ad`,
		`CREATE TRIGGER trg_comments_ai AFTER INSERT ON comments BEGIN
			INSERT INTO search_index(entity_type, entity_id, content) VALUES('comment', NEW.issue_id, NEW.body);
		END`,
		`CREATE TRIGGER trg_comments_ad AFTER DELETE ON comments BEGIN
			DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.issue_id AND content=OLD.body;
		END`,

		`PRAGMA foreign_keys=ON`,
	}},

	// M56 — system tags + rules table + project rate fields
	{56, []string{
		`ALTER TABLE tags ADD COLUMN system INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS system_tag_rules (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			tag_id          INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			condition_type  TEXT NOT NULL DEFAULT 'budget_threshold',
			threshold       REAL NOT NULL DEFAULT 0.8,
			excluded_statuses TEXT NOT NULL DEFAULT 'qa,done,accepted,invoiced,cancelled',
			created_at      TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`ALTER TABLE projects ADD COLUMN rate_hourly REAL`,
		`ALTER TABLE projects ADD COLUMN rate_lp REAL`,
	}},

	// M57 — target_ar field for sprints (stored on issues table since sprints are issues)
	{57, []string{
		`ALTER TABLE issues ADD COLUMN target_ar REAL`,
	}},

	// ── M58: Add 'delivered' status to issues CHECK constraint ───────────────
	// Adds 'delivered' between 'done' and 'accepted' in the status lifecycle.
	// Also updates system_tag_rules default excluded_statuses.
	{58, []string{
		`PRAGMA foreign_keys=OFF`,

		`DROP TABLE IF EXISTS issues_old58`,
		`ALTER TABLE issues RENAME TO issues_old58`,
		`CREATE TABLE issues (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id          INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			issue_number        INTEGER NOT NULL DEFAULT 0,
			type                TEXT NOT NULL DEFAULT 'ticket'
			                    CHECK(type IN ('epic','cost_unit','release','sprint','ticket','task')),
			parent_id           INTEGER REFERENCES issues(id) ON DELETE SET NULL,
			title               TEXT NOT NULL,
			description         TEXT NOT NULL DEFAULT '',
			acceptance_criteria TEXT NOT NULL DEFAULT '',
			notes               TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'new'
			                    CHECK(status IN ('new','backlog','in-progress','qa','done','delivered','accepted','invoiced','cancelled')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('low','medium','high')),
			cost_unit           TEXT NOT NULL DEFAULT '',
			release             TEXT NOT NULL DEFAULT '',
			billing_type        TEXT NOT NULL DEFAULT '',
			total_budget        REAL,
			rate_hourly         REAL,
			rate_lp             REAL,
			start_date          TEXT NOT NULL DEFAULT '',
			end_date            TEXT NOT NULL DEFAULT '',
			group_state         TEXT NOT NULL DEFAULT '',
			sprint_state        TEXT NOT NULL DEFAULT '',
			jira_id             TEXT NOT NULL DEFAULT '',
			jira_version        TEXT NOT NULL DEFAULT '',
			jira_text           TEXT NOT NULL DEFAULT '',
			estimate_hours      REAL,
			estimate_lp         REAL,
			ar_hours            REAL,
			ar_lp               REAL,
			time_override       REAL,
			color               TEXT,
			archived            INTEGER NOT NULL DEFAULT 0,
			assignee_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_by          INTEGER REFERENCES users(id) ON DELETE SET NULL,
			accepted_at         TEXT,
			accepted_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
			invoiced_at         TEXT,
			invoice_number      TEXT NOT NULL DEFAULT '',
			created_at          TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
			target_ar           REAL
		)`,
		`INSERT INTO issues SELECT * FROM issues_old58`,
		`DROP TABLE issues_old58`,

		// Recreate child tables with correct FK references
		`DROP TABLE IF EXISTS issue_tags_old58`,
		`ALTER TABLE issue_tags RENAME TO issue_tags_old58`,
		`CREATE TABLE issue_tags (
			issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (issue_id, tag_id)
		)`,
		`INSERT OR IGNORE INTO issue_tags SELECT * FROM issue_tags_old58`,
		`DROP TABLE issue_tags_old58`,

		`DROP TABLE IF EXISTS comments_old58`,
		`ALTER TABLE comments RENAME TO comments_old58`,
		`CREATE TABLE comments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO comments SELECT * FROM comments_old58`,
		`DROP TABLE comments_old58`,

		`DROP TABLE IF EXISTS issue_history_old58`,
		`ALTER TABLE issue_history RENAME TO issue_history_old58`,
		`CREATE TABLE issue_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			changed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			snapshot   TEXT NOT NULL DEFAULT '',
			changed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO issue_history SELECT * FROM issue_history_old58`,
		`DROP TABLE issue_history_old58`,

		`DROP TABLE IF EXISTS issue_relations_old58`,
		`ALTER TABLE issue_relations RENAME TO issue_relations_old58`,
		`CREATE TABLE issue_relations (
			source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			type      TEXT NOT NULL
			          CHECK(type IN ('groups','sprint','depends_on','impacts')),
			PRIMARY KEY (source_id, target_id, type)
		)`,
		`INSERT OR IGNORE INTO issue_relations SELECT * FROM issue_relations_old58`,
		`DROP TABLE issue_relations_old58`,

		`DROP TABLE IF EXISTS time_entries_old58`,
		`ALTER TABLE time_entries RENAME TO time_entries_old58`,
		`CREATE TABLE time_entries (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id             INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			user_id              INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			started_at           TEXT NOT NULL DEFAULT (datetime('now')),
			stopped_at           TEXT,
			override             REAL,
			comment              TEXT NOT NULL DEFAULT '',
			created_at           TEXT NOT NULL DEFAULT (datetime('now')),
			internal_rate_hourly REAL
		)`,
		`INSERT OR IGNORE INTO time_entries SELECT * FROM time_entries_old58`,
		`DROP TABLE time_entries_old58`,

		`DROP TABLE IF EXISTS attachments_old58`,
		`ALTER TABLE attachments RENAME TO attachments_old58`,
		`CREATE TABLE attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id     INTEGER REFERENCES issues(id) ON DELETE CASCADE,
			object_key   TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL DEFAULT 0,
			uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO attachments SELECT * FROM attachments_old58`,
		`DROP TABLE attachments_old58`,

		// Recreate indexes
		`CREATE INDEX IF NOT EXISTS idx_issues_project  ON issues(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_parent   ON issues(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_assignee  ON issues(assignee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_status    ON issues(status)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_type      ON issues(type)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_number    ON issues(project_id, issue_number)`,

		// Recreate FTS triggers
		`DROP TRIGGER IF EXISTS trg_issues_ai`,
		`DROP TRIGGER IF EXISTS trg_issues_au`,
		`DROP TRIGGER IF EXISTS trg_issues_ad`,
		`CREATE TRIGGER trg_issues_ai AFTER INSERT ON issues BEGIN
			INSERT INTO search_index(entity_type, entity_id, content)
			VALUES('issue', NEW.id,
				COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
				COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
				COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
				COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,''));
		END`,
		`CREATE TRIGGER trg_issues_au AFTER UPDATE ON issues BEGIN
			UPDATE search_index SET content =
				COALESCE(NEW.title,'') || ' ' || COALESCE(NEW.description,'') || ' ' ||
				COALESCE(NEW.acceptance_criteria,'') || ' ' || COALESCE(NEW.notes,'') || ' ' ||
				COALESCE(NEW.cost_unit,'') || ' ' || COALESCE(NEW.release,'') || ' ' ||
				COALESCE(NEW.jira_id,'') || ' ' || COALESCE(NEW.jira_version,'') || ' ' || COALESCE(NEW.jira_text,'')
			WHERE entity_type='issue' AND entity_id=NEW.id;
		END`,
		`CREATE TRIGGER trg_issues_ad AFTER DELETE ON issues BEGIN
			DELETE FROM search_index WHERE entity_type='issue' AND entity_id=OLD.id;
		END`,

		// Recreate comment FTS triggers
		`DROP TRIGGER IF EXISTS trg_comments_ai`,
		`DROP TRIGGER IF EXISTS trg_comments_ad`,
		`CREATE TRIGGER trg_comments_ai AFTER INSERT ON comments BEGIN
			INSERT INTO search_index(entity_type, entity_id, content) VALUES('comment', NEW.issue_id, NEW.body);
		END`,
		`CREATE TRIGGER trg_comments_ad AFTER DELETE ON comments BEGIN
			DELETE FROM search_index WHERE entity_type='comment' AND entity_id=OLD.issue_id AND content=OLD.body;
		END`,

		`PRAGMA foreign_keys=ON`,

		// Update system_tag_rules to include 'delivered' in excluded statuses
		`UPDATE system_tag_rules SET excluded_statuses='qa,done,delivered,accepted,invoiced,cancelled' WHERE excluded_statuses='qa,done,accepted,invoiced,cancelled'`,
	}},

	// M59 — add rank column to issue_relations for sprint board ordering
	{59, []string{
		`ALTER TABLE issue_relations ADD COLUMN rank INTEGER NOT NULL DEFAULT 0`,
	}},
	{60, []string{
		`ALTER TABLE time_entries ADD COLUMN mite_id INTEGER`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_mite_id ON time_entries(mite_id)`,
	}},
	// M61: fix mite-imported entries that appear as running timers
	{61, []string{
		`UPDATE time_entries SET stopped_at = started_at WHERE mite_id IS NOT NULL AND stopped_at IS NULL`,
	}},
	// M62: per-user accruals report preferences (admin-only feature)
	{62, []string{
		`ALTER TABLE users ADD COLUMN accruals_stats_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN accruals_extra_statuses TEXT NOT NULL DEFAULT ''`,
	}},
	// M63: password reset tokens (forgot-password email magic link flow).
	// Tokens are random 32-byte values stored hashed (sha256 — high-entropy input
	// doesn't need bcrypt). used_at=NULL → unused, single-use consume on reset.
	{63, []string{
		`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			used_at    TEXT,
			ip_address TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_prt_user ON password_reset_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_prt_expires ON password_reset_tokens(expires_at)`,
	}},
	// M64: per-project access control (project_members + access_audit).
	// Replaces user_project_access with a richer model that supports three
	// access levels — 'viewer' (read-only), 'editor' (read+write), and
	// 'none' (explicit denial, overrides the default member-has-all-access).
	// Backfills: existing user_project_access rows become 'viewer' grants
	// (they were read-only portal access); all active admin+member users
	// are seeded as 'editor' for every non-deleted project.
	{64, []string{
		`CREATE TABLE IF NOT EXISTS project_members (
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			access_level TEXT NOT NULL DEFAULT 'editor'
			             CHECK(access_level IN ('none','viewer','editor')),
			created_at   TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (user_id, project_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_user    ON project_members(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_project ON project_members(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_level   ON project_members(access_level)`,

		`CREATE TABLE IF NOT EXISTS access_audit (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
			user_id    INTEGER REFERENCES users(id)    ON DELETE SET NULL,
			actor_id   INTEGER REFERENCES users(id)    ON DELETE SET NULL,
			action     TEXT NOT NULL,
			old_level  TEXT NOT NULL DEFAULT '',
			new_level  TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_access_audit_project ON access_audit(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_access_audit_user    ON access_audit(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_access_audit_created ON access_audit(created_at)`,

		// Backfill: existing portal grants become 'viewer' rows.
		`INSERT OR IGNORE INTO project_members(user_id, project_id, access_level)
		 SELECT user_id, project_id, 'viewer' FROM user_project_access`,

		// Seed editor access for every current admin/member on every
		// non-deleted project. External users are NOT auto-seeded — they
		// must be granted per-project access explicitly.
		`INSERT OR IGNORE INTO project_members(user_id, project_id, access_level)
		 SELECT u.id, p.id, 'editor'
		 FROM users u
		 CROSS JOIN projects p
		 WHERE u.role IN ('admin','member')
		   AND u.status = 'active'
		   AND p.status != 'deleted'`,
	}},

	// M65: drop the obsolete user_project_access table. Safety re-insert
	// covers rows added between M64 being applied and this migration
	// running (unlikely in practice — both ship together — but cheap
	// to do before dropping the source table).
	{65, []string{
		`INSERT OR IGNORE INTO project_members(user_id, project_id, access_level)
		 SELECT user_id, project_id, 'viewer' FROM user_project_access`,
		`DROP INDEX IF EXISTS idx_upa_user`,
		`DROP TABLE IF EXISTS user_project_access`,
	}},

	// M66: soft-delete for issues. NULL = live, non-NULL = trashed.
	// deleted_by tracks who moved it to trash; stays as a plain INTEGER
	// (no FK constraint can be added via ALTER TABLE on a populated
	// table in SQLite — a stale user id after a user purge is
	// acceptable, the field is used for display only).
	{66, []string{
		`ALTER TABLE issues ADD COLUMN deleted_at TEXT`,
		`ALTER TABLE issues ADD COLUMN deleted_by INTEGER`,
		`CREATE INDEX IF NOT EXISTS idx_issues_deleted_at ON issues(deleted_at)`,
	}},

	// M67: extend issue_relations.type CHECK constraint with three new
	// directional types — follows_from (spin-off), blocks, related
	// (loose "see also"). Purely additive: existing rows stay valid
	// under the new CHECK. SQLite can't ALTER a CHECK constraint, so
	// the usual rename+recreate+copy dance. See PAI-89.
	{67, []string{
		`ALTER TABLE issue_relations RENAME TO issue_relations_old66`,
		`CREATE TABLE issue_relations (
			source_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			type      TEXT NOT NULL
			          CHECK(type IN ('groups','sprint','depends_on','impacts',
			                         'follows_from','blocks','related')),
			rank      INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (source_id, target_id, type)
		)`,
		`INSERT OR IGNORE INTO issue_relations
		 SELECT source_id, target_id, type, rank FROM issue_relations_old66`,
		`DROP TABLE issue_relations_old66`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_source
		 ON issue_relations(source_id, type)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_relations_target
		 ON issue_relations(target_id, type)`,
	}},

	// M68: session-scoped mutation audit (PAI-97). One row per mutation
	// request, tagged with X-PAIMOS-Session-Id. session_id is nullable
	// so requests without the header still get audited (null tag) —
	// catches misbehaving callers that fail to set the header.
	// user_id is also nullable for the same reason.
	{68, []string{
		`CREATE TABLE IF NOT EXISTS session_activity (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id  TEXT,
			user_id     INTEGER,
			method      TEXT NOT NULL,
			path        TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			occurred_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		// (session_id, id) gets us fast keyset pagination by session.
		`CREATE INDEX IF NOT EXISTS idx_session_activity_session
		 ON session_activity(session_id, id)`,
		`CREATE INDEX IF NOT EXISTS idx_session_activity_occurred
		 ON session_activity(occurred_at)`,
	}},

	// M69: customers table (PAI-53). CRM-agnostic by design — provider-side
	// IDs and deep-link URLs live in generic columns (`external_*`) so the
	// schema doesn't bind PAIMOS to any particular CRM. Manual customers
	// are first-class: NULL `external_*` is the no-CRM mode (PAI-28
	// audience #1). FTS5 entry built from name + contact + industry.
	{69, []string{
		`CREATE TABLE IF NOT EXISTS customers (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			name               TEXT NOT NULL,
			external_id        TEXT,
			external_url       TEXT,
			external_provider  TEXT,
			synced_at          TEXT,
			contact_name       TEXT NOT NULL DEFAULT '',
			contact_email      TEXT NOT NULL DEFAULT '',
			address            TEXT NOT NULL DEFAULT '',
			country            TEXT NOT NULL DEFAULT '',
			industry           TEXT NOT NULL DEFAULT '',
			rate_hourly        REAL,
			rate_lp            REAL,
			notes              TEXT NOT NULL DEFAULT '',
			created_at         TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at         TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		// Same pair-must-be-set semantic as the API layer: a customer
		// linked to an external CRM has both id and provider; a manual
		// customer has neither. Enforced at the DB so a malformed
		// migration / direct-write can't sneak past.
		`CREATE TRIGGER IF NOT EXISTS trg_customers_external_pair_ai
			BEFORE INSERT ON customers
			WHEN (NEW.external_id IS NULL) <> (NEW.external_provider IS NULL)
			BEGIN
				SELECT RAISE(ABORT, 'external_id and external_provider must be both set or both null');
			END`,
		`CREATE TRIGGER IF NOT EXISTS trg_customers_external_pair_au
			BEFORE UPDATE ON customers
			WHEN (NEW.external_id IS NULL) <> (NEW.external_provider IS NULL)
			BEGIN
				SELECT RAISE(ABORT, 'external_id and external_provider must be both set or both null');
			END`,
		`CREATE INDEX IF NOT EXISTS idx_customers_external
		 ON customers(external_provider, external_id)`,
		// FTS triggers
		`CREATE TRIGGER IF NOT EXISTS trg_customers_ai
			AFTER INSERT ON customers BEGIN
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('customer', NEW.id,
					NEW.name || ' ' || NEW.contact_name || ' ' ||
					NEW.contact_email || ' ' || NEW.industry || ' ' ||
					NEW.country || ' ' || NEW.notes);
			END`,
		`CREATE TRIGGER IF NOT EXISTS trg_customers_au
			AFTER UPDATE ON customers BEGIN
				DELETE FROM search_index WHERE entity_type='customer' AND entity_id=OLD.id;
				INSERT INTO search_index(entity_type, entity_id, content)
				VALUES('customer', NEW.id,
					NEW.name || ' ' || NEW.contact_name || ' ' ||
					NEW.contact_email || ' ' || NEW.industry || ' ' ||
					NEW.country || ' ' || NEW.notes);
			END`,
		`CREATE TRIGGER IF NOT EXISTS trg_customers_ad
			AFTER DELETE ON customers BEGIN
				DELETE FROM search_index WHERE entity_type='customer' AND entity_id=OLD.id;
			END`,
	}},

	// M70: projects ↔ customers FK + documents + provider_configs.
	// SQLite can't ALTER an existing column to add a FK on a populated
	// table, and the existing `customer_id` is a freeform TEXT label
	// (PMO26 legacy). Rename it to `customer_label` and add a clean
	// `customer_id INTEGER` FK so the rate-cascading + assignment logic
	// (PAI-54) works against the new customers table.
	{70, []string{
		// ── Rename existing customer_id → customer_label, add FK ────
		// SQLite supports RENAME COLUMN since 3.25; this codebase uses
		// modernc.org/sqlite which is well past that.
		`ALTER TABLE projects RENAME COLUMN customer_id TO customer_label`,
		`ALTER TABLE projects ADD COLUMN customer_id INTEGER REFERENCES customers(id)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_customer_id
		 ON projects(customer_id)`,

		// ── documents (PAI-55) ──────────────────────────────────────
		// Metadata-only table for customer- and project-scoped uploads;
		// the file bytes live in MinIO (same bucket as attachments,
		// namespaced under "documents/…"). object_key below is the
		// pointer; handlers/documents.go does all the storage.Put /
		// .Get / .Delete calls.
		//
		// scope is checked so exactly one of customer_id / project_id
		// is set; orphan docs (both NULL) are rejected.
		`CREATE TABLE IF NOT EXISTS documents (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			scope         TEXT NOT NULL CHECK(scope IN ('customer','project')),
			customer_id   INTEGER REFERENCES customers(id) ON DELETE CASCADE,
			project_id    INTEGER REFERENCES projects(id)  ON DELETE CASCADE,
			filename      TEXT NOT NULL,
			mime_type     TEXT NOT NULL,
			size_bytes    INTEGER NOT NULL,
			-- object_key is the path inside the MinIO bucket (same storage
			-- layer as attachments). Documents and attachments share one
			-- bucket; the key namespace separates them ("documents/…" vs
			-- the bare "<issueId>/…" attachments use).
			object_key    TEXT NOT NULL,
			label         TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL DEFAULT 'active'
			              CHECK(status IN ('draft','active','expired')),
			valid_from    TEXT,
			valid_until   TEXT,
			uploaded_by   INTEGER,
			uploaded_at   TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
			CHECK(
				(scope = 'customer' AND customer_id IS NOT NULL AND project_id IS NULL) OR
				(scope = 'project'  AND project_id  IS NOT NULL AND customer_id IS NULL)
			)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_customer
		 ON documents(customer_id) WHERE customer_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_documents_project
		 ON documents(project_id)  WHERE project_id  IS NOT NULL`,

		// ── provider_configs (PAI-104) ──────────────────────────────
		// Per-provider settings. config_json holds non-secret fields as
		// a plain JSON map; secret fields are encrypted at rest with
		// AES-GCM and stored separately under config_secret_json (so
		// non-secret reads in the API never even touch the ciphertext).
		`CREATE TABLE IF NOT EXISTS provider_configs (
			provider_id           TEXT PRIMARY KEY,
			enabled               INTEGER NOT NULL DEFAULT 0,
			config_json           TEXT NOT NULL DEFAULT '{}',
			config_secret_json    BLOB,
			updated_at            TEXT NOT NULL DEFAULT (datetime('now')),
			updated_by            INTEGER REFERENCES users(id)
		)`,
	}},

	// M71: per-project cooperation metadata (PAI-61). 1:1 with projects.
	// Structured columns for the four dimensions PMs reach for repeatedly
	// (engagement type, code ownership, env responsibility, SLA flags),
	// plus two markdown freeform fields for the long tail. Informational
	// only in v1 — no behavioural effects elsewhere.
	{71, []string{
		`CREATE TABLE IF NOT EXISTS project_cooperation (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id          INTEGER NOT NULL UNIQUE
			                    REFERENCES projects(id) ON DELETE CASCADE,
			engagement_type     TEXT
			                    CHECK(engagement_type IN
			                        ('consultancy','project_delivery','managed_service','retainer')),
			code_ownership      TEXT
			                    CHECK(code_ownership IN
			                        ('client_repo','own_repo','mixed')),
			env_responsibility  TEXT
			                    CHECK(env_responsibility IN
			                        ('dev_staging','dev_staging_prod','full_stack')),
			has_sla             INTEGER NOT NULL DEFAULT 0,
			uptime_sla          TEXT NOT NULL DEFAULT '',
			response_time_sla   TEXT NOT NULL DEFAULT '',
			backup_responsible  INTEGER NOT NULL DEFAULT 0,
			oncall              INTEGER NOT NULL DEFAULT 0,
			sla_details         TEXT NOT NULL DEFAULT '',
			cooperation_notes   TEXT NOT NULL DEFAULT '',
			created_at          TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_cooperation_project
		 ON project_cooperation(project_id)`,
	}},

	// M72: per-session CSRF token (PAI-113). Bound to the session so
	// rotation happens automatically on logout/reset. Existing sessions
	// keep an empty token until the next sessionUser() call upgrades them
	// — see auth.Middleware for the lazy-issue path.
	{72, []string{
		`ALTER TABLE sessions ADD COLUMN csrf_token TEXT NOT NULL DEFAULT ''`,
	}},

	// M73: incident_log for first-class operator-recorded security and
	// availability incidents (PAI-116). Intentionally minimal — admins
	// can insert/update/close rows; export endpoints stream the table to
	// JSON or CSV for SIEM ingestion. severity / status are CHECK-bounded
	// so the API layer can rely on them without re-validating.
	{73, []string{
		`CREATE TABLE IF NOT EXISTS incident_log (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			severity        TEXT NOT NULL
			                CHECK(severity IN ('low','medium','high','critical')),
			kind            TEXT NOT NULL DEFAULT 'other',
			title           TEXT NOT NULL,
			summary         TEXT NOT NULL DEFAULT '',
			details         TEXT NOT NULL DEFAULT '',
			reported_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
			status          TEXT NOT NULL DEFAULT 'open'
			                CHECK(status IN ('open','investigating','resolved','closed')),
			detected_at     TEXT NOT NULL DEFAULT (datetime('now')),
			resolved_at     TEXT,
			created_at      TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_incident_log_status ON incident_log(status)`,
		`CREATE INDEX IF NOT EXISTS idx_incident_log_detected_at ON incident_log(detected_at)`,
	}},

	// M74: ai_settings (PAI-149). Singleton row holding the system-wide
	// configuration for the LLM text-optimization feature (PAI-146). One
	// row, id=1, seeded by the handler on first read so the table is safe
	// to query without a "no rows" branch. The api_key column is plaintext
	// in the DB by design — operators who need stronger secrets handling
	// should mount the SQLite volume on encrypted storage. Treating it as
	// "secret" here would imply guarantees we don't actually keep.
	{74, []string{
		`CREATE TABLE IF NOT EXISTS ai_settings (
			id                   INTEGER PRIMARY KEY CHECK(id = 1),
			enabled              INTEGER NOT NULL DEFAULT 0,
			provider             TEXT NOT NULL DEFAULT 'openrouter',
			model                TEXT NOT NULL DEFAULT '',
			api_key              TEXT NOT NULL DEFAULT '',
			optimize_instruction TEXT NOT NULL DEFAULT '',
			updated_at           TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO ai_settings (id) VALUES (1)`,
	}},

	// M75: PAI-29 foundations — project repos, code anchors, and the
	// PMO-hosted project manifest. The manifest is intentionally stored
	// as a validated JSON blob in v1 so the API contract can stabilize
	// before we explode it into many specialised tables.
	{75, []string{
		`CREATE TABLE IF NOT EXISTS project_repos (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			url            TEXT NOT NULL,
			default_branch TEXT NOT NULL DEFAULT 'main',
			label          TEXT NOT NULL DEFAULT '',
			sort_order     INTEGER NOT NULL DEFAULT 0,
			created_at     TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_repos_project ON project_repos(project_id, sort_order, id)`,
		`CREATE TABLE IF NOT EXISTS issue_anchors (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			issue_id       INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			repo_id        INTEGER NOT NULL REFERENCES project_repos(id) ON DELETE CASCADE,
			file_path      TEXT NOT NULL,
			line           INTEGER NOT NULL,
			label          TEXT NOT NULL DEFAULT '',
			confidence     TEXT NOT NULL DEFAULT 'declared'
			               CHECK(confidence IN ('declared','derived','suggested')),
			symbol_json    TEXT NOT NULL DEFAULT '',
			schema_version TEXT NOT NULL DEFAULT '',
			repo_revision  TEXT NOT NULL DEFAULT '',
			generated_at   TEXT NOT NULL DEFAULT '',
			hidden         INTEGER NOT NULL DEFAULT 0,
			stale          INTEGER NOT NULL DEFAULT 0,
			created_at     TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_anchors_issue ON issue_anchors(issue_id, repo_id, file_path, line)`,
		`CREATE INDEX IF NOT EXISTS idx_issue_anchors_repo ON issue_anchors(project_id, repo_id, issue_id)`,
		`CREATE TABLE IF NOT EXISTS project_manifests (
			project_id     INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			manifest_json  TEXT NOT NULL DEFAULT '{}',
			updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
			updated_by     INTEGER REFERENCES users(id)
		)`,
	}},

	// M76: PAI-30 foundations — generic entity relations and embeddings.
	// issue_relations remains in place for backward compatibility; the
	// handlers layer can dual-write or bridge incrementally.
	{76, []string{
		`CREATE TABLE IF NOT EXISTS entity_relations (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id    INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			source_type   TEXT NOT NULL,
			source_id     INTEGER NOT NULL,
			target_type   TEXT NOT NULL,
			target_id     INTEGER NOT NULL,
			edge_type     TEXT NOT NULL,
			confidence    TEXT NOT NULL CHECK(confidence IN ('declared','derived','suggested')),
			metadata      TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(source_type, source_id, target_type, target_id, edge_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entity_relations_src  ON entity_relations(source_type, source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_entity_relations_tgt  ON entity_relations(target_type, target_id)`,
		`CREATE INDEX IF NOT EXISTS idx_entity_relations_type ON entity_relations(project_id, edge_type)`,
		`CREATE TABLE IF NOT EXISTS entity_embeddings (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id      INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			entity_type     TEXT NOT NULL,
			entity_id       INTEGER NOT NULL,
			model           TEXT NOT NULL,
			dim             INTEGER NOT NULL,
			vector          BLOB NOT NULL,
			source_hash     TEXT NOT NULL,
			last_indexed_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(entity_type, entity_id, model)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entity_embeddings_lookup ON entity_embeddings(project_id, entity_type, entity_id)`,
		`INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata)
		 SELECT i.project_id, 'issue', ir.source_id, 'issue', ir.target_id, ir.type, 'declared', ''
		 FROM issue_relations ir
		 JOIN issues i ON i.id = ir.source_id
		 WHERE i.project_id IS NOT NULL`,
	}},

	// PAI-161: per-user AI usage tracking and admin-overridable cap.
	// One row per (user, day) — `day` is the YYYY-MM-DD UTC date so
	// rolling-day windows are trivial. Numbers are append-only via
	// ON CONFLICT increment, so a missed mid-call crash leaves the
	// counter slightly low but never wrong by more than one call.
	//
	// users.ai_cap_override_tokens (nullable INT): null means
	// "use the default daily cap" (configurable via env). Setting
	// to 0 explicitly disables AI for that user; a positive integer
	// raises the cap. Mirrors the pattern other per-user opt-in
	// flags follow elsewhere in PAIMOS.
	{77, []string{
		`CREATE TABLE IF NOT EXISTS ai_usage (
			user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			day               TEXT NOT NULL,
			prompt_tokens     INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			request_count     INTEGER NOT NULL DEFAULT 0,
			updated_at        TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (user_id, day)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_usage_day ON ai_usage(day)`,
		`ALTER TABLE users ADD COLUMN ai_cap_override_tokens INTEGER`,
	}},

	// PAI-175: AI prompt CRUD. Each AI action's prompt template is
	// admin-editable through Settings → AI. Built-in actions are
	// code-defined (label / surface / parent / sub locked) but their
	// prompt text is overridable via a row in this table. Custom
	// actions are also stored here with `is_builtin = 0`.
	//
	// Schema notes:
	//   - `key` is the action key the dispatcher resolves at request
	//     time. Built-in keys mirror the registered actions
	//     (PAI-164–172, PAI-173).
	//   - `prompt_template` is the admin-edited override. Empty
	//     string means "use the code-defined default" — keeps the
	//     reset-to-default path trivial.
	//   - `default_template_hash` is reserved for the change-detection
	//     UI from PAI-176 ("default has shipped a change — review");
	//     populated by handlers when seeding builtins.
	{78, []string{
		`CREATE TABLE IF NOT EXISTS ai_prompts (
			id                    INTEGER PRIMARY KEY AUTOINCREMENT,
			key                   TEXT NOT NULL UNIQUE,
			label                 TEXT NOT NULL,
			surface               TEXT NOT NULL,
			parent_action         TEXT,
			sub_action            TEXT,
			prompt_template       TEXT NOT NULL DEFAULT '',
			enabled               INTEGER NOT NULL DEFAULT 1,
			is_builtin            INTEGER NOT NULL DEFAULT 0,
			default_template_hash TEXT NOT NULL DEFAULT '',
			created_at            TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at            TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_prompts_surface ON ai_prompts(surface)`,
	}},

	// PAI-179: AI action placement.
	//
	// Adds a `placement` column to ai_prompts so each action can be
	// pinned to text-field menus, issue-level menus, or both. The
	// column is admin-overridable through Settings → AI prompts;
	// the registry default applies when the column is empty (which
	// is exactly what we set on backfill so existing rows pick up
	// the defaults the next time the catalogue endpoint runs).
	{79, []string{
		`ALTER TABLE ai_prompts ADD COLUMN placement TEXT NOT NULL DEFAULT ''`,
		// Empty means "use the registry default" — the catalogue
		// endpoint resolves that lazily, so no per-key seed migration
		// is needed. Admins who edit a placement override the default;
		// admins who reset clear back to ''.
	}},
	// PAI-189 / PAI-192: align indexes with real query paths. entity_relations
	// is typically filtered by project + endpoint entity, and ai_prompts
	// prompt resolution is by key + enabled.
	{80, []string{
		`CREATE INDEX IF NOT EXISTS idx_entity_relations_project_src
		 ON entity_relations(project_id, source_type, source_id, edge_type)`,
		`CREATE INDEX IF NOT EXISTS idx_entity_relations_project_tgt
		 ON entity_relations(project_id, target_type, target_id, edge_type)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_prompts_key_enabled
		 ON ai_prompts(key, enabled)`,
	}},
	}

	for _, m := range migrations {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_versions WHERE version=?", m.version).Scan(&count); err != nil {
			return fmt.Errorf("check migration %d: %w", m.version, err)
		}
		if count > 0 {
			continue
		}
		// Pin all steps to a single connection so PRAGMA foreign_keys=OFF/ON
		// applies to every subsequent DDL step in the same migration.
		conn, err := db.Conn(context.Background())
		if err != nil {
			return fmt.Errorf("migration %d: get conn: %w", m.version, err)
		}
		var migErr error
		for _, step := range m.steps {
			if _, err := conn.ExecContext(context.Background(), step); err != nil {
				label := step
				if len(label) > 60 {
					label = label[:60]
				}
				migErr = fmt.Errorf("run migration %d step %q: %w", m.version, label, err)
				break
			}
		}
		conn.Close()
		if migErr != nil {
			return migErr
		}
		if _, err := db.Exec("INSERT INTO schema_versions(version) VALUES(?)", m.version); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
		fmt.Printf("db: applied migration %d\n", m.version)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
