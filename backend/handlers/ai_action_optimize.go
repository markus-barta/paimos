// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-164. The "optimize wording" action — port of the v1.x
// /api/ai/optimize handler into the new dispatcher (PAI-163).
//
// What changes from PAI-146
// -------------------------
//   - Same prompt structure (PAI-150 4-layer wrapper).
//   - Same diff overlay UX on the frontend.
//   - Same per-field reminders.
//   - Audit verb shifts from `ai_optimize` to `ai_action action=optimize`,
//     so dashboards built on the old line need a one-line update; this
//     is documented in CHANGELOG. The legacy /api/ai/optimize route
//     stays alive as a thin compatibility shim for one release.

package handlers

import (
	"context"
	"strings"

	"github.com/markus-barta/paimos/backend/ai"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "optimize",
		Label:       "Optimize wording",
		Surface:     "issue",
		Placement:   "text",
		Handler:     optimizeActionHandler,
		Implemented: true,
	})
	// The customer-surface variant of optimize re-uses the same
	// handler logic — the per-field reminders in ai/prompt.go are
	// keyed on Field, so the customer-fields path drops out from
	// allowedActionFields naturally. We register a separate
	// descriptor for the customer surface so the frontend menu
	// can list it under that surface.
	registerAction(actionDescriptor{
		Key:         "optimize_customer",
		Label:       "Optimize wording",
		Surface:     "customer",
		Placement:   "text",
		Handler:     optimizeActionHandler,
		Implemented: true,
	})
}

// optimizeActionResponse is the shape rendered by the diff overlay.
// Identical to the v1 optimizeResponse so the SPA needs no changes
// to the overlay rendering path.
type optimizeActionResponse struct {
	Optimized string `json:"optimized"`
}

// optimizeActionHandler is the real handler that replaces the stub
// registered in ai_action_registry.go.
func optimizeActionHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	// Field is required for the optimize action; the dispatcher
	// already enforced the allow-list when a non-empty field was
	// present, but we additionally require it here so the prompt
	// gets a per-field reminder rather than a generic one.
	if ax.Field == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "optimize action requires a field"}
	}
	if ax.Text == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "text must not be empty"}
	}
	if len(ax.Text) > optimizeMaxInputBytes {
		return nil, "", 0, 0, "", &userError{status: 413, msg: "text too large"}
	}

	// PAI-178: resolve the system prompt from ai_prompts (admin
	// override, falling back to the constant default in
	// ai_action_prompts.go). This used to call
	// ai.BuildSystemPrompt(settings.OptimizeInstruction) which
	// composed the wrapper + admin instruction; for compatibility
	// we still invoke that path when the live prompt is the
	// canonical default and an admin instruction exists.
	systemPrompt := resolveActionPrompt("optimize")
	if systemPrompt == optimizeDefaultPrompt && strings.TrimSpace(ax.Settings.OptimizeInstruction) != "" {
		systemPrompt = ai.BuildSystemPrompt(ax.Settings.OptimizeInstruction)
	}
	userPrompt := ai.BuildUserPrompt(ax.Text, ax.IssueData)

	callCtx, cancel := context.WithTimeout(ax.Ctx, optimizeRequestTimeout)
	defer cancel()
	resp, err := ax.Provider.Optimize(callCtx, ai.OptimizeRequest{
		Model:           ax.Settings.Model,
		APIKey:          ax.Settings.APIKey,
		SystemPrompt:    systemPrompt,
		UserPrompt:      userPrompt,
		MaxOutputTokens: optimizeMaxOutputTokens,
	})
	if err != nil {
		return nil, "", 0, 0, "", err
	}
	cleaned := ai.StripFenceEcho(resp.Text)
	return optimizeActionResponse{Optimized: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}
