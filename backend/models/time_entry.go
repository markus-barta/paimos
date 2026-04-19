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

// TimeEntry represents a time tracking record on an issue.
type TimeEntry struct {
	ID        int64    `json:"id"`
	IssueID   int64    `json:"issue_id"` // renamed from ticket_id in migration 32
	UserID    int64    `json:"user_id"`
	Username  string   `json:"username,omitempty"`
	StartedAt string   `json:"started_at"`
	StoppedAt *string  `json:"stopped_at"` // nil = timer still running
	Override  *float64 `json:"override"`    // manual hours override
	Comment   string   `json:"comment"`
	CreatedAt string   `json:"created_at"`
	// Internal rate snapshot (stamped at creation from user's current rate)
	InternalRateHourly *float64 `json:"internal_rate_hourly"`
	// computed
	Hours      *float64 `json:"hours,omitempty"`        // override if set, else (stopped_at - started_at)
	IssueKey   string   `json:"issue_key,omitempty"`   // populated by running/recent endpoints
	IssueTitle string   `json:"issue_title,omitempty"` // populated by running/recent endpoints
	ProjectID  int64    `json:"project_id,omitempty"`  // populated by running/recent endpoints
}
