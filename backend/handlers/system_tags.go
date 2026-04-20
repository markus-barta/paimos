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
	"log"
	"net/http"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// EnsureAtRiskTag creates the "At Risk" system tag and default rule if they don't exist.
// Called on startup.
func EnsureAtRiskTag() {
	var tagID int64
	err := db.DB.QueryRow(`SELECT id FROM tags WHERE name='At Risk' AND system=1`).Scan(&tagID)
	if err != nil {
		// Create the tag
		res, err := db.DB.Exec(`INSERT INTO tags(name, color, description, system) VALUES('At Risk', 'orange', 'Auto: booked hours near estimate', 1)`)
		if err != nil {
			return
		}
		tagID, _ = res.LastInsertId()
	}
	// Ensure default rule exists
	var ruleCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM system_tag_rules WHERE tag_id=?`, tagID).Scan(&ruleCount); err != nil {
		log.Printf("scan error: %v", err)
		return
	}
	if ruleCount == 0 {
		if _, err := db.DB.Exec(`INSERT INTO system_tag_rules(tag_id, condition_type, threshold, excluded_statuses)
			VALUES(?, 'budget_threshold', 0.8, 'qa,done,accepted,invoiced,cancelled')`, tagID); err != nil {
			log.Printf("EnsureAtRiskTag: insert default rule: %v", err)
		}
	}

	// Batch re-evaluate on startup to catch up any issues that were modified
	// before system tags were deployed or between deploys
	go batchReEvaluateSystemTags()
}

// ListSystemTagRules returns all system tag rules.
// GET /api/system-tag-rules
func ListSystemTagRules(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(`
		SELECT r.id, r.tag_id, r.condition_type, r.threshold, r.excluded_statuses, r.created_at,
		       t.name, t.color
		FROM system_tag_rules r
		JOIN tags t ON t.id = r.tag_id
		ORDER BY r.id
	`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RuleWithTag struct {
		models.SystemTagRule
		TagName  string `json:"tag_name"`
		TagColor string `json:"tag_color"`
	}
	rules := []RuleWithTag{}
	for rows.Next() {
		var r RuleWithTag
		rows.Scan(&r.ID, &r.TagID, &r.ConditionType, &r.Threshold, &r.ExcludedStatuses, &r.CreatedAt,
			&r.TagName, &r.TagColor)
		rules = append(rules, r)
	}
	jsonOK(w, rules)
}

// UpdateSystemTagRule updates a rule's threshold and excluded statuses.
// PUT /api/system-tag-rules/:id
func UpdateSystemTagRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Threshold        *float64 `json:"threshold"`
		ExcludedStatuses *string  `json:"excluded_statuses"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Update the first (and typically only) rule
	_, err := db.DB.Exec(`
		UPDATE system_tag_rules SET
			threshold = COALESCE(?, threshold),
			excluded_statuses = COALESCE(?, excluded_statuses)
		WHERE id = (SELECT MIN(id) FROM system_tag_rules)
	`, body.Threshold, body.ExcludedStatuses)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	// Batch re-evaluate all issues with estimates
	go batchReEvaluateSystemTags()

	jsonOK(w, map[string]string{"status": "ok"})
}

