// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"strings"

	"github.com/inspr-at/paimos/backend/models"
)

type aiExecutionProfile struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	Effort          string   `json:"effort"`
	SpeedLabel      string   `json:"speed_label"`
	CostLabel       string   `json:"cost_label"`
	CapabilityHints []string `json:"capability_hints,omitempty"`
}

type aiActionOptions struct {
	ProfileID       string `json:"profile_id,omitempty"`
	Profile         string `json:"profile,omitempty"`
	ModelProfile    string `json:"model_profile,omitempty"`
	ModelID         string `json:"model_id,omitempty"`
	Model           string `json:"model,omitempty"`
	Effort          string `json:"effort,omitempty"`
	PromptPreset    string `json:"prompt_preset,omitempty"`
	PromptPresetRef string `json:"prompt_preset_ref,omitempty"`
	ContextPack     string `json:"context_pack,omitempty"`
}

type resolvedAIActionOptions struct {
	ProfileID         string            `json:"profile_id"`
	Model             string            `json:"model"`
	Effort            string            `json:"effort"`
	PromptPresetRef   string            `json:"prompt_preset_ref"`
	PromptPresetLabel string            `json:"prompt_preset_label,omitempty"`
	ContextPack       string            `json:"context_pack"`
	ContextPackLabel  string            `json:"context_pack_label,omitempty"`
	ContextTruncated  bool              `json:"context_truncated,omitempty"`
	ContextSources    []aiContextSource `json:"context_sources,omitempty"`

	promptPresetBody  string
	promptPresetLabel string
	contextPackBody   string
}

func (o resolvedAIActionOptions) isZero() bool {
	return o.ProfileID == "" &&
		o.Model == "" &&
		o.Effort == "" &&
		o.PromptPresetRef == "" &&
		o.PromptPresetLabel == "" &&
		o.ContextPack == "" &&
		o.ContextPackLabel == "" &&
		!o.ContextTruncated &&
		len(o.ContextSources) == 0
}

func (o resolvedAIActionOptions) applyToAICallArgs(args *aiCallArgs) {
	args.ProfileID = o.ProfileID
	args.Effort = o.Effort
	args.PromptPresetRef = o.PromptPresetRef
	args.ContextPack = o.ContextPack
}

type aiActionDefaultOptions struct {
	ProfileID string `json:"profile_id"`
	Effort    string `json:"effort"`
}

var supportedAIActionEfforts = map[string]struct{}{
	"low":      {},
	"standard": {},
	"deep":     {},
}

var aiActionDefaults = map[string]aiActionDefaultOptions{
	"optimize":                    {ProfileID: "fast", Effort: "low"},
	"optimize_customer":           {ProfileID: "fast", Effort: "low"},
	"translate":                   {ProfileID: "fast", Effort: "low"},
	"tone_check":                  {ProfileID: "fast", Effort: "low"},
	"customer_rewrite":            {ProfileID: "balanced", Effort: "standard"},
	"exec_summary":                {ProfileID: "balanced", Effort: "standard"},
	"suggest_enhancement":         {ProfileID: "balanced", Effort: "standard"},
	"spec_out":                    {ProfileID: "balanced", Effort: "standard"},
	"ui_generation":               {ProfileID: "balanced", Effort: "standard"},
	"estimate_effort":             {ProfileID: "balanced", Effort: "standard"},
	"generate_subtasks":           {ProfileID: "balanced", Effort: "standard"},
	"find_parent":                 {ProfileID: "deep", Effort: "deep"},
	"detect_duplicates":           {ProfileID: "deep", Effort: "deep"},
	"openrouter_draft.implement":  {ProfileID: "balanced", Effort: "standard"},
	"local_model_draft.implement": {ProfileID: "balanced", Effort: "standard"},
}

func defaultAIActionOptionsFor(actionKey string) aiActionDefaultOptions {
	if d, ok := aiActionDefaults[actionKey]; ok {
		return d
	}
	return aiActionDefaultOptions{ProfileID: "default", Effort: "standard"}
}

func listAIExecutionProfiles(settings AISettings) []aiExecutionProfile {
	provider := strings.TrimSpace(settings.Provider)
	model := strings.TrimSpace(settings.Model)
	if provider == "" {
		provider = "openrouter"
	}
	return []aiExecutionProfile{
		{
			ID:              "default",
			Label:           "Default",
			Provider:        provider,
			Model:           model,
			Effort:          "standard",
			SpeedLabel:      "Admin default",
			CostLabel:       "Admin default",
			CapabilityHints: []string{"Uses the current Settings AI model."},
		},
		{
			ID:              "fast",
			Label:           "Fast",
			Provider:        provider,
			Model:           model,
			Effort:          "low",
			SpeedLabel:      "Faster",
			CostLabel:       "Lower cost",
			CapabilityHints: []string{"Good for short rewrites and small text assists."},
		},
		{
			ID:              "balanced",
			Label:           "Balanced",
			Provider:        provider,
			Model:           model,
			Effort:          "standard",
			SpeedLabel:      "Balanced",
			CostLabel:       "Balanced",
			CapabilityHints: []string{"Recommended default for most issue work."},
		},
		{
			ID:              "deep",
			Label:           "Deep",
			Provider:        provider,
			Model:           model,
			Effort:          "deep",
			SpeedLabel:      "Slower",
			CostLabel:       "Higher cost",
			CapabilityHints: []string{"Better for duplicate detection, parent choice, and synthesis."},
		},
	}
}

