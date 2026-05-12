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
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

type auditExec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func recordSuperAdminAuditTx(ctx context.Context, exec auditExec, r *http.Request, actor *models.User, targetUserID int64, capability string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var actorID any
	if actor != nil {
		actorID = actor.ID
	}
	var targetID any
	if targetUserID > 0 {
		targetID = targetUserID
	}
	endpoint := ""
	requestID := ""
	if r != nil {
		endpoint = strings.TrimSpace(r.Method + " " + r.URL.Path)
		requestID = requestIDFromRequest(r)
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO super_admin_audit(actor_user_id, target_user_id, capability, endpoint, request_id, details_json)
		VALUES(?,?,?,?,?,?)
	`, actorID, targetID, capability, endpoint, requestID, string(detailsJSON))
	return err
}

type superAdminAuditRow struct {
	ID             int64           `json:"id"`
	ActorUserID    *int64          `json:"actor_user_id"`
	ActorUsername  *string         `json:"actor_username"`
	TargetUserID   *int64          `json:"target_user_id"`
	TargetUsername *string         `json:"target_username"`
	Capability     string          `json:"capability"`
	Endpoint       string          `json:"endpoint"`
	RequestID      string          `json:"request_id"`
	Details        json.RawMessage `json:"details"`
	CreatedAt      string          `json:"created_at"`
}

func nullableInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func nullableStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func ListSuperAdminActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			jsonError(w, "invalid limit", http.StatusBadRequest)
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}
	days := 30
	if raw := strings.TrimSpace(q.Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 365 {
			jsonError(w, "invalid days", http.StatusBadRequest)
			return
		}
		days = n
	}

	where := []string{"a.created_at >= datetime('now', ?)"}
	args := []any{fmt.Sprintf("-%d days", days)}
	for _, filter := range []struct {
		query string
		sql   string
	}{
		{"actor_id", "a.actor_user_id = ?"},
		{"target_user_id", "a.target_user_id = ?"},
	} {
		raw := strings.TrimSpace(q.Get(filter.query))
		if raw == "" {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			jsonError(w, "invalid "+filter.query, http.StatusBadRequest)
			return
		}
		where = append(where, filter.sql)
		args = append(args, id)
	}
	if capability := strings.TrimSpace(q.Get("capability")); capability != "" {
		where = append(where, "a.capability = ?")
		args = append(args, capability)
	}
	args = append(args, limit)

	rows, err := db.DB.QueryContext(r.Context(), `
		SELECT a.id, a.actor_user_id, actor.username,
		       a.target_user_id, target.username,
		       a.capability, a.endpoint, a.request_id, a.details_json, a.created_at
		FROM super_admin_audit a
		LEFT JOIN users actor ON actor.id = a.actor_user_id
		LEFT JOIN users target ON target.id = a.target_user_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT ?
	`, args...)
	if err != nil {
		log.Printf("ListSuperAdminActivity: query: %v", err)
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []superAdminAuditRow{}
	for rows.Next() {
		var row superAdminAuditRow
		var actorID, targetID sql.NullInt64
		var actorUsername, targetUsername sql.NullString
		var details string
		if err := rows.Scan(
			&row.ID, &actorID, &actorUsername, &targetID, &targetUsername,
			&row.Capability, &row.Endpoint, &row.RequestID, &details, &row.CreatedAt,
		); err != nil {
			log.Printf("ListSuperAdminActivity: scan: %v", err)
			continue
		}
		row.ActorUserID = nullableInt64Ptr(actorID)
		row.ActorUsername = nullableStringPtr(actorUsername)
		row.TargetUserID = nullableInt64Ptr(targetID)
		row.TargetUsername = nullableStringPtr(targetUsername)
		if json.Valid([]byte(details)) {
			row.Details = json.RawMessage(details)
		} else {
			row.Details = json.RawMessage(`{}`)
		}
		items = append(items, row)
	}
	jsonOK(w, map[string]any{"items": items})
}
