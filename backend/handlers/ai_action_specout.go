// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-166. The "spec-out" action — turn a description into a
// structured AC checklist organised under four headings:
//
//   1. Product outcome — what the user / customer can do after this
//      ships that they couldn't before.
//   2. Behavioural guarantees — invariants, transitions, edge cases
//      that must hold.
//   3. Edge cases — concrete failure / boundary scenarios with
//      expected handling.
//   4. Regression checks — what existing flows must keep working.
//
// Modeled on the AC patterns Markus has been writing manually
// across the recent PAI-146 / PAI-153 work. The model must keep
// existing AC items intact — the frontend appends, never replaces
// (unless the user explicitly asks for replace mode).

package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "spec_out",
		Label:       "Spec-out (description → AC checklist)",
		Surface:     "issue",
		Handler:     specOutHandler,
		Implemented: true,
	})
}

type specOutBody struct {
	Items []specOutItem `json:"items"`
}

type specOutItem struct {
	Category string `json:"category"` // outcome | behavior | edge | regression
	Text     string `json:"text"`
}

func specOutHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if strings.TrimSpace(ax.Text) == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "spec_out requires a non-empty description"}
	}

	// PAI-178: resolved system prompt; admin-editable in
	// Settings → AI prompts. Default lives in ai_action_prompts.go.
	systemPrompt := resolveActionPrompt("spec_out")

	var u strings.Builder
	if ax.IssueData.IssueKey != "" {
		fmt.Fprintf(&u, "Issue: %s", ax.IssueData.IssueKey)
		if ax.IssueData.IssueType != "" {
			fmt.Fprintf(&u, " (%s)", ax.IssueData.IssueType)
		}
		fmt.Fprintln(&u)
	}
	if ax.IssueData.IssueTitle != "" {
		fmt.Fprintf(&u, "Title: %s\n", ax.IssueData.IssueTitle)
	}
	if ax.IssueData.ProjectName != "" {
		fmt.Fprintf(&u, "Project: %s\n", ax.IssueData.ProjectName)
	}
	u.WriteString("\nDescription:\n")
	u.WriteString(ax.Text)
	u.WriteString("\n\nReturn the JSON object with 4-12 items, distributed across all four categories where applicable.")

	ctx, cancel := context.WithTimeout(ax.Ctx, 60*time.Second)
	defer cancel()
	var body specOutBody
	model, ptok, ctok, finish, err := callJSONAction(ctx, ax, systemPrompt, u.String(), 2000, &body)
	if err != nil {
		return nil, model, ptok, ctok, finish, err
	}
	for i := range body.Items {
		body.Items[i].Category = normaliseCategory(body.Items[i].Category)
	}
	return body, model, ptok, ctok, finish, nil
}

func normaliseCategory(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "outcome", "outcomes", "product outcome":
		return "outcome"
	case "behavior", "behaviour", "behavioural", "behavioral":
		return "behavior"
	case "edge", "edge case", "edge cases":
		return "edge"
	case "regression", "regressions":
		return "regression"
	default:
		return "behavior"
	}
}
