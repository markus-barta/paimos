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

// import_engine.go — shared import logic for CSV and (future) Jira imports.
//
// Both sources parse their data into []ImportRow, then call RunPreflight or
// RunImport. This keeps collision handling, tag creation, parent remapping,
// and type-ordering in one place.

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
)

// ── Data types ────────────────────────────────────────────────────────────────

// ImportRow is the normalised representation of one issue to be imported,
// regardless of source (CSV, Jira, …).
type ImportRow struct {
	OrigKey            string   // original issue key, e.g. "ACME-1"
	Type               string   // "epic" | "ticket" | "task"
	ParentKey          string   // original key of parent issue, or ""
	Title              string
	Description        string
	AcceptanceCriteria string
	Notes              string
	Status             string
	Priority           string
	CostUnit           string
	Release            string
	Assignee           string   // username (matched on import)
	Tags               []string // tag names
}

// CollisionStrategy controls what happens when an issue with the same key
// already exists in the target project.
type CollisionStrategy string

const (
	StrategySkip      CollisionStrategy = "skip"
	StrategyOverwrite CollisionStrategy = "overwrite"
	StrategyInsert    CollisionStrategy = "insert"
)

// PreflightResult is returned before a real import so the UI can show a
// confirmation dialog.
type PreflightResult struct {
	ProjectKey    string   `json:"project_key"`
	ProjectExists bool     `json:"project_exists"`
	Total         int      `json:"total"`
	CollisionKeys []string `json:"collision_keys"`
	CollisionCount int     `json:"collision_count"`
	NewCount      int      `json:"new_count"`
}

// ImportResult is returned after a successful import.
type ImportResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

// ── Preflight ─────────────────────────────────────────────────────────────────

// RunPreflight checks which rows would collide with existing issues.
// projectKey is the key under which rows will be imported.
func RunPreflight(rows []ImportRow, projectKey string) PreflightResult {
	result := PreflightResult{
		ProjectKey: projectKey,
		Total:      len(rows),
	}

	// Does the project exist?
	var projectID int64
	err := db.DB.QueryRow("SELECT id FROM projects WHERE UPPER(key)=UPPER(?)", projectKey).Scan(&projectID)
	result.ProjectExists = err == nil

	if !result.ProjectExists {
		result.NewCount = len(rows)
		return result
	}

	// Build set of existing issue numbers in this project
	existingNums := map[int]bool{}
	erows, err := db.DB.Query("SELECT issue_number FROM issues WHERE project_id=?", projectID)
	if err == nil {
		defer erows.Close()
		for erows.Next() {
			var n int
			erows.Scan(&n)
			existingNums[n] = true
		}
	}

	for _, row := range rows {
		num := issueNumFromKey(row.OrigKey)
		if num > 0 && existingNums[num] {
			result.CollisionKeys = append(result.CollisionKeys, row.OrigKey)
			result.CollisionCount++
		} else {
			result.NewCount++
		}
	}
	return result
}

// ── Import ────────────────────────────────────────────────────────────────────

// EnsureProjectExists returns the project ID for projectKey, creating it
// (with projectName as name) if it doesn't exist yet.
func EnsureProjectExists(projectKey, projectName string) (int64, error) {
	var id int64
	err := db.DB.QueryRow("SELECT id FROM projects WHERE UPPER(key)=UPPER(?)", projectKey).Scan(&id)
	if err == nil {
		return id, nil
	}
	// Create new project
	name := projectName
	if name == "" {
		name = projectKey
	}
	res, err := db.DB.Exec(
		"INSERT INTO projects(name,key,description,status) VALUES(?,?,?,?)",
		name, strings.ToUpper(projectKey), "", "active",
	)
	if err != nil {
		return 0, fmt.Errorf("create project: %w", err)
	}
	return res.LastInsertId()
}

// RunImport inserts/updates/skips rows according to strategy.
// All rows are imported into projectID.
func RunImport(rows []ImportRow, projectID int64, strategy CollisionStrategy) ImportResult {
	result := ImportResult{Errors: []string{}}

	// Sort: epics first, then tickets, then tasks (so parents exist before children)
	typeOrder := map[string]int{"epic": 1, "ticket": 2, "task": 3}
	sort.SliceStable(rows, func(i, j int) bool {
		return typeOrder[rows[i].Type] < typeOrder[rows[j].Type]
	})

	// Build existing issue_number → issue ID map for this project
	existingNumToID := map[int]int64{}
	erows, _ := db.DB.Query("SELECT id, issue_number FROM issues WHERE project_id=?", projectID)
	if erows != nil {
		defer erows.Close()
		for erows.Next() {
			var id int64
			var num int
			erows.Scan(&id, &num)
			existingNumToID[num] = id
		}
	}

	// origKey → new DB id (populated as we insert, for parent resolution)
	keyToID := map[string]int64{}
	// Also pre-populate from existing issues (for overwrite parent resolution)
	ekrows, _ := db.DB.Query(`
		SELECT i.id, i.issue_number, proj.key
		FROM issues i JOIN projects proj ON proj.id = i.project_id
		WHERE i.project_id = ?
	`, projectID)
	if ekrows != nil {
		defer ekrows.Close()
		for ekrows.Next() {
			var id int64
			var num int
			var key string
			ekrows.Scan(&id, &num, &key)
			keyToID[fmt.Sprintf("%s-%d", key, num)] = id
		}
	}

	for _, row := range rows {
		typ := strings.ToLower(row.Type)
		if row.Title == "" || (typ != "epic" && typ != "ticket" && typ != "task") {
			result.Skipped++
			continue
		}

		origNum := issueNumFromKey(row.OrigKey)
		existingID, exists := existingNumToID[origNum]

		// Apply collision strategy
		if exists {
			switch strategy {
			case StrategySkip:
				if row.OrigKey != "" {
					keyToID[row.OrigKey] = existingID
				}
				result.Skipped++
				continue

			case StrategyOverwrite:
				if err := overwriteIssue(existingID, row, keyToID); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", row.OrigKey, err))
					result.Skipped++
				} else {
					keyToID[row.OrigKey] = existingID
					result.Updated++
				}
				// sync tags
				syncIssueTags(existingID, row.Tags)
				continue

			case StrategyInsert:
				// fall through to insert
			}
		}

		// INSERT new issue
		newID, err := insertIssue(projectID, row, keyToID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", row.OrigKey, err))
			result.Skipped++
			continue
		}
		if row.OrigKey != "" {
			keyToID[row.OrigKey] = newID
		}
		syncIssueTags(newID, row.Tags)
		result.Imported++
	}

	return result
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// issueNumFromKey extracts the numeric suffix from "ACME-1" → 7.
func issueNumFromKey(key string) int {
	parts := strings.SplitN(key, "-", 2)
	if len(parts) != 2 {
		return 0
	}
	n := 0
	fmt.Sscanf(parts[1], "%d", &n)
	return n
}

