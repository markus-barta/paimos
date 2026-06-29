// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-606 (epic PAI-605): agent-run lifecycle API for the "Implement this"
// feature. The UI button creates a queued run on an issue; the developer's
// local runner transitions it and posts the structured report. SSE delivery
// to online runners is PAI-607; this slice is the persistence + REST surface.
package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/sse"
)

// AgentRun is the lifecycle record for one "Implement this" run.
type AgentRun struct {
	ID              int64   `json:"id"`
	IssueID         int64   `json:"issue_id"`
	ProjectID       *int64  `json:"project_id"`
	DeviceID        string  `json:"device_id"`
	RequestedBy     *int64  `json:"requested_by"`
	AgentName       string  `json:"agent_name"`
	SessionID       string  `json:"session_id"`
	Status          string  `json:"status"`
	Version         string  `json:"version"`
	TestsSummary    *string `json:"tests_summary"`
	DeployTarget    string  `json:"deploy_target"`
	LogAttachmentID *int64  `json:"log_attachment_id"`
	Error           string  `json:"error"`
	CreatedAt       string  `json:"created_at"`
	StartedAt       *string `json:"started_at"`
	FinishedAt      *string `json:"finished_at"`
}

// agentRunStatuses is the allowed lifecycle set (mirrors the DB CHECK).
var agentRunStatuses = map[string]bool{
	"queued": true, "running": true, "tests_passed": true, "tests_failed": true,
	"deployed": true, "failed": true, "cancelled": true,
}

func agentRunIsTerminal(s string) bool {
	return s == "deployed" || s == "failed" || s == "cancelled"
}

const agentRunCols = `id, issue_id, project_id, device_id, requested_by, agent_name, session_id, ` +
	`status, version, tests_summary, deploy_target, log_attachment_id, error, created_at, started_at, finished_at`

