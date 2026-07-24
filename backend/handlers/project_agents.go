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
// License against version 3. If not, see <https://www.gnu.org/licenses/>.

// PAI-326 — declarable agents per project. CRUD for the project_agents
// table introduced in M94. The schema choice (one row per agent, not a
// JSON blob on projects) deliberately mirrors project_repos /
// project_tags so PAI-329 can extend with additional columns without
// schema-architectural surgery.
//
// PAI-329 extends the per-agent shape with body / bootstrap_steps /
// non_negotiable_rules columns (M95). The CRUD handlers persist all
// of these in one PUT — there's no partial agent update today; the
// editor writes the whole record. Unknown fields in the payload are
// ignored (the json decoder discards them silently).

package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/models"
)

// agentNamePattern enforces the canonical agent slug shape: lowercase
// start, then lowercase letters / digits / underscore / hyphen. Max 32
// chars is enforced separately so we can return a precise error.
var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

const agentNameMaxLen = 32

// reservedAgentNames are sentinel values that must never be persisted
// as user-declared agents — they're claimed by the platform itself.
// `web-ui` is the established sentinel for changes that originate from
// the SPA rather than an automated agent.
var reservedAgentNames = map[string]bool{
	"web-ui": true,
}

type projectAgentPayload struct {
	Name               string                      `json:"name"`
	Description        string                      `json:"description"`
	SlashCommandName   string                      `json:"slash_command_name"`
	LaneTags           []string                    `json:"lane_tags"`
	Metadata           map[string]any              `json:"metadata"`
	Body               string                      `json:"body"`
	BootstrapSteps     []models.AgentBootstrapStep `json:"bootstrap_steps"`
	NonNegotiableRules []models.AgentRule          `json:"non_negotiable_rules"`
}