// projectKeyFromKey extracts the project key prefix from "ACME-1" → "ACME".
func projectKeyFromKey(key string) string {
	parts := strings.SplitN(key, "-", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func resolveParent(parentKey string, keyToID map[string]int64) *int64 {
	if parentKey == "" {
		return nil
	}
	if id, ok := keyToID[parentKey]; ok {
		return &id
	}
	return nil
}

func resolveAssignee(username string) *int64 {
	if username == "" {
		return nil
	}
	var id int64
	if db.DB.QueryRow("SELECT id FROM users WHERE username=?", username).Scan(&id) == nil {
		return &id
	}
	return nil
}

func nextIssueNumber(projectID int64) int {
	var n int
	db.DB.QueryRow(
		"SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?", projectID,
	).Scan(&n)
	return n
}

func insertIssue(projectID int64, row ImportRow, keyToID map[string]int64) (int64, error) {
	status := normaliseImportStatus(row.Status)
	priority := row.Priority
	if priority == "" {
		priority = "medium"
	}
	num := nextIssueNumber(projectID)
	res, err := db.DB.Exec(`
		INSERT INTO issues(
			project_id, issue_number, type, parent_id,
			title, description, acceptance_criteria, notes,
			status, priority, cost_unit, release, assignee_id
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		projectID, num, row.Type, resolveParent(row.ParentKey, keyToID),
		row.Title, row.Description, row.AcceptanceCriteria, row.Notes,
		status, priority, row.CostUnit, row.Release, resolveAssignee(row.Assignee),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func overwriteIssue(id int64, row ImportRow, keyToID map[string]int64) error {
	status := normaliseImportStatus(row.Status)
	priority := row.Priority
	if priority == "" {
		priority = "medium"
	}
	_, err := db.DB.Exec(`
		UPDATE issues SET
			type=?, parent_id=?,
			title=?, description=?, acceptance_criteria=?, notes=?,
			status=?, priority=?, cost_unit=?, release=?, assignee_id=?,
			updated_at=datetime('now')
		WHERE id=?
	`,
		row.Type, resolveParent(row.ParentKey, keyToID),
		row.Title, row.Description, row.AcceptanceCriteria, row.Notes,
		status, priority, row.CostUnit, row.Release, resolveAssignee(row.Assignee),
		id,
	)
	return err
}

// normaliseImportStatus maps legacy/external status strings to canonical PAIMOS values.
// Unknown values fall back to "backlog" to prevent DB CHECK constraint failures.
func normaliseImportStatus(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	switch lower {
	case "", "open", "to do", "selected for development":
		return "backlog"
	case "new":
		return "new"
	case "in-progress", "in progress", "in review":
		return "in-progress"
	case "qa", "testing", "in testing":
		return "qa"
	case "done", "complete", "completed", "resolved":
		return "done"
	case "delivered":
		return "delivered"
	case "accepted":
		return "accepted"
	case "invoiced":
		return "invoiced"
	case "cancelled", "canceled", "closed", "rejected":
		return "cancelled"
	}
	// Already a valid PAIMOS status — pass through
	valid := map[string]bool{"new": true, "backlog": true, "in-progress": true, "qa": true, "done": true, "delivered": true, "accepted": true, "invoiced": true, "cancelled": true}
	if valid[s] {
		return s
	}
	return "backlog"
}

func syncIssueTags(issueID int64, tagNames []string) {
	if len(tagNames) == 0 {
		return
	}
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tagID := ensureTagByName(name)
		if tagID > 0 {
			if _, err := db.DB.Exec(
				"INSERT OR IGNORE INTO issue_tags(issue_id, tag_id) VALUES(?,?)",
				issueID, tagID,
			); err != nil {
				log.Printf("syncIssueTags: issue=%d tag=%d: %v", issueID, tagID, err)
				continue
			}
		}
	}
}

func ensureTagByName(name string) int64 {
	var id int64
	if db.DB.QueryRow("SELECT id FROM tags WHERE name=?", name).Scan(&id) == nil {
		return id
	}
	res, err := db.DB.Exec("INSERT OR IGNORE INTO tags(name, color) VALUES(?,?)", name, "gray")
	if err != nil {
		return 0
	}
	id, _ = res.LastInsertId()
	if id == 0 {
		db.DB.QueryRow("SELECT id FROM tags WHERE name=?", name).Scan(&id)
	}
	return id
}
