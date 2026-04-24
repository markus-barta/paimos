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
type Customer struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	ExternalID       *string  `json:"external_id"`
	ExternalURL      *string  `json:"external_url"`
	ExternalProvider *string  `json:"external_provider"`
	SyncedAt         *string  `json:"synced_at"`
	ContactName      string   `json:"contact_name"`
	ContactEmail     string   `json:"contact_email"`
	Address          string   `json:"address"`
	Country          string   `json:"country"`
	Industry         string   `json:"industry"`
	RateHourly       *float64 `json:"rate_hourly"`
	RateLp           *float64 `json:"rate_lp"`
	Notes            string   `json:"notes"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	// Aggregates filled in by list / detail handlers. Omitted from the
	// response when zero so the JSON stays tight for tests.
	ProjectCount int `json:"project_count,omitempty"`
}
