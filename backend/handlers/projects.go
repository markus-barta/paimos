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

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

func ListProjects(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	// Never leak deleted projects through normal listing; require explicit opt-in
	if status != "active" && status != "archived" && status != "deleted" {
		status = "active"
	}

	filter, filterArgs := projectIDFilter(r, "p.id", false)
	args := append([]any{status}, filterArgs...)

	// #nosec G202 G701 -- projectIDFilter returns a fixed SQL fragment plus placeholder args; status is allowlisted above.
	rows, err := db.DB.Query(`
		SELECT p.id, p.name, p.key, p.description, p.status,
		       p.product_owner, p.customer_label, p.customer_id,
		       COALESCE(c.name, ''),
		       p.created_at, p.updated_at,
		       COUNT(CASE WHEN i.type NOT IN ('memory','runbook','external_system','related_project','guideline') THEN 1 END) as issue_count,
		       COALESCE(p.logo_path, ''),
		       COALESCE(MAX(i.updated_at), '') as last_activity,
		       COUNT(CASE
		               WHEN i.status NOT IN ('done','delivered','cancelled')
		                AND i.type NOT IN ('memory','runbook','external_system','related_project','guideline')
		              THEN 1 END) as open_issue_count,
		       COUNT(CASE
		               WHEN i.status IN ('done','delivered')
		                AND i.type NOT IN ('memory','runbook','external_system','related_project','guideline')
		              THEN 1 END) as done_issue_count,
		       COUNT(CASE
		               WHEN i.status != 'cancelled'
		                AND i.type NOT IN ('memory','runbook','external_system','related_project','guideline')
		              THEN 1 END) as active_issue_count,
		       p.rate_hourly, p.rate_lp,
		       c.rate_hourly, c.rate_lp
		FROM projects p
		LEFT JOIN issues i    ON i.project_id = p.id
		LEFT JOIN customers c ON c.id = p.customer_id
		WHERE p.status = ?`+filter+`
		GROUP BY p.id
		ORDER BY last_activity DESC, p.updated_at DESC
	`, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := []models.Project{}
	for rows.Next() {
		var p models.Project
		var custRateHourly, custRateLp *float64
		if err := rows.Scan(&p.ID, &p.Name, &p.Key, &p.Description, &p.Status,
			&p.ProductOwner, &p.CustomerLabel, &p.CustomerID, &p.CustomerName,
			&p.CreatedAt, &p.UpdatedAt, &p.IssueCount, &p.LogoPath,
			&p.LastActivity, &p.OpenIssueCount, &p.DoneIssueCount, &p.ActiveIssueCount,
			&p.RateHourly, &p.RateLp,
			&custRateHourly, &custRateLp); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		applyEffectiveRates(&p, custRateHourly, custRateLp)
		projects = append(projects, p)
	}
	projects = LoadTagsForProjects(projects)
	jsonOK(w, projects)
}

// applyEffectiveRates fills in EffectiveRate* + RateInherited (PAI-54).
// Project override takes precedence; falls back to the linked customer's
// rate; nil if neither is set. The two rate kinds (hourly / lp) are
// computed independently — one can be inherited while the other is
// overridden.
func applyEffectiveRates(p *models.Project, custH, custLp *float64) {
	hInherited := false
	if p.RateHourly != nil {
		v := *p.RateHourly
		p.EffectiveRateHourly = &v
	} else if custH != nil {
		v := *custH
		p.EffectiveRateHourly = &v
		hInherited = true
	}
	lpInherited := false
	if p.RateLp != nil {
		v := *p.RateLp
		p.EffectiveRateLp = &v
	} else if custLp != nil {
		v := *custLp
		p.EffectiveRateLp = &v
		lpInherited = true
	}
	// "Inherited" = at least one rate kind is sourced from the customer.
	// Frontend can still tell the two apart by checking the project's own
	// nullable rate fields, but the boolean is convenient for "show the
	// inherited badge anywhere on this card" decisions.
	p.RateInherited = hInherited || lpInherited
}

func CreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		Key          string `json:"key"`
		Description  string `json:"description"`
		ProductOwner *int64 `json:"product_owner"`
		// CustomerLabel is the legacy freeform string (PAI-54 rename).
		// CustomerID is the FK to customers.id; nil = unassigned.
		CustomerLabel string `json:"customer_label"`
		CustomerID    *int64 `json:"customer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}

	if body.Key == "" {
		body.Key = suggestKey(body.Name)
	}
	body.Key = sanitizeKey(body.Key)
	if msg := validateKey(body.Key); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}

	res, err := db.DB.Exec(
		"INSERT INTO projects(name,key,description,product_owner,customer_label,customer_id) VALUES(?,?,?,?,?,?)",
		body.Name, body.Key, body.Description, body.ProductOwner, body.CustomerLabel, body.CustomerID,
	)
	if handleDBError(w, err, "project key") {
		return
	}
	id, _ := res.LastInsertId()
	// Seed editor access for all current admin/member users so they don't
	// suddenly lose visibility into a new project.
	auth.SeedAccessForProject(id)
	p := getProjectByID(id)
	if p == nil {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, p)
}

// projectDetailResponse is the wire shape returned by GET /api/projects/{id}.
// PAI-329 expands the response with project-level inventories (agents,
// repos, environments, deploy_recipes) so the SPA / adapters can fetch
// the full project state in a single round-trip. The Project model
// itself stays unchanged so list endpoints (which return many rows)
// don't grow unbounded.
//
// PAI-356 adds the `counts` aggregate so the project-page footer bar
// can render badges next to "Issues" and "Knowledge" without firing
// extra GETs. Cheap to compute (one indexed scan over `issues`).
type projectDetailResponse struct {
	*models.Project
	Agents        []models.ProjectAgent        `json:"agents"`
	Repos         []models.ProjectRepo         `json:"repos"`
	Environments  []models.ProjectEnvironment  `json:"environments"`
	DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
	Counts        projectCounts                `json:"counts"`
}

// projectCounts is the PAI-356 aggregate. `OpenIssues` excludes
// knowledge entries (they live in the same table since PAI-346 but
// shouldn't show up in the issue counter); `KnowledgeEntries` counts
// the five knowledge types together excluding cancelled rows. Both
// fields are non-negative; absence is encoded as 0.
type projectCounts struct {
	OpenIssues       int `json:"open_issues"`
	KnowledgeEntries int `json:"knowledge_entries"`
}

func GetProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	p := getProjectByID(id)
	if p == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	// Defensive on the inventory queries — surface as best-effort
	// empty arrays rather than failing the whole detail response.
	// All four queries hit small-cardinality, project-scoped tables;
	// any error is a real DB problem worth surfacing as 500.
	agents, err := loadProjectAgents(id)
	if err != nil {
		jsonError(w, "query failed (agents)", http.StatusInternalServerError)
		return
	}
	repos, err := listProjectReposData(id)
	if err != nil {
		jsonError(w, "query failed (repos)", http.StatusInternalServerError)
		return
	}
	envs, err := loadProjectEnvironments(id)
	if err != nil {
		jsonError(w, "query failed (environments)", http.StatusInternalServerError)
		return
	}
	recipes, err := loadProjectDeployRecipes(id)
	if err != nil {
		jsonError(w, "query failed (deploy_recipes)", http.StatusInternalServerError)
		return
	}

	counts, _ := loadProjectCounts(id)
	jsonOK(w, projectDetailResponse{
		Project:       p,
		Agents:        agents,
		Repos:         repos,
		Environments:  envs,
		DeployRecipes: recipes,
		Counts:        counts,
	})
}

func UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Name          *string `json:"name"`
		Key           *string `json:"key"`
		Description   *string `json:"description"`
		Status        *string `json:"status"`
		ProductOwner  *int64  `json:"product_owner"`
		CustomerLabel *string `json:"customer_label"`
		// CustomerID is the FK. Pass `null` to detach (the CASE WHEN below
		// preserves the current value when omitted, writes NULL when
		// explicitly null in the request body).
		CustomerID    *int64   `json:"customer_id"`
		ClearCustomer bool     `json:"clear_customer"`
		RateHourly    *float64 `json:"rate_hourly"`
		RateLp        *float64 `json:"rate_lp"`
		// PAI-329 — project-level inventory replace-all hooks. When
		// any of these arrays is non-nil (vs. omitted), the
		// corresponding table is wiped + re-inserted in a single
		// transaction. Per-item CRUD remains the preferred path for
		// targeted edits; this hook exists for hand-authored writes
		// and round-trip byte-identity testing (acceptance #6).
		// Omitting a field leaves the table untouched.
		Environments  *[]projectEnvironmentPayload  `json:"environments"`
		DeployRecipes *[]projectDeployRecipePayload `json:"deploy_recipes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	if body.Key != nil {
		k := sanitizeKey(*body.Key)
		if msg := validateKey(k); msg != "" {
			jsonError(w, msg, http.StatusBadRequest)
			return
		}
		body.Key = &k
	}

	// Two-step expression for customer_id so detach (set NULL) is
	// expressible distinct from "leave as-is": pass clear_customer:true
	// to set NULL; otherwise customer_id is a normal COALESCE.
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = db.DB.Exec(`
		UPDATE projects SET
			name           = COALESCE(?, name),
			key            = COALESCE(?, key),
			description    = COALESCE(?, description),
			status         = COALESCE(?, status),
			product_owner  = CASE WHEN ? IS NOT NULL THEN ? ELSE product_owner END,
			customer_label = COALESCE(?, customer_label),
			customer_id    = CASE WHEN ? THEN NULL ELSE COALESCE(?, customer_id) END,
			rate_hourly    = COALESCE(?, rate_hourly),
			rate_lp        = COALESCE(?, rate_lp),
			updated_at     = ?
		WHERE id=?
	`, body.Name, body.Key, body.Description, body.Status,
		body.ProductOwner, body.ProductOwner,
		body.CustomerLabel,
		body.ClearCustomer, body.CustomerID,
		body.RateHourly, body.RateLp, now, id)
	if handleDBError(w, err, "project key") {
		return
	}

	// PAI-329 — replace-all of inventory arrays, when provided.
	// Validation runs before any mutation so a bad payload aborts
	// without partial writes. Every replace runs in its own
	// transaction; failures are surfaced as 4xx/5xx and roll back
	// only that table.
	if body.Environments != nil {
		if msg := validateEnvironmentsPayload(*body.Environments); msg != "" {
			jsonError(w, msg, http.StatusBadRequest)
			return
		}
		if err := replaceProjectEnvironments(id, *body.Environments); err != nil {
			jsonError(w, "environments write failed", http.StatusInternalServerError)
			return
		}
	}
	if body.DeployRecipes != nil {
		if msg := validateDeployRecipesPayload(*body.DeployRecipes); msg != "" {
			jsonError(w, msg, http.StatusBadRequest)
			return
		}
		if err := replaceProjectDeployRecipes(id, *body.DeployRecipes); err != nil {
			jsonError(w, "deploy_recipes write failed", http.StatusInternalServerError)
			return
		}
	}

	// Re-fetch + return the same extended detail shape as GetProject
	// so callers see the post-write state in one round-trip.
	p := getProjectByID(id)
	if p == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	agents, _ := loadProjectAgents(id)
	repos, _ := listProjectReposData(id)
	envs, _ := loadProjectEnvironments(id)
	recipes, _ := loadProjectDeployRecipes(id)
	counts, _ := loadProjectCounts(id)
	jsonOK(w, projectDetailResponse{
		Project:       p,
		Agents:        agents,
		Repos:         repos,
		Environments:  envs,
		DeployRecipes: recipes,
		Counts:        counts,
	})
}

