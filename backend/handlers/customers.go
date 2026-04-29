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
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
//
// PAI-273 expansion:
//   - Customer carries new metadata fields (website / VAT / employees /
//     revenue / phone / billing & visit address quartets); CRUD handles
//     them in the same shape as the existing fields.
//   - The legacy ContactName / ContactEmail / Address / Country columns
//     stay alive for one release as a read-compat shim. GetCustomer /
//     ListCustomers populate them from the primary `contacts` row when
//     one exists, falling through to the legacy column otherwise. The
//     write path on PUT mirrors any legacy-field change back into the
//     primary contact (creating one when missing) so callers stuck on
//     the v1 shape keep working.
//   - Contact CRUD lives in handlers/contacts.go.

func ListCustomers(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(customerListSelect())
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
		applyPrimaryContactCompat(c)
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
	// PAI-273 read-compat: a v1 caller can still send these; we pass
	// them through to the customers row AND seed a primary contact
	// downstream of the insert so future GETs read from the new model.
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	Address      string `json:"address"`
	Country      string `json:"country"`
	Industry     string `json:"industry"`
	// PAI-273 metadata expansion.
	Website                string   `json:"website"`
	Domain                 string   `json:"domain"`
	VATID                  string   `json:"vat_id"`
	EmployeeCount          *int64   `json:"employee_count"`
	AnnualRevenueCents     *int64   `json:"annual_revenue_cents"`
	Description            string   `json:"description"`
	Phone                  string   `json:"phone"`
	BillingAddressStreet   string   `json:"billing_address_street"`
	BillingAddressCity     string   `json:"billing_address_city"`
	BillingAddressZip      string   `json:"billing_address_zip"`
	BillingAddressCountry  string   `json:"billing_address_country"`
	VisitAddressStreet     string   `json:"visit_address_street"`
	VisitAddressZip        string   `json:"visit_address_zip"`
	RateHourly             *float64 `json:"rate_hourly"`
	RateLp                 *float64 `json:"rate_lp"`
	Notes                  string   `json:"notes"`
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
			website, domain, vat_id, employee_count, annual_revenue_cents,
			description, phone,
			billing_address_street, billing_address_city, billing_address_zip, billing_address_country,
			visit_address_street, visit_address_zip,
			rate_hourly, rate_lp, notes
		) VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?,
		          ?, ?, ?, ?, ?,
		          ?, ?,
		          ?, ?, ?, ?,
		          ?, ?,
		          ?, ?, ?)
	`,
		body.Name, body.ExternalID, body.ExternalURL, body.ExternalProvider,
		body.ContactName, body.ContactEmail, body.Address, body.Country, body.Industry,
		body.Website, body.Domain, body.VATID, body.EmployeeCount, body.AnnualRevenueCents,
		body.Description, body.Phone,
		body.BillingAddressStreet, body.BillingAddressCity, body.BillingAddressZip, body.BillingAddressCountry,
		body.VisitAddressStreet, body.VisitAddressZip,
		body.RateHourly, body.RateLp, body.Notes,
	)
	if handleDBError(w, err, "customer") {
		return
	}
	id, _ := res.LastInsertId()

	// PAI-273 write-compat: if the v1 caller seeded inline contact
	// fields, mirror them into a primary `contacts` row so future GETs
	// read from the new model.
	if strings.TrimSpace(body.ContactName) != "" || strings.TrimSpace(body.ContactEmail) != "" {
		_, _ = db.DB.Exec(`
			INSERT INTO contacts(customer_id, name, email, is_primary)
			VALUES (?, ?, ?, 1)
		`, id, body.ContactName, body.ContactEmail)
	}

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
	Website                *string  `json:"website"`
	Domain                 *string  `json:"domain"`
	VATID                  *string  `json:"vat_id"`
	EmployeeCount          *int64   `json:"employee_count"`
	AnnualRevenueCents     *int64   `json:"annual_revenue_cents"`
	Description            *string  `json:"description"`
	Phone                  *string  `json:"phone"`
	BillingAddressStreet   *string  `json:"billing_address_street"`
	BillingAddressCity     *string  `json:"billing_address_city"`
	BillingAddressZip      *string  `json:"billing_address_zip"`
	BillingAddressCountry  *string  `json:"billing_address_country"`
	VisitAddressStreet     *string  `json:"visit_address_street"`
	VisitAddressZip        *string  `json:"visit_address_zip"`
	RateHourly             *float64 `json:"rate_hourly"`
	RateLp                 *float64 `json:"rate_lp"`
	Notes                  *string  `json:"notes"`
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
			website                 = COALESCE(?, website),
			domain                  = COALESCE(?, domain),
			vat_id                  = COALESCE(?, vat_id),
			employee_count          = CASE WHEN ? IS NOT NULL THEN ? ELSE employee_count END,
			annual_revenue_cents    = CASE WHEN ? IS NOT NULL THEN ? ELSE annual_revenue_cents END,
			description             = COALESCE(?, description),
			phone                   = COALESCE(?, phone),
			billing_address_street  = COALESCE(?, billing_address_street),
			billing_address_city    = COALESCE(?, billing_address_city),
			billing_address_zip     = COALESCE(?, billing_address_zip),
			billing_address_country = COALESCE(?, billing_address_country),
			visit_address_street    = COALESCE(?, visit_address_street),
			visit_address_zip       = COALESCE(?, visit_address_zip),
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
		body.Website, body.Domain, body.VATID,
		body.EmployeeCount, body.EmployeeCount,
		body.AnnualRevenueCents, body.AnnualRevenueCents,
		body.Description, body.Phone,
		body.BillingAddressStreet, body.BillingAddressCity, body.BillingAddressZip, body.BillingAddressCountry,
		body.VisitAddressStreet, body.VisitAddressZip,
		body.RateHourly, body.RateHourly,
		body.RateLp, body.RateLp,
		body.Notes, now, id,
	)
	if handleDBError(w, err, "customer") {
		return
	}

	// PAI-273 write-compat: any v1 caller writing contact_name /
	// contact_email / address (the city portion) routes to the primary
	// contact. Address & country don't map cleanly to the new
	// (street/city/zip/country) shape, so we leave them on the customer
	// row only — those callers are pre-PAI-273 and won't break.
	if body.ContactName != nil || body.ContactEmail != nil {
		mirrorLegacyContactToPrimary(id, body.ContactName, body.ContactEmail)
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

// customerListSelect / customerSelectByID: kept as functions so the column
// list lives in one place. The trailing aggregate column is `project_count`
// in both.
func customerListSelect() string {
	return `
		SELECT ` + customerSelectColumns() + `,
		       COUNT(p.id)
		FROM customers c
		LEFT JOIN projects p ON p.customer_id = c.id AND p.status != 'deleted'
		GROUP BY c.id
		ORDER BY c.name COLLATE NOCASE
	`
}

// customerSelectColumns is the canonical column list for the customers
// SELECT shape — kept in one place so adding a column doesn't require
// hunting through three queries.
func customerSelectColumns() string {
	return `c.id, c.name, c.external_id, c.external_url, c.external_provider,
		c.synced_at, c.contact_name, c.contact_email, c.address, c.country,
		c.industry,
		c.website, c.domain, c.vat_id, c.employee_count, c.annual_revenue_cents,
		c.description, c.phone,
		c.billing_address_street, c.billing_address_city,
		c.billing_address_zip, c.billing_address_country,
		c.visit_address_street, c.visit_address_zip,
		c.rate_hourly, c.rate_lp, c.notes,
		c.created_at, c.updated_at`
}

func scanCustomer(s rowScanner) *models.Customer {
	var c models.Customer
	err := s.Scan(
		&c.ID, &c.Name, &c.ExternalID, &c.ExternalURL, &c.ExternalProvider,
		&c.SyncedAt, &c.ContactName, &c.ContactEmail, &c.Address, &c.Country,
		&c.Industry,
		&c.Website, &c.Domain, &c.VATID, &c.EmployeeCount, &c.AnnualRevenueCents,
		&c.Description, &c.Phone,
		&c.BillingAddressStreet, &c.BillingAddressCity,
		&c.BillingAddressZip, &c.BillingAddressCountry,
		&c.VisitAddressStreet, &c.VisitAddressZip,
		&c.RateHourly, &c.RateLp, &c.Notes,
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
		SELECT `+customerSelectColumns()+`,
		       (SELECT COUNT(*) FROM projects p
		         WHERE p.customer_id = c.id AND p.status != 'deleted')
		FROM customers c WHERE c.id=?
	`, id)
	c := scanCustomer(row)
	if c == nil {
		return nil
	}
	applyPrimaryContactCompat(c)
	return c
}

