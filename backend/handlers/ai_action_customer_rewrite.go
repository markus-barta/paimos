// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-418 / PAI-421. The "customer summary" generator — produces the
// warm, Apple-style positive German release-note copy used as the
// customer-facing text in Projektberichte.
//
// Input source priority:
//   1. ax.Text — the live field text the user is editing. Used when
//      they want to refine an already-drafted customer summary.
//   2. The issue's description (read from DB by ax.IssueID). The
//      common path: the user clicks Generate on an empty
//      customer_summary field and we seed from description.
//
// Acceptance criteria + title flow in as supporting context so the
// model can stay anchored on what the ticket is actually about.

package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/markus-barta/paimos/backend/ai"
	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "customer_rewrite",
		Label:       "Generate as customer copy",
		Surface:     "customer",
		Placement:   "text",
		Handler:     customerRewriteHandler,
		SubKeys:     []string{"release_note", "feature", "fix", "stability", "security_hardening"},
		Implemented: true,
	})
}

// customerRewriteBody is the response shape. The diff-overlay UX
// reads `optimized`, so we mirror the optimize/translate convention.
type customerRewriteBody struct {
	Optimized string `json:"optimized"`
}

func customerRewriteHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	src, ctxErr := customerOrExecSource(ax)
	if ctxErr != nil {
		return nil, "", 0, 0, "", ctxErr
	}

	subAction := strings.TrimSpace(ax.SubAction)
	if subAction == "" {
		subAction = "release_note"
	}

	systemPrompt := resolveActionPrompt("customer_rewrite")

	var u strings.Builder
	fmt.Fprintf(&u, "Sub-action (Ton-Bias): %s\n\n", subAction)
	if ax.IssueData.IssueTitle != "" {
		fmt.Fprintf(&u, "Titel des Tickets (nur Kontext, nicht wiederholen):\n%s\n\n", ax.IssueData.IssueTitle)
	}
	if src.AcceptanceCriteria != "" {
		fmt.Fprintf(&u, "Akzeptanzkriterien (Kontext):\n%s\n\n", src.AcceptanceCriteria)
	}
	if strings.TrimSpace(ax.Text) != "" {
		u.WriteString("Aktuelle Kundenfassung (refine, nicht ersetzen):\n")
		u.WriteString(ax.Text)
		u.WriteString("\n\n")
	}
	u.WriteString("Beschreibung des Tickets (Quelle):\n")
	u.WriteString(src.Description)
	u.WriteString("\n\nGib AUSSCHLIESSLICH die fertige Kundenfassung zurück — 1–2 deutsche Sätze, keine Markdown-Fences.")

	callCtx, cancel := context.WithTimeout(ax.Ctx, optimizeRequestTimeout)
	defer cancel()
	resp, err := ax.Provider.Optimize(callCtx, ai.OptimizeRequest{
		Model:           ax.Settings.Model,
		APIKey:          ax.Settings.APIKey,
		SystemPrompt:    systemPrompt,
		UserPrompt:      u.String(),
		MaxOutputTokens: optimizeMaxOutputTokens,
	})
	if err != nil {
		return nil, "", 0, 0, "", err
	}
	cleaned := ai.StripFenceEcho(resp.Text)
	return customerRewriteBody{Optimized: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}

// summarySourceFields carries the source text variants the
// customer_rewrite / exec_summary handlers read from the issue row.
// Read by customerOrExecSource() — neither handler should query the
// DB directly so we keep auth + missing-issue handling in one place.
type summarySourceFields struct {
	Description        string
	AcceptanceCriteria string
}

// customerOrExecSource pulls the issue's description and AC for use
// as the rewrite source. Authorization runs through CanViewProject
// just like loadOptimizeContext does, so callers can't fish text
// out of issues they couldn't open via the normal SPA.
//
// Returns userError on auth / not-found, plain error on real DB
// trouble. Description is required (the rewrite has nothing to work
// from without it); AC is optional context.
func customerOrExecSource(ax *aiActionContext) (summarySourceFields, error) {
	var s summarySourceFields
	if ax.IssueID <= 0 {
		return s, &userError{status: 400, msg: "issue_id is required for this action"}
	}
	const q = `
SELECT i.project_id, i.description, i.acceptance_criteria
FROM issues i
WHERE i.id = ? AND i.deleted_at IS NULL
`
	var projectID sql.NullInt64
	err := db.DB.QueryRowContext(ax.Ctx, q, ax.IssueID).Scan(&projectID, &s.Description, &s.AcceptanceCriteria)
	if errors.Is(err, sql.ErrNoRows) {
		return s, &userError{status: 404, msg: "issue not found"}
	}
	if err != nil {
		return s, fmt.Errorf("scan issue: %w", err)
	}
	// loadOptimizeContext already enforced auth via the dispatcher
	// when the action was dispatched; we re-fetch here only to read
	// description + AC, both of which are already viewable through
	// the same auth check. The auth re-check below is defensive:
	// it catches the (rare) case where the dispatcher path changes
	// and forgets to authorize.
	if projectID.Valid && !auth.CanViewProject(ax.Request, projectID.Int64) {
		return s, &userError{status: 403, msg: "issue not accessible"}
	}
	if strings.TrimSpace(s.Description) == "" {
		return s, &userError{status: 400, msg: "issue has no description to summarise"}
	}
	return s, nil
}
