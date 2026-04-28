// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

//go:build dev_login

// PAI-267 — minimal-fixture dev seeder.
//
// Phase-1 scope: 4 dev users (pinned ids 9001–9004, no password) + 4
// fixture projects (PAIT/ACME/BUGZ/LOGS) with ~5 issues each + the
// per-user × per-project access-level matrix from the ticket. The
// rich fixture variety (ACME's 30-40 issues across 3 sprints with
// time entries, BUGZ's 100+ with relations + soft-deletes, LOGS's
// attachments + comment threads) is deferred — see follow-up tickets
// filed alongside the PAI-267 commit.
//
// Idempotency: re-running is safe. Users use pinned ids + INSERT OR
// IGNORE; projects use unique keys + INSERT OR IGNORE; memberships
// use INSERT OR REPLACE (so re-runs after editing the matrix
// converge); issues skip the seed when the project already has any
// issues (avoiding duplicate-with-different-issue-number rows on
// re-run).

package devseed

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/markus-barta/paimos/backend/db"
)

// devUser is one row in the user matrix. ID is pinned so the
// frontend's playwright selectors can refer to fixture users
// stably across machines.
type devUser struct {
	ID       int64
	Username string
	Role     string // admin | member | external
}

// devProject is one fixture project.
type devProject struct {
	Key         string
	Name        string
	Description string
}

// memberRow is one cell of the user × project access matrix.
// Level is the access_level string written to project_members
// (none / viewer / editor) — the project_members.access_level
// CHECK constraint forbids anything else. dev_admin has Role=admin
// globally, so it inherits all-access without explicit memberships;
// only the three non-admin users need rows.
type memberRow struct {
	UserID    int64
	ProjectID int64
	Level     string
}

var (
	devUsers = []devUser{
		{ID: 9001, Username: "dev_admin", Role: "admin"},
		{ID: 9002, Username: "dev_editor", Role: "member"},
		{ID: 9003, Username: "dev_viewer", Role: "member"},
		{ID: 9004, Username: "dev_outsider", Role: "external"},
	}

	devProjects = []devProject{
		{Key: "PAIT", Name: "Paimos Testing", Description: "RBAC sandbox — dev_admin / dev_editor / dev_viewer / dev_outsider exercise the permissions matrix here."},
		{Key: "ACME", Name: "Acme GmbH", Description: "Commercial customer engagement fixture — billing surface, sprint flows, customer detail."},
		{Key: "BUGZ", Name: "Open-source bug tracker", Description: "List virtualisation, search, bulk ops, trash + restore. Phase-1 carries minimal issues; rich fixture variety is a follow-up."},
		{Key: "LOGS", Name: "Personal-OS captain's log", Description: "Issue detail surfaces — comments, attachments, activity timeline. Phase-1 carries minimal issues; rich fixture variety is a follow-up."},
	}
)

// Run is the dev-seed entrypoint called from main.go's dev-seed
// subcommand. Wraps the whole seed in a single transaction so a
// partial failure doesn't leave the DB in a confused half-state.
func Run() error {
	tx, err := db.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op if committed

	if err := seedUsers(tx); err != nil {
		return fmt.Errorf("seed users: %w", err)
	}
	projectIDs, err := seedProjects(tx)
	if err != nil {
		return fmt.Errorf("seed projects: %w", err)
	}
	if err := seedMemberships(tx, projectIDs); err != nil {
		return fmt.Errorf("seed memberships: %w", err)
	}
	if err := seedIssues(tx, projectIDs); err != nil {
		return fmt.Errorf("seed issues: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	log.Printf("dev-seed: %d users, %d projects, memberships + minimal issues — re-run-safe",
		len(devUsers), len(devProjects))
	return nil
}

// seedUsers idempotently inserts dev_admin / dev_editor / dev_viewer /
// dev_outsider with pinned ids. password='' so the normal /api/auth/login
// flow's bcrypt compare always fails — the only way in is dev-login.
func seedUsers(tx *sql.Tx) error {
	for _, u := range devUsers {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO users (id, username, password, role, status, first_name, last_name)
			VALUES (?, ?, '', ?, 'active', ?, 'Dev')
		`, u.ID, u.Username, u.Role, u.Username)
		if err != nil {
			return fmt.Errorf("insert user %s: %w", u.Username, err)
		}
	}
	return nil
}

// seedProjects idempotently inserts the four fixture projects keyed on
// PAIT/ACME/BUGZ/LOGS. Returns project_id-by-key so downstream seeders
// can resolve memberships + issues without re-querying.
func seedProjects(tx *sql.Tx) (map[string]int64, error) {
	out := map[string]int64{}
	for _, p := range devProjects {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO projects (name, key, description, status)
			VALUES (?, ?, ?, 'active')
		`, p.Name, p.Key, p.Description); err != nil {
			return nil, fmt.Errorf("insert project %s: %w", p.Key, err)
		}
		var id int64
		if err := tx.QueryRow("SELECT id FROM projects WHERE key=?", p.Key).Scan(&id); err != nil {
			return nil, fmt.Errorf("resolve project %s: %w", p.Key, err)
		}
		out[p.Key] = id
	}
	return out, nil
}

