// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

// PAI-326. Pin the load-bearing invariants on /api/projects/:id/agents:
//   1. CRUD round-trips through the JSON layer with the right shape.
//   2. Empty projects return [] (not null), so consumers can iterate
//      without nil checks.
//   3. Validation rejects empty / over-length / pattern-mismatch names
//      and the reserved `web-ui` sentinel — server-side, not just UI.
//   4. (project_id, name) UNIQUE: duplicate POST returns 409.
//   5. PUT renames in place and respects the same uniqueness rule.
//   6. DELETE 204 on hit, 404 on miss.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/models"
)

func createTestProject(t *testing.T, ts *testServer, name, key string) int64 {
	t.Helper()
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": name,
		"key":  key,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project %q: status %d", name, resp.StatusCode)
	}
	return responseID(t, resp)
}

func agentsURL(projectID int64) string {
	return fmt.Sprintf("/api/projects/%d/agents", projectID)
}

func agentURL(projectID int64, name string) string {
	return fmt.Sprintf("/api/projects/%d/agents/%s", projectID, name)
}

// ── tests ───────────────────────────────────────────────────────────

func Test_ProjectAgents_EmptyProjectReturnsEmptyArray(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents Empty", "AGE")

	resp := ts.get(t, agentsURL(projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var out []models.ProjectAgent
	decode(t, resp, &out)
	if out == nil {
		t.Fatal("expected [] not null for projects without agents")
	}
	if len(out) != 0 {
		t.Fatalf("expected zero agents on fresh project; got %d", len(out))
	}
}

func Test_ProjectAgents_CRUDRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents CRUD", "ACR")

	// Create
	createResp := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{
		"name":               "ops",
		"description":        "Infrastructure, deploys, secrets, runtime.",
		"slash_command_name": "ops",
		"lane_tags":          []string{"ops", "infra"},
		"metadata":           map[string]any{"color": "#ff8800", "icon": "wrench"},
	})
	assertStatus(t, createResp, http.StatusCreated)
	var created models.ProjectAgent
	decode(t, createResp, &created)
	if created.Name != "ops" {
		t.Errorf("name round-trip: got %q", created.Name)
	}
	if len(created.LaneTags) != 2 || created.LaneTags[0] != "ops" {
		t.Errorf("lane_tags round-trip: got %v", created.LaneTags)
	}
	if created.Metadata["color"] != "#ff8800" {
		t.Errorf("metadata round-trip: got %v", created.Metadata)
	}

	// List shows the new agent
	listResp := ts.get(t, agentsURL(projectID), ts.adminCookie)
	assertStatus(t, listResp, http.StatusOK)
	var listed []models.ProjectAgent
	decode(t, listResp, &listed)
	if len(listed) != 1 {
		t.Fatalf("expected 1 agent in list; got %d", len(listed))
	}

	// Update — change description, drop one lane tag, swap metadata.
	updResp := ts.put(t, agentURL(projectID, "ops"), ts.adminCookie, map[string]any{
		"name":               "ops",
		"description":        "Infra only.",
		"slash_command_name": "ops",
		"lane_tags":          []string{"ops"},
		"metadata":           map[string]any{"color": "#0088ff"},
	})
	assertStatus(t, updResp, http.StatusOK)
	var updated models.ProjectAgent
	decode(t, updResp, &updated)
	if updated.Description != "Infra only." {
		t.Errorf("description not updated; got %q", updated.Description)
	}
	if len(updated.LaneTags) != 1 || updated.LaneTags[0] != "ops" {
		t.Errorf("lane_tags not updated; got %v", updated.LaneTags)
	}
	if updated.Metadata["color"] != "#0088ff" {
		t.Errorf("metadata not updated; got %v", updated.Metadata)
	}
	if updated.ID != created.ID {
		t.Errorf("PUT created a new row instead of updating; ids %d -> %d", created.ID, updated.ID)
	}

	// Delete
	delResp := ts.del(t, agentURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, delResp, http.StatusNoContent)

	// And it's gone
	listResp2 := ts.get(t, agentsURL(projectID), ts.adminCookie)
	assertStatus(t, listResp2, http.StatusOK)
	var afterDelete []models.ProjectAgent
	decode(t, listResp2, &afterDelete)
	if len(afterDelete) != 0 {
		t.Fatalf("expected 0 agents after delete; got %d", len(afterDelete))
	}
}

func Test_ProjectAgents_DuplicateNameReturns409(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents Dup", "ADP")

	first := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{
		"name": "dev",
	})
	assertStatus(t, first, http.StatusCreated)

	second := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{
		"name": "dev",
	})
	assertStatus(t, second, http.StatusConflict)
}

func Test_ProjectAgents_RejectsBadNames(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents Validation", "AVA")

	cases := []struct {
		label string
		name  string
		want  int
	}{
		{"empty", "", http.StatusBadRequest},
		{"uppercase", "Ops", http.StatusBadRequest},
		{"leading digit", "1ops", http.StatusBadRequest},
		{"space", "ops dev", http.StatusBadRequest},
		{"too long", strings.Repeat("a", 33), http.StatusBadRequest},
		{"reserved web-ui", "web-ui", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			resp := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{
				"name": tc.name,
			})
			assertStatus(t, resp, tc.want)
		})
	}

	// And the boundary case — exactly 32 chars passes.
	ok := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{
		"name": strings.Repeat("a", 32),
	})
	assertStatus(t, ok, http.StatusCreated)
}

func Test_ProjectAgents_PUTRenameRespectsUniqueness(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents Rename", "ARN")

	// Seed two agents.
	a := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{"name": "ops"})
	assertStatus(t, a, http.StatusCreated)
	b := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{"name": "dev"})
	assertStatus(t, b, http.StatusCreated)

	// Renaming "ops" → "dev" must fail with 409.
	collide := ts.put(t, agentURL(projectID, "ops"), ts.adminCookie, map[string]any{
		"name":        "dev",
		"description": "should not land",
	})
	assertStatus(t, collide, http.StatusConflict)

	// Renaming "ops" → "refinement" succeeds.
	rename := ts.put(t, agentURL(projectID, "ops"), ts.adminCookie, map[string]any{
		"name":        "refinement",
		"description": "ticket grooming",
	})
	assertStatus(t, rename, http.StatusOK)

	// "ops" no longer resolves; "refinement" does.
	gone := ts.del(t, agentURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, gone, http.StatusNotFound)
	hit := ts.del(t, agentURL(projectID, "refinement"), ts.adminCookie)
	assertStatus(t, hit, http.StatusNoContent)
}

func Test_ProjectAgents_PUTOnMissingReturns404(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents Missing", "AMS")

	resp := ts.put(t, agentURL(projectID, "ghost"), ts.adminCookie, map[string]any{
		"name":        "ghost",
		"description": "should not be created via PUT",
	})
	assertStatus(t, resp, http.StatusNotFound)
}

func Test_ProjectAgents_DELETEOnMissingReturns404(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents Delete404", "AD4")

	resp := ts.del(t, agentURL(projectID, "ghost"), ts.adminCookie)
	assertStatus(t, resp, http.StatusNotFound)
}

func Test_ProjectAgents_EmptyBodyReturns400(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Agents EmptyBody", "AEB")

	resp := ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusBadRequest)
}
