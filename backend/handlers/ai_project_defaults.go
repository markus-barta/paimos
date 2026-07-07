// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

type projectAIConfig struct {
	Defaults models.ProjectAIDefaults
	Policy   models.ProjectAIPolicy
}

func loadProjectAIConfig(projectID int64) projectAIConfig {
	if projectID <= 0 || db.DB == nil {
		return projectAIConfig{}
	}
	var defaultsRaw, policyRaw string
	if err := db.DB.QueryRow(
		`SELECT COALESCE(ai_defaults_json, ''), COALESCE(ai_policy_json, '') FROM projects WHERE id=?`,
		projectID,
	).Scan(&defaultsRaw, &policyRaw); err != nil && err != sql.ErrNoRows {
		return projectAIConfig{}
	}
	cfg := projectAIConfig{}
	if strings.TrimSpace(defaultsRaw) != "" {
		_ = json.Unmarshal([]byte(defaultsRaw), &cfg.Defaults)
	}
	if strings.TrimSpace(policyRaw) != "" {
		_ = json.Unmarshal([]byte(policyRaw), &cfg.Policy)
	}
	return cfg
}

func projectAIDefaultSetFor(cfg projectAIConfig, actionKey, agentName string, run bool) models.ProjectAIDefaultSet {
	set := cfg.Defaults.Global
	actionKey = strings.TrimSpace(actionKey)
	if actionKey != "" {
		if actionSet, ok := cfg.Defaults.Actions[actionKey]; ok {
			set = mergeProjectAIDefaultSet(set, actionSet)
		}
		if run {
			if runSet, ok := cfg.Defaults.Runs[actionKey]; ok {
				set = mergeProjectAIDefaultSet(set, runSet)
			}
		}
	}
	agentName = strings.TrimSpace(agentName)
	if agentName != "" {
		if agentSet, ok := cfg.Defaults.Agents[agentName]; ok {
			set = mergeProjectAIDefaultSet(set, agentSet)
		}
	}
	return set
}

func mergeProjectAIDefaultSet(base, overlay models.ProjectAIDefaultSet) models.ProjectAIDefaultSet {
	if strings.TrimSpace(overlay.ProfileID) != "" {
		base.ProfileID = overlay.ProfileID
	}
	if strings.TrimSpace(overlay.Effort) != "" {
		base.Effort = overlay.Effort
	}
	if strings.TrimSpace(overlay.PromptPresetRef) != "" {
		base.PromptPresetRef = overlay.PromptPresetRef
	}
	if strings.TrimSpace(overlay.ContextPack) != "" {
		base.ContextPack = overlay.ContextPack
	}
	if strings.TrimSpace(overlay.PreferredProviderClass) != "" {
		base.PreferredProviderClass = overlay.PreferredProviderClass
	}
	return base
}

func projectAIDefaultSetHasValues(set models.ProjectAIDefaultSet) bool {
	return !projectAIDefaultSetEmpty(set)
}

func projectAIPolicyDisablesAction(policy models.ProjectAIPolicy, action agentRunAction) bool {
	return (policy.DisableHostedDraft && action.ProviderKind == "hosted_model") ||
		(policy.DisableLocalModelDraft && action.ProviderKind == "local_model")
}
