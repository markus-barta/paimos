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

	rows, err := db.DB.Query(`
		SELECT p.id, p.name, p.key, p.description, p.status,
		       p.product_owner, p.customer_label, p.customer_id,
		       COALESCE(c.name, ''),
		       p.created_at, p.updated_at,
		       COUNT(i.id) as issue_count,
		       COALESCE(p.logo_path, ''),
		       COALESCE(MAX(i.updated_at), '') as last_activity,
		       COUNT(CASE WHEN i.status NOT IN ('done','delivered','cancelled') THEN 1 END) as open_issue_count,
		       COUNT(CASE WHEN i.status IN ('done','delivered') THEN 1 END) as done_issue_count,
		       COUNT(CASE WHEN i.status != 'cancelled' THEN 1 END) as active_issue_count,
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
		Name          string `json:"name"`
		Key           string `json:"key"`
		Description   string `json:"description"`
		ProductOwner  *int64 `json:"product_owner"`
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
	jsonOK(w, p)
}

func UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Name          *string  `json:"name"`
		Key           *string  `json:"key"`
		Description   *string  `json:"description"`
		Status        *string  `json:"status"`
		ProductOwner  *int64   `json:"product_owner"`
		CustomerLabel *string  `json:"customer_label"`
		// CustomerID is the FK. Pass `null` to detach (the CASE WHEN below
		// preserves the current value when omitted, writes NULL when
		// explicitly null in the request body).
		CustomerID    *int64   `json:"customer_id"`
		ClearCustomer bool     `json:"clear_customer"`
		RateHourly    *float64 `json:"rate_hourly"`
		RateLp        *float64 `json:"rate_lp"`
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

	p := getProjectByID(id)
	if p == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, p)
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
