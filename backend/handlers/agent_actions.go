// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"strings"

	"github.com/markus-barta/paimos/backend/sse"
)

type agentRunAction struct {
	ActionKey     string
	ProviderKind  string
	ProviderID    string
	ProviderLabel string
	Model         string
	RunMode       string
}

const defaultAgentRunActionKey = "claude_cli.implement"

var agentRunActions = map[string]agentRunAction{
	defaultAgentRunActionKey: {
		ActionKey:     defaultAgentRunActionKey,
		ProviderKind:  "local_cli",
		ProviderID:    "claude_cli",
		ProviderLabel: "Claude Code",
		RunMode:       "edit",
	},
	"codex_cli.implement": {
		ActionKey:     "codex_cli.implement",
		ProviderKind:  "local_cli",
		ProviderID:    "codex_cli",
		ProviderLabel: "Codex CLI",
		RunMode:       "edit",
	},
	"openrouter_draft.implement": {
		ActionKey:     "openrouter_draft.implement",
		ProviderKind:  "hosted_model",
		ProviderID:    "openrouter",
		ProviderLabel: "OpenRouter Draft",
		RunMode:       "draft",
	},
	"local_model_draft.implement": {
		ActionKey:     "local_model_draft.implement",
		ProviderKind:  "local_model",
		ProviderID:    "local_model",
		ProviderLabel: "Local Model Draft",
		RunMode:       "draft",
	},
}

func resolveAgentRunAction(actionKey string) (agentRunAction, bool) {
	key := strings.TrimSpace(actionKey)
	if key == "" {
		key = defaultAgentRunActionKey
	}
	action, ok := agentRunActions[key]
	return action, ok
}

func agentActionCapability(action agentRunAction, canTest, canDeploy bool) sse.ActionCapability {
	return sse.ActionCapability{
		ActionKey:      action.ActionKey,
		ProviderKind:   action.ProviderKind,
		ProviderID:     action.ProviderID,
		Label:          action.ProviderLabel,
		RunModes:       []string{action.RunMode},
		CanTest:        canTest,
		CanDeploy:      canDeploy,
		Available:      true,
		RequiresRunner: action.ProviderKind == "local_cli",
	}
}

func defaultAgentActionCapability() sse.ActionCapability {
	action, _ := resolveAgentRunAction("")
	return agentActionCapability(action, true, false)
}

func presenceActions(p sse.Presence) []sse.ActionCapability {
	if len(p.Actions) > 0 {
		return p.Actions
	}
	if p.CanImplement {
		return []sse.ActionCapability{defaultAgentActionCapability()}
	}
	return nil
}

func actionCapabilitiesContain(actions []sse.ActionCapability, actionKey string) bool {
	for _, action := range actions {
		if action.ActionKey == actionKey && actionCapabilityAvailable(action) {
			return true
		}
	}
	return false
}

func isDraftAgentRunAction(action agentRunAction) bool {
	return action.RunMode == "draft"
}

func actionCapabilityAvailable(action sse.ActionCapability) bool {
	return action.Available || strings.TrimSpace(action.UnavailableReason) == ""
}
