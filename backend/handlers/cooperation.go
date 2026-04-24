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
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// PAI-61. Per-project cooperation metadata.
//
// Two endpoints, both project-scoped:
//   - GET  /api/projects/:id/cooperation  → existing row, or zero-value defaults
//   - PUT  /api/projects/:id/cooperation  → upsert (admin-only)
//
// The GET-returns-defaults pattern means the frontend never has to
// special-case "no row yet" — it just renders the form with empty
// fields, and a save creates the row on demand.

// allowedCoopValues mirrors the CHECK constraints in migration 71 so
// the API rejects bad values with a clean 400 instead of letting the
// constraint trip a 500.
var allowedCoopValues = map[string]map[string]bool{
	"engagement_type": {
		"consultancy": true, "project_delivery": true,
		"managed_service": true, "retainer": true,
	},
	"code_ownership": {
		"client_repo": true, "own_repo": true, "mixed": true,
	},
	"env_responsibility": {
		"dev_staging": true, "dev_staging_prod": true, "full_stack": true,
	},
}

func GetCooperation(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	c, err := loadCooperation(pid)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	if c == nil {
		// Empty defaults so the frontend can render the form straight away.
		jsonOK(w, models.CooperationMetadata{ProjectID: pid})
		return
	}
	jsonOK(w, c)
}

func PutCooperation(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body models.CooperationMetadata
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Validate the structured enums up-front. Empty pointer ("") is
	// treated as "not set" and stored as NULL.
	for _, pair := range []struct {
		field string
		value *string
	}{
		{"engagement_type", body.EngagementType},
		{"code_ownership", body.CodeOwnership},
		{"env_responsibility", body.EnvResponsibility},
	} {
		if pair.value != nil && *pair.value != "" {
			if !allowedCoopValues[pair.field][*pair.value] {
				jsonError(w, "invalid value for "+pair.field, http.StatusBadRequest)
				return
			}
		}
	}

	// Verify project exists so a typo doesn't silently create an orphan row.
	var exists int
	if err := db.DB.QueryRow("SELECT 1 FROM projects WHERE id=?", pid).Scan(&exists); err != nil {
		jsonError(w, "project not found", http.StatusNotFound)
		return
	}

	// Upsert. The UNIQUE(project_id) index lets ON CONFLICT do the right
	// thing here — a fresh project gets a new row, a re-edit updates in place.
	_, err = db.DB.Exec(`
		INSERT INTO project_cooperation(
			project_id, engagement_type, code_ownership, env_responsibility,
			has_sla, uptime_sla, response_time_sla,
			backup_responsible, oncall,
			sla_details, cooperation_notes,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(project_id) DO UPDATE SET
			engagement_type    = excluded.engagement_type,
			code_ownership     = excluded.code_ownership,
			env_responsibility = excluded.env_responsibility,
			has_sla            = excluded.has_sla,
			uptime_sla         = excluded.uptime_sla,
			response_time_sla  = excluded.response_time_sla,
			backup_responsible = excluded.backup_responsible,
			oncall             = excluded.oncall,
			sla_details        = excluded.sla_details,
			cooperation_notes  = excluded.cooperation_notes,
			updated_at         = excluded.updated_at
	`,
		pid,
		nullableEnum(body.EngagementType),
		nullableEnum(body.CodeOwnership),
		nullableEnum(body.EnvResponsibility),
		coopBoolInt(body.HasSLA),
		body.UptimeSLA, body.ResponseTimeSLA,
		coopBoolInt(body.BackupResponsible), coopBoolInt(body.OnCall),
		body.SLADetails, body.CooperationNotes,
	)
	if err != nil {
		jsonError(w, "save failed", http.StatusInternalServerError)
		return
	}
	c, err := loadCooperation(pid)
	if err != nil || c == nil {
		jsonError(w, "reload failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, c)
}

// loadCooperation returns the cooperation row for a project, or nil if
// none exists yet. Distinguishing nil-vs-error matters because the GET
// handler returns synthetic defaults for the nil case.
func loadCooperation(projectID int64) (*models.CooperationMetadata, error) {
	var c models.CooperationMetadata
	var hasSLA, backup, oncall int
	err := db.DB.QueryRow(`
		SELECT project_id, engagement_type, code_ownership, env_responsibility,
		       has_sla, uptime_sla, response_time_sla,
		       backup_responsible, oncall,
		       sla_details, cooperation_notes,
		       created_at, updated_at
		FROM project_cooperation WHERE project_id=?
	`, projectID).Scan(
		&c.ProjectID, &c.EngagementType, &c.CodeOwnership, &c.EnvResponsibility,
		&hasSLA, &c.UptimeSLA, &c.ResponseTimeSLA,
		&backup, &oncall,
		&c.SLADetails, &c.CooperationNotes,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.HasSLA = hasSLA != 0
	c.BackupResponsible = backup != 0
	c.OnCall = oncall != 0
	return &c, nil
}

// nullableEnum returns nil for an unset / empty pointer so the DB stores
// NULL (and the CHECK constraint is satisfied) instead of the empty
// string (which would fail the CHECK).
func nullableEnum(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

func coopBoolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