// ListProjectAgents returns the array of agents declared on the given
// project. Empty array (never null) when none have been declared yet —
// existing projects without an explicit declaration get [], not 404.
func ListProjectAgents(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	out, err := loadProjectAgents(projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

// CreateProjectAgent appends a single agent to the project. Returns 409
// on duplicate (project_id, name); 400 on invalid name / reserved
// sentinel. The endpoint is admin-gated at the route layer, but we do
// not assume that here — validation runs unconditionally so misuse is
// always rejected with a clear error.
func CreateProjectAgent(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body projectAgentPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if msg := validateProjectAgentPayload(body); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}

	encoded, err := encodeAgentJSONFields(body)
	if err != nil {
		jsonError(w, "invalid lane_tags / metadata / bootstrap_steps / non_negotiable_rules", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		INSERT INTO project_agents(project_id, name, description, slash_command_name, lane_tags, metadata, body, bootstrap_steps, non_negotiable_rules, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
	`, projectID, body.Name, strings.TrimSpace(body.Description), strings.TrimSpace(body.SlashCommandName),
		encoded.LaneTags, encoded.Metadata, body.Body, encoded.BootstrapSteps, encoded.NonNegotiableRules,
		now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "agent name already exists for this project", http.StatusConflict)
			return
		}
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	agent := getProjectAgentByID(id)
	if agent == nil {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	// PAI-331: notify any active sync watchers.
	PublishAgentChanged(projectID, agent.Name, "")
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, agent)
}

// UpdateProjectAgent replaces all fields of an existing agent in-place.
// The {name} URL param identifies the row; the body's `name` field, if
// provided, can rename the agent. Strict update (no upsert) — returns
// 404 if no row matches (project_id, current name). 409 on rename
// collision with another existing agent on the same project.
func UpdateProjectAgent(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	currentName := strings.TrimSpace(chi.URLParam(r, "name"))
	if currentName == "" {
		jsonError(w, "agent name required", http.StatusBadRequest)
		return
	}

	var body projectAgentPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	// Body name defaults to URL name when omitted (PUT semantics where
	// the caller treats the URL as the canonical identifier).
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		body.Name = currentName
	}
	if msg := validateProjectAgentPayload(body); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	encoded, err := encodeAgentJSONFields(body)
	if err != nil {
		jsonError(w, "invalid lane_tags / metadata / bootstrap_steps / non_negotiable_rules", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		UPDATE project_agents
		SET name=?, description=?, slash_command_name=?,
		    lane_tags=?, metadata=?,
		    body=?, bootstrap_steps=?, non_negotiable_rules=?,
		    updated_at=?
		WHERE project_id=? AND name=?
	`, body.Name, strings.TrimSpace(body.Description), strings.TrimSpace(body.SlashCommandName),
		encoded.LaneTags, encoded.Metadata,
		body.Body, encoded.BootstrapSteps, encoded.NonNegotiableRules,
		now, projectID, currentName)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "agent name already exists for this project", http.StatusConflict)
			return
		}
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	agent := getProjectAgentByProjectAndName(projectID, body.Name)
	if agent == nil {
		jsonError(w, "not found after update", http.StatusInternalServerError)
		return
	}
	// PAI-331: notify any active sync watchers. If the rename happened
	// (currentName != body.Name), publish for both names — old watchers
	// may need to drop their stale file.
	PublishAgentChanged(projectID, body.Name, "")
	if currentName != body.Name {
		PublishAgentChanged(projectID, currentName, "")
	}
	jsonOK(w, agent)
}

// DeleteProjectAgent removes a single agent identified by project_id +
// name. Returns 204 on success, 404 if no row matched. Idempotent
// callers should check for both 204 and 404.
func DeleteProjectAgent(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		jsonError(w, "agent name required", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec(`DELETE FROM project_agents WHERE project_id=? AND name=?`, projectID, name)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	// PAI-331: notify any active sync watchers.
	PublishAgentChanged(projectID, name, "")
	w.WriteHeader(http.StatusNoContent)
}

// validateProjectAgentPayload returns a non-empty error string when the
// payload is invalid, empty string otherwise. Splits errors so the
// caller can surface the most actionable message: empty / too-long /
// pattern / reserved are distinct conditions.
func validateProjectAgentPayload(p projectAgentPayload) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return "name required"
	}
	if len(name) > agentNameMaxLen {
		return "name too long (max 32 chars)"
	}
	if !agentNamePattern.MatchString(name) {
		return "name must match [a-z][a-z0-9_-]*"
	}
	if reservedAgentNames[name] {
		return "name is reserved (e.g. web-ui)"
	}
	for _, tag := range p.LaneTags {
		if strings.TrimSpace(tag) == "" {
			return "lane_tags entries must be non-empty"
		}
	}
	return ""
}

// encodedAgentJSON bundles the JSON-text on-disk shapes for every
// structured agent column. Returned together (rather than as named
// returns) so callers thread one struct rather than 4 strings —
// PAI-329 added two more fields and a positional-arg explosion was
// already pushing the limits of readability.
type encodedAgentJSON struct {
	LaneTags           string
	Metadata           string
	BootstrapSteps     string
	NonNegotiableRules string
}

// encodeAgentJSONFields normalises every JSON-blob agent column to
// its on-disk text shape. nil / empty collections become "[]" / "{}"
// so empty rows round-trip cleanly through the API and renderers can
// rely on `len(...)` checks rather than nil checks.
func encodeAgentJSONFields(p projectAgentPayload) (encodedAgentJSON, error) {
	var out encodedAgentJSON

	laneTags := p.LaneTags
	if laneTags == nil {
		laneTags = []string{}
	}
	laneJSON, err := json.Marshal(laneTags)
	if err != nil {
		return out, err
	}
	out.LaneTags = string(laneJSON)

	metadata := p.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return out, err
	}
	out.Metadata = string(metaJSON)

	steps := p.BootstrapSteps
	if steps == nil {
		steps = []models.AgentBootstrapStep{}
	}
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		return out, err
	}
	out.BootstrapSteps = string(stepsJSON)

	rules := p.NonNegotiableRules
	if rules == nil {
		rules = []models.AgentRule{}
	}
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return out, err
	}
	out.NonNegotiableRules = string(rulesJSON)

	return out, nil
}

