// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-171. Detect duplicates / similar issues.
//
// v1 uses LLM-based ranking over titles + 200-char description
// summaries — no embeddings store yet. Trade-offs:
//   + zero new infrastructure, ships next to the other actions.
//   - cost scales linearly with project size; ~200 issues per
//     project is the practical limit before token budgets pinch.
//   - quality is decent for moderate semantic match, less precise
//     than a real embedding cosine.
//
// We can revisit with embeddings (PAI-30 entity_embeddings table is
// already there) once the action's actually used in anger.

package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const detectDuplicatesMaxIssues = 200

func init() {
	replaceAction(actionDescriptor{
		Key:         "detect_duplicates",
		Label:       "Detect duplicates",
		Surface:     "issue",
		Handler:     detectDuplicatesHandler,
		Implemented: true,
	})
}

type duplicatesBody struct {
	Matches   []duplicateMatch `json:"matches"`
	Truncated bool             `json:"truncated"`
}

type duplicateMatch struct {
	IssueKey   string `json:"issue_key"`
	Title      string `json:"title"`
	Similarity string `json:"similarity"` // high | med | low
	Rationale  string `json:"rationale"`
}

func detectDuplicatesHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if ax.IssueID == 0 {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "detect_duplicates needs an existing issue"}
	}

	rows, projectName, truncated, err := loadProjectIssueTree(ax.Ctx, ax.IssueID, detectDuplicatesMaxIssues, true)
	if err != nil {
		return nil, "", 0, 0, "", fmt.Errorf("load tree: %w", err)
	}
	if len(rows) == 0 {
		return duplicatesBody{Matches: nil}, "", 0, 0, "", nil
	}

	tree := renderIssueTree(rows, true)

	systemPrompt := resolveActionPrompt("detect_duplicates")

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
	u.WriteString("\nProject issue list (JSON array, key/type/title/parent_id/summary):\n")
	u.WriteString(tree)
	u.WriteString("\n\nReturn the top 5 most-similar matches per the schema.")

	ctx, cancel := context.WithTimeout(ax.Ctx, 60*time.Second)
	defer cancel()
	var body duplicatesBody
	model, ptok, ctok, finish, err := callJSONAction(ctx, ax, systemPrompt, u.String(), 1500, &body)
	if err != nil {
		return nil, model, ptok, ctok, finish, err
	}
	body.Truncated = truncated
	for i := range body.Matches {
		body.Matches[i].Similarity = normaliseConfidence(body.Matches[i].Similarity)
	}
	if len(body.Matches) > 5 {
		body.Matches = body.Matches[:5]
	}
	return body, model, ptok, ctok, finish, nil
}
