// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-167. The "find parent / sibling" action.
//
// Given the current issue, scan the project's issue tree (titles +
// types + parent_ids) and suggest the top 3 plausible parent
// candidates with rationale + confidence. The frontend renders 1-3
// candidate cards; "Move under" updates parent_id after a confirm.
//
// Cap: 200 issues per project. A 200-issue list serialised to JSON
// runs ~12 KB — comfortably below any model's prompt budget while
// still covering most real projects without truncation.

package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const findParentMaxIssues = 200

func init() {
	replaceAction(actionDescriptor{
		Key:         "find_parent",
		Label:       "Find parent / sibling",
		Surface:     "issue",
		Handler:     findParentHandler,
		Implemented: true,
	})
}

type findParentBody struct {
	Candidates []findParentCandidate `json:"candidates"`
	Truncated  bool                  `json:"truncated"`
}

type findParentCandidate struct {
	IssueKey   string `json:"issue_key"`
	Title      string `json:"title"`
	Rationale  string `json:"rationale"`
	Confidence string `json:"confidence"` // high | med | low
}

func findParentHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if ax.IssueID == 0 {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "find_parent requires an existing issue"}
	}
	if strings.ToLower(ax.IssueData.IssueType) == "epic" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "epics have no parent in PAIMOS"}
	}

	rows, projectName, truncated, err := loadProjectIssueTree(ax.Ctx, ax.IssueID, findParentMaxIssues, false)
	if err != nil {
		return nil, "", 0, 0, "", fmt.Errorf("load tree: %w", err)
	}
	if len(rows) == 0 {
		// Empty project — graceful "no candidates" return rather
		// than calling the model.
		return findParentBody{Candidates: nil, Truncated: false}, "", 0, 0, "", nil
	}

	tree := renderIssueTree(rows, false)

	systemPrompt := `You are a senior engineer triaging an issue inside PAIMOS, a project-management tool. Given the current issue and the project's issue tree, suggest the TOP 3 plausible parent candidates. A "candidate" must be an issue (epic, ticket, or task) the current issue would naturally fit under.

Selection rules:
  - The current issue cannot parent itself or its own descendants. Don't suggest those.
  - Match by topic, scope, and naming similarity. Prefer parents whose title or type makes the current issue read as a natural sub-item.
  - "Confidence" should be honest:
      "high" — same topic, very strong match
      "med"  — same area, plausible match
      "low"  — weak / speculative
  - Return AT MOST 3 candidates. Fewer is fine when only 1-2 are plausible. Empty list is fine when nothing fits.

Schema: {"candidates":[{"issue_key":"...","title":"...","rationale":"...","confidence":"high|med|low"}]}`

	var u strings.Builder
	if ax.IssueData.IssueKey != "" {
		fmt.Fprintf(&u, "Current issue: %s", ax.IssueData.IssueKey)
		if ax.IssueData.IssueType != "" {
			fmt.Fprintf(&u, " (%s)", ax.IssueData.IssueType)
		}
		fmt.Fprintln(&u)
	}
	if ax.IssueData.IssueTitle != "" {
		fmt.Fprintf(&u, "Title: %s\n", ax.IssueData.IssueTitle)
	}
	if projectName != "" {
		fmt.Fprintf(&u, "Project: %s\n", projectName)
	}
	if ax.Text != "" {
		fmt.Fprintf(&u, "\nField content (%s):\n%s\n", ax.IssueData.FieldName, ax.Text)
	}
	u.WriteString("\nProject issue tree (JSON array, key/type/title/parent_id):\n")
	u.WriteString(tree)
	u.WriteString("\n\nReturn the top 3 plausible parent candidates per the schema.")

	ctx, cancel := context.WithTimeout(ax.Ctx, 60*time.Second)
	defer cancel()
	var body findParentBody
	model, ptok, ctok, finish, err := callJSONAction(ctx, ax, systemPrompt, u.String(), 1500, &body)
	if err != nil {
		// A model that flatly refuses to parse the tree is not a
		// hard failure — return an empty-candidates body so the
		// frontend can show "no obvious parent" without bubbling
		// a 502 to the user.
		if errors.Is(err, errParseFallthrough) {
			return findParentBody{Candidates: nil, Truncated: truncated}, model, ptok, ctok, finish, nil
		}
		return nil, model, ptok, ctok, finish, err
	}
	body.Truncated = truncated
	for i := range body.Candidates {
		body.Candidates[i].Confidence = normaliseConfidence(body.Candidates[i].Confidence)
	}
	if len(body.Candidates) > 3 {
		body.Candidates = body.Candidates[:3]
	}
	return body, model, ptok, ctok, finish, nil
}

func normaliseConfidence(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return "high"
	case "med", "medium":
		return "med"
	default:
		return "low"
	}
}

// errParseFallthrough is a sentinel for "the model returned no
// usable candidates, but the call itself succeeded". Reserved for
// future use; today's parser never emits it.
var errParseFallthrough = errors.New("ai_action: parse fallthrough")