// agentColumns is the canonical SELECT list for project_agents, kept
// as a single constant so list / detail / single-name fetches stay
// in lock-step with scanProjectAgent.
const agentColumns = `id, project_id, name, description, slash_command_name,
		lane_tags, metadata, body, bootstrap_steps, non_negotiable_rules,
		created_at, updated_at`

// loadProjectAgents returns the array of agents for a project, sorted
// by name (stable order). Returns an empty slice (never nil) so JSON
// callers always see [] for empty projects.
func loadProjectAgents(projectID int64) ([]models.ProjectAgent, error) {
	rows, err := db.DB.Query(`
		SELECT `+agentColumns+`
		FROM project_agents
		WHERE project_id = ?
		ORDER BY name ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ProjectAgent{}
	for rows.Next() {
		agent, err := scanProjectAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, agent)
	}
	return out, rows.Err()
}

func getProjectAgentByID(id int64) *models.ProjectAgent {
	row := db.DB.QueryRow(`
		SELECT `+agentColumns+`
		FROM project_agents WHERE id = ?
	`, id)
	agent, err := scanProjectAgent(row)
	if err != nil {
		return nil
	}
	return &agent
}

func getProjectAgentByProjectAndName(projectID int64, name string) *models.ProjectAgent {
	row := db.DB.QueryRow(`
		SELECT `+agentColumns+`
		FROM project_agents WHERE project_id = ? AND name = ?
	`, projectID, name)
	agent, err := scanProjectAgent(row)
	if err != nil {
		return nil
	}
	return &agent
}

// scanProjectAgent serves both list (*sql.Rows) and single-record
// (*sql.Row) paths via the package-level rowScanner abstraction
// declared in customers.go. All JSON columns degrade to empty native
// shapes on parse error — defensive against hand-edited rows.
func scanProjectAgent(s rowScanner) (models.ProjectAgent, error) {
	var agent models.ProjectAgent
	var laneJSON, metaJSON, bootstrapJSON, rulesJSON string
	if err := s.Scan(
		&agent.ID, &agent.ProjectID, &agent.Name, &agent.Description,
		&agent.SlashCommandName,
		&laneJSON, &metaJSON, &agent.Body, &bootstrapJSON, &rulesJSON,
		&agent.CreatedAt, &agent.UpdatedAt,
	); err != nil {
		return agent, err
	}
	agent.LaneTags = []string{}
	if strings.TrimSpace(laneJSON) != "" {
		_ = json.Unmarshal([]byte(laneJSON), &agent.LaneTags)
		if agent.LaneTags == nil {
			agent.LaneTags = []string{}
		}
	}
	agent.Metadata = map[string]any{}
	if strings.TrimSpace(metaJSON) != "" {
		_ = json.Unmarshal([]byte(metaJSON), &agent.Metadata)
		if agent.Metadata == nil {
			agent.Metadata = map[string]any{}
		}
	}
	agent.BootstrapSteps = []models.AgentBootstrapStep{}
	if strings.TrimSpace(bootstrapJSON) != "" {
		_ = json.Unmarshal([]byte(bootstrapJSON), &agent.BootstrapSteps)
		if agent.BootstrapSteps == nil {
			agent.BootstrapSteps = []models.AgentBootstrapStep{}
		}
	}
	agent.NonNegotiableRules = []models.AgentRule{}
	if strings.TrimSpace(rulesJSON) != "" {
		_ = json.Unmarshal([]byte(rulesJSON), &agent.NonNegotiableRules)
		if agent.NonNegotiableRules == nil {
			agent.NonNegotiableRules = []models.AgentRule{}
		}
	}
	return agent, nil
}

// suppress unused import warning when sql.ErrNoRows is otherwise unused.
var _ = sql.ErrNoRows