// loadProjectCounts returns the PAI-356 footer-bar counts for a
// single project. Knowledge types are listed inline rather than
// pulled from `knowledge.AllTypes()` to avoid an import cycle and
// because the SQL planner inlines the literal IN clause anyway.
func loadProjectCounts(projectID int64) (projectCounts, error) {
	var c projectCounts
	err := db.DB.QueryRow(`
		SELECT
		  COUNT(CASE
		          WHEN status NOT IN ('done','delivered','cancelled')
		           AND type NOT IN ('memory','runbook','external_system','related_project','guideline')
		         THEN 1 END),
		  COUNT(CASE
		          WHEN type IN ('memory','runbook','external_system','related_project','guideline')
		           AND status != 'cancelled'
		         THEN 1 END)
		FROM issues
		WHERE project_id = ? AND deleted_at IS NULL
	`, projectID).Scan(&c.OpenIssues, &c.KnowledgeEntries)
	return c, err
}

// DeleteProject soft-deletes: sets status to 'deleted'. Hidden from UI, restorable via DB.
func DeleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec(
		"UPDATE projects SET status='deleted', updated_at=datetime('now') WHERE id=? AND status != 'deleted'", id,
	)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found or already deleted", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func SuggestProjectKey(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	jsonOK(w, map[string]string{"key": suggestKey(name)})
}