func scanAgentRun(row interface{ Scan(...any) error }) (*AgentRun, error) {
	var ar AgentRun
	var projectID, requestedBy, logAtt sql.NullInt64
	var tests, startedAt, finishedAt sql.NullString
	if err := row.Scan(&ar.ID, &ar.IssueID, &projectID, &ar.DeviceID, &requestedBy,
		&ar.AgentName, &ar.SessionID, &ar.Status, &ar.Version, &tests,
		&ar.DeployTarget, &logAtt, &ar.Error, &ar.CreatedAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	if projectID.Valid {
		ar.ProjectID = &projectID.Int64
	}
	if requestedBy.Valid {
		ar.RequestedBy = &requestedBy.Int64
	}
	if logAtt.Valid {
		ar.LogAttachmentID = &logAtt.Int64
	}
	if tests.Valid {
		ar.TestsSummary = &tests.String
	}
	if startedAt.Valid {
		ar.StartedAt = &startedAt.String
	}
	if finishedAt.Valid {
		ar.FinishedAt = &finishedAt.String
	}
	return &ar, nil
}

func getAgentRunByID(id int64) (*AgentRun, error) {
	return scanAgentRun(db.DB.QueryRow(`SELECT `+agentRunCols+` FROM agent_runs WHERE id=?`, id))
}

// canManageAgentRun: an admin, or the user who requested the run, may read
// and update a single run. The list endpoint is gated by issue access, so
// any project member can watch a ticket's runs in the UI.
func canManageAgentRun(r *http.Request, run *AgentRun) bool {
	u := auth.GetUser(r)
	if u == nil {
		return false
	}
	if u.Role == auth.RoleAdmin || u.Role == auth.RoleSuperAdmin {
		return true
	}
	return run.RequestedBy != nil && *run.RequestedBy == u.ID
}

// ImplementIssue — POST /api/issues/{id}/implement (RequireIssueEdit).
// Creates a queued run for the issue. SSE notification of online runners is
// wired in PAI-607.
func ImplementIssue(w http.ResponseWriter, r *http.Request) {
	issueID, ok := resolveIssueIDFromRequest(r)
	if !ok {
		jsonError(w, "issue not found", http.StatusNotFound)
		return
	}
	var body struct {
		DeviceID     string `json:"device_id"`
		DeployTarget string `json:"deploy_target"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body) // body is optional
	}
	var projectID sql.NullInt64
	_ = db.DB.QueryRow(`SELECT project_id FROM issues WHERE id=?`, issueID).Scan(&projectID)
	var requestedBy *int64
	if u := auth.GetUser(r); u != nil {
		requestedBy = &u.ID
	}
	res, err := db.DB.Exec(
		`INSERT INTO agent_runs(issue_id, project_id, device_id, requested_by, deploy_target, status)
		 VALUES(?,?,?,?,?, 'queued')`,
		issueID, projectID, strings.TrimSpace(body.DeviceID), requestedBy, strings.TrimSpace(body.DeployTarget))
	if err != nil {
		jsonError(w, "could not create run", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	run, err := getAgentRunByID(id)
	if err != nil {
		jsonError(w, "run created but reload failed", http.StatusInternalServerError)
		return
	}
	// PAI-607: notify the project's online runners. The event carries the
	// run id (Rev) so a runner can GET /api/runs/{id} for the full detail;
	// Name is the issue key for human-readable logging.
	if projectID.Valid {
		sse.GlobalBroker().PublishProject(projectID.Int64, sse.Event{
			Type: "implement_requested",
			Name: agentRunIssueKey(issueID),
			Rev:  strconv.FormatInt(id, 10),
		})
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, run)
}

// agentRunIssueKey returns the "PAI-123" key for an issue id (best-effort;
// empty string if the lookup fails).
func agentRunIssueKey(issueID int64) string {
	var key string
	_ = db.DB.QueryRow(
		`SELECT p.key || '-' || i.issue_number FROM issues i JOIN projects p ON p.id = i.project_id WHERE i.id = ?`,
		issueID).Scan(&key)
	return key
}

// ProjectRunner is an online, implement-capable runner for a project.
type ProjectRunner struct {
	UserID   int64  `json:"user_id"`
	DeviceID string `json:"device_id"`
	LastSeen string `json:"last_seen"`
}

// ListProjectRunners — GET /api/projects/{id}/runners (project-view gated).
// Intersects the broker's live subscribers for the project with the
// auto-watch rows that advertised implement-capability, so the UI can offer
// a device picker of runners that can actually take an "Implement this" job.
func ListProjectRunners(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	out := make([]ProjectRunner, 0, 4)
	for _, p := range sse.GlobalBroker().ProjectSubscribers(projectID) {
		var canImplement int
		var lastSeen string
		err := db.DB.QueryRow(
			`SELECT can_implement, updated_at FROM auto_watch_subscriptions WHERE user_id=? AND device_id=? AND project_id=?`,
			p.UserID, p.DeviceID, projectID).Scan(&canImplement, &lastSeen)
		if err != nil || canImplement == 0 {
			continue
		}
		out = append(out, ProjectRunner{UserID: p.UserID, DeviceID: p.DeviceID, LastSeen: lastSeen})
	}
	jsonOK(w, map[string]any{"runners": out})
}

// ListIssueRuns — GET /api/issues/{id}/runs (RequireIssueAccess). Newest first.
func ListIssueRuns(w http.ResponseWriter, r *http.Request) {
	issueID, ok := resolveIssueIDFromRequest(r)
	if !ok {
		jsonError(w, "issue not found", http.StatusNotFound)
		return
	}
	rows, err := db.DB.Query(`SELECT `+agentRunCols+` FROM agent_runs WHERE issue_id=? ORDER BY id DESC`, issueID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := make([]*AgentRun, 0, 8)
	for rows.Next() {
		ar, err := scanAgentRun(rows)
		if err != nil {
			continue
		}
		out = append(out, ar)
	}
	jsonOK(w, map[string]any{"runs": out})
}

// GetAgentRun — GET /api/runs/{id}. Requester or admin.
func GetAgentRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	run, err := getAgentRunByID(id)
	if err != nil {
		jsonError(w, "run not found", http.StatusNotFound)
		return
	}
	if !canManageAgentRun(r, run) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	jsonOK(w, run)
}

// PatchAgentRun — PATCH /api/runs/{id}. The local runner posts status
// transitions + the structured report. Requester or admin only. started_at is
// stamped on the first move to `running`; finished_at on any terminal status.
func PatchAgentRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	existing, err := getAgentRunByID(id)
	if err != nil {
		jsonError(w, "run not found", http.StatusNotFound)
		return
	}
	if !canManageAgentRun(r, existing) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Status       *string `json:"status"`
		Version      *string `json:"version"`
		TestsSummary *string `json:"tests_summary"`
		DeployTarget *string `json:"deploy_target"`
		Error        *string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	sets := make([]string, 0, 8)
	args := make([]any, 0, 8)
	if body.Status != nil {
		s := strings.TrimSpace(*body.Status)
		if !agentRunStatuses[s] {
			jsonError(w, "invalid status", http.StatusBadRequest)
			return
		}
		sets = append(sets, "status=?")
		args = append(args, s)
		if s == "running" && existing.StartedAt == nil {
			sets = append(sets, "started_at=datetime('now')")
		}
		if agentRunIsTerminal(s) {
			sets = append(sets, "finished_at=datetime('now')")
		}
	}
	if body.Version != nil {
		sets = append(sets, "version=?")
		args = append(args, strings.TrimSpace(*body.Version))
	}
	if body.TestsSummary != nil {
		sets = append(sets, "tests_summary=?")
		args = append(args, *body.TestsSummary)
	}
	if body.DeployTarget != nil {
		sets = append(sets, "deploy_target=?")
		args = append(args, strings.TrimSpace(*body.DeployTarget))
	}
	if body.Error != nil {
		sets = append(sets, "error=?")
		args = append(args, *body.Error)
	}
	// Stamp the attributing agent/session if the runner forwarded them.
	if an := agentNameFromRequest(r); an != "" {
		sets = append(sets, "agent_name=?")
		args = append(args, an)
	}
	if sid := sessionIDFromRequest(r); sid != "" {
		sets = append(sets, "session_id=?")
		args = append(args, sid)
	}
	if len(sets) == 0 {
		jsonError(w, "no fields to update", http.StatusBadRequest)
		return
	}
	args = append(args, id)
	// #nosec G202 -- `sets` holds only hardcoded column-assignment fragments
	// (status=?, version=?, started_at=datetime('now'), …); every value is bound
	// via ? placeholders in args, so no user input enters the SQL string.
	if _, err := db.DB.Exec(`UPDATE agent_runs SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...); err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	run, err := getAgentRunByID(id)
	if err != nil {
		jsonError(w, "reload failed", http.StatusInternalServerError)
		return
	}
	// PAI-609: on the transition INTO a reportable terminal status, post a
	// human-readable summary comment on the issue so the trail stays consistent
	// with the structured run record. Best-effort — never fails the PATCH.
	if run.Status != existing.Status && agentRunIsReportable(run.Status) {
		postAgentRunReport(run.IssueID, run.RequestedBy, run)
	}
	jsonOK(w, run)
}

// agentRunIsReportable reports whether a terminal status warrants a summary
// comment (cancelled/declined runs are intentionally silent).
func agentRunIsReportable(s string) bool {
	return s == "deployed" || s == "tests_passed" || s == "tests_failed" || s == "failed"
}

// postAgentRunReport writes an internal issue comment summarizing a finished
// run (PAI-609). Attributed to the run's requester; best-effort.
func postAgentRunReport(issueID int64, authorID *int64, run *AgentRun) {
	body := agentRunReportBody(run)
	if body == "" {
		return
	}
	if _, err := db.DB.Exec(
		`INSERT INTO comments(issue_id, author_id, body, visibility) VALUES(?, ?, ?, ?)`,
		issueID, authorID, body, CommentVisibilityInternal); err != nil {
		log.Printf("agent run report comment: issue=%d run=%d: %v", issueID, run.ID, err)
	}
}

func agentRunReportBody(run *AgentRun) string {
	on := "an agent"
	if run.DeviceID != "" {
		on = "device " + run.DeviceID
	}
	at := ""
	if run.FinishedAt != nil {
		at = " at " + *run.FinishedAt
	}
	tests := ""
	if run.TestsSummary != nil && strings.TrimSpace(*run.TestsSummary) != "" {
		tests = " Tests: " + *run.TestsSummary + "."
	}
	switch run.Status {
	case "deployed":
		ver := ""
		if run.Version != "" {
			ver = " in v" + run.Version
		}
		target := ""
		if run.DeployTarget != "" {
			target = ", deployed to " + run.DeployTarget
		}
		return fmt.Sprintf("🤖 Implemented%s%s%s.%s (run #%d on %s)", ver, at, target, tests, run.ID, on)
	case "tests_passed":
		ver := ""
		if run.Version != "" {
			ver = " (v" + run.Version + ")"
		}
		return fmt.Sprintf("🤖 Implemented%s%s.%s (run #%d on %s)", at, ver, tests, run.ID, on)
	case "tests_failed", "failed":
		reason := run.Error
		if strings.TrimSpace(reason) == "" {
			reason = "no detail provided"
		}
		return fmt.Sprintf("🤖 Run #%d %s%s: %s (on %s)", run.ID, run.Status, at, reason, on)
	}
	return ""
}
