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
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// PAI-53. CRM-agnostic customer CRUD. The HTTP layer enforces the same
// "external_id and external_provider must agree" invariant the DB triggers
// enforce as a defence in depth — surfacing a clean 400 before the
// constraint trips.
//
// Provider-driven import / sync endpoints live in handlers/crm (PAI-103);
// this file is the manual-customer path that the no-CRM audience uses
// directly and that the plugin layer also calls into after `ImportRef`.

func ListCustomers(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(`
		SELECT c.id, c.name, c.external_id, c.external_url, c.external_provider,
		       c.synced_at, c.contact_name, c.contact_email, c.address, c.country,
		       c.industry, c.rate_hourly, c.rate_lp, c.notes,
		       c.created_at, c.updated_at,
		       COUNT(p.id)
		FROM customers c
		LEFT JOIN projects p ON p.customer_id = c.id AND p.status != 'deleted'
		GROUP BY c.id
		ORDER BY c.name COLLATE NOCASE
	`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.Customer{}
	for rows.Next() {
		c := scanCustomer(rows)
		if c == nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		out = append(out, *c)
	}
	jsonOK(w, out)
}

func GetCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	c := getCustomerByID(id)
	if c == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, c)
}

// customerCreateBody mirrors the column set 1:1. External-CRM fields
// are optional; manual customers leave all three nil.
type customerCreateBody struct {
	Name             string   `json:"name"`
	ExternalID       *string  `json:"external_id"`
	ExternalURL      *string  `json:"external_url"`
	ExternalProvider *string  `json:"external_provider"`
	ContactName      string   `json:"contact_name"`
	ContactEmail     string   `json:"contact_email"`
	Address          string   `json:"address"`
	Country          string   `json:"country"`
	Industry         string   `json:"industry"`
	RateHourly       *float64 `json:"rate_hourly"`
	RateLp           *float64 `json:"rate_lp"`
	Notes            string   `json:"notes"`
}

