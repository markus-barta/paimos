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

// CooperationMetadata is the per-project engagement profile (PAI-61).
//
// Informational in v1 — no behavioural effects elsewhere in the app.
// Structured fields are nullable / "" because not every project needs
// every dimension filled; the UI shows an empty-state setup prompt
// when no fields are populated.
type CooperationMetadata struct {
	// ProjectID is the FK; the wrapping handler always sets it from the
	// URL param, so callers don't need to pass it in the request body.
	ProjectID int64 `json:"project_id"`

	EngagementType    *string `json:"engagement_type"`     // consultancy | project_delivery | managed_service | retainer
	CodeOwnership     *string `json:"code_ownership"`      // client_repo | own_repo | mixed
	EnvResponsibility *string `json:"env_responsibility"`  // dev_staging | dev_staging_prod | full_stack

	HasSLA             bool   `json:"has_sla"`
	UptimeSLA          string `json:"uptime_sla"`
	ResponseTimeSLA    string `json:"response_time_sla"`
	BackupResponsible  bool   `json:"backup_responsible"`
	OnCall             bool   `json:"oncall"`

	SLADetails       string `json:"sla_details"`
	CooperationNotes string `json:"cooperation_notes"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}
