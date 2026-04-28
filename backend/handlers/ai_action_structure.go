// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-252 follow-up. Backend handlers for the "Structure with AI"
// actions exposed by the project-manifest tabbed editor (frontend
// shipped in v2.1.16; Dev + Ops tabs added alongside this handler).
//
//   structure_manifest    — ProjectManifestTabs.vue "Manifest" tab
//   structure_guardrails  — ProjectManifestTabs.vue "Guardrails" tab
//   structure_glossary    — ProjectManifestTabs.vue "Glossary" tab
//   structure_dev         — ProjectManifestTabs.vue "Dev" tab
//   structure_ops         — ProjectManifestTabs.vue "Ops" tab
//
// Shape: each takes free-form prose pasted by the user, asks the
// configured LLM to emit a JSON object that fits the tab's schema,
// and returns the text through the same diff overlay the optimize
// action uses. The frontend pretty-prints the candidate before
// dropping it into the tab's draft on accept.
//
// Why one file for three actions
// ------------------------------
// All three share the same handler shape (input prose → JSON object
// scoped to one slice). Only the system prompt changes per action,
// which the resolver already keys on the action's registry key.
// Three near-identical files would be more code without buying any
// clarity.

package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/markus-barta/paimos/backend/ai"
)

// structureMaxInputBytes mirrors optimizeMaxInputBytes — these tabs
// hold the same kind of human-authored multiline content and the
// upstream provider has the same per-call ceiling either way.
const structureMaxInputBytes = optimizeMaxInputBytes

func init() {
	for _, key := range []string{
		"structure_manifest",
		"structure_guardrails",
		"structure_glossary",
		"structure_dev",
		"structure_ops",
	} {
		k := key // closure capture
		replaceAction(actionDescriptor{
			Key:         k,
			Label:       structureActionLabel(k),
			Surface:     "issue",
			Placement:   "text",
			Handler:     makeStructureHandler(k),
			Implemented: true,
		})
	}
}

// structureActionLabel keeps the catalog labels in one place rather
// than duplicating them between the registry stub and the real
// descriptor.
func structureActionLabel(key string) string {
	switch key {
	case "structure_manifest":
		return "Structure project manifest"
	case "structure_guardrails":
		return "Structure project guardrails"
	case "structure_glossary":
		return "Structure project glossary"
	case "structure_dev":
		return "Structure project dev rules"
	case "structure_ops":
		return "Structure project ops rules"
	}
	return key
}

// makeStructureHandler returns an actionHandler bound to one of the
// three structure_* keys. The handler closure captures the key so
// it can resolve the right system prompt at request time.
func makeStructureHandler(actionKey string) actionHandler {
	return func(ax *aiActionContext) (any, string, int, int, string, error) {
		text := strings.TrimSpace(ax.Text)
		if text == "" {
			return nil, "", 0, 0, "", &userError{status: 400, msg: "text must not be empty"}
		}
		if len(text) > structureMaxInputBytes {
			return nil, "", 0, 0, "", &userError{status: 413, msg: "text too large"}
		}

		systemPrompt := resolveActionPrompt(actionKey)
		if strings.TrimSpace(systemPrompt) == "" {
			// Defensive: every action's key is in builtinDefaultPrompts,
			// but if a future refactor drops one, fail loudly rather
			// than ship an empty prompt to the provider.
			return nil, "", 0, 0, "", fmt.Errorf("no system prompt configured for %s", actionKey)
		}

		userPrompt := buildStructureUserPrompt(actionKey, text, ax.IssueData.ProjectName)

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

		// Reuse the optimize response shape so the diff overlay reads
		// the candidate from the same `body.optimized` field. The
		// frontend's formatJsonCandidate helper pretty-prints valid
		// JSON and falls back to the raw text otherwise, so a model
		// that leaks a fence or a stray sentence still surfaces
		// usefully — the user just rejects and retries.
		cleaned := ai.StripFenceEcho(resp.Text)
		return optimizeActionResponse{Optimized: cleaned}, resp.Model, resp.PromptTokens, resp.CompletionTokens, resp.FinishReason, nil
	}
}

// buildStructureUserPrompt is the structure_* counterpart to
// ai.BuildUserPrompt. It does the same thing — frame the source
// text in a way the model can't confuse with instructions — but
// the framing is "convert prose to JSON" rather than "rewrite
// this field", and it includes the slice name so the model
// knows which schema to emit.
func buildStructureUserPrompt(actionKey, source, projectName string) string {
	var b strings.Builder

	switch actionKey {
	case "structure_manifest":
		b.WriteString("Convert the following project description into a project-manifest JSON object.\n\n")
	case "structure_guardrails":
		b.WriteString("Convert the following description of project rules into a guardrails JSON object.\n\n")
	case "structure_glossary":
		b.WriteString("Convert the following description of project terms into a glossary JSON object.\n\n")
	case "structure_dev":
		b.WriteString("Convert the following description of development workflow rules into a dev-rules JSON object.\n\n")
	case "structure_ops":
		b.WriteString("Convert the following description of operations rules into an ops-rules JSON object.\n\n")
	default:
		b.WriteString("Convert the following text into the JSON shape described in the system prompt.\n\n")
	}

	if projectName != "" {
		b.WriteString(fmt.Sprintf("- Project: %s\n", projectName))
	}
	b.WriteString("\nIf the input is already valid JSON of the expected shape, normalise it (consistent formatting, sorted keys where natural) and return it. If the input is a mix of prose and partial JSON, merge the two into one JSON object — do not lose data.\n\n")

	b.WriteString("Source text (between the fences):\n")
	b.WriteString("```\n")
	b.WriteString(source)
	if !strings.HasSuffix(source, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n")
	b.WriteString("\nReturn ONLY the JSON object — no preamble, no explanation, no markdown fences around the reply.")

	return b.String()
}
