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
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/models"
)

// AgentNameHeader is the per-request header that names the calling
// agent (e.g. `ops`, `dev`, `refinement`, `tooling`, or the reserved
// `web-ui` sentinel). When set, the value is persisted on the
// issue_history snapshot row so the audit trail can answer "which
// agent made this change?".
//
// The companion session-id header is the existing
// `X-PAIMOS-Session-Id` constant `SessionHeader` from session_audit.go;
// PAI-324 reuses it rather than introducing a parallel name.
const AgentNameHeader = "X-Paimos-Agent-Name"

// agentAttrCap is the application-side length cap (chars) for the
// agent_name and session_id columns added in M93. SQLite ALTER TABLE
// can't add CHECK constraints retroactively, so we truncate before
// the INSERT — defensive against accidental log-spam payloads.
const agentAttrCap = 64

// readAgentAttribution pulls X-Paimos-Agent-Name and X-Paimos-Session-Id
// off the request and returns them as nullable strings. Empty/whitespace
// values become nil so the columns persist as SQL NULL (not "").
func readAgentAttribution(r *http.Request) (agent *string, session *string) {
	if r == nil {
		return nil, nil
	}
	if v := strings.TrimSpace(r.Header.Get(AgentNameHeader)); v != "" {
		if len(v) > agentAttrCap {
			v = v[:agentAttrCap]
		}
		agent = &v
	}
	if v := strings.TrimSpace(r.Header.Get(SessionHeader)); v != "" {
		if len(v) > agentAttrCap {
			v = v[:agentAttrCap]
		}
		session = &v
	}
	return agent, session
}

// saveSnapshot inserts a full JSON snapshot of the issue into
// issue_history. The optional *http.Request supplies the
// X-Paimos-Agent-Name and X-Paimos-Session-Id headers; when r is nil
// or the headers are absent, both columns persist as SQL NULL —
// backwards-compatible with rows written before PAI-324.
func saveSnapshot(issue *models.Issue, changedBy *models.User, r *http.Request) {
	blob, err := json.Marshal(issue)
	if err != nil {
		return
	}
	var uid *int64
	if changedBy != nil {
		uid = &changedBy.ID
	}
	agent, session := readAgentAttribution(r)
	if _, err := db.DB.Exec(
		`INSERT INTO issue_history(issue_id, changed_by, snapshot, changed_at, agent_name, session_id) VALUES(?,?,?,?,?,?)`,
		issue.ID, uid, string(blob), time.Now().UTC().Format("2006-01-02 15:04:05"), agent, session,
	); err != nil {
		log.Printf("saveSnapshot: issue_id=%d: %v", issue.ID, err)
	}
	if issue.ProjectID != nil && *issue.ProjectID > 0 {
		enqueueProjectContextEmbeddingIndex(*issue.ProjectID)
	}
}

// ── History endpoints ─────────────────────────────────────────────────────────

type HistoryEntry struct {
	ID            int64   `json:"id"`
	IssueID       int64   `json:"issue_id"`
	ChangedBy     *int64  `json:"changed_by"`
	ChangedByName string  `json:"changed_by_name"`
	Snapshot      any     `json:"snapshot"`
	ChangedAt     string  `json:"changed_at"`
	AgentName     *string `json:"agent_name"`
	SessionID     *string `json:"session_id"`
}

// GetIssueHistory returns all history entries for an issue, oldest→newest.
func GetIssueHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(`
		SELECT h.id, h.issue_id, h.changed_by, COALESCE(u.username,''), h.snapshot, h.changed_at, h.agent_name, h.session_id
		FROM issue_history h
		LEFT JOIN users u ON u.id = h.changed_by
		WHERE h.issue_id = ?
		ORDER BY h.changed_at ASC, h.id ASC
	`, id)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		var rawSnapshot string
		if err := rows.Scan(&e.ID, &e.IssueID, &e.ChangedBy, &e.ChangedByName, &rawSnapshot, &e.ChangedAt, &e.AgentName, &e.SessionID); err != nil {
			continue
		}
		// Unmarshal snapshot so it's returned as a proper JSON object (not a string)
		var snap any
		if err := json.Unmarshal([]byte(rawSnapshot), &snap); err == nil {
			e.Snapshot = snap
		}
		entries = append(entries, e)
	}
	jsonOK(w, entries)
}
