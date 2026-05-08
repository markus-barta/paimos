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

// PAI-329 — project-level shared inventories that the canonical agent
// artifact endpoint (`/api/projects/:id/agents/:name.json`) merges into
// every rendered agent. Keeping them as separate tables (mirrors the
// project_repos precedent from M75) lets the editor be a flat list-of-
// items UI rather than a JSON-blob editing dance, and lets agents
// reference items by name (deploy_recipes_used: ["backend-staging"]).

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// inventoryNamePattern enforces a permissive slug for environment /
// recipe names: must start with a letter, then letters / digits /
// underscore / hyphen. Project-scoped unique. Names are user-visible
// labels (env.name e.g. "staging") but also referenced by other
// fields (e.g. deploy_recipes_used: ["backend-staging"]) so a stable,
// shell-friendly shape matters.
var inventoryNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

const inventoryNameMaxLen = 64

// ── project_environments ────────────────────────────────────────────

type projectEnvironmentPayload struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	HostAlias string `json:"host_alias"`
	HostIP    string `json:"host_ip"`
	SortOrder int    `json:"sort_order"`
}

// ListProjectEnvironments returns the array of declared environments
// for a project, ordered by sort_order then id. Empty array (never
// null) when none are declared.
func ListProjectEnvironments(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	out, err := loadProjectEnvironments(projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

func CreateProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body projectEnvironmentPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if msg := validateInventoryName(body.Name); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	if body.SortOrder == 0 {
		_ = db.DB.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM project_environments WHERE project_id=?`, projectID).Scan(&body.SortOrder)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		INSERT INTO project_environments(project_id, name, url, host_alias, host_ip, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)
	`, projectID, body.Name, strings.TrimSpace(body.URL),
		strings.TrimSpace(body.HostAlias), strings.TrimSpace(body.HostIP),
		body.SortOrder, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "environment name already exists for this project", http.StatusConflict)
			return
		}
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	env := getProjectEnvironmentByID(id)
	if env == nil {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, env)
}

func UpdateProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	envID, err := strconv.ParseInt(chi.URLParam(r, "envId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid environment id", http.StatusBadRequest)
		return
	}
	var body projectEnvironmentPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if msg := validateInventoryName(body.Name); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		UPDATE project_environments
		SET name=?, url=?, host_alias=?, host_ip=?, sort_order=?, updated_at=?
		WHERE id=?
	`, body.Name, strings.TrimSpace(body.URL),
		strings.TrimSpace(body.HostAlias), strings.TrimSpace(body.HostIP),
		body.SortOrder, now, envID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "environment name already exists for this project", http.StatusConflict)
			return
		}
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "environment not found", http.StatusNotFound)
		return
	}
	env := getProjectEnvironmentByID(envID)
	if env == nil {
		jsonError(w, "not found after update", http.StatusInternalServerError)
		return
	}
	jsonOK(w, env)
}

func DeleteProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	envID, err := strconv.ParseInt(chi.URLParam(r, "envId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid environment id", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec(`DELETE FROM project_environments WHERE id=?`, envID)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "environment not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func loadProjectEnvironments(projectID int64) ([]models.ProjectEnvironment, error) {
	rows, err := db.DB.Query(`
		SELECT id, project_id, name, url, host_alias, host_ip, sort_order, created_at, updated_at
		FROM project_environments
		WHERE project_id = ?
		ORDER BY sort_order ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ProjectEnvironment{}
	for rows.Next() {
		var env models.ProjectEnvironment
		if err := rows.Scan(&env.ID, &env.ProjectID, &env.Name, &env.URL,
			&env.HostAlias, &env.HostIP, &env.SortOrder,
			&env.CreatedAt, &env.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, rows.Err()
}

func getProjectEnvironmentByID(id int64) *models.ProjectEnvironment {
	var env models.ProjectEnvironment
	err := db.DB.QueryRow(`
		SELECT id, project_id, name, url, host_alias, host_ip, sort_order, created_at, updated_at
		FROM project_environments WHERE id=?
	`, id).Scan(&env.ID, &env.ProjectID, &env.Name, &env.URL,
		&env.HostAlias, &env.HostIP, &env.SortOrder,
		&env.CreatedAt, &env.UpdatedAt)
	if err != nil {
		return nil
	}
	return &env
}

// ── project_deploy_recipes ──────────────────────────────────────────

type projectDeployRecipePayload struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Summary   string `json:"summary"`
	SortOrder int    `json:"sort_order"`
}

