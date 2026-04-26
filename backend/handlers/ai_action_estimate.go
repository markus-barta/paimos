// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-170. Estimate effort — outputs hours + license points (LP)
// with one-line reasoning.
//
// Why both hours AND LP?
// PAIMOS surfaces both numbers because customers price work in
// LP while internal planning happens in hours. The relationship is
// not always 1:1 — some teams' LP-rate is 8h/LP, some 6h. The
// model just emits both; the apply step writes both fields.

package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "estimate_effort",
		Label:       "Estimate effort (h + LP)",
		Surface:     "issue",
		Placement:   "issue", // operates on the whole issue (sets fields)
		Handler:     estimateEffortHandler,
		Implemented: true,
	})
}

type estimateBody struct {
	Hours     float64 `json:"hours"`
	LP        float64 `json:"lp"`
	Reasoning string  `json:"reasoning"`
}

func estimateEffortHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if ax.IssueData.IssueTitle == "" && ax.Text == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "estimate_effort needs at least a title or description"}
	}

	systemPrompt := resolveActionPrompt("estimate_effort")

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
		fmt.Fprintf(&u, "\n%s:\n%s\n", ax.IssueData.FieldName, ax.Text)
	}
	u.WriteString("\nReturn the estimate per the schema.")

	ctx, cancel := context.WithTimeout(ax.Ctx, 30*time.Second)
	defer cancel()
	var body estimateBody
	model, ptok, ctok, finish, err := callJSONAction(ctx, ax, systemPrompt, u.String(), 400, &body)
	if err != nil {
		return nil, model, ptok, ctok, finish, err
	}
	// Clamp values: defensive against models that hallucinate
	// negative or absurd numbers ("estimate: -2.5 hours").
	if body.Hours < 0 {
		body.Hours = 0
	}
	if body.LP < 0 {
		body.LP = 0
	}
	if body.Hours == 0 && body.LP == 0 {
		body.Hours = 0.5
	}
	return body, model, ptok, ctok, finish, nil
}
