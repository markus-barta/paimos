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
	CustomerID   string `json:"customer_id"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	IssueCount       int    `json:"issue_count,omitempty"`
	LogoPath         string `json:"logo_path"`
	LastActivity     string `json:"last_activity"`
	OpenIssueCount   int    `json:"open_issue_count"`
	DoneIssueCount   int    `json:"done_issue_count"`
	ActiveIssueCount int    `json:"active_issue_count"`
	Tags             []Tag    `json:"tags"`
	RateHourly       *float64 `json:"rate_hourly"`
	RateLp           *float64 `json:"rate_lp"`
}
