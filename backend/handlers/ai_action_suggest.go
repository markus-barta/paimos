// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-165. The "suggest enhancement" action — six sub-categories.
//
// Reads the surrounding issue context (title, description, AC,
// notes, type, project) and asks the model for 3-5 concrete
// enhancements in the chosen sub-category. The frontend renders
// the result as a checklist: the user picks which suggestions to
// apply, each chosen one is appended (with a "(suggested by AI)"
// marker) to AC or notes — never overwriting existing content.

package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "suggest_enhancement",
		Label:       "Suggest enhancement",
		Surface:     "issue",
		Handler:     suggestEnhancementHandler,
		SubKeys:     []string{"security", "performance", "ux", "dx", "flow", "risks"},
		Implemented: true,
	})
}

type suggestEnhancementBody struct {
	Suggestions []suggestEnhancementItem `json:"suggestions"`
}

type suggestEnhancementItem struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	Impact      string `json:"impact"`        // low | med | high
	TargetField string `json:"target_field"`  // ac | notes
}

// subActionGuidance gives the model a one-line lens per sub-action.
// Kept short — each is just enough to anchor the lens, not a full
// rubric.
var subActionGuidance = map[string]string{
	"security":    "Focus on authentication, authorization, input validation, secret handling, attack surface, and crypto choices.",
	"performance": "Focus on algorithmic complexity, query patterns, caching opportunities, payload size, and memory pressure.",
	"ux":          "Focus on happy-path clarity, edge-case coverage, error copy, empty/loading/error states, and discoverability.",
	"dx":          "Focus on developer ergonomics, testability, type safety, observability hooks, and onboarding friction for the next contributor.",
	"flow":        "Focus on state-machine completeness, transitions, cancellation, idempotence, and recovery from partial failure.",
	"risks":       "Focus on what can go wrong: blocked dependencies, unknowns, security/compliance/scale risks, plus what THIS work blocks elsewhere.",
}

func suggestEnhancementHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	guidance, ok := subActionGuidance[ax.SubAction]
	if !ok {
		return nil, "", 0, 0, "", fmt.Errorf("unknown sub_action %q", ax.SubAction)
	}

	// PAI-178: resolved prompt from ai_prompts (admin-editable)
	// + a per-call sub-category lens line. Splitting like this
	// keeps the editable surface manageable — admins tune ONE
	// prompt that covers all 6 sub-categories, and the lens is
	// substituted at call time.
	base := resolveActionPrompt("suggest_enhancement")
	systemPrompt := base + "\n\nSub-category lens for this call: " + guidance

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
	if ax.IssueData.ParentEpic != "" {
		fmt.Fprintf(&u, "Parent epic: %s\n", ax.IssueData.ParentEpic)
	}
	if ax.Text != "" {
		fmt.Fprintf(&u, "\n%s field content:\n", ax.IssueData.FieldName)
		u.WriteString(ax.Text)
		u.WriteString("\n")
	}
	fmt.Fprintf(&u, "\nReturn 3-5 enhancements in the JSON shape above. Sub-category: %s.", ax.SubAction)

	ctx, cancel := context.WithTimeout(ax.Ctx, 60*time.Second)
	defer cancel()
	var body suggestEnhancementBody
	model, ptok, ctok, finish, err := callJSONAction(ctx, ax, systemPrompt, u.String(), 1500, &body)
	if err != nil {
		return nil, model, ptok, ctok, finish, err
	}
	// Validate impact / target_field values; coerce unknown strings
	// to safe defaults so the frontend never has to handle nulls.
	for i := range body.Suggestions {
		body.Suggestions[i].Impact = normaliseImpact(body.Suggestions[i].Impact)
		body.Suggestions[i].TargetField = normaliseTargetField(body.Suggestions[i].TargetField)
	}
	return body, model, ptok, ctok, finish, nil
}

func normaliseImpact(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return "high"
	case "med", "medium":
		return "med"
	default:
		return "low"
	}
}

func normaliseTargetField(s string) string {
	if strings.ToLower(strings.TrimSpace(s)) == "ac" {
		return "ac"
	}
	return "notes"
}
