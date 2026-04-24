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

type Project struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Key          string `json:"key"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	ProductOwner *int64 `json:"product_owner"`
	// CustomerLabel is the freeform legacy customer label (PMO26 era).
	// Kept for backward compat; new code should use CustomerID (FK).
	CustomerLabel string `json:"customer_label"`
	// CustomerID is the FK to customers.id (PAI-54). Nullable: existing
	// projects from before PAI-28 are unassigned.
	CustomerID *int64 `json:"customer_id"`
	// CustomerName is the linked customer's display name, populated by
	// list / detail handlers when CustomerID is set. Omitted when nil so
	// the JSON stays tight.
	CustomerName string `json:"customer_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	IssueCount       int    `json:"issue_count,omitempty"`
	LogoPath         string `json:"logo_path"`
	LastActivity     string `json:"last_activity"`
	OpenIssueCount   int    `json:"open_issue_count"`
	DoneIssueCount   int    `json:"done_issue_count"`
	ActiveIssueCount int    `json:"active_issue_count"`
	Tags             []Tag    `json:"tags"`
	// RateHourly / RateLp are the project-level overrides (NULL = inherit).
	RateHourly *float64 `json:"rate_hourly"`
	RateLp     *float64 `json:"rate_lp"`
	// EffectiveRateHourly / EffectiveRateLp are the values clients should
	// quote: the project override when set, else the linked customer's
	// rate, else nil. RateInherited is true when the effective value comes
	// from the customer (PAI-54).
	EffectiveRateHourly *float64 `json:"effective_rate_hourly"`
	EffectiveRateLp     *float64 `json:"effective_rate_lp"`
	RateInherited       bool     `json:"rate_inherited"`
}