func CreateCustomer(w http.ResponseWriter, r *http.Request) {
	var body customerCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}
	if !externalPairValid(body.ExternalID, body.ExternalProvider) {
		jsonError(w, "external_id and external_provider must be both set or both null", http.StatusBadRequest)
		return
	}

	res, err := db.DB.Exec(`
		INSERT INTO customers(
			name, external_id, external_url, external_provider, synced_at,
			contact_name, contact_email, address, country, industry,
			rate_hourly, rate_lp, notes
		) VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		body.Name, body.ExternalID, body.ExternalURL, body.ExternalProvider,
		body.ContactName, body.ContactEmail, body.Address, body.Country, body.Industry,
		body.RateHourly, body.RateLp, body.Notes,
	)
	if handleDBError(w, err, "customer") {
		return
	}
	id, _ := res.LastInsertId()
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, getCustomerByID(id))
}

// customerUpdateBody is the same shape but every field is a pointer so
// the COALESCE pattern ("only fields you pass are written") matches the
// existing issue / project handlers.
type customerUpdateBody struct {
	Name             *string  `json:"name"`
	ExternalID       *string  `json:"external_id"`
	ExternalURL      *string  `json:"external_url"`
	ExternalProvider *string  `json:"external_provider"`
	ContactName      *string  `json:"contact_name"`
	ContactEmail     *string  `json:"contact_email"`
	Address          *string  `json:"address"`
	Country          *string  `json:"country"`
	Industry         *string  `json:"industry"`
	RateHourly       *float64 `json:"rate_hourly"`
	RateLp           *float64 `json:"rate_lp"`
	Notes            *string  `json:"notes"`
}

func UpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body customerUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	// External-pair validation: only enforced when at least one of the two
	// is being explicitly written. Validate against the resulting value
	// (incoming vs current) so a partial update can't break the invariant.
	if body.ExternalID != nil || body.ExternalProvider != nil {
		current := getCustomerByID(id)
		if current == nil {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		next := externalPairAfterUpdate(current, body.ExternalID, body.ExternalProvider)
		if !externalPairValid(next.id, next.provider) {
			jsonError(w, "external_id and external_provider must be both set or both null", http.StatusBadRequest)
			return
		}
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = db.DB.Exec(`
		UPDATE customers SET
			name              = COALESCE(?, name),
			external_id       = CASE WHEN ? IS NOT NULL THEN ? ELSE external_id END,
			external_url      = CASE WHEN ? IS NOT NULL THEN ? ELSE external_url END,
			external_provider = CASE WHEN ? IS NOT NULL THEN ? ELSE external_provider END,
			contact_name      = COALESCE(?, contact_name),
			contact_email     = COALESCE(?, contact_email),
			address           = COALESCE(?, address),
			country           = COALESCE(?, country),
			industry          = COALESCE(?, industry),
			rate_hourly       = CASE WHEN ? IS NOT NULL THEN ? ELSE rate_hourly END,
			rate_lp           = CASE WHEN ? IS NOT NULL THEN ? ELSE rate_lp END,
			notes             = COALESCE(?, notes),
			updated_at        = ?
		WHERE id=?
	`,
		body.Name,
		body.ExternalID, body.ExternalID,
		body.ExternalURL, body.ExternalURL,
		body.ExternalProvider, body.ExternalProvider,
		body.ContactName, body.ContactEmail, body.Address, body.Country, body.Industry,
		body.RateHourly, body.RateHourly,
		body.RateLp, body.RateLp,
		body.Notes, now, id,
	)
	if handleDBError(w, err, "customer") {
		return
	}
	c := getCustomerByID(id)
	if c == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, c)
}

func DeleteCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Refuse to delete a customer with assigned projects — the FK is
	// nullable so cascading would silently strand projects. The 409
	// matches the convention used elsewhere ("conflict, fix it first").
	var n int
	if err := db.DB.QueryRow(
		"SELECT COUNT(*) FROM projects WHERE customer_id=? AND status != 'deleted'", id,
	).Scan(&n); err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	if n > 0 {
		jsonError(w, "customer has assigned projects; reassign or archive them first", http.StatusConflict)
		return
	}
	res, err := db.DB.Exec("DELETE FROM customers WHERE id=?", id)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ─────────────────────────────────────────────────────────

// rowScanner abstracts *sql.Row and *sql.Rows so scanCustomer can serve
// both list and single-get handlers.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCustomer(s rowScanner) *models.Customer {
	var c models.Customer
	err := s.Scan(
		&c.ID, &c.Name, &c.ExternalID, &c.ExternalURL, &c.ExternalProvider,
		&c.SyncedAt, &c.ContactName, &c.ContactEmail, &c.Address, &c.Country,
		&c.Industry, &c.RateHourly, &c.RateLp, &c.Notes,
		&c.CreatedAt, &c.UpdatedAt,
		&c.ProjectCount,
	)
	if err != nil {
		return nil
	}
	return &c
}

func getCustomerByID(id int64) *models.Customer {
	row := db.DB.QueryRow(`
		SELECT c.id, c.name, c.external_id, c.external_url, c.external_provider,
		       c.synced_at, c.contact_name, c.contact_email, c.address, c.country,
		       c.industry, c.rate_hourly, c.rate_lp, c.notes,
		       c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM projects p
		         WHERE p.customer_id = c.id AND p.status != 'deleted')
		FROM customers c WHERE c.id=?
	`, id)
	return scanCustomer(row)
}

// externalPairValid: both must be set or both must be null.
func externalPairValid(id, provider *string) bool {
	return (id == nil) == (provider == nil)
}

// externalPair represents the post-update state of the (id, provider) pair
// after partial-update merging.
type externalPair struct {
	id, provider *string
}

// externalPairAfterUpdate returns what the (external_id, external_provider)
// pair will look like after the update is applied. The body fields are
// pointers; nil = leave alone, non-nil = write (including pointer-to-empty
// for "clear" — but we treat empty string as a NULL signal too, so the
// caller can `"external_id": ""` to detach from a CRM).
func externalPairAfterUpdate(curr *models.Customer, bodyID, bodyProv *string) externalPair {
	out := externalPair{id: curr.ExternalID, provider: curr.ExternalProvider}
	if bodyID != nil {
		v := *bodyID
		if v == "" {
			out.id = nil
		} else {
			out.id = &v
		}
	}
	if bodyProv != nil {
		v := *bodyProv
		if v == "" {
			out.provider = nil
		} else {
			out.provider = &v
		}
	}
	return out
}
