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
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// ListTimeEntries returns all time entries for a ticket.
// GET /api/issues/:id/time-entries
func ListTimeEntries(w http.ResponseWriter, r *http.Request) {
	ticketID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(`
		SELECT te.id, te.issue_id, te.user_id, COALESCE(NULLIF(u.nickname,''), u.username, ''),
		       te.started_at, te.stopped_at, te.override, te.comment, te.created_at,
		       te.internal_rate_hourly
		FROM time_entries te
		LEFT JOIN users u ON u.id = te.user_id
		WHERE te.issue_id = ?
		ORDER BY te.started_at DESC
	`, ticketID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []models.TimeEntry{}
	for rows.Next() {
		e := scanTimeEntry(rows)
		if e != nil {
			entries = append(entries, *e)
		}
	}
	jsonOK(w, entries)
}

// CreateTimeEntry starts a new time entry (or records a manual one).
// POST /api/issues/:id/time-entries
//
// PAI-335: when the caller is a super-admin, `user_id` may be set to
// any other user — useful for retrospectively adding a forgotten
// timer for a teammate, or correcting a wrong-user assignment.
// Non-super-admins sending a foreign `user_id` get 403; absent /
// matching `user_id` keeps the existing self-only behaviour.
func CreateTimeEntry(w http.ResponseWriter, r *http.Request) {
	ticketID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		StartedAt string   `json:"started_at"`
		StoppedAt *string  `json:"stopped_at"`
		Override  *float64 `json:"override"`
		Comment   string   `json:"comment"`
		UserID    *int64   `json:"user_id"` // PAI-335
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.StartedAt == "" {
		body.StartedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}

	caller := auth.GetUser(r)
	if caller == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// PAI-335: decide whose entry this is. Default = caller. A super-
	// admin can pass any user_id; anyone else passing a foreign id is
	// denied even if the field's value would have been a no-op
	// (paranoia: never silently accept a bad client request).
	targetUserID := caller.ID
	crossUser := false
	if body.UserID != nil && *body.UserID != caller.ID {
		if !auth.IsSuperAdmin(caller) {
			jsonError(w, "only super-admin can create time entries for other users", http.StatusForbidden)
			return
		}
		targetUserID = *body.UserID
		crossUser = true
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("CreateTimeEntry: begin tx: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Snapshot the TARGET user's rate, not the caller's — otherwise
	// the super-admin's rate would silently shadow the actual worker's.
	var userRate *float64
	if err := tx.QueryRow("SELECT internal_rate_hourly FROM users WHERE id=?", targetUserID).Scan(&userRate); err != nil {
		log.Printf("CreateTimeEntry: rate snapshot user=%d: %v", targetUserID, err)
		jsonError(w, "target user not found", http.StatusBadRequest)
		return
	}

	res, err := tx.ExecContext(r.Context(), `
		INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, comment, internal_rate_hourly)
		VALUES(?,?,?,?,?,?,?)
	`, ticketID, targetUserID, body.StartedAt, body.StoppedAt, body.Override, body.Comment, userRate)
	if handleDBError(w, err, "time entry") {
		return
	}
	id, _ := res.LastInsertId()
	before := timeEntryMutationSnapshot{ID: id}
	after, err := fetchTimeEntryMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("CreateTimeEntry: snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !after.Exists {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	userID := caller.ID
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       &userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.time_entry.create"),
		SubjectType:  "time_entry",
		SubjectID:    id,
		InverseOp: InverseOp{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/time-entries/%d", id),
		},
		BeforeState: before,
		AfterState:  after,
		Undoable:    true,
	}); err != nil {
		log.Printf("CreateTimeEntry: recordMutation: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("CreateTimeEntry: commit: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	entry := getTimeEntryByID(id)
	if entry == nil {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	// PAI-335: structured audit line for the cross-user case. Same
	// shape as the auth audit lines (login_ok / login_failed / …) so
	// operators can grep for `super_admin_act` to find every
	// privileged action across the deployment. PAI-336 will replace
	// this with a queryable mutation_log entry + dashboard.
	if crossUser {
		log.Printf(
			"audit: super_admin_act actor_id=%d actor=%q action=time_entry_create target_user_id=%d entry_id=%d issue_id=%d",
			caller.ID, caller.Username, targetUserID, id, ticketID,
		)
	}
	// Re-evaluate system tags if this entry has hours (stopped or override)
	if body.StoppedAt != nil || body.Override != nil {
		EvaluateSystemTags(ticketID)
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, entry)
}

// UpdateTimeEntry updates a time entry (e.g. stop a running timer, add override).
// PUT /api/time-entries/:id
func UpdateTimeEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// own-or-admin check (PAI-335: super-admin is always allowed too,
	// even though admin already covers it today — keeps the gate
	// explicit so a future role-cascade refactor that narrows admin
	// can't quietly take this away).
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		StoppedAt     *string  `json:"stopped_at"`
		Override      *float64 `json:"override"`
		Comment       *string  `json:"comment"`
		ClearOverride bool     `json:"clear_override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("UpdateTimeEntry: begin tx: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	before, err := fetchTimeEntryMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("UpdateTimeEntry: snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !before.Exists {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	isOwner := before.UserID == user.ID
	if !isOwner && user.Role != "admin" && !auth.IsSuperAdmin(user) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	crossUser := !isOwner

	if body.ClearOverride {
		_, err = tx.ExecContext(r.Context(), `
			UPDATE time_entries SET
				stopped_at = CASE WHEN ? IS NOT NULL THEN ? ELSE stopped_at END,
				override   = NULL,
				comment    = COALESCE(?, comment)
			WHERE id = ?
		`, body.StoppedAt, body.StoppedAt, body.Comment, id)
	} else {
		_, err = tx.ExecContext(r.Context(), `
			UPDATE time_entries SET
				stopped_at = CASE WHEN ? IS NOT NULL THEN ? ELSE stopped_at END,
				override   = COALESCE(?, override),
				comment    = COALESCE(?, comment)
			WHERE id = ?
		`, body.StoppedAt, body.StoppedAt, body.Override, body.Comment, id)
	}
	if handleDBError(w, err, "time entry") {
		return
	}

	after, err := fetchTimeEntryMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("UpdateTimeEntry: after snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	userID := user.ID
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       &userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.time_entry.update"),
		SubjectType:  "time_entry",
		SubjectID:    id,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/time-entries/%d", id),
			Body:   before,
		},
		BeforeState: before,
		AfterState:  after,
		Undoable:    !valuesEqual(snapshotMap(before), snapshotMap(after)),
	}); err != nil {
		log.Printf("UpdateTimeEntry: recordMutation: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("UpdateTimeEntry: commit: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	entry := getTimeEntryByID(id)
	if entry == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	// PAI-335: stamp every cross-user write so a paper-trail grep
	// surfaces every privileged action.
	if crossUser {
		log.Printf(
			"audit: super_admin_act actor_id=%d actor=%q action=time_entry_update target_user_id=%d entry_id=%d issue_id=%d",
			user.ID, user.Username, before.UserID, id, entry.IssueID,
		)
	}
	// Re-evaluate system tags (timer stopped or override changed)
	EvaluateSystemTags(entry.IssueID)
	jsonOK(w, entry)
}

// DeleteTimeEntry deletes a time entry.
// DELETE /api/time-entries/:id
func DeleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// own-or-admin check (PAI-335: super-admin too — see UpdateTimeEntry).
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("DeleteTimeEntry: begin tx: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	before, err := fetchTimeEntryMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("DeleteTimeEntry: snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !before.Exists {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	isOwner := before.UserID == user.ID
	if !isOwner && user.Role != "admin" && !auth.IsSuperAdmin(user) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	crossUser := !isOwner

	res, err := tx.ExecContext(r.Context(), "DELETE FROM time_entries WHERE id=?", id)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	after, err := fetchTimeEntryMutationSnapshotTx(tx, id)
	if err != nil {
		log.Printf("DeleteTimeEntry: after snapshot id=%d: %v", id, err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	userID := user.ID
	if _, err := recordMutation(r.Context(), tx, mutationRecordArgs{
		RequestID:    requestIDFromRequest(r),
		UserID:       &userID,
		SessionID:    sessionIDFromRequest(r),
		AgentName:    agentNameFromRequest(r),
		MutationType: mutationTypeForRequest(r, "issue.time_entry.delete"),
		SubjectType:  "time_entry",
		SubjectID:    id,
		InverseOp: InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/time-entries/%d", id),
			Body:   before,
		},
		BeforeState: before,
		AfterState:  after,
		Undoable:    true,
	}); err != nil {
		log.Printf("DeleteTimeEntry: recordMutation: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("DeleteTimeEntry: commit: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	// PAI-335: paper-trail.
	if crossUser {
		log.Printf(
			"audit: super_admin_act actor_id=%d actor=%q action=time_entry_delete target_user_id=%d entry_id=%d issue_id=%d",
			user.ID, user.Username, before.UserID, id, before.IssueID,
		)
	}
	// Re-evaluate system tags after time entry removal
	EvaluateSystemTags(before.IssueID)
	w.WriteHeader(http.StatusNoContent)
}

// timerSelectCols is the column list for running/recent timer queries (with issue JOIN).
const timerSelectCols = `
	te.id, te.issue_id, te.user_id, COALESCE(NULLIF(u.nickname,''), u.username, ''),
	te.started_at, te.stopped_at, te.override, te.comment, te.created_at,
	te.internal_rate_hourly,
	COALESCE(p.key || '-' || CAST(i.issue_number AS TEXT), ''),
	COALESCE(i.title, ''),
	COALESCE(i.project_id, 0)
`

func scanTimerEntry(row teScanner) *models.TimeEntry {
	var e models.TimeEntry
	if err := row.Scan(
		&e.ID, &e.IssueID, &e.UserID, &e.Username,
		&e.StartedAt, &e.StoppedAt, &e.Override, &e.Comment, &e.CreatedAt,
		&e.InternalRateHourly, &e.IssueKey, &e.IssueTitle, &e.ProjectID,
	); err != nil {
		return nil
	}
	computeHours(&e)
	return &e
}

// GetRunningTimers returns all running time entries for the session user.
// GET /api/time-entries/running
func GetRunningTimers(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := db.DB.Query(`
		SELECT `+timerSelectCols+`
		FROM time_entries te
		LEFT JOIN users u ON u.id = te.user_id
		LEFT JOIN issues i ON i.id = te.issue_id
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE te.user_id = ? AND te.stopped_at IS NULL
		ORDER BY te.started_at DESC
	`, user.ID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []models.TimeEntry{}
	for rows.Next() {
		e := scanTimerEntry(rows)
		if e != nil {
			entries = append(entries, *e)
		}
	}
	jsonOK(w, entries)
}

// GetRecentTimers returns the N most recently stopped time entries for the session user.
// GET /api/time-entries/recent
func GetRecentTimers(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get user's recent_timers_limit
	var limit int
	if err := db.DB.QueryRow("SELECT recent_timers_limit FROM users WHERE id=?", user.ID).Scan(&limit); err != nil {
		limit = 5
	}

	// Deduplicate by issue_id (most recent entry per issue) and exclude
	// issues that currently have a running timer for this user.
	rows, err := db.DB.Query(`
		SELECT `+timerSelectCols+`
		FROM time_entries te
		LEFT JOIN users u ON u.id = te.user_id
		LEFT JOIN issues i ON i.id = te.issue_id
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE te.user_id = ? AND te.stopped_at IS NOT NULL
		  AND te.issue_id NOT IN (
		    SELECT issue_id FROM time_entries WHERE user_id = ? AND stopped_at IS NULL
		  )
		  AND te.id IN (
		    SELECT id FROM (
		      SELECT id, ROW_NUMBER() OVER (PARTITION BY issue_id ORDER BY stopped_at DESC) AS rn
		      FROM time_entries
		      WHERE user_id = ? AND stopped_at IS NOT NULL
		    ) WHERE rn = 1
		  )
		ORDER BY te.stopped_at DESC
		LIMIT ?
	`, user.ID, user.ID, user.ID, limit)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []models.TimeEntry{}
	for rows.Next() {
		e := scanTimerEntry(rows)
		if e != nil {
			entries = append(entries, *e)
		}
	}
	jsonOK(w, entries)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func getTimeEntryByID(id int64) *models.TimeEntry {
	row := db.DB.QueryRow(`
		SELECT te.id, te.issue_id, te.user_id, COALESCE(NULLIF(u.nickname,''), u.username, ''),
		       te.started_at, te.stopped_at, te.override, te.comment, te.created_at,
		       te.internal_rate_hourly
		FROM time_entries te
		LEFT JOIN users u ON u.id = te.user_id
		WHERE te.id = ?
	`, id)
	return scanTimeEntry(row)
}

type teScanner interface {
	Scan(...any) error
}

func scanTimeEntry(row teScanner) *models.TimeEntry {
	var e models.TimeEntry
	if err := row.Scan(
		&e.ID, &e.IssueID, &e.UserID, &e.Username,
		&e.StartedAt, &e.StoppedAt, &e.Override, &e.Comment, &e.CreatedAt,
		&e.InternalRateHourly,
	); err != nil {
		return nil
	}
	computeHours(&e)
	return &e
}

// computeHours sets e.Hours from override or (stopped_at - started_at).
func computeHours(e *models.TimeEntry) {
	if e.Override != nil {
		e.Hours = e.Override
		return
	}
	if e.StoppedAt == nil {
		return // timer still running
	}
	layouts := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	var start, stop time.Time
	for _, layout := range layouts {
		if t, err := time.Parse(layout, e.StartedAt); err == nil {
			start = t
			break
		}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, *e.StoppedAt); err == nil {
			stop = t
			break
		}
	}
	if !start.IsZero() && !stop.IsZero() && stop.After(start) {
		h := stop.Sub(start).Hours()
		e.Hours = &h
	}
}
