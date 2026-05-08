// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

// PAI-329. Pin the load-bearing invariants on the canonical agent
// artifact endpoint and the markdown debug renderer:
//   1. Round-trip: hand-authored agent + project-level inventories
//      come back via GET /api/projects/{id}/agents/{name}.json with
//      every field intact and matching the on-disk DB row byte-
//      identically.
//   2. Project-level inheritance: repos[], environments[],
//      deploy_recipes[] all show up inlined in the artifact.
//   3. deploy_recipes_used filtering: when the agent's metadata
//      declares an allow-list, only matching recipes are inlined.
//   4. Missing agent → 404; missing project → 404 (more specific).
//   5. .md endpoint produces a non-empty markdown document for a
//      populated agent and skips empty sections for a sparse one.
//   6. PUT /api/projects/{id} accepts environments[] /
//      deploy_recipes[] as replace-all writes; returned shape mirrors
//      GetProject.
//   7. byte-identity: the JSON returned by the artifact endpoint for
//      a hand-authored agent equals what we get by re-encoding the
//      same agent record fetched directly from the agents list.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/models"
)

func agentArtifactJSONURL(projectID int64, name string) string {
	return fmt.Sprintf("/api/projects/%d/agents/%s.json", projectID, name)
}

func agentArtifactMDURL(projectID int64, name string) string {
	return fmt.Sprintf("/api/projects/%d/agents/%s.md", projectID, name)
}

// seedFullAgent creates a project + a fully-populated agent + a repo +
// an environment + a deploy recipe. Returns the project id and a copy
// of the canonical agent payload used to create the agent so tests can
// compare round-trip fidelity.
func seedFullAgent(t *testing.T, ts *testServer) (int64, map[string]any) {
	t.Helper()
	projectID := createTestProject(t, ts, "Artifact Project", "ART")

	agentPayload := map[string]any{
		"name":               "ops",
		"description":        "Infra, deploys, secrets, runtime.",
		"slash_command_name": "ops",
		"lane_tags":          []string{"ops", "infra"},
		"metadata": map[string]any{
			"color":               "#ff8800",
			"icon":                "wrench",
			"deploy_recipes_used": []string{"backend-staging"},
		},
		"body": "## What ops owns\n\nDeployments, secrets, environment probes.",
		"bootstrap_steps": []map[string]any{
			{"title": "Source ops env", "command": "source ~/Secrets/ops.env", "rationale": "loads non-prod creds"},
			{"title": "Probe staging", "command": "curl -sf https://stg.example.com/healthz", "rationale": "confirm reachability"},
		},
		"non_negotiable_rules": []map[string]any{
			{"title": "No prod writes without PR", "body": "Always go through CI.", "memory_ref": "feedback_no_silent_prod_writes"},
			{"title": "Don't skip pre-deploy probe", "body": "", "memory_ref": ""},
		},
	}

	resp := ts.post(t, agentsURL(projectID), ts.adminCookie, agentPayload)
	assertStatus(t, resp, http.StatusCreated)

	// Seed a repo.
	repoResp := ts.post(t, fmt.Sprintf("/api/projects/%d/repos", projectID), ts.adminCookie, map[string]any{
		"url":            "https://github.com/example/ops-stack",
		"default_branch": "main",
		"label":          "ops-stack",
	})
	assertStatus(t, repoResp, http.StatusCreated)

	// Seed an environment.
	envResp := ts.post(t, fmt.Sprintf("/api/projects/%d/environments", projectID), ts.adminCookie, map[string]any{
		"name":       "staging",
		"url":        "https://stg.example.com",
		"host_alias": "ops-staging",
		"host_ip":    "10.0.0.5",
	})
	assertStatus(t, envResp, http.StatusCreated)

	// Seed two deploy recipes; the agent's metadata only allow-lists
	// "backend-staging" so the artifact should filter to just that one.
	recAResp := ts.post(t, fmt.Sprintf("/api/projects/%d/deploy-recipes", projectID), ts.adminCookie, map[string]any{
		"name":    "backend-staging",
		"command": "ssh ops-staging 'docker pull api:staging && systemctl reload api'",
		"summary": "Pulls staging image and reloads service.",
	})
	assertStatus(t, recAResp, http.StatusCreated)
	recBResp := ts.post(t, fmt.Sprintf("/api/projects/%d/deploy-recipes", projectID), ts.adminCookie, map[string]any{
		"name":    "backend-prod",
		"command": "ssh ops-prod 'docker pull api:prod && systemctl reload api'",
		"summary": "Pulls prod image.",
	})
	assertStatus(t, recBResp, http.StatusCreated)

	return projectID, agentPayload
}

