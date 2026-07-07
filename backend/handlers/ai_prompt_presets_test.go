// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func TestResolveAIActionOptionsUsesProjectKnowledgePromptPresetSafely(t *testing.T) {
	teardown := withTempDB(t)
	defer teardown()

	projectID := seedAIPromptPresetProject(t, "AIPP")
	seedAIPromptPreset(t, projectID, "memory", "spec_writer", "Spec Writer", "Use crisp acceptance criteria.", "backlog", map[string]any{
		"ai_prompt_preset": map[string]any{
			"enabled": true,
			"label":   "Spec Writer",
			"status":  "active",
			"actions": []any{"spec_out"},
		},
	})

	got, err := resolveAIActionOptions(
		AISettings{Provider: "openrouter", Model: "test/model"},
		"spec_out",
		aiActionOptions{PromptPresetRef: "kb:memory:spec_writer"},
		&projectID,
	)
	if err != nil {
		t.Fatalf("resolve prompt preset: %v", err)
	}
	if !strings.HasPrefix(got.PromptPresetRef, "kb:memory:spec_writer@") {
		t.Fatalf("prompt preset ref = %q, want ref with revision", got.PromptPresetRef)
	}
	if got.promptPresetBody != "Use crisp acceptance criteria." {
		t.Fatalf("internal prompt body not resolved: %q", got.promptPresetBody)
	}

	wire, jerr := json.Marshal(got)
	if jerr != nil {
		t.Fatalf("marshal resolved options: %v", jerr)
	}
	if strings.Contains(string(wire), "Use crisp acceptance criteria") {
		t.Fatalf("prompt body leaked into options JSON: %s", string(wire))
	}

	composed := applyAIPromptPreset("BASE PROMPT", &aiActionContext{Options: got})
	if !strings.Contains(composed, "BASE PROMPT") || !strings.Contains(composed, "Use crisp acceptance criteria.") {
		t.Fatalf("composed prompt missing base or preset body: %q", composed)
	}
}

func TestResolveAIActionOptionsRejectsUnavailableKnowledgePromptPresets(t *testing.T) {
	teardown := withTempDB(t)
	defer teardown()

	projectID := seedAIPromptPresetProject(t, "AIPR")
	seedAIPromptPreset(t, projectID, "memory", "estimate_only", "Estimate Only", "Estimate prompt.", "backlog", map[string]any{
		"ai_prompt_preset": map[string]any{
			"enabled": true,
			"status":  "active",
			"actions": []any{"estimate_effort"},
		},
	})
	seedAIPromptPreset(t, projectID, "runbook", "archived_prompt", "Archived", "Archived prompt.", "cancelled", map[string]any{
		"ai_prompt_preset": map[string]any{
			"enabled": true,
			"status":  "active",
			"actions": []any{"spec_out"},
		},
	})
	seedAIPromptPreset(t, projectID, "guideline", "draft_prompt", "Draft", "Draft prompt.", "backlog", map[string]any{
		"ai_prompt_preset": map[string]any{
			"enabled": true,
			"status":  "draft",
			"actions": []any{"spec_out"},
		},
	})

	for _, ref := range []string{
		"kb:memory:estimate_only",
		"kb:runbook:archived_prompt",
		"kb:guideline:draft_prompt",
		"kb:external_system:not_allowed",
	} {
		t.Run(ref, func(t *testing.T) {
			_, err := resolveAIActionOptions(
				AISettings{Provider: "openrouter", Model: "test/model"},
				"spec_out",
				aiActionOptions{PromptPresetRef: ref},
				&projectID,
			)
			if err == nil {
				t.Fatal("expected prompt preset rejection")
			}
			if !strings.Contains(err.msg, "prompt preset is not available") {
				t.Fatalf("error msg=%q", err.msg)
			}
		})
	}
}

func seedAIPromptPreset(t *testing.T, projectID int64, typ, slug, title, body, status string, meta map[string]any) {
	t.Helper()
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	var next int64
	if err := db.DB.QueryRow(`SELECT COALESCE(MAX(issue_number), 0) + 1 FROM issues WHERE project_id=?`, projectID).Scan(&next); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	if _, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, description, status, priority, slug, category_metadata, updated_at, content_revised_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
	`, projectID, next, typ, title, body, status, "medium", slug, string(raw), "2026-07-07 12:00:00", "2026-07-07 12:00:00"); err != nil {
		t.Fatalf("insert prompt preset %s/%s: %v", typ, slug, err)
	}
}

func seedAIPromptPresetProject(t *testing.T, key string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO projects(name, key, status) VALUES(?, ?, 'active')`, key+" Project", key)
	if err != nil {
		t.Fatalf("insert project %s: %v", key, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("project id %s: %v", key, err)
	}
	return id
}
