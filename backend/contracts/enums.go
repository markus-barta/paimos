// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package contracts

// Canonical enum domains shared by schema publication and handler-side
// validation paths that cannot import the root handlers package.
var (
	IssueStatuses     = []string{"new", "backlog", "in-progress", "qa", "done", "delivered", "accepted", "invoiced", "cancelled"}
	KnowledgeStatuses = []string{"new", "backlog", "proposed", "in-progress", "qa", "done", "delivered", "accepted", "invoiced", "cancelled"}
	IssuePriorities   = []string{"low", "medium", "high"}
	IssueTypes        = []string{"epic", "cost_unit", "release", "sprint", "ticket", "task"}
	// "parent" (PAI-584) is the issue-hierarchy edge (epic⊃ticket, ticket⊃task)
	// and the SSOT for parentage; "groups" is now only cost_unit/release
	// container membership (epic→ticket via groups is auto-translated to parent).
	RelationTypes = []string{"parent", "groups", "sprint", "depends_on", "impacts", "follows_from", "blocks", "related", "applies_to_memory"}
)

func Contains(values []string, raw string) bool {
	for _, value := range values {
		if raw == value {
			return true
		}
	}
	return false
}
