// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-165+. Shared helpers for the multi-action AI dispatcher.
//
// Most non-optimize actions ask the model for STRUCTURED output: a
// suggestions list, a checklist with categories, candidate-issue
// cards, etc. Building that structure by hand on the frontend would
// drift fast — the right shape lives in code, served back as JSON,
// and rendered by a generic modal that switches on the action key.
//
// What lives here
// ---------------
//   - callJSONAction: thin wrapper around provider.Optimize that
//     prepends a JSON-only output instruction and parses the reply.
//   - issueSiblingsForProject: load the project's issue tree for
//     the find_parent / detect_duplicates actions (capped to keep
//     prompts within token budget).
//   - The shared "preserve markdown / no preamble" prompt fragments
//     used by multiple actions.

package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/markus-barta/paimos/backend/ai"
	"github.com/markus-barta/paimos/backend/db"
)

// jsonActionInstructions is appended to every JSON-output action's
// system prompt. We say "ONLY a JSON object" three different ways
// because models occasionally smuggle a preamble or trailing prose.
const jsonActionInstructions = `

CRITICAL OUTPUT RULES — return ONLY a JSON object matching the schema below.
- No preamble, no explanation, no markdown fences.
- The first character of your response MUST be "{".
- The last character of your response MUST be "}".
- Do not include any text before or after the JSON object.`

// callJSONAction issues one provider call with a JSON-output instruction
// appended to the system prompt, then unmarshals the reply into `out`.
// The fence-stripper handles models that wrap their output in ``` fences
// despite the instructions; the attempt-to-find-first-brace handles
// models that prepend a few words anyway.
func callJSONAction(
	ctx context.Context,
	ax *aiActionContext,
	systemPrompt, userPrompt string,
	maxTokens int,
	out any,
) (model string, promptTokens, completionTokens int, finishReason string, err error) {
	resp, err := ax.Provider.Optimize(ctx, ai.OptimizeRequest{
		Model:           ax.Settings.Model,
		APIKey:          ax.Settings.APIKey,
		SystemPrompt:    systemPrompt + jsonActionInstructions,
		UserPrompt:      userPrompt,
		MaxOutputTokens: maxTokens,
	})
	if err != nil {
		return "", 0, 0, "", err
	}
	cleaned := stripJSONFences(resp.Text)
	if uerr := json.Unmarshal([]byte(cleaned), out); uerr != nil {
		return resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason,
			fmt.Errorf("ai_action: failed to parse model JSON: %w (raw: %.200s)", uerr, cleaned)
	}
	return resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}

// stripJSONFences trims a leading code-fence + trailing code-fence
// from the model's reply, then extracts the substring between the
// first "{" and last "}" to discard any chatty preamble. Idempotent
// when the input is already a clean JSON object.
func stripJSONFences(s string) string {
	t := strings.TrimSpace(s)
	t = ai.StripFenceEcho(t)
	// Extract from first { to last } so a stray "Sure, here is the JSON:"
	// doesn't break parsing.
	first := strings.IndexByte(t, '{')
	last := strings.LastIndexByte(t, '}')
	if first >= 0 && last > first {
		return t[first : last+1]
	}
	return t
}

// projectIssueRow is one entry in the issue tree we hand to
// find_parent / detect_duplicates / etc. Kept narrow on purpose:
// long descriptions blow the prompt budget.
type projectIssueRow struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	ParentID *int64 `json:"parent_id,omitempty"`
	Summary  string `json:"summary,omitempty"` // first 200 chars of description, optional
}

// loadProjectIssueTree loads up to `cap` issues for the project the
// given issue belongs to. Returns the rows, the project name, and a
// `truncated` flag the caller can pipe back to the model + UI so
// the user knows if the suggestion was made on a partial view.
//
// `withSummary` includes the first 200 chars of description in each
// row — used by detect_duplicates which needs richer signal than
// titles alone.
func loadProjectIssueTree(
	ctx context.Context,
	issueID int64,
	cap int,
	withSummary bool,
) (rows []projectIssueRow, projectName string, truncated bool, err error) {
	if issueID == 0 {
		return nil, "", false, errors.New("issue_id required")
	}
	const projQ = `
SELECT p.id, COALESCE(p.name, '')
FROM issues i
JOIN projects p ON p.id = i.project_id
WHERE i.id = ?
`
	var projectID int64
	if err := db.DB.QueryRowContext(ctx, projQ, issueID).Scan(&projectID, &projectName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", false, errors.New("issue not found")
		}
		return nil, "", false, err
	}

	var totalCount int
	_ = db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issues WHERE project_id = ? AND deleted_at IS NULL`,
		projectID).Scan(&totalCount)
	if totalCount > cap {
		truncated = true
	}

	q := `
SELECT
  COALESCE(p.key, '') || '-' || i.issue_number AS issue_key,
  i.type,
  COALESCE(i.title, ''),
  i.parent_id,
  COALESCE(SUBSTR(i.description, 1, 200), '')
FROM issues i
JOIN projects p ON p.id = i.project_id
WHERE i.project_id = ? AND i.deleted_at IS NULL AND i.id != ?
ORDER BY i.id ASC
LIMIT ?
`
	dbRows, err := db.DB.QueryContext(ctx, q, projectID, issueID, cap)
	if err != nil {
		return nil, "", false, err
	}
	defer dbRows.Close()
	for dbRows.Next() {
		var r projectIssueRow
		var parent sql.NullInt64
		var summary string
		if err := dbRows.Scan(&r.Key, &r.Type, &r.Title, &parent, &summary); err != nil {
			return nil, "", false, err
		}
		if parent.Valid {
			v := parent.Int64
			r.ParentID = &v
		}
		if withSummary {
			r.Summary = strings.TrimSpace(summary)
		}
		rows = append(rows, r)
	}
	return rows, projectName, truncated, nil
}

// renderIssueTree turns a slice of projectIssueRow into a compact
// JSON-array string ready to drop into a prompt. Uses minimal field
// names so the model's prompt budget stays modest on large projects.
func renderIssueTree(rows []projectIssueRow, withSummary bool) string {
	type compactRow struct {
		Key      string `json:"key"`
		Type     string `json:"type"`
		Title    string `json:"title"`
		ParentID *int64 `json:"parent_id,omitempty"`
		Summary  string `json:"summary,omitempty"`
	}
	out := make([]compactRow, 0, len(rows))
	for _, r := range rows {
		c := compactRow{
			Key: r.Key, Type: r.Type, Title: r.Title, ParentID: r.ParentID,
		}
		if withSummary {
			c.Summary = r.Summary
		}
		out = append(out, c)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// _ keeps Go happy when only some of the helpers above are referenced
// from any single action file. The compiler's dead-code analysis won't
// trip on unused imports here because every helper IS used by at least
// one action file.
var _ = stripJSONFences
