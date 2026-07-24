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
	"fmt"
	"strings"

	"github.com/inspr-at/paimos/backend/ai"
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
	if ax.IssueID <= 0 {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "issue_id is required for this action"}
	}
	description := strings.TrimSpace(ax.IssueData.Description)
	if description == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "issue has no description to summarise"}
	}

	subAction := strings.TrimSpace(ax.SubAction)
	if subAction == "" {
		subAction = "release_note"
	}

	systemPrompt := resolveActionPromptWithPreset(ax, "customer_rewrite")

	var u strings.Builder
	fmt.Fprintf(&u, "Sub-action (Ton-Bias): %s\n\n", subAction)
	if ax.IssueData.IssueTitle != "" {
		fmt.Fprintf(&u, "Titel des Tickets (nur Kontext, nicht wiederholen):\n%s\n\n", ax.IssueData.IssueTitle)
	}
	if ac := strings.TrimSpace(ax.IssueData.AcceptanceCriteria); ac != "" {
		fmt.Fprintf(&u, "Akzeptanzkriterien (Kontext):\n%s\n\n", ac)
	}
	if strings.TrimSpace(ax.Text) != "" {
		u.WriteString("Aktuelle Kundenfassung (refine, nicht ersetzen):\n")
		u.WriteString(ax.Text)
		u.WriteString("\n\n")
	}
	u.WriteString("Beschreibung des Tickets (Quelle):\n")
	u.WriteString(description)
	u.WriteString("\n\nGib AUSSCHLIESSLICH die fertige Kundenfassung zurück — 1–2 deutsche Sätze, keine Markdown-Fences.")

	callCtx, cancel := context.WithTimeout(ax.Ctx, optimizeRequestTimeout)
	defer cancel()
	resp, err := ax.Provider.Optimize(callCtx, ai.OptimizeRequest{
		Model:           ax.Settings.Model,
		APIKey:          ax.Settings.APIKey,
		BaseURL:         ax.Settings.BaseURL,
		SystemPrompt:    systemPrompt,
		UserPrompt:      aiUserPromptWithContext(ax, u.String()),
		MaxOutputTokens: optimizeMaxOutputTokens,
	})
	if err != nil {
		return nil, "", 0, 0, "", err
	}
	cleaned := ai.StripFenceEcho(resp.Text)
	return customerRewriteBody{Optimized: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}
