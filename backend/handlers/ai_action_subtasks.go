// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-169. Generate sub-tasks — proposes 3-7 child tickets/tasks
// that decompose the parent's work. The frontend renders the
// suggestions as an editable checklist; the user picks which to
// create, can edit titles inline, and the create runs as a
// best-effort batch (one creation failing does not roll back
// already-created siblings).

package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "generate_subtasks",
		Label:       "Generate sub-tasks",
		Surface:     "issue",
		Handler:     subtasksHandler,
		Implemented: true,
	})
}

type subtasksBody struct {
	Suggestions []subtaskSuggestion `json:"suggestions"`
}

type subtaskSuggestion struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"` // task | ticket
}

func subtasksHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	parentType := strings.ToLower(strings.TrimSpace(ax.IssueData.IssueType))
	if parentType != "epic" && parentType != "ticket" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "generate_subtasks works only on epic or ticket parents"}
	}
	if strings.TrimSpace(ax.Text) == "" && ax.IssueData.IssueTitle == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "generate_subtasks requires at least a title or a description"}
	}

	childType := "ticket"
	if parentType == "ticket" {
		childType = "task"
	}

	systemPrompt := fmt.Sprintf(`You are a senior engineer breaking down work for a software team using PAIMOS. The parent issue below is a %s. Suggest 3-7 child %ss that decompose its work.

Decomposition rules:
  - Each child is a self-contained piece of work with a clear deliverable. Avoid "do half of X" / "continue X".
  - Each child should be sequenceable: ideally pick-up-able by one engineer in 1-3 days.
  - Children must NOT add scope beyond what the parent describes. If the parent doesn't mention a thing, don't decompose into it.
  - Title: imperative verb-led, ≤ 80 chars ("Add CSP report endpoint", not "CSP report endpoint should exist").
  - Description: 1-3 sentences with concrete next steps; reference the parent's own entities verbatim where applicable.
  - Type: "%s" for every child (we know the parent type).

Schema: {"suggestions":[{"title":"...","description":"...","type":"%s"}]}`, parentType, childType, childType, childType)

	var u strings.Builder
	if ax.IssueData.IssueKey != "" {
		fmt.Fprintf(&u, "Parent issue: %s", ax.IssueData.IssueKey)
		if parentType != "" {
			fmt.Fprintf(&u, " (%s)", parentType)
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
		fmt.Fprintf(&u, "Grandparent epic: %s\n", ax.IssueData.ParentEpic)
	}
	if ax.Text != "" {
		fmt.Fprintf(&u, "\n%s field:\n%s\n", ax.IssueData.FieldName, ax.Text)
	}
	fmt.Fprintf(&u, "\nReturn 3-7 child %s suggestions per the schema.", childType)

	ctx, cancel := context.WithTimeout(ax.Ctx, 60*time.Second)
	defer cancel()
	var body subtasksBody
	model, ptok, ctok, finish, err := callJSONAction(ctx, ax, systemPrompt, u.String(), 2500, &body)
	if err != nil {
		return nil, model, ptok, ctok, finish, err
	}
	for i := range body.Suggestions {
		t := strings.ToLower(strings.TrimSpace(body.Suggestions[i].Type))
		if t != "task" && t != "ticket" {
			t = childType
		}
		body.Suggestions[i].Type = t
	}
	return body, model, ptok, ctok, finish, nil
}