// seedMemberships writes the per-user × per-project access matrix.
// dev_admin has global role=admin so it does not need explicit rows
// (admin inherits all-access); the other three get explicit grants
// (or absent rows = no access for non-admin).
//
// User × project access matrix (per PAI-267 spec):
//
//	dev_editor:   PAIT=editor, ACME=editor, BUGZ=viewer, LOGS=(absent)
//	dev_viewer:   PAIT=viewer, ACME=(absent), BUGZ=(absent), LOGS=viewer
//	dev_outsider: all absent (and global role=external means no
//	              auto-grants either)
func seedMemberships(tx *sql.Tx, projectIDs map[string]int64) error {
	idByKey := func(k string) int64 { return projectIDs[k] }
	devEditor := devUserByName("dev_editor").ID
	devViewer := devUserByName("dev_viewer").ID

	rows := []memberRow{
		{UserID: devEditor, ProjectID: idByKey("PAIT"), Level: "editor"},
		{UserID: devEditor, ProjectID: idByKey("ACME"), Level: "editor"},
		{UserID: devEditor, ProjectID: idByKey("BUGZ"), Level: "viewer"},
		// dev_editor on LOGS: no row (absent = no access since global role=member
		// only gets editor on projects via explicit grants in this seed model)
		{UserID: devViewer, ProjectID: idByKey("PAIT"), Level: "viewer"},
		{UserID: devViewer, ProjectID: idByKey("LOGS"), Level: "viewer"},
		// dev_outsider: no rows at all.
	}
	for _, r := range rows {
		// Use INSERT OR REPLACE so re-running with a tweaked matrix
		// converges instead of erroring on the (user_id, project_id)
		// PRIMARY KEY uniqueness.
		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO project_members (user_id, project_id, access_level)
			VALUES (?, ?, ?)
		`, r.UserID, r.ProjectID, r.Level); err != nil {
			return fmt.Errorf("upsert membership user=%d project=%d: %w", r.UserID, r.ProjectID, err)
		}
	}
	return nil
}

// seedIssues creates a small set of fixture issues per project — just
// enough surface for an agent to drive a basic UI walkthrough. Skips a
// project entirely if it already has any issues, so re-running this
// seeder is safe and never grows the issue count past the initial set.
func seedIssues(tx *sql.Tx, projectIDs map[string]int64) error {
	devAdmin := devUserByName("dev_admin").ID
	for _, p := range devProjects {
		pid := projectIDs[p.Key]
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM issues WHERE project_id=?", pid).Scan(&count); err != nil {
			return fmt.Errorf("count issues for %s: %w", p.Key, err)
		}
		if count > 0 {
			continue // already seeded — no duplicate fixtures
		}
		// Minimal variety — varies status / priority / type so each
		// project has at least one row in each list-view filter group.
		issues := []struct {
			Title    string
			Status   string
			Priority string
			Type     string
		}{
			// status enum (per db.go M-status check): new | backlog |
			// in-progress | qa | done | delivered | accepted | invoiced |
			// cancelled. Using a spread so each status filter has at
			// least one fixture row.
			{p.Key + " smoke #1: backlog ticket — high priority", "backlog", "high", "ticket"},
			{p.Key + " smoke #2: in-progress task", "in-progress", "medium", "task"},
			{p.Key + " smoke #3: completed work", "done", "low", "ticket"},
			{p.Key + " smoke #4: cancelled scope", "cancelled", "low", "ticket"},
			{p.Key + " smoke #5: epic with no children", "backlog", "medium", "epic"},
		}
		for i, it := range issues {
			if _, err := tx.Exec(`
				INSERT INTO issues (project_id, title, status, priority, type, issue_number, assignee_id)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, pid, it.Title, it.Status, it.Priority, it.Type, i+1, devAdmin); err != nil {
				return fmt.Errorf("insert issue #%d for %s: %w", i+1, p.Key, err)
			}
		}
	}
	return nil
}

func devUserByName(name string) devUser {
	for _, u := range devUsers {
		if u.Username == name {
			return u
		}
	}
	panic("devseed: unknown dev user " + name) // build-time invariant
}