func resolveAIActionOptions(settings AISettings, actionKey string, opts aiActionOptions, projectID *int64) (resolvedAIActionOptions, *userError) {
	if projectID != nil && *projectID > 0 {
		opts = applyProjectAIDefaultsToOptions(opts, projectAIDefaultSetFor(loadProjectAIConfig(*projectID), actionKey, "", false))
	}
	defaults := defaultAIActionOptionsFor(actionKey)
	resolved := resolvedAIActionOptions{
		ProfileID:       defaults.ProfileID,
		Model:           strings.TrimSpace(settings.Model),
		Effort:          defaults.Effort,
		PromptPresetRef: aiPromptPresetDefaultRef,
		ContextPack:     "issue",
	}

	if profile := firstNonEmptyOption(opts.ProfileID, opts.ModelProfile, opts.Profile); profile != "" {
		profile = strings.ToLower(strings.TrimSpace(profile))
		if _, ok := profileByID(settings, profile); !ok {
			return resolved, &userError{status: 400, msg: "profile is not available for AI actions"}
		}
		resolved.ProfileID = profile
		resolved.Effort = profileByIDMust(settings, profile).Effort
	}

	if model := firstNonEmptyOption(opts.ModelID, opts.Model); model != "" {
		model = strings.TrimSpace(model)
		if model != resolved.Model {
			return resolved, &userError{status: 400, msg: "model is not available for AI actions"}
		}
		resolved.Model = model
	}

	if effort := strings.TrimSpace(opts.Effort); effort != "" {
		effort = strings.ToLower(effort)
		if _, ok := supportedAIActionEfforts[effort]; !ok {
			return resolved, &userError{status: 400, msg: "effort is not supported by the current AI action resolver"}
		}
		resolved.Effort = effort
	}

	if prompt := firstNonEmptyOption(opts.PromptPresetRef, opts.PromptPreset); prompt != "" {
		prompt = strings.TrimSpace(prompt)
		if prompt != aiPromptPresetDefaultRef {
			preset, err := resolveProjectAIPromptPreset(projectID, actionKey, prompt)
			if err != nil {
				return resolved, err
			}
			resolved.PromptPresetRef = preset.Ref + "@" + preset.Revision
			resolved.PromptPresetLabel = preset.Label
			resolved.promptPresetBody = preset.Body
			resolved.promptPresetLabel = preset.Label
		} else {
			resolved.PromptPresetRef = prompt
		}
	}

	if contextPack := strings.TrimSpace(opts.ContextPack); contextPack != "" {
		canonical, ok := canonicalAIContextPack(contextPack)
		if !ok {
			return resolved, &userError{status: 400, msg: "context pack is not available for AI actions"}
		}
		if err := validateAIContextPack(canonical, projectID); err != nil {
			return resolved, err
		}
		resolved.ContextPack = canonical
		if canonical != aiContextPackIssue {
			resolved.ContextPackLabel = aiContextPackChoiceFor(canonical).Label
		}
	}

	return resolved, nil
}

func applyProjectAIDefaultsToOptions(opts aiActionOptions, defaults models.ProjectAIDefaultSet) aiActionOptions {
	if strings.TrimSpace(opts.ProfileID) == "" && strings.TrimSpace(opts.Profile) == "" && strings.TrimSpace(opts.ModelProfile) == "" {
		opts.ProfileID = defaults.ProfileID
	}
	if strings.TrimSpace(opts.Effort) == "" {
		opts.Effort = defaults.Effort
	}
	if strings.TrimSpace(opts.PromptPresetRef) == "" && strings.TrimSpace(opts.PromptPreset) == "" {
		opts.PromptPresetRef = defaults.PromptPresetRef
	}
	if strings.TrimSpace(opts.ContextPack) == "" {
		opts.ContextPack = defaults.ContextPack
	}
	return opts
}

func profileByID(settings AISettings, id string) (aiExecutionProfile, bool) {
	for _, profile := range listAIExecutionProfiles(settings) {
		if profile.ID == id {
			return profile, true
		}
	}
	return aiExecutionProfile{}, false
}

func profileByIDMust(settings AISettings, id string) aiExecutionProfile {
	profile, _ := profileByID(settings, id)
	return profile
}

func firstNonEmptyOption(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