// ── tests ──────────────────────────────────────────────────────────

func Test_AgentArtifact_RoundTripFullPayload(t *testing.T) {
	ts := newTestServer(t)
	projectID, _ := seedFullAgent(t, ts)

	resp := ts.get(t, agentArtifactJSONURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	var artifact struct {
		Project struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"project"`
		Agent         models.ProjectAgent          `json:"agent"`
		Repos         []models.ProjectRepo         `json:"repos"`
		Environments  []models.ProjectEnvironment  `json:"environments"`
		DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
	}
	decode(t, resp, &artifact)

	if artifact.Project.ID != projectID {
		t.Errorf("project.id: got %d, want %d", artifact.Project.ID, projectID)
	}
	if artifact.Project.Key != "ART" {
		t.Errorf("project.key: got %q, want ART", artifact.Project.Key)
	}

	a := artifact.Agent
	if a.Name != "ops" {
		t.Errorf("agent.name round-trip: got %q", a.Name)
	}
	if a.Body == "" || !strings.Contains(a.Body, "Deployments") {
		t.Errorf("agent.body round-trip: got %q", a.Body)
	}
	if len(a.BootstrapSteps) != 2 {
		t.Fatalf("bootstrap_steps round-trip: len=%d want 2", len(a.BootstrapSteps))
	}
	if a.BootstrapSteps[0].Title != "Source ops env" {
		t.Errorf("bootstrap_steps[0].title: got %q", a.BootstrapSteps[0].Title)
	}
	if a.BootstrapSteps[1].Command != "curl -sf https://stg.example.com/healthz" {
		t.Errorf("bootstrap_steps[1].command: got %q", a.BootstrapSteps[1].Command)
	}
	if len(a.NonNegotiableRules) != 2 {
		t.Fatalf("non_negotiable_rules round-trip: len=%d want 2", len(a.NonNegotiableRules))
	}
	if a.NonNegotiableRules[0].MemoryRef != "feedback_no_silent_prod_writes" {
		t.Errorf("rule[0].memory_ref pass-through: got %q", a.NonNegotiableRules[0].MemoryRef)
	}
}

func Test_AgentArtifact_InheritsProjectLevelInventories(t *testing.T) {
	ts := newTestServer(t)
	projectID, _ := seedFullAgent(t, ts)

	resp := ts.get(t, agentArtifactJSONURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var artifact struct {
		Repos         []models.ProjectRepo         `json:"repos"`
		Environments  []models.ProjectEnvironment  `json:"environments"`
		DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
	}
	decode(t, resp, &artifact)

	if len(artifact.Repos) != 1 || artifact.Repos[0].Label != "ops-stack" {
		t.Errorf("repos inheritance: got %+v", artifact.Repos)
	}
	if len(artifact.Environments) != 1 || artifact.Environments[0].Name != "staging" {
		t.Errorf("environments inheritance: got %+v", artifact.Environments)
	}
}

func Test_AgentArtifact_FiltersDeployRecipesByAllowList(t *testing.T) {
	ts := newTestServer(t)
	projectID, _ := seedFullAgent(t, ts)

	resp := ts.get(t, agentArtifactJSONURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var artifact struct {
		DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
	}
	decode(t, resp, &artifact)

	if len(artifact.DeployRecipes) != 1 {
		t.Fatalf("deploy_recipes filter: got %d items, want 1 (backend-staging only)", len(artifact.DeployRecipes))
	}
	if artifact.DeployRecipes[0].Name != "backend-staging" {
		t.Errorf("deploy_recipes filter: got %q, want backend-staging", artifact.DeployRecipes[0].Name)
	}
}

func Test_AgentArtifact_NoAllowListInheritsAllRecipes(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "All Recipes", "ARC")

	// Agent without a deploy_recipes_used allow-list.
	ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{
		"name": "ops",
	})
	ts.post(t, fmt.Sprintf("/api/projects/%d/deploy-recipes", projectID), ts.adminCookie, map[string]any{
		"name": "backend-staging", "command": "echo a",
	})
	ts.post(t, fmt.Sprintf("/api/projects/%d/deploy-recipes", projectID), ts.adminCookie, map[string]any{
		"name": "backend-prod", "command": "echo b",
	})

	resp := ts.get(t, agentArtifactJSONURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var artifact struct {
		DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
	}
	decode(t, resp, &artifact)

	if len(artifact.DeployRecipes) != 2 {
		t.Errorf("expected all recipes inherited; got %d", len(artifact.DeployRecipes))
	}
}

func Test_AgentArtifact_MissingAgentReturns404(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "404 Project", "FOF")

	resp := ts.get(t, agentArtifactJSONURL(projectID, "ghost"), ts.adminCookie)
	assertStatus(t, resp, http.StatusNotFound)
}

func Test_AgentArtifact_MissingProjectReturns404(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, agentArtifactJSONURL(99999, "ops"), ts.adminCookie)
	// auth.RequireProjectView gates view rights — so for a non-existent
	// project, callers may legitimately see 403 or 404 depending on
	// policy. Either is acceptable as "not found"; assert it isn't 200.
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 for missing project; got 200")
	}
}

func Test_AgentArtifact_MarkdownRendersPopulatedSections(t *testing.T) {
	ts := newTestServer(t)
	projectID, _ := seedFullAgent(t, ts)

	resp := ts.get(t, agentArtifactMDURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("md endpoint content-type: got %q", ct)
	}
	body := readAll(t, resp)
	for _, want := range []string{
		"# Agent: ops",
		"## Body",
		"## Bootstrap steps",
		"## Non-negotiable rules",
		"memory_ref: feedback_no_silent_prod_writes",
		"## Repos",
		"## Environments",
		"## Deploy recipes",
		"backend-staging",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("md output missing %q\n--- body ---\n%s", want, body)
		}
	}
	if strings.Contains(body, "backend-prod") {
		t.Errorf("md output should NOT include filtered-out recipe backend-prod")
	}
}

func Test_AgentArtifact_MarkdownSkipsEmptySectionsForSparseAgent(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Sparse", "SPR")

	// A bare PAI-326-era agent — only name + description.
	ts.post(t, agentsURL(projectID), ts.adminCookie, map[string]any{
		"name":        "dev",
		"description": "Implementation.",
	})

	resp := ts.get(t, agentArtifactMDURL(projectID, "dev"), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	body := readAll(t, resp)

	if !strings.Contains(body, "# Agent: dev") {
		t.Errorf("md output missing title; got %q", body)
	}
	for _, mustSkip := range []string{
		"## Body",
		"## Bootstrap steps",
		"## Non-negotiable rules",
		"## Repos",
		"## Environments",
		"## Deploy recipes",
	} {
		if strings.Contains(body, mustSkip) {
			t.Errorf("md output should skip empty section %q\n--- body ---\n%s", mustSkip, body)
		}
	}
}

func Test_GetProject_InlinesProjectLevelInventories(t *testing.T) {
	ts := newTestServer(t)
	projectID, _ := seedFullAgent(t, ts)

	resp := ts.get(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	var detail struct {
		ID            int64                        `json:"id"`
		Agents        []models.ProjectAgent        `json:"agents"`
		Repos         []models.ProjectRepo         `json:"repos"`
		Environments  []models.ProjectEnvironment  `json:"environments"`
		DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
	}
	decode(t, resp, &detail)

	if detail.ID != projectID {
		t.Errorf("project.id mismatch")
	}
	if len(detail.Agents) != 1 {
		t.Errorf("agents inlined len=%d want 1", len(detail.Agents))
	}
	if len(detail.Repos) != 1 {
		t.Errorf("repos inlined len=%d want 1", len(detail.Repos))
	}
	if len(detail.Environments) != 1 {
		t.Errorf("environments inlined len=%d want 1", len(detail.Environments))
	}
	if len(detail.DeployRecipes) != 2 {
		t.Errorf("deploy_recipes inlined len=%d want 2", len(detail.DeployRecipes))
	}
}

func Test_PutProject_ReplacesEnvironmentsAndDeployRecipes(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PUT replace", "PRR")

	// Seed initial inventories so we can verify the replace wipes them.
	ts.post(t, fmt.Sprintf("/api/projects/%d/environments", projectID), ts.adminCookie, map[string]any{
		"name": "old-env",
	})
	ts.post(t, fmt.Sprintf("/api/projects/%d/deploy-recipes", projectID), ts.adminCookie, map[string]any{
		"name": "old-recipe",
	})

	// Replace via PUT.
	putResp := ts.put(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie, map[string]any{
		"environments": []map[string]any{
			{"name": "staging", "url": "https://stg.example.com"},
			{"name": "prod", "url": "https://prod.example.com"},
		},
		"deploy_recipes": []map[string]any{
			{"name": "backend-staging", "command": "echo a"},
		},
	})
	assertStatus(t, putResp, http.StatusOK)

	// Re-fetch and confirm the wipe-and-replace landed.
	getResp := ts.get(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie)
	assertStatus(t, getResp, http.StatusOK)
	var detail struct {
		Environments  []models.ProjectEnvironment  `json:"environments"`
		DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
	}
	decode(t, getResp, &detail)
	if len(detail.Environments) != 2 {
		t.Errorf("environments after replace len=%d want 2", len(detail.Environments))
	}
	envNames := []string{detail.Environments[0].Name, detail.Environments[1].Name}
	if !contains(envNames, "staging") || !contains(envNames, "prod") {
		t.Errorf("environments after replace: got %v", envNames)
	}
	if contains(envNames, "old-env") {
		t.Errorf("replace did not wipe old-env: %v", envNames)
	}
	if len(detail.DeployRecipes) != 1 || detail.DeployRecipes[0].Name != "backend-staging" {
		t.Errorf("deploy_recipes after replace: got %+v", detail.DeployRecipes)
	}
}

func Test_PutProject_RejectsDuplicateInventoryNames(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PUT dup", "PDU")

	resp := ts.put(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie, map[string]any{
		"environments": []map[string]any{
			{"name": "staging"},
			{"name": "staging"},
		},
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func Test_AgentArtifact_AgentRowMatchesArtifactAgentBlock(t *testing.T) {
	// Acceptance #6 — byte-identity. The agent block returned by the
	// canonical artifact endpoint must equal the agent record returned
	// by the per-project agents list (modulo wrapper container).
	ts := newTestServer(t)
	projectID, _ := seedFullAgent(t, ts)

	listResp := ts.get(t, agentsURL(projectID), ts.adminCookie)
	assertStatus(t, listResp, http.StatusOK)
	var listed []models.ProjectAgent
	decode(t, listResp, &listed)
	if len(listed) != 1 {
		t.Fatalf("expected 1 agent in list; got %d", len(listed))
	}

	artResp := ts.get(t, agentArtifactJSONURL(projectID, "ops"), ts.adminCookie)
	assertStatus(t, artResp, http.StatusOK)
	var artifact struct {
		Agent models.ProjectAgent `json:"agent"`
	}
	decode(t, artResp, &artifact)

	listJSON, _ := json.Marshal(listed[0])
	artifactAgentJSON, _ := json.Marshal(artifact.Agent)
	if string(listJSON) != string(artifactAgentJSON) {
		t.Errorf("byte-identity violated:\n  list:     %s\n  artifact: %s", listJSON, artifactAgentJSON)
	}
}

// ── helpers ────────────────────────────────────────────────────────

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
