// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package claudecode

import (
	"strings"
	"testing"
)

// fixtureBon26 is a representative BON26 / ops canonical artifact, the
// shape PAI-329 emits. Stays inline so the adapter test is self-contained.
const fixtureBon26 = `{
  "project": {"id": 6, "name": "Bonelio 2026", "key": "BON26"},
  "agent": {
    "name": "ops",
    "description": "Infra, deploys, secrets, runtime.",
    "slash_command_name": "ops",
    "lane_tags": ["ops", "infra"],
    "metadata": {"deploy_recipes_used": ["backend-staging"]},
    "body": "## What ops owns\n\nDeployments, secrets, environment probes.",
    "bootstrap_steps": [
      {"title": "Source ops env", "command": "source ~/Secrets/ops.env", "rationale": "loads non-prod creds"},
      {"title": "Probe staging", "command": "curl -sf https://stg.example.com/healthz", "rationale": "confirm reachability"}
    ],
    "non_negotiable_rules": [
      {"title": "No prod writes without PR", "body": "Always go through CI.", "memory_ref": "feedback_no_silent_prod_writes"},
      {"title": "Don't skip pre-deploy probe", "body": "", "memory_ref": ""}
    ]
  },
  "repos": [
    {"label": "bonelio26-backend", "url": "https://github.com/example/bonelio26-backend", "default_branch": "main"}
  ],
  "environments": [
    {"name": "staging", "url": "https://stg.example.com", "host_alias": "ops-staging", "host_ip": "10.0.0.5"}
  ],
  "deploy_recipes": [
    {"name": "backend-staging", "command": "ssh ops-staging 'docker pull img && systemctl reload stack'", "summary": "Reload backend on staging"}
  ]
}`

// fixtureFreshProject is a totally different project shape — used for
// the "no Bonelio assumptions" acceptance test.
const fixtureFreshProject = `{
  "project": {"id": 42, "name": "Acme Widgets", "key": "ACME"},
  "agent": {
    "name": "qa",
    "description": "Test the widgets.",
    "slash_command_name": "qa",
    "lane_tags": ["qa"],
    "body": "Run the end-to-end suite before sign-off.",
    "bootstrap_steps": [
      {"title": "Bootstrap suite", "command": "npm run e2e:bootstrap", "rationale": ""}
    ],
    "non_negotiable_rules": []
  },
  "repos": [],
  "environments": [],
  "deploy_recipes": []
}`

// fixtureSparseAgent: PAI-326-era agent with no body / steps / rules.
// Empty sections must be skipped (graceful degrade).
const fixtureSparseAgent = `{
  "project": {"id": 7, "name": "Tiny", "key": "TINY"},
  "agent": {
    "name": "s",
    "description": "",
    "slash_command_name": "",
    "lane_tags": [],
    "body": "",
    "bootstrap_steps": [],
    "non_negotiable_rules": []
  },
  "repos": [],
  "environments": [],
  "deploy_recipes": []
}`

func TestAdapter_RegistryFields(t *testing.T) {
	a := New()
	if a.Name() != "claude-code" {
		t.Fatalf("name: got %q", a.Name())
	}
	if a.Version() == "" {
		t.Fatal("version empty")
	}
	if a.Supports() != ">=1.0.0 <2.0.0" {
		t.Fatalf("supports: got %q", a.Supports())
	}
	if a.Describe() == "" {
		t.Fatal("describe empty")
	}
}