func getProjectByID(id int64) *models.Project {
	var p models.Project
	var custRateHourly, custRateLp *float64
	err := db.DB.QueryRow(`
		SELECT p.id, p.name, p.key, p.description, p.status,
		       p.product_owner, p.customer_label, p.customer_id,
		       COALESCE(c.name, ''),
		       p.created_at, p.updated_at,
		       COUNT(i.id),
		       COALESCE(p.logo_path, ''),
		       COALESCE(MAX(i.updated_at), ''),
		       COUNT(CASE WHEN i.status NOT IN ('done','delivered','cancelled') THEN 1 END),
		       COUNT(CASE WHEN i.status IN ('done','delivered') THEN 1 END),
		       COUNT(CASE WHEN i.status != 'cancelled' THEN 1 END),
		       p.rate_hourly, p.rate_lp,
		       c.rate_hourly, c.rate_lp
		FROM projects p
		LEFT JOIN issues i    ON i.project_id=p.id
		LEFT JOIN customers c ON c.id = p.customer_id
		WHERE p.id=?
		GROUP BY p.id
	`, id).Scan(&p.ID, &p.Name, &p.Key, &p.Description, &p.Status,
		&p.ProductOwner, &p.CustomerLabel, &p.CustomerID, &p.CustomerName,
		&p.CreatedAt, &p.UpdatedAt, &p.IssueCount, &p.LogoPath,
		&p.LastActivity, &p.OpenIssueCount, &p.DoneIssueCount, &p.ActiveIssueCount,
		&p.RateHourly, &p.RateLp,
		&custRateHourly, &custRateLp)
	if err != nil {
		return nil
	}
	applyEffectiveRates(&p, custRateHourly, custRateLp)
	LoadTagsForProject(&p)
	return &p
}

