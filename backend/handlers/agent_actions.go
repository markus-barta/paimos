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
		ActionKey:    action.ActionKey,
		ProviderKind: action.ProviderKind,
		ProviderID:   action.ProviderID,
		Label:        action.ProviderLabel,
		RunModes:     []string{action.RunMode},
		CanTest:      canTest,
		CanDeploy:    canDeploy,
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
		if action.ActionKey == actionKey {
			return true
		}
	}
	return false
}