// ListProjectDeployRecipes returns named, reusable deployment commands
// for a project. Empty array when none are declared.
func ListProjectDeployRecipes(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	out, err := loadProjectDeployRecipes(projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

func CreateProjectDeployRecipe(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body projectDeployRecipePayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if msg := validateInventoryName(body.Name); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	if body.SortOrder == 0 {
		_ = db.DB.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM project_deploy_recipes WHERE project_id=?`, projectID).Scan(&body.SortOrder)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		INSERT INTO project_deploy_recipes(project_id, name, command, summary, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)
	`, projectID, body.Name, strings.TrimSpace(body.Command), strings.TrimSpace(body.Summary),
		body.SortOrder, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "deploy recipe name already exists for this project", http.StatusConflict)
			return
		}
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	rec := getProjectDeployRecipeByID(id)
	if rec == nil {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, rec)
}

func UpdateProjectDeployRecipe(w http.ResponseWriter, r *http.Request) {
	recID, err := strconv.ParseInt(chi.URLParam(r, "recipeId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid recipe id", http.StatusBadRequest)
		return
	}
	var body projectDeployRecipePayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if msg := validateInventoryName(body.Name); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		UPDATE project_deploy_recipes
		SET name=?, command=?, summary=?, sort_order=?, updated_at=?
		WHERE id=?
	`, body.Name, strings.TrimSpace(body.Command), strings.TrimSpace(body.Summary),
		body.SortOrder, now, recID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "deploy recipe name already exists for this project", http.StatusConflict)
			return
		}
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "deploy recipe not found", http.StatusNotFound)
		return
	}
	rec := getProjectDeployRecipeByID(recID)
	if rec == nil {
		jsonError(w, "not found after update", http.StatusInternalServerError)
		return
	}
	jsonOK(w, rec)
}

func DeleteProjectDeployRecipe(w http.ResponseWriter, r *http.Request) {
	recID, err := strconv.ParseInt(chi.URLParam(r, "recipeId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid recipe id", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec(`DELETE FROM project_deploy_recipes WHERE id=?`, recID)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "deploy recipe not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func loadProjectDeployRecipes(projectID int64) ([]models.ProjectDeployRecipe, error) {
	rows, err := db.DB.Query(`
		SELECT id, project_id, name, command, summary, sort_order, created_at, updated_at
		FROM project_deploy_recipes
		WHERE project_id = ?
		ORDER BY sort_order ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ProjectDeployRecipe{}
	for rows.Next() {
		var rec models.ProjectDeployRecipe
		if err := rows.Scan(&rec.ID, &rec.ProjectID, &rec.Name, &rec.Command,
			&rec.Summary, &rec.SortOrder, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func getProjectDeployRecipeByID(id int64) *models.ProjectDeployRecipe {
	var rec models.ProjectDeployRecipe
	err := db.DB.QueryRow(`
		SELECT id, project_id, name, command, summary, sort_order, created_at, updated_at
		FROM project_deploy_recipes WHERE id=?
	`, id).Scan(&rec.ID, &rec.ProjectID, &rec.Name, &rec.Command,
		&rec.Summary, &rec.SortOrder, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return nil
	}
	return &rec
}

// validateInventoryName returns a non-empty error string when the
// candidate name violates the inventory naming rules. Shared between
// environments and deploy recipes — same shape, same rules.
func validateInventoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "name required"
	}
	if len(name) > inventoryNameMaxLen {
		return "name too long (max 64 chars)"
	}
	if !inventoryNamePattern.MatchString(name) {
		return "name must match [a-zA-Z][a-zA-Z0-9_-]*"
	}
	return ""
}

// ── replace-all helpers (PAI-329, used by PUT /api/projects/{id}) ──

// validateEnvironmentsPayload enforces uniqueness + name rules across
// the whole array before any DB mutation, so a single bad row aborts
// the replace cleanly instead of leaving the table half-rewritten.
func validateEnvironmentsPayload(items []projectEnvironmentPayload) string {
	seen := map[string]bool{}
	for i, env := range items {
		if msg := validateInventoryName(env.Name); msg != "" {
			return fmt.Sprintf("environments[%d]: %s", i, msg)
		}
		key := strings.TrimSpace(env.Name)
		if seen[key] {
			return fmt.Sprintf("environments[%d]: duplicate name %q", i, key)
		}
		seen[key] = true
	}
	return ""
}

// validateDeployRecipesPayload — same shape as environments. Enforces
// project-scoped uniqueness across the whole array.
func validateDeployRecipesPayload(items []projectDeployRecipePayload) string {
	seen := map[string]bool{}
	for i, rec := range items {
		if msg := validateInventoryName(rec.Name); msg != "" {
			return fmt.Sprintf("deploy_recipes[%d]: %s", i, msg)
		}
		key := strings.TrimSpace(rec.Name)
		if seen[key] {
			return fmt.Sprintf("deploy_recipes[%d]: duplicate name %q", i, key)
		}
		seen[key] = true
	}
	return ""
}

// replaceProjectEnvironments wipes + re-inserts the environments table
// for one project in a single transaction. Sort order defaults to
// the array index when the caller leaves it 0, so the on-disk order
// mirrors the JSON order (acceptance #6 — byte-identity).
func replaceProjectEnvironments(projectID int64, items []projectEnvironmentPayload) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit

	if _, err := tx.Exec(`DELETE FROM project_environments WHERE project_id=?`, projectID); err != nil {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for i, env := range items {
		order := env.SortOrder
		if order == 0 {
			order = i
		}
		if _, err := tx.Exec(`
			INSERT INTO project_environments(project_id, name, url, host_alias, host_ip, sort_order, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?)
		`, projectID, strings.TrimSpace(env.Name),
			strings.TrimSpace(env.URL),
			strings.TrimSpace(env.HostAlias), strings.TrimSpace(env.HostIP),
			order, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// replaceProjectDeployRecipes — same pattern.
func replaceProjectDeployRecipes(projectID int64, items []projectDeployRecipePayload) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM project_deploy_recipes WHERE project_id=?`, projectID); err != nil {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for i, rec := range items {
		order := rec.SortOrder
		if order == 0 {
			order = i
		}
		if _, err := tx.Exec(`
			INSERT INTO project_deploy_recipes(project_id, name, command, summary, sort_order, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?)
		`, projectID, strings.TrimSpace(rec.Name),
			strings.TrimSpace(rec.Command), strings.TrimSpace(rec.Summary),
			order, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
