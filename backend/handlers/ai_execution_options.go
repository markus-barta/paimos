// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/models"
	"github.com/markus-barta/paimos/backend/sse"
)

type aiExecutionOptionsResponse struct {
	Profiles             []aiExecutionProfile              `json:"profiles"`
	Efforts              []string                          `json:"efforts"`
	ActionDefaults       map[string]aiActionDefaultOptions `json:"action_defaults"`
	SelectorDefaults     aiSelectorDefaults                `json:"selector_defaults"`
	PromptPresets        []aiPromptPresetChoice            `json:"prompt_presets,omitempty"`
	KnowledgeSuggestions []aiKnowledgeSuggestion           `json:"knowledge_suggestions,omitempty"`
	ContextPacks         []aiContextPackChoice             `json:"context_packs"`
	RunProviders         []sse.ActionCapability            `json:"run_providers,omitempty"`
	ProjectPolicy        projectAIPolicyResponse           `json:"project_policy,omitempty"`
}

type aiSelectorDefaults struct {
	Actions   map[string]aiSelectorDefault `json:"actions"`
	Runs      map[string]aiSelectorDefault `json:"runs"`
	RowLaunch aiSelectorDefault            `json:"row_launch"`
	Workbench aiSelectorDefault            `json:"workbench"`
}

type aiSelectorDefault struct {
	ActionKey        string `json:"action_key,omitempty"`
	ProfileID        string `json:"profile_id"`
	ProfileLabel     string `json:"profile_label,omitempty"`
	Model            string `json:"model,omitempty"`
	Effort           string `json:"effort"`
	PromptPresetRef  string `json:"prompt_preset_ref"`
	ContextPack      string `json:"context_pack"`
	ContextPackLabel string `json:"context_pack_label,omitempty"`
	ProviderID       string `json:"provider_id,omitempty"`
	ProviderLabel    string `json:"provider_label,omitempty"`
	AgentName        string `json:"agent_name,omitempty"`
	Source           string `json:"source"`
}

type projectAIPolicyResponse struct {
	DisableHostedDraft     bool `json:"disable_hosted_draft,omitempty"`
	DisableLocalModelDraft bool `json:"disable_local_model_draft,omitempty"`
}

