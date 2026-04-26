// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-168. Translate DE ↔ EN — outputs rewritten text into the
// existing diff overlay UX so the user reviews before applying.
//
// Sub-actions: "de_en" (German source → English) and "en_de"
// (English source → German). The handler tells the model the
// expected source/target explicitly rather than asking it to
// auto-detect — auto-detection on a 3-line note often picks the
// wrong language. The frontend may surface a soft warning when
// the source language doesn't match the user's selection, but
// the call still goes through.

package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/markus-barta/paimos/backend/ai"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "translate",
		Label:       "Translate",
		Surface:     "issue",
		Placement:   "text",
		Handler:     translateHandler,
		SubKeys:     []string{"de_en", "en_de"},
		Implemented: true,
	})
}

// translateBody is the response shape. Keyed `optimized` so the
// existing diff overlay UX (PAI-148) can render it without changes —
// the overlay only cares about source-text vs new-text, regardless
// of whether the new text is a translation or a rewrite.
type translateBody struct {
	Optimized string `json:"optimized"`
}

func translateHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if strings.TrimSpace(ax.Text) == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "translate requires non-empty text"}
	}
	src, tgt, err := languagesFor(ax.SubAction)
	if err != nil {
		return nil, "", 0, 0, "", err
	}

	// PAI-178: resolved prompt + per-call source/target language
	// substitution. The default constant talks about
	// "SOURCE / TARGET languages given in the user prompt"; we
	// pass them in the user prompt so the system prompt stays
	// admin-editable without language-specific clutter.
	systemPrompt := resolveActionPrompt("translate")
	userPrompt := fmt.Sprintf("Translate from %s to %s:\n\n%s", src, tgt, ax.Text)

	ctx, cancel := context.WithTimeout(ax.Ctx, optimizeRequestTimeout)
	defer cancel()
	resp, err := ax.Provider.Optimize(ctx, ai.OptimizeRequest{
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
	return translateBody{Optimized: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}

func languagesFor(sub string) (src, tgt string, err error) {
	switch sub {
	case "de_en":
		return "German", "English", nil
	case "en_de":
		return "English", "German", nil
	default:
		return "", "", fmt.Errorf("unknown translate sub_action %q", sub)
	}
}