// batchReEvaluateSystemTags re-evaluates system tags for all issues that have estimates.
func batchReEvaluateSystemTags() {
	rows, err := db.DB.Query(`
		SELECT id FROM issues
		WHERE (estimate_hours IS NOT NULL OR estimate_lp IS NOT NULL)
		  AND type IN ('ticket','task')
		  AND deleted_at IS NULL
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	for _, id := range ids {
		EvaluateSystemTags(id)
	}
	fmt.Printf("system tags: batch re-evaluated %d issues\n", len(ids))
}

// EvaluateSystemTags checks all budget_threshold rules and applies/removes system tags.
// Called after issue updates and time entry changes.
// Designed to be safe to call from any context — never panics, silently handles errors.
func EvaluateSystemTags(issueID int64) {
	if issueID <= 0 {
		return
	}

	// Verify the issue exists and is live before doing anything
	var exists int
	if err := db.DB.QueryRow(`SELECT 1 FROM issues WHERE id=? AND deleted_at IS NULL`, issueID).Scan(&exists); err != nil {
		return // issue doesn't exist, or is in the Trash — nothing to evaluate
	}

	// Load all budget_threshold rules
	rows, err := db.DB.Query(`SELECT tag_id, threshold, excluded_statuses FROM system_tag_rules WHERE condition_type='budget_threshold'`)
	if err != nil {
		return
	}
	defer rows.Close()

	// Collect rules first to avoid nested query issues
	type rule struct {
		tagID     int64
		threshold float64
		excluded  map[string]bool
	}
	var rules []rule
	for rows.Next() {
		var r rule
		var excludedStr string
		if err := rows.Scan(&r.tagID, &r.threshold, &excludedStr); err != nil {
			continue
		}
		// Validate threshold
		if r.threshold <= 0 || r.threshold > 10 {
			continue // nonsensical threshold — skip
		}
		r.excluded = map[string]bool{}
		for _, s := range strings.Split(excludedStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				r.excluded[s] = true
			}
		}
		rules = append(rules, r)
	}
	rows.Close() // close before further queries

	if len(rules) == 0 {
		return
	}

	// Get issue data (single query, reused for all rules)
	var status string
	var bookedHours float64
	var estimateHours, estimateLp, rateHourly, rateLp *float64
	var costUnit string
	var projectID *int64

	err = db.DB.QueryRow(`
		SELECT i.status,
			COALESCE((SELECT SUM(
				CASE WHEN te.override IS NOT NULL THEN te.override
				     WHEN te.stopped_at IS NOT NULL THEN
				         CASE WHEN julianday(te.stopped_at) > julianday(te.started_at)
				              THEN (julianday(te.stopped_at) - julianday(te.started_at)) * 24
				              ELSE 0 END
				     ELSE 0 END
			) FROM time_entries te WHERE te.issue_id = i.id), 0),
			i.estimate_hours, i.estimate_lp, i.rate_hourly, i.rate_lp,
			COALESCE(i.cost_unit, ''), i.project_id
		FROM issues i WHERE i.id = ?
	`, issueID).Scan(&status, &bookedHours, &estimateHours, &estimateLp, &rateHourly, &rateLp, &costUnit, &projectID)
	if err != nil {
		return
	}

	// Guard: negative booked hours shouldn't happen but treat as zero
	if bookedHours < 0 {
		bookedHours = 0
	}

	for _, r := range rules {
		// Skip excluded statuses
		if r.excluded[status] {
			removeSystemTag(issueID, r.tagID)
			continue
		}

		// Compute budget hours via rate cascade
		budgetHours := computeBudgetHours(estimateHours, estimateLp, rateHourly, rateLp, costUnit, projectID)
		if budgetHours == nil || *budgetHours <= 0 {
			removeSystemTag(issueID, r.tagID)
			continue
		}

		// Check threshold
		if bookedHours >= *budgetHours*r.threshold {
			addSystemTag(issueID, r.tagID)
		} else {
			removeSystemTag(issueID, r.tagID)
		}
	}
}

// computeBudgetHours resolves the budget in hours using the rate cascade.
func computeBudgetHours(estHours, estLp, issueRateH, issueRateLP *float64, costUnit string, projectID *int64) *float64 {
	// If estimate_hours is set directly, use that
	if estHours != nil && *estHours > 0 {
		return estHours
	}
	// If estimate_lp is set, convert via rates
	if estLp == nil || *estLp <= 0 {
		return nil
	}

	// Use shared cascade helper via a temporary issue struct
	tmp := models.Issue{
		RateHourly: issueRateH,
		RateLp:     issueRateLP,
		CostUnit:   costUnit,
		ProjectID:  projectID,
	}
	ResolveRateCascade(&tmp)

	// Convert LP to hours
	if tmp.RateHourly == nil || tmp.RateLp == nil || *tmp.RateHourly == 0 {
		return nil
	}
	hours := *estLp * (*tmp.RateLp / *tmp.RateHourly)
	return &hours
}

// ResolveRateCascade fills in missing rate_hourly / rate_lp on an issue
// using the cascade: issue → cost_unit → project.
func ResolveRateCascade(issue *models.Issue) {
	rh, rl := issue.RateHourly, issue.RateLp
	if rh != nil && rl != nil {
		return // both already set
	}
	// Try cost unit rates
	if issue.CostUnit != "" && issue.ProjectID != nil {
		var cuRH, cuRL *float64
		db.DB.QueryRow(`
			SELECT rate_hourly, rate_lp FROM issues
			WHERE project_id = ? AND type = 'cost_unit' AND title = ?
		`, *issue.ProjectID, issue.CostUnit).Scan(&cuRH, &cuRL)
		if rh == nil {
			rh = cuRH
		}
		if rl == nil {
			rl = cuRL
		}
	}
	// Try project rates
	if (rh == nil || rl == nil) && issue.ProjectID != nil {
		var prH, prL *float64
		db.DB.QueryRow(`SELECT rate_hourly, rate_lp FROM projects WHERE id = ?`, *issue.ProjectID).Scan(&prH, &prL)
		if rh == nil {
			rh = prH
		}
		if rl == nil {
			rl = prL
		}
	}
	issue.RateHourly = rh
	issue.RateLp = rl
}

func addSystemTag(issueID, tagID int64) {
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id, tag_id) VALUES(?, ?)`, issueID, tagID); err != nil {
		log.Printf("addSystemTag: issue=%d tag=%d: %v", issueID, tagID, err)
	}
}

func removeSystemTag(issueID, tagID int64) {
	if _, err := db.DB.Exec(`DELETE FROM issue_tags WHERE issue_id = ? AND tag_id = ?`, issueID, tagID); err != nil {
		log.Printf("removeSystemTag: issue=%d tag=%d: %v", issueID, tagID, err)
	}
}
