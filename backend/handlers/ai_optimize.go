// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-150 + PAI-152 + PAI-153. Optimize-call shared helpers.
//
// History
// -------
// In v1 this file held the entire POST /api/ai/optimize handler. After
// PAI-164, the optimize behaviour moved to the unified action
// dispatcher (ai_action.go + ai_action_optimize.go) and this file
// shrank to the helpers that survived: project-context loading
// (loadOptimizeContext), the input-cap constants, the userError
// type, and the outcome enum referenced by audit shapes.
//
// Why keep the helpers in a separate file
// ---------------------------------------
// The dispatcher and every action handler share the project-context
// loader. Putting it here keeps the dispatcher file focused on
// dispatch, and any future "this used to be at /api/ai/optimize"
// archaeology lands on a comment that explains the move rather than
// disappearing into a generic helpers file.

package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/markus-barta/paimos/backend/ai"
	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

const (
	// optimizeMaxInputBytes caps a single optimize call. PAIMOS multiline
	// fields are typically <8 KiB; the cap is generous to allow long
	// epics with embedded code blocks. Anything larger is almost always
	// a bug or an attempt to abuse the operator's token budget.
	optimizeMaxInputBytes = 32 << 10 // 32 KiB

	// optimizeMaxOutputTokens is the soft cap requested from the
	// provider. ~3000 tokens at ~4 chars each is roughly 12 KiB, more
	// than enough for the rewrites we expect.
	optimizeMaxOutputTokens = 3000

	// optimizeRequestTimeout bounds the whole call. The router has its
	// own ~30s default but rewrite calls can take longer on bigger
	// models; 60s is the band most providers stay inside while still
	// catching obvious wedges.
	optimizeRequestTimeout = 60 * time.Second
)

// Audit outcome enum (PAI-153). Stable strings used by audit lines
// so operators / log analysis can group every attempt — successful,
// failed, denied, or rejected before the provider was called — into
// the same dashboard. After PAI-164 these are referenced by the
// dispatcher (auditAction) rather than the legacy auditOptimize.
const (
	outcomeOK              = "ok"
	outcomeFailTimeout     = "fail_timeout"
	outcomeFailUpstream    = "fail_upstream"
	outcomeDenied          = "denied"
	outcomeUnauth          = "unauth"
	outcomeCfgLoadFail     = "cfg_load_fail"
	outcomeUnconfigured    = "unconfigured"
	outcomeBadRequest      = "bad_request"
	outcomeProviderMissing = "provider_missing"
	outcomeCtxFail         = "ctx_fail"
)

// userError lets loadOptimizeContext signal "client problem, not
// server bug" without wrapping http.Error patterns into the helper.
// Caller inspects via errors.As.
type userError struct {
	status int
	msg    string
}

func (e *userError) Error() string { return e.msg }

// loadOptimizeContext fetches the issue / project / parent epic for
// the prompt context block. Authorization re-uses auth.CanViewProject
// so every action enforces the same project-visibility rule the rest
// of the SPA does.
//
// Missing issueID returns an empty Context with the FieldName filled
// in — actions still work, just without the surrounding metadata
// (which is correct for new-issue / new-customer forms where no row
// exists yet).
func loadOptimizeContext(r *http.Request, issueID int64, field string) (ai.Context, error) {
	c := ai.Context{FieldName: field}
	if issueID == 0 {
		return c, nil
	}

	const q = `
SELECT
  i.project_id,
  COALESCE(p.key,  '')                               AS project_key,
  COALESCE(p.name, '')                               AS project_name,
  i.issue_number,
  i.type,
  i.title,
  i.parent_id,
  COALESCE(pp.key, '') || CASE WHEN pp.id IS NULL THEN '' ELSE '-' END
    || COALESCE(parent.issue_number, '')             AS parent_key,
  COALESCE(parent.title, '')                         AS parent_title
FROM issues i
LEFT JOIN projects p      ON p.id = i.project_id
LEFT JOIN issues   parent ON parent.id = i.parent_id
LEFT JOIN projects pp     ON pp.id = parent.project_id
WHERE i.id = ? AND i.deleted_at IS NULL
`
	var (
		projectID               sql.NullInt64
		projectKey, projectName string
		issueNum                int
		issueType, issueTitle   string
		parentID                sql.NullInt64
		parentKey, parentTitle  string
	)
	err := db.DB.QueryRowContext(r.Context(), q, issueID).Scan(
		&projectID, &projectKey, &projectName,
		&issueNum, &issueType, &issueTitle,
		&parentID, &parentKey, &parentTitle,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ai.Context{}, &userError{status: http.StatusNotFound, msg: "issue not found"}
	}
	if err != nil {
		return ai.Context{}, fmt.Errorf("scan issue: %w", err)
	}

	if projectID.Valid {
		if !auth.CanViewProject(r, projectID.Int64) {
			return ai.Context{}, &userError{status: http.StatusForbidden, msg: "issue not accessible"}
		}
	}

	if projectKey != "" {
		c.IssueKey = fmt.Sprintf("%s-%d", projectKey, issueNum)
	}
	c.IssueType = issueType
	c.IssueTitle = issueTitle
	c.ProjectName = projectName
	if parentID.Valid {
		switch {
		case parentKey != "" && parentTitle != "":
			c.ParentEpic = fmt.Sprintf("%s — %s", parentKey, parentTitle)
		case parentTitle != "":
			c.ParentEpic = parentTitle
		}
	}
	return c, nil
}
