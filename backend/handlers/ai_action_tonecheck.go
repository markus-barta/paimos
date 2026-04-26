// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-173. Tone check — neutralises sales-y / persuasive language
// in customer-bound text while preserving facts, names, dates,
// and verbatim quotes. Surfaces in the customer-side dropdown
// alongside Optimize wording.
//
// Available on:
//   - customer_notes
//   - cooperation_sla_details
//   - cooperation_notes

package handlers

import (
	"context"
	"strings"

	"github.com/markus-barta/paimos/backend/ai"
)

func init() {
	replaceAction(actionDescriptor{
		Key:         "tone_check",
		Label:       "Tone check (de-sales)",
		Surface:     "customer",
		Placement:   "text",
		Handler:     toneCheckHandler,
		Implemented: true,
	})
}

type toneCheckBody struct {
	Optimized string `json:"optimized"`
}

// toneCheckAllowedFields restricts tone-check to actual customer-
// bound fields. The dispatcher already gates on allowedActionFields,
// but we additionally narrow here so a misconfigured menu (e.g.
// surface="customer" pointing at a generic field) doesn't apply
// the "remove sales language" lens to issue notes.
var toneCheckAllowedFields = map[string]bool{
	"customer_notes":          true,
	"cooperation_sla_details": true,
	"cooperation_notes":       true,
}

func toneCheckHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	if !toneCheckAllowedFields[ax.Field] {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "tone_check is only available on customer-side fields"}
	}
	if strings.TrimSpace(ax.Text) == "" {
		return nil, "", 0, 0, "", &userError{status: 400, msg: "tone_check requires non-empty text"}
	}

	systemPrompt := resolveActionPrompt("tone_check")

	var u strings.Builder
	u.WriteString("Field: ")
	u.WriteString(ax.Field)
	u.WriteString("\n\nSource text:\n")
	u.WriteString(ax.Text)
	u.WriteString("\n\nReturn the de-salesed rewrite (or the source unchanged if already neutral).")

	ctx, cancel := context.WithTimeout(ax.Ctx, optimizeRequestTimeout)
	defer cancel()
	resp, err := ax.Provider.Optimize(ctx, ai.OptimizeRequest{
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
	return toneCheckBody{Optimized: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
}