// TestAdapter_RenderBon26 verifies the Bon26 canonical artifact
// produces a recognisable Claude-Code skill (matches the look-and-feel
// the ticket calls out).
func TestAdapter_RenderBon26(t *testing.T) {
	a := New()
	res, err := a.Render([]byte(fixtureBon26))
	if err != nil {
		t.Fatal(err)
	}

	// Adapter must not embed the paimos header itself — that's the
	// dispatcher's job. PAI-331 relies on injectHeader as the single
	// source of truth for the format.
	if strings.HasPrefix(res.Content, "<!-- paimos: rendered from") {
		t.Fatal("adapter must not emit the dispatcher header")
	}

	mustContain := []string{
		"You are operating as the **ops session** for Bonelio 2026 (PMO project **BON26**)",
		"## Your lane",
		"Infra, deploys, secrets, runtime.",
		"## Bootstrap",
		"Source ops env",
		"source ~/Secrets/ops.env",
		"## Non-negotiable rules",
		"No prod writes without PR",
		"feedback_no_silent_prod_writes",
		"## Deploy cheat sheet",
		"backend-staging",
		"## Free body",
		"What ops owns",
		"bonelio26-backend",
		"staging",
		"ops-staging",
	}
	for _, s := range mustContain {
		if !strings.Contains(res.Content, s) {
			t.Fatalf("rendered output missing %q\n--- output ---\n%s", s, res.Content)
		}
	}

	if res.SuggestedPath != ".claude/commands/ops.md" {
		t.Fatalf("suggested path: got %q want .claude/commands/ops.md", res.SuggestedPath)
	}
}

// TestAdapter_NoBonelioAssumptions: render against a fresh project
// with different agents → succeeds with no hardcoded "BON26"/"Bonelio"
// strings. AC requirement.
func TestAdapter_NoBonelioAssumptions(t *testing.T) {
	a := New()
	res, err := a.Render([]byte(fixtureFreshProject))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Content, "BON26") || strings.Contains(res.Content, "Bonelio") {
		t.Fatalf("output must not contain Bonelio-specific strings:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "Acme Widgets") || !strings.Contains(res.Content, "ACME") {
		t.Fatalf("output should reference fresh project values:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "Bootstrap") {
		t.Fatal("non-empty section should still render")
	}
	if strings.Contains(res.Content, "## Non-negotiable rules") {
		t.Fatal("empty rules section should be skipped (graceful degrade)")
	}
	if strings.Contains(res.Content, "## Deploy cheat sheet") {
		t.Fatal("empty recipes section should be skipped")
	}
	if res.SuggestedPath != ".claude/commands/qa.md" {
		t.Fatalf("suggested path: got %q", res.SuggestedPath)
	}
}

// TestAdapter_GracefulDegradeOnSparseAgent: a PAI-326-era agent with
// no PAI-329 fields renders the preamble and nothing else (no ## Bootstrap
// heading hanging without content).
func TestAdapter_GracefulDegradeOnSparseAgent(t *testing.T) {
	a := New()
	res, err := a.Render([]byte(fixtureSparseAgent))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "TINY") {
		t.Fatalf("preamble missing project key:\n%s", res.Content)
	}
	for _, heading := range []string{"## Bootstrap", "## Non-negotiable rules", "## Deploy cheat sheet", "## Free body", "## Your lane"} {
		if strings.Contains(res.Content, heading) {
			t.Fatalf("sparse agent should skip empty %s heading:\n%s", heading, res.Content)
		}
	}
}

// TestAdapter_MissingNameRejected: an artifact without agent.name is a
// hard error — the dispatch wrapper should not have to second-guess.
func TestAdapter_MissingNameRejected(t *testing.T) {
	a := New()
	_, err := a.Render([]byte(`{"project":{"key":"X"},"agent":{}}`))
	if err == nil {
		t.Fatal("expected error for missing agent.name")
	}
}

// TestAdapter_MalformedJSONRejected
func TestAdapter_MalformedJSONRejected(t *testing.T) {
	a := New()
	_, err := a.Render([]byte(`not json`))
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// TestAdapter_SuggestedPathFallsBackToName: when slash_command_name is
// absent the path falls back to agent.name.
func TestAdapter_SuggestedPathFallsBackToName(t *testing.T) {
	a := New()
	const noSlash = `{
		"project": {"key": "X", "name": "X"},
		"agent": {"name": "weird", "slash_command_name": ""}
	}`
	res, err := a.Render([]byte(noSlash))
	if err != nil {
		t.Fatal(err)
	}
	if res.SuggestedPath != ".claude/commands/weird.md" {
		t.Fatalf("got %q want .claude/commands/weird.md", res.SuggestedPath)
	}
}