// applyPrimaryContactCompat: PAI-273 read-fallback. When a primary
// contact exists, surface its name/email through the legacy
// ContactName/ContactEmail fields so v1 API consumers keep working
// while the frontend transitions to fetching /customers/:id/contacts.
//
// We deliberately do NOT touch Address / Country here — those are
// distinct concepts from the contact's own address (which doesn't even
// exist as a single field on the Contact entity). The customer's
// visit-address quartet is the canonical answer; the legacy address
// column is whatever the caller most recently wrote.
func applyPrimaryContactCompat(c *models.Customer) {
	if c == nil {
		return
	}
	var name, email string
	err := db.DB.QueryRow(`
		SELECT name, email FROM contacts
		WHERE customer_id = ? AND is_primary = 1
		LIMIT 1
	`, c.ID).Scan(&name, &email)
	if err == sql.ErrNoRows {
		return // fall through to legacy column values already on c
	}
	if err != nil {
		return // soft fail — legacy values are still correct
	}
	if name != "" {
		c.ContactName = name
	}
	if email != "" {
		c.ContactEmail = email
	}
}

// mirrorLegacyContactToPrimary: PAI-273 write-fallback. v1 callers
// PUT-ing contact_name / contact_email get those values mirrored into
// the primary contact (creating one if none exists). Idempotent.
func mirrorLegacyContactToPrimary(customerID int64, name, email *string) {
	var primaryID int64
	err := db.DB.QueryRow(`
		SELECT id FROM contacts WHERE customer_id=? AND is_primary=1 LIMIT 1
	`, customerID).Scan(&primaryID)
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err == sql.ErrNoRows {
		var n, e string
		if name != nil {
			n = *name
		}
		if email != nil {
			e = *email
		}
		if strings.TrimSpace(n) == "" && strings.TrimSpace(e) == "" {
			return
		}
		_, _ = db.DB.Exec(`
			INSERT INTO contacts(customer_id, name, email, is_primary, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?)
		`, customerID, n, e, now, now)
		return
	}
	if err != nil {
		return
	}
	_, _ = db.DB.Exec(`
		UPDATE contacts SET
			name       = COALESCE(?, name),
			email      = COALESCE(?, email),
			updated_at = ?
		WHERE id = ?
	`, name, email, now, primaryID)
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