func AIExecutionOptions(w http.ResponseWriter, r *http.Request) {
	settings, err := LoadAISettings()
	if err != nil {
		log.Printf("ai_execution_options: load settings: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	defaults := make(map[string]aiActionDefaultOptions, len(actionRegistry))
	for key := range actionRegistry {
		defaults[key] = defaultAIActionOptionsFor(key)
	}

	efforts := make([]string, 0, len(supportedAIActionEfforts))
	for effort := range supportedAIActionEfforts {
		efforts = append(efforts, effort)
	}
	sort.Strings(efforts)

	projectID, ok := executionOptionsProjectID(w, r)
	if !ok {
		return
	}
	promptPresets := []aiPromptPresetChoice(nil)
	knowledgeSuggestions := []aiKnowledgeSuggestion(nil)
	aiConfig := projectAIConfig{}
	if projectID > 0 {
		if !auth.CanViewProject(r, projectID) {
			jsonError(w, "project not found", http.StatusNotFound)
			return
		}
		aiConfig = loadProjectAIConfig(projectID)
		var err error
		promptPresets, err = listProjectAIPromptPresets(projectID)
		if err != nil {
			log.Printf("ai_execution_options: prompt presets: %v", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		knowledgeSuggestions, err = listAIKnowledgeSuggestions(projectID)
		if err != nil {
			log.Printf("ai_execution_options: knowledge suggestions: %v", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	profiles := listAIExecutionProfiles(settings)
	contextPacks := listAIContextPackChoices(projectID)
	runProviders := listDraftRunProviderCapabilities(settings, aiConfig.Policy)

	jsonOK(w, aiExecutionOptionsResponse{
		Profiles:             profiles,
		Efforts:              efforts,
		ActionDefaults:       defaults,
		SelectorDefaults:     buildAISelectorDefaults(settings, profiles, contextPacks, runProviders, projectID, aiConfig),
		PromptPresets:        promptPresets,
		KnowledgeSuggestions: knowledgeSuggestions,
		ContextPacks:         contextPacks,
		RunProviders:         runProviders,
		ProjectPolicy: projectAIPolicyResponse{
			DisableHostedDraft:     aiConfig.Policy.DisableHostedDraft,
			DisableLocalModelDraft: aiConfig.Policy.DisableLocalModelDraft,
		},
	})
}

func buildAISelectorDefaults(settings AISettings, profiles []aiExecutionProfile, contextPacks []aiContextPackChoice, runProviders []sse.ActionCapability, projectID int64, aiConfig projectAIConfig) aiSelectorDefaults {
	actions := make(map[string]aiSelectorDefault, len(actionRegistry))
	for key := range actionRegistry {
		actions[key] = selectorDefaultForAction(settings, profiles, contextPacks, key, aiConfig)
	}

	defaultAgent := defaultProjectAgentName(projectID)
	runs := map[string]aiSelectorDefault{}
	for _, key := range sortedAgentRunActionKeys() {
		action, ok := resolveAgentRunAction(key)
		if !ok {
			continue
		}
		runs[key] = selectorDefaultForRun(settings, profiles, contextPacks, action, defaultAgent, runProviders, aiConfig)
	}

	rowLaunchAction, _ := resolveAgentRunAction(defaultRunActionKeyForProject(aiConfig, defaultAgent))
	rowLaunch := selectorDefaultForRun(settings, profiles, contextPacks, rowLaunchAction, defaultAgent, runProviders, aiConfig)
	workbench := rowLaunch

	return aiSelectorDefaults{
		Actions:   actions,
		Runs:      runs,
		RowLaunch: rowLaunch,
		Workbench: workbench,
	}
}

func selectorDefaultForAction(settings AISettings, profiles []aiExecutionProfile, contextPacks []aiContextPackChoice, actionKey string, aiConfig projectAIConfig) aiSelectorDefault {
	defaults := defaultAIActionOptionsFor(actionKey)
	projectDefaults := projectAIDefaultSetFor(aiConfig, actionKey, "", false)
	if strings.TrimSpace(projectDefaults.ProfileID) != "" {
		defaults.ProfileID = projectDefaults.ProfileID
	}
	if strings.TrimSpace(projectDefaults.Effort) != "" {
		defaults.Effort = projectDefaults.Effort
	}
	profile := profileFromList(profiles, defaults.ProfileID)
	promptRef := firstNonEmptyOption(projectDefaults.PromptPresetRef, aiPromptPresetDefaultRef)
	contextPack := selectorContextPack(projectDefaults.ContextPack, contextPacks)
	return aiSelectorDefault{
		ActionKey:        actionKey,
		ProfileID:        defaults.ProfileID,
		ProfileLabel:     profile.Label,
		Model:            strings.TrimSpace(settings.Model),
		Effort:           defaults.Effort,
		PromptPresetRef:  promptRef,
		ContextPack:      contextPack,
		ContextPackLabel: contextPackLabelFromList(contextPacks, contextPack),
		ProviderID:       strings.TrimSpace(settings.Provider),
		ProviderLabel:    providerLabel(strings.TrimSpace(settings.Provider)),
		Source:           selectorDefaultSourceFor(projectAIDefaultSetHasValues(projectDefaults), ""),
	}
}

func selectorDefaultForRun(settings AISettings, profiles []aiExecutionProfile, contextPacks []aiContextPackChoice, action agentRunAction, defaultAgent string, runProviders []sse.ActionCapability, aiConfig projectAIConfig) aiSelectorDefault {
	defaults := defaultAIActionOptionsFor(action.ActionKey)
	projectDefaults := projectAIDefaultSetFor(aiConfig, action.ActionKey, defaultAgent, true)
	if strings.TrimSpace(projectDefaults.ProfileID) != "" {
		defaults.ProfileID = projectDefaults.ProfileID
	}
	if strings.TrimSpace(projectDefaults.Effort) != "" {
		defaults.Effort = projectDefaults.Effort
	}
	profile := profileFromList(profiles, defaults.ProfileID)
	model := strings.TrimSpace(action.Model)
	if model == "" && action.RunMode == "draft" {
		model = firstProviderModel(runProviders, action.ActionKey)
	}
	if model == "" && action.RunMode == "draft" {
		model = strings.TrimSpace(settings.Model)
	}
	promptRef := firstNonEmptyOption(projectDefaults.PromptPresetRef, aiPromptPresetDefaultRef)
	contextPack := selectorContextPack(projectDefaults.ContextPack, contextPacks)
	return aiSelectorDefault{
		ActionKey:        action.ActionKey,
		ProfileID:        defaults.ProfileID,
		ProfileLabel:     profile.Label,
		Model:            model,
		Effort:           defaults.Effort,
		PromptPresetRef:  promptRef,
		ContextPack:      contextPack,
		ContextPackLabel: contextPackLabelFromList(contextPacks, contextPack),
		ProviderID:       action.ProviderID,
		ProviderLabel:    action.ProviderLabel,
		AgentName:        defaultAgent,
		Source:           selectorDefaultSourceFor(projectAIDefaultSetHasValues(projectDefaults), defaultAgent),
	}
}

func selectorContextPack(raw string, packs []aiContextPackChoice) string {
	pack := strings.TrimSpace(raw)
	if pack == "" {
		return aiContextPackIssue
	}
	canonical, ok := canonicalAIContextPack(pack)
	if !ok {
		return aiContextPackIssue
	}
	for _, available := range packs {
		if available.ID == canonical {
			return canonical
		}
	}
	return aiContextPackIssue
}

func defaultRunActionKeyForProject(aiConfig projectAIConfig, agentName string) string {
	preferred := projectAIDefaultSetFor(aiConfig, defaultAgentRunActionKey, agentName, true).PreferredProviderClass
	if preferred == "" {
		preferred = aiConfig.Defaults.Global.PreferredProviderClass
	}
	for _, key := range sortedAgentRunActionKeys() {
		action, ok := resolveAgentRunAction(key)
		if ok && action.ProviderKind == preferred && !projectAIPolicyDisablesAction(aiConfig.Policy, action) {
			return key
		}
	}
	return defaultAgentRunActionKey
}

func sortedAgentRunActionKeys() []string {
	keys := make([]string, 0, len(agentRunActions))
	for key := range agentRunActions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func profileFromList(profiles []aiExecutionProfile, id string) aiExecutionProfile {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile
		}
	}
	return aiExecutionProfile{ID: id, Label: id}
}

func contextPackLabelFromList(packs []aiContextPackChoice, id string) string {
	for _, pack := range packs {
		if pack.ID == id {
			return pack.Label
		}
	}
	return id
}

func firstProviderModel(providers []sse.ActionCapability, actionKey string) string {
	for _, provider := range providers {
		if provider.ActionKey == actionKey && len(provider.Models) > 0 {
			return provider.Models[0]
		}
	}
	return ""
}

func providerLabel(provider string) string {
	switch provider {
	case "openrouter":
		return "OpenRouter"
	case "local_model":
		return "Local model"
	case "":
		return ""
	default:
		return provider
	}
}

func defaultProjectAgentName(projectID int64) string {
	if projectID <= 0 {
		return ""
	}
	agents, err := loadProjectAgents(projectID)
	if err != nil || len(agents) != 1 {
		return ""
	}
	return agents[0].Name
}

func selectorDefaultSourceFor(hasProjectDefaults bool, agentName string) string {
	if hasProjectDefaults || strings.TrimSpace(agentName) != "" {
		return "project"
	}
	return "global"
}

func listDraftRunProviderCapabilities(settings AISettings, policy models.ProjectAIPolicy) []sse.ActionCapability {
	efforts := make([]string, 0, len(supportedAIActionEfforts))
	for effort := range supportedAIActionEfforts {
		efforts = append(efforts, effort)
	}
	sort.Strings(efforts)
	profileIDs := []string{"default", "fast", "balanced", "deep"}

	openRouterAction, _ := resolveAgentRunAction("openrouter_draft.implement")
	localAction, _ := resolveAgentRunAction("local_model_draft.implement")

	openRouter := agentActionCapability(openRouterAction, false, false)
	openRouter.RequiresRunner = false
	openRouter.ProfileIDs = profileIDs
	openRouter.Efforts = efforts
	if settings.Provider == "openrouter" && strings.TrimSpace(settings.Model) != "" {
		openRouter.Models = []string{strings.TrimSpace(settings.Model)}
	}
	if !(settings.Enabled && settings.Provider == "openrouter" && strings.TrimSpace(settings.APIKey) != "" && strings.TrimSpace(settings.Model) != "") {
		openRouter.Available = false
		openRouter.UnavailableReason = "OpenRouter draft mode needs enabled AI settings, a model, and an API key."
	}
	if projectAIPolicyDisablesAction(policy, openRouterAction) {
		openRouter.Available = false
		openRouter.UnavailableReason = "Disabled by project AI policy."
	}

	local := agentActionCapability(localAction, false, false)
	local.RequiresRunner = false
	local.ProfileIDs = profileIDs
	local.Efforts = efforts
	if settings.Provider == "local_model" {
		if strings.TrimSpace(settings.Model) != "" {
			local.Models = []string{strings.TrimSpace(settings.Model)}
		}
		if strings.TrimSpace(settings.BaseURL) != "" {
			local.EndpointLabel = safeEndpointLabel(settings.BaseURL)
		}
	}
	if !(settings.Enabled && settings.Provider == "local_model" && strings.TrimSpace(settings.BaseURL) != "" && strings.TrimSpace(settings.Model) != "") {
		local.Available = false
		local.UnavailableReason = "Local model draft mode needs enabled AI settings, a model, and an OpenAI-compatible local endpoint."
	}
	if projectAIPolicyDisablesAction(policy, localAction) {
		local.Available = false
		local.UnavailableReason = "Disabled by project AI policy."
	}

	return []sse.ActionCapability{openRouter, local}
}

func safeEndpointLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		raw = u.String()
	}
	if len(raw) > 80 {
		return raw[:77] + "..."
	}
	return raw
}

func executionOptionsProjectID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	q := r.URL.Query()
	if raw := q.Get("project_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			jsonError(w, "invalid project id", http.StatusBadRequest)
			return 0, false
		}
		return id, true
	}
	if raw := q.Get("issue_id"); raw != "" {
		issueID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || issueID < 0 {
			jsonError(w, "invalid issue id", http.StatusBadRequest)
			return 0, false
		}
		if issueID == 0 {
			return 0, true
		}
		projectID, err := issueProjectID(issueID)
		if err != nil || projectID <= 0 {
			return 0, true
		}
		return projectID, true
	}
	return 0, true
}