// validKeyRe matches a valid project key: uppercase letters/digits, starts with letter, 3–10 chars.
var validKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{2,9}$`)

// nonAlphaNum strips anything that is not an uppercase letter or digit.
var nonAlphaNum = regexp.MustCompile(`[^A-Z0-9]`)

// validateKey returns a non-empty error string if key is invalid.
func validateKey(key string) string {
	if !validKeyRe.MatchString(key) {
		return "project key must be 3–10 characters, uppercase letters and digits only, starting with a letter (e.g. BPM26)"
	}
	return ""
}

// suggestKey derives a CCC##-style uppercase key from a project name.
// Takes up to 3 leading letters from words, appends last 2 digits of current year.
// "PAIMOS PM 2026" -> "BPM26", "My Cool API" -> "MCA26".
func suggestKey(name string) string {
	year := fmt.Sprintf("%d", time.Now().Year())[2:] // last 2 digits
	words := strings.Fields(name)
	var letters strings.Builder
	for _, w := range words {
		for _, r := range w {
			if unicode.IsLetter(r) {
				letters.WriteRune(unicode.ToUpper(r))
				break
			}
		}
		if letters.Len() >= 3 {
			break
		}
	}
	if letters.Len() == 0 {
		// fallback: first 3 uppercase letters from name
		for _, r := range strings.ToUpper(name) {
			if unicode.IsLetter(r) {
				letters.WriteRune(r)
			}
			if letters.Len() >= 3 {
				break
			}
		}
	}
	prefix := letters.String()
	if len(prefix) == 0 {
		return ""
	}
	return prefix + year
}

// sanitizeKey uppercases, strips whitespace, and removes invalid characters.
func sanitizeKey(key string) string {
	return nonAlphaNum.ReplaceAllString(strings.ToUpper(strings.TrimSpace(key)), "")
}
