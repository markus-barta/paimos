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

// PAI-273. Contact (Ansprechpartner) CRUD. The Contact entity replaces
// the inline `customers.contact_name / contact_email` columns; one
// customer holds any number of contacts, exactly one of which is the
// primary at a time. The "exactly one primary" invariant is enforced
// here, in transactional code — a partial-unique index in SQLite is
// awkward to maintain across drivers, and a CHECK constraint can't
// cover the "across rows" predicate.

package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// ── List + create (customer-scoped) ─────────────────────────────────

// ListCustomerContacts handles GET /api/customers/:id/contacts.
// Available to anyone with customer-read; per-contact granular
// permissions are out of scope (see PAI-273 "out of scope").
func ListCustomerContacts(w http.ResponseWriter, r *http.Request) {
	customerID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(`
		SELECT `+contactSelectColumns()+`
		FROM contacts
		WHERE customer_id = ?
		ORDER BY is_primary DESC, name COLLATE NOCASE, id
	`, customerID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.Contact{}
	for rows.Next() {
		c := scanContact(rows)
		if c == nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		out = append(out, *c)
	}
	jsonOK(w, out)
}

type contactCreateBody struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Role      string `json:"role"`
	IsPrimary bool   `json:"is_primary"`
	Notes     string `json:"notes"`
}

// CreateCustomerContact handles POST /api/customers/:id/contacts.
// If the new contact is_primary=true, any existing primary on the same
// customer is demoted in the same transaction (atomic).
func CreateCustomerContact(w http.ResponseWriter, r *http.Request) {
	customerID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !customerExists(customerID) {
		jsonError(w, "customer not found", http.StatusNotFound)
		return
	}
	var body contactCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}

	// Implicit-primary rule: if the customer has no primary yet, the
	// first contact becomes primary regardless of what the caller
	// asked. Saves clients from having to make two calls.
	if !body.IsPrimary {
		var hasPrimary int
		_ = db.DB.QueryRow(
			"SELECT COUNT(*) FROM contacts WHERE customer_id=? AND is_primary=1", customerID,
		).Scan(&hasPrimary)
		if hasPrimary == 0 {
			body.IsPrimary = true
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "tx begin failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if body.IsPrimary {
		if _, err := tx.Exec(
			`UPDATE contacts SET is_primary=0 WHERE customer_id=? AND is_primary=1`,
			customerID,
		); err != nil {
			jsonError(w, "demote previous primary failed", http.StatusInternalServerError)
			return
		}
	}

	res, err := tx.Exec(`
		INSERT INTO contacts(customer_id, name, email, phone, role, is_primary, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, customerID, body.Name, body.Email, body.Phone, body.Role, boolToInt(body.IsPrimary), body.Notes)
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		jsonError(w, "tx commit failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, getContactByID(id))
}

// ── Single-contact CRUD ─────────────────────────────────────────────

func GetContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	c := getContactByID(id)
	if c == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, c)
}

type contactUpdateBody struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
	Phone *string `json:"phone"`
	Role  *string `json:"role"`
	Notes *string `json:"notes"`
	// IsPrimary is intentionally NOT modifiable here — promotion uses
	// the dedicated /promote-primary endpoint so the demote-then-promote
	// is always atomic. A PUT that flipped is_primary in isolation could
	// leave the customer with zero or two primaries on the wire.
}

func UpdateContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body contactUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		UPDATE contacts SET
			name       = COALESCE(?, name),
			email      = COALESCE(?, email),
			phone      = COALESCE(?, phone),
			role       = COALESCE(?, role),
			notes      = COALESCE(?, notes),
			updated_at = ?
		WHERE id = ?
	`, body.Name, body.Email, body.Phone, body.Role, body.Notes, now, id)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, getContactByID(id))
}

// DeleteContact removes a contact. If the deleted contact was the
// primary, the most-recently-updated remaining contact (if any) is
// promoted to primary in the same transaction so the customer never
// ends up in a "had a primary, now has none even though contacts
// exist" state.
func DeleteContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "tx begin failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var customerID int64
	var wasPrimary int
	if err := tx.QueryRow(
		"SELECT customer_id, is_primary FROM contacts WHERE id=?", id,
	).Scan(&customerID, &wasPrimary); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		jsonError(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM contacts WHERE id=?", id); err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if wasPrimary == 1 {
		// Promote the most-recently-updated remaining contact, if any.
		_, _ = tx.Exec(`
			UPDATE contacts SET is_primary=1
			WHERE id = (
				SELECT id FROM contacts
				WHERE customer_id=? ORDER BY updated_at DESC, id DESC LIMIT 1
			)
		`, customerID)
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "tx commit failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PromoteContactPrimary handles POST /api/contacts/:id/promote-primary.
// Atomic flip — the previous primary (if any) is demoted in the same
// transaction so the wire never sees two primaries.
func PromoteContactPrimary(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "tx begin failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var customerID int64
	if err := tx.QueryRow("SELECT customer_id FROM contacts WHERE id=?", id).Scan(&customerID); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		jsonError(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(
		`UPDATE contacts SET is_primary=0 WHERE customer_id=? AND is_primary=1 AND id<>?`,
		customerID, id,
	); err != nil {
		jsonError(w, "demote previous primary failed", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(
		`UPDATE contacts SET is_primary=1 WHERE id=?`, id,
	); err != nil {
		jsonError(w, "promote failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "tx commit failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, getContactByID(id))
}

// ── helpers ─────────────────────────────────────────────────────────

func contactSelectColumns() string {
	return `id, customer_id, name, email, phone, role, is_primary, notes,
		external_id, external_provider, external_url, synced_at,
		created_at, updated_at`
}

func scanContact(s rowScanner) *models.Contact {
	var c models.Contact
	var primary int
	err := s.Scan(
		&c.ID, &c.CustomerID, &c.Name, &c.Email, &c.Phone, &c.Role,
		&primary, &c.Notes,
		&c.ExternalID, &c.ExternalProvider, &c.ExternalURL, &c.SyncedAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	c.IsPrimary = primary != 0
	return &c
}

func getContactByID(id int64) *models.Contact {
	row := db.DB.QueryRow(`SELECT `+contactSelectColumns()+` FROM contacts WHERE id=?`, id)
	return scanContact(row)
}

func customerExists(id int64) bool {
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM customers WHERE id=?`, id).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
