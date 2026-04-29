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

package models

// Customer is the PAIMOS-native customer record.
//
// CRM-agnostic: ExternalID / ExternalURL / ExternalProvider are all
// nullable. NULL across all three = a manually-managed customer (no
// external CRM linked). External CRM sync lives behind the plugin layer
// (see backend/handlers/crm); this model carries no provider-specific
// fields.
//
// PAI-273: ContactName / ContactEmail / Address / Country are kept here
// for one release as a read-compat shim — the canonical contact lives in
// the `contacts` table now (one row per Ansprechpartner). GetCustomer
// populates the legacy fields from the primary Contact when present, so
// existing API consumers keep working until the read-fallback hits zero
// in prod logs.
type Customer struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	ExternalID       *string  `json:"external_id"`
	ExternalURL      *string  `json:"external_url"`
	ExternalProvider *string  `json:"external_provider"`
	SyncedAt         *string  `json:"synced_at"`
	// Legacy single-contact fields — see PAI-273 read/write compat in
	// handlers/customers.go. Removed in a follow-up after prod logs
	// confirm zero fallback hits.
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	Address      string `json:"address"`
	Country      string `json:"country"`
	Industry     string `json:"industry"`
	// PAI-273 metadata expansion. All nullable / empty-default; the
	// frontend About card hides any row whose value is empty so
	// adding a field later is purely additive.
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
	CreatedAt              string   `json:"created_at"`
	UpdatedAt              string   `json:"updated_at"`
	// Aggregates filled in by list / detail handlers. Omitted from the
	// response when zero so the JSON stays tight for tests.
	ProjectCount int `json:"project_count,omitempty"`
}

// Contact is one Ansprechpartner attached to a Customer (PAI-273). A
// customer can hold any number of contacts; exactly one is_primary at a
// time — enforced at the application layer because partial-unique
// indexes on a boolean column don't fit cleanly under SQLite (different
// drivers normalize 0/1/true/false inconsistently).
//
// External-CRM fields let HubSpot Contact sync upsert by
// (external_provider, external_id) without a separate mapping table.
type Contact struct {
	ID               int64   `json:"id"`
	CustomerID       int64   `json:"customer_id"`
	Name             string  `json:"name"`
	Email            string  `json:"email"`
	Phone            string  `json:"phone"`
	Role             string  `json:"role"`
	IsPrimary        bool    `json:"is_primary"`
	Notes            string  `json:"notes"`
	ExternalID       *string `json:"external_id"`
	ExternalProvider *string `json:"external_provider"`
	ExternalURL      *string `json:"external_url"`
	SyncedAt         *string `json:"synced_at"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}
