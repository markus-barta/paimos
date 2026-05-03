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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// saveSnapshot inserts a full JSON snapshot of the issue into issue_history.
func saveSnapshot(issue *models.Issue, changedBy *models.User) {
	blob, err := json.Marshal(issue)
	if err != nil {
		return
	}
	var uid *int64
	if changedBy != nil {
		uid = &changedBy.ID
	}
	if _, err := db.DB.Exec(
		`INSERT INTO issue_history(issue_id, changed_by, snapshot, changed_at) VALUES(?,?,?,?)`,
		issue.ID, uid, string(blob), time.Now().UTC().Format("2006-01-02 15:04:05"),
	); err != nil {
		log.Printf("saveSnapshot: issue_id=%d: %v", issue.ID, err)
	}
}

// ── History endpoints ─────────────────────────────────────────────────────────

type HistoryEntry struct {
	ID            int64  `json:"id"`
	IssueID       int64  `json:"issue_id"`
	ChangedBy     *int64 `json:"changed_by"`
	ChangedByName string `json:"changed_by_name"`
	Snapshot      any    `json:"snapshot"`
	ChangedAt     string `json:"changed_at"`
}

// GetIssueHistory returns all history entries for an issue, oldest→newest.
func GetIssueHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(`
		SELECT h.id, h.issue_id, h.changed_by, COALESCE(u.username,''), h.snapshot, h.changed_at
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
		if err := rows.Scan(&e.ID, &e.IssueID, &e.ChangedBy, &e.ChangedByName, &rawSnapshot, &e.ChangedAt); err != nil {
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
