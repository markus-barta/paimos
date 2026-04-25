// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-172. UI generation — produces a textual UI spec in markdown.
//
// Out of scope for v1: rendered mockups, image generation,
// Figma export. The output is a markdown spec covering layout,
// components, states (default / loading / error / empty), keyboard
// nav, accessibility, and microcopy. The user can append to notes
// or replace the description with the spec.

package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/ai"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "ui_generation",
		Label:       "UI generation (textual spec)",
		Surface:     "issue",
		Handler:     uiGenerationHandler,
		Implemented: true,
	})
}

type uiGenBody struct {
	SpecMarkdown string `json:"spec_markdown"`
}

func uiGenerationHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if ax.IssueData.IssueTitle == "" && ax.Text == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "ui_generation needs a title or description"}
	}

	systemPrompt := `You are a senior product designer who writes implementation-ready UI specs in markdown. The issue below describes a feature or screen; produce a TEXTUAL ui spec a frontend engineer can hand to a designer or implement directly.

Spec sections (use these exact ## headings, in this order):
  ## Layout
  ## Components
  ## States
  ## Interactions & keyboard
  ## Accessibility
  ## Microcopy

Style rules:
  - Markdown only. No image links, no embedded HTML, no rendered mockups (text spec, not picture).
  - Reference concrete entities from the issue (table names, API endpoints, copy strings) verbatim where applicable.
  - The "States" section MUST cover at least: default, loading, error, empty. Add more states only if the issue calls for them.
  - "Microcopy" is short copy strings: button labels, error toasts, empty-state lines. Keep them short and natural.
  - DO NOT propose tech-stack choices ("use Vue 3 + Tailwind") — assume PAIMOS conventions and stay UI-shape-focused.
  - Total length: 30-80 lines. Aim for tight and useful, not exhaustive.

Return ONLY the markdown spec. No preamble, no explanation, no fences around the whole reply.`

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
	if ax.Text != "" {
		fmt.Fprintf(&u, "\n%s:\n%s\n", ax.IssueData.FieldName, ax.Text)
	}
	u.WriteString("\nProduce the UI spec as markdown.")

	ctx, cancel := context.WithTimeout(ax.Ctx, 90*time.Second)
	defer cancel()
	resp, err := ax.Provider.Optimize(ctx, ai.OptimizeRequest{
		Model:           ax.Settings.Model,
		APIKey:          ax.Settings.APIKey,
		SystemPrompt:    systemPrompt,
		UserPrompt:      u.String(),
		MaxOutputTokens: 4000,
	})
	if err != nil {
		return nil, "", 0, 0, "", err
	}
	cleaned := ai.StripFenceEcho(resp.Text)
	return uiGenBody{SpecMarkdown: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}
