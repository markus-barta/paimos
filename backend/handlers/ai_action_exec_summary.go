// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-418 / PAI-421. The "executive summary" generator — produces
// the crisp technical TL;DR used as the executive-readable text in
// Projektberichte. Audience: technically literate decision-makers
// (CTO, engineering lead, technical customer-side stakeholder).
//
// Shares the source-loader helper with customer_rewrite — the prompt
// differs but the inputs are the same: issue description (required)
// plus title + AC + live field text as context.

package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/markus-barta/paimos/backend/ai"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "exec_summary",
		Label:       "Generate as executive summary",
		Surface:     "customer",
		Placement:   "text",
		Handler:     execSummaryHandler,
		Implemented: true,
	})
}

type execSummaryBody struct {
	Optimized string `json:"optimized"`
}

func execSummaryHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if ax.IssueID <= 0 {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "issue_id is required for this action"}
	}
	description := strings.TrimSpace(ax.IssueData.Description)
	if description == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "issue has no description to summarise"}
	}

	systemPrompt := resolveActionPrompt("exec_summary")

	var u strings.Builder
	if ax.IssueData.IssueTitle != "" {
		fmt.Fprintf(&u, "Titel des Tickets (Kontext, nicht wiederholen):\n%s\n\n", ax.IssueData.IssueTitle)
	}
	if ac := strings.TrimSpace(ax.IssueData.AcceptanceCriteria); ac != "" {
		fmt.Fprintf(&u, "Akzeptanzkriterien (Kontext, kann Risiko/Compliance enthalten):\n%s\n\n", ac)
	}
	if strings.TrimSpace(ax.Text) != "" {
		u.WriteString("Aktuelle Executive-Fassung (refine, nicht ersetzen):\n")
		u.WriteString(ax.Text)
		u.WriteString("\n\n")
	}
	u.WriteString("Beschreibung des Tickets (Quelle):\n")
	u.WriteString(description)
	u.WriteString("\n\nGib AUSSCHLIESSLICH die fertige Executive-Zusammenfassung zurück — 1–3 deutsche Sätze, keine Markdown-Fences.")

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
	return execSummaryBody{Optimized: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}
