// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-163. Stub registration for every action key the multi-action
// dropdown (PAI-162) needs to show in the menu.
//
// Each action has its own implementation file (ai_action_<key>.go)
// that overrides the stub by calling registerAction() with
// Implemented: true. Registration order is irrelevant — the last
// registerAction() call wins because the registry-map insertion
// is the single source of truth, but in practice the action files'
// init() funcs always run after this file's init() (alphabetical
// init order within the same package), so the stubs are quietly
// replaced.
//
// Why stub-first
// --------------
// Building the menu UI without all 9 actions implemented would mean
// hard-coding a "what's available" list in the frontend that goes
// stale every time we ship a new action. Registering stubs for the
// full set lets the menu render from /api/ai/actions on every
// page load and stay in sync with what the backend actually serves.

package handlers

import "log"

// init registers a stub for every action key in the PAI-162 menu
// catalogue. Real handlers replace these via re-registration in
// their own init() (registerAction panics on dup, so each action
// file's init() should call replaceAction() — added below).
func init() {
	stubs := []actionDescriptor{
		// Issue editor surface
		{Key: "optimize", Label: "Optimize wording", Surface: "issue", Handler: stubHandler},
		{Key: "suggest_enhancement", Label: "Suggest enhancement", Surface: "issue",
			Handler: stubHandler,
			SubKeys: []string{"security", "performance", "ux", "dx", "flow", "risks"}},
		{Key: "spec_out", Label: "Spec-out (description → AC)", Surface: "issue", Handler: stubHandler},
		{Key: "find_parent", Label: "Find parent / sibling", Surface: "issue", Handler: stubHandler},
		{Key: "translate", Label: "Translate", Surface: "issue",
			Handler: stubHandler,
			SubKeys: []string{"de_en", "en_de"}},
		{Key: "generate_subtasks", Label: "Generate sub-tasks", Surface: "issue", Handler: stubHandler},
		{Key: "estimate_effort", Label: "Estimate effort", Surface: "issue", Handler: stubHandler},
		{Key: "detect_duplicates", Label: "Detect duplicates", Surface: "issue", Handler: stubHandler},
		{Key: "ui_generation", Label: "UI generation", Surface: "issue", Handler: stubHandler},

		// Customer surface (PAI-173)
		{Key: "tone_check", Label: "Tone check", Surface: "customer", Handler: stubHandler},
	}
	for _, d := range stubs {
		registerAction(d)
	}
}

// replaceAction overrides an existing registry entry, or creates a
// fresh one if no stub had registered first. Lenient by design: Go
// orders init() alphabetically by file name, and a strict
// "stub-must-exist" check would couple the stubs file's name to
// every action file. Last-write-wins keeps the registration
// semantics order-independent.
//
// We log a console line if there was no stub: the catalog endpoint
// relies on stubs to surface "Coming soon" items in the menu, so a
// real handler that lands without one means the menu never showed
// the item as planned — usually a typo on the key.
func replaceAction(d actionDescriptor) {
	if _, ok := actionRegistry[d.Key]; !ok {
		log.Printf("ai_action: replaceAction registered new key %q with no prior stub", d.Key)
	}
	actionRegistry[d.Key] = d
}
