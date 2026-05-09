// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

// PAI-340 — bundle filter logic + format outputs + cache manifest
// behaviour. The tests deliberately exercise the filters as plain
// functions (no HTTP) so failure modes attribute to the filter rather
// than to the fake server, and a separate end-to-end test confirms
// the wiring (CLI → resolveBundle → emitter → cache).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ── filter logic ─────────────────────────────────────────────────

// TestFilterMemory_ScopeAndEnvironment pins the memory filter rules.
// The taxonomy lives in `category_metadata` (free-form JSON) so the
// filter must be tolerant of missing fields — see filterMemory's
// rationale comment for the "include unless explicitly excluded"
// principle.
func TestFilterMemory_ScopeAndEnvironment(t *testing.T) {
	currentUserID := int64(7)
	agentEnvs := []string{"prod"}

	entries := []knowledgeEntry{
		// 0 — project-scoped, no environment filter → always passes.
		{Slug: "fact-prod", Title: "fact prod", Status: "backlog",
			Metadata: map[string]any{"scope": "project"}},
		// 1 — implicit scope (missing field) treated as project.
		{Slug: "implicit", Title: "implicit", Status: "backlog",
			Metadata: map[string]any{}},
		// 2 — user-scoped, matches current user.
		{Slug: "mine", Title: "my notes", Status: "backlog",
			Metadata: map[string]any{
				"scope":   "user-on-this-project",
				"user_id": float64(7),
			}},
		// 3 — user-scoped, different user → DROPPED.
		{Slug: "yours", Title: "their notes", Status: "backlog",
			Metadata: map[string]any{
				"scope":   "user-on-this-project",
				"user_id": float64(99),
			}},
		// 4 — env-filtered, matches agent → passes.
		{Slug: "prod-rule", Title: "prod rule", Status: "backlog",
			Metadata: map[string]any{
				"scope":                    "project",
				"applies_to_environments":  []any{"prod", "staging"},
			}},
		// 5 — env-filtered, mismatched → DROPPED.
		{Slug: "staging-only", Title: "staging only", Status: "backlog",
			Metadata: map[string]any{
				"scope":                    "project",
				"applies_to_environments":  []any{"staging"},
			}},
		// 6 — archived (cancelled) → DROPPED.
		{Slug: "old", Title: "old", Status: "cancelled",
			Metadata: map[string]any{"scope": "project"}},
		// 7 — user-scoped, no user_id field → INCLUDE (best-effort
		// behaviour while PAI-339 wires the editor field).
		{Slug: "user-no-uid", Title: "user no uid", Status: "backlog",
			Metadata: map[string]any{"scope": "user-on-this-project"}},
	}

	got := filterMemory(entries, currentUserID, agentEnvs, true)
	gotSlugs := []string{}
	for _, e := range got {
		gotSlugs = append(gotSlugs, e.Slug)
	}

	want := []string{"fact-prod", "implicit", "mine", "prod-rule", "user-no-uid"}
	if !sameStringSet(gotSlugs, want) {
		t.Fatalf("got %v, want %v", gotSlugs, want)
	}
}

// TestFilterMemory_NoAgentEnvironments — when the agent has no
// environments declared, applies_to_environments on memory entries
// is ignored (universal entries pass; env-tagged entries also pass
// because there's nothing to mismatch against).
func TestFilterMemory_NoAgentEnvironments(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "any", Title: "any", Status: "backlog", Metadata: map[string]any{}},
		{Slug: "tagged", Title: "tagged", Status: "backlog",
			Metadata: map[string]any{
				"applies_to_environments": []any{"prod"},
			}},
	}
	got := filterMemory(entries, 0, nil, true)
	if len(got) != 2 {
		t.Fatalf("expected both entries with no agent envs, got %d (%v)", len(got), got)
	}
}

// TestFilterMemory_ConfidenceGate (PAI-347) — `low` is excluded by
// default; `--include-low` flips the gate. Missing / unknown
// confidence is treated as medium (backwards-compat).
func TestFilterMemory_ConfidenceGate(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "high-rule", Title: "high", Status: "backlog",
			Metadata: map[string]any{"confidence": "high"}},
		{Slug: "med-rule", Title: "med", Status: "backlog",
			Metadata: map[string]any{"confidence": "medium"}},
		{Slug: "low-rule", Title: "low", Status: "backlog",
			Metadata: map[string]any{"confidence": "low"}},
		// Missing confidence — defaults to medium.
		{Slug: "no-confidence", Title: "no", Status: "backlog",
			Metadata: map[string]any{}},
	}

	// Default gate (includeLow=false) drops only "low-rule".
	got := filterMemory(entries, 0, nil, false)
	gotSlugs := []string{}
	for _, e := range got {
		gotSlugs = append(gotSlugs, e.Slug)
	}
	want := []string{"high-rule", "med-rule", "no-confidence"}
	if !sameStringSet(gotSlugs, want) {
		t.Fatalf("default gate: got %v, want %v", gotSlugs, want)
	}

	// includeLow=true keeps everything.
	got = filterMemory(entries, 0, nil, true)
	if len(got) != 4 {
		t.Fatalf("includeLow: got %d entries, want 4", len(got))
	}
}

// TestFilterRunbooks_RelatedAgents — runbooks with `related_agents`
// must contain the current agent's name; runbooks without the field
// are universal and always pass (modulo archived).
func TestFilterRunbooks_RelatedAgents(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "ops-only", Title: "ops only", Status: "backlog",
			Metadata: map[string]any{"related_agents": []any{"ops"}}},
		{Slug: "qa-only", Title: "qa only", Status: "backlog",
			Metadata: map[string]any{"related_agents": []any{"qa"}}},
		{Slug: "shared", Title: "shared", Status: "backlog",
			Metadata: map[string]any{"related_agents": []any{"ops", "qa"}}},
		{Slug: "universal-empty", Title: "universal empty", Status: "backlog",
			Metadata: map[string]any{"related_agents": []any{}}},
		{Slug: "universal-missing", Title: "universal missing", Status: "backlog",
			Metadata: map[string]any{}},
		{Slug: "archived", Title: "archived", Status: "cancelled",
			Metadata: map[string]any{"related_agents": []any{"ops"}}},
	}

	got := filterRunbooks(entries, "ops")
	gotSlugs := []string{}
	for _, e := range got {
		gotSlugs = append(gotSlugs, e.Slug)
	}
	want := []string{"ops-only", "shared", "universal-empty", "universal-missing"}
	if !sameStringSet(gotSlugs, want) {
		t.Fatalf("got %v, want %v", gotSlugs, want)
	}
}

// TestFilterGuidelines_AppliesToAgents — symmetric to runbooks but
// keyed off `applies_to_agents`. Pinning the field name catches
// accidental copy-paste bugs.
func TestFilterGuidelines_AppliesToAgents(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "for-ops", Title: "for ops", Status: "backlog",
			Metadata: map[string]any{"applies_to_agents": []any{"ops"}}},
		{Slug: "for-others", Title: "for others", Status: "backlog",
			Metadata: map[string]any{"applies_to_agents": []any{"qa", "dev"}}},
		{Slug: "universal", Title: "universal", Status: "backlog",
			Metadata: map[string]any{}},
	}
	got := filterGuidelines(entries, "ops")
	gotSlugs := []string{}
	for _, e := range got {
		gotSlugs = append(gotSlugs, e.Slug)
	}
	want := []string{"for-ops", "universal"}
	if !sameStringSet(gotSlugs, want) {
		t.Fatalf("got %v, want %v", gotSlugs, want)
	}
}

// TestFilterAlwaysLive — external systems and related projects only
// drop the archived flag; everything else passes unchanged.
func TestFilterAlwaysLive(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "live", Title: "live", Status: "backlog"},
		{Slug: "archived", Title: "archived", Status: "cancelled"},
	}
	got := filterAlwaysLive(entries)
	if len(got) != 1 || got[0].Slug != "live" {
		t.Fatalf("filterAlwaysLive: got %v", got)
	}
}

// TestResolveBundleMode pins the validator behaviour. Empty/minimal/
// full are accepted; anything else is a usageError.
func TestResolveBundleMode(t *testing.T) {
	cases := []struct {
		raw       string
		want      bundleMode
		wantUsage bool
	}{
		{raw: "", want: bundleModeNone},
		{raw: "minimal", want: bundleModeMinimal},
		{raw: "MINIMAL", want: bundleModeMinimal},
		{raw: "  full  ", want: bundleModeFull},
		{raw: "garbage", wantUsage: true},
	}
	for _, c := range cases {
		got, err := resolveBundleMode(c.raw)
		if c.wantUsage {
			if _, ok := err.(*usageError); !ok {
				t.Errorf("raw=%q: want usageError, got %v", c.raw, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("raw=%q: unexpected err %v", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("raw=%q: got %q want %q", c.raw, got, c.want)
		}
	}
}

// TestComputeBundleRev_StableUnderMapOrder — the rev hash must not
// depend on Go map iteration order, otherwise `--refresh` would be
// mandatory on every run. Assemble the same payload twice with
// shuffled metadata maps and confirm the rev is identical.
func TestComputeBundleRev_StableUnderMapOrder(t *testing.T) {
	mk := func(order []string) *bundlePayload {
		meta := map[string]any{}
		for _, k := range order {
			meta[k] = k + "_value"
		}
		return &bundlePayload{
			Project: projectSummary{ID: 1, Key: "PAI"},
			Agent:   json.RawMessage(`{"agent": {"name": "ops"}}`),
			Memory: []knowledgeEntry{
				{Slug: "a", Title: "A", Body: "body", Metadata: meta},
			},
		}
	}
	a := computeBundleRev(mk([]string{"alpha", "beta", "gamma"}))
	b := computeBundleRev(mk([]string{"gamma", "alpha", "beta"}))
	if a != b {
		t.Errorf("rev differs across map order: a=%s b=%s", a, b)
	}

	// Sanity: changing actual content does change the rev.
	c := computeBundleRev(&bundlePayload{
		Project: projectSummary{ID: 1, Key: "PAI"},
		Agent:   json.RawMessage(`{"agent": {"name": "ops"}}`),
		Memory: []knowledgeEntry{
			{Slug: "a", Title: "A", Body: "different body", Metadata: map[string]any{}},
		},
	})
	if a == c {
		t.Errorf("rev should change with body content; both=%s", a)
	}
}

// ── end-to-end CLI ──────────────────────────────────────────────

// fakeBundleAPI returns a fake server that wires every endpoint the
// bundle resolver hits: project list, agent list, agent artifact,
// the five knowledge convenience endpoints, and /api/auth/me. Each
// hit is recorded so the cache test can assert "no requests on the
// warm path".
type bundleHits struct {
	mu        sync.Mutex
	endpoints map[string]int
	total     atomic.Int32
}

func (b *bundleHits) record(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.endpoints == nil {
		b.endpoints = map[string]int{}
	}
	b.endpoints[path]++
	b.total.Add(1)
}

func startBundleAPI(t *testing.T, hits *bundleHits) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.record(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":42,"key":"BON26"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/agents":
			_, _ = w.Write([]byte(`[{"name":"ops"},{"name":"qa"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/agents/ops.json":
			_, _ = w.Write([]byte(`{
				"project": {"id":42,"key":"BON26","name":"Bonelio"},
				"agent": {
					"name": "ops",
					"description": "Operate the prod rig.",
					"metadata": {"environments": ["prod"]}
				},
				"repos": [],
				"environments": [],
				"deploy_recipes": []
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/memory":
			_, _ = w.Write([]byte(`[
				{"id":1,"project_id":42,"type":"memory","slug":"prod-host","title":"Prod host alias","body":"Use 'imac' not 'laptop'.","status":"backlog","metadata":{"scope":"project"},"created_at":"","updated_at":""},
				{"id":2,"project_id":42,"type":"memory","slug":"staging-only","title":"Staging note","body":"…","status":"backlog","metadata":{"scope":"project","applies_to_environments":["staging"]},"created_at":"","updated_at":""},
				{"id":3,"project_id":42,"type":"memory","slug":"old-rule","title":"old rule","body":"…","status":"cancelled","metadata":{},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/runbooks":
			_, _ = w.Write([]byte(`[
				{"id":4,"project_id":42,"type":"runbook","slug":"deploy","title":"Deploy","body":"step 1","status":"backlog","metadata":{"related_agents":["ops"]},"created_at":"","updated_at":""},
				{"id":5,"project_id":42,"type":"runbook","slug":"qa-only","title":"QA only","body":"…","status":"backlog","metadata":{"related_agents":["qa"]},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/external-systems":
			_, _ = w.Write([]byte(`[
				{"id":6,"project_id":42,"type":"external_system","slug":"sentry","title":"Sentry","body":"errors","status":"backlog","metadata":{"url":"https://sentry.example"},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/related-projects":
			_, _ = w.Write([]byte(`[
				{"id":7,"project_id":42,"type":"related_project","slug":"sister","title":"Sister project","body":"linked","status":"backlog","metadata":{"key":"BON27"},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/guidelines":
			_, _ = w.Write([]byte(`[
				{"id":8,"project_id":42,"type":"guideline","slug":"prod-naming","title":"Prod naming","body":"Use 'prod' not 'live'.","status":"backlog","metadata":{"applies_to_agents":["ops"]},"created_at":"","updated_at":""},
				{"id":9,"project_id":42,"type":"guideline","slug":"qa-only-rule","title":"qa only","body":"…","status":"backlog","metadata":{"applies_to_agents":["qa"]},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":7,"username":"mba"}}`))
		default:
			http.Error(w, `{"error":"unmocked: `+r.Method+" "+r.URL.Path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// useTempCWD switches the test's working directory to a fresh tmp
// dir so the default `.paimos/cache` root doesn't collide with other
// runs. The original cwd is restored on cleanup. Returns the
// realpath-resolved cwd inside the tmp (macOS aliases /var → /private/var
// so callers comparing CLI-emitted paths must use the resolved form).
func useTempCWD(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	// Resolve symlinks so the returned path matches whatever
	// os.Getwd() inside the CLI reports — on macOS, t.TempDir() can
	// return a /var path that resolves through to /private/var.
	resolved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after chdir: %v", err)
	}
	return resolved
}

// TestSessionStart_BundleFull_FilesFormat — end-to-end exercise of
// `--bundle full --format files`. Confirms the cache directory layout
// (manifest + agent + per-category folders + per-entry markdown) and
// that the filter logic actually prunes the API responses correctly.
func TestSessionStart_BundleFull_FilesFormat(t *testing.T) {
	hits := &bundleHits{}
	srv := startBundleAPI(t, hits)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	tmp := useTempCWD(t)

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "full",
		"--format", "files",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, "wrote bundle to") {
		t.Errorf("expected 'wrote bundle to' confirmation, got %q", out)
	}
	if !strings.Contains(out, "rev=") {
		t.Errorf("expected rev in output, got %q", out)
	}

	cacheDir := filepath.Join(tmp, ".paimos", "cache", "BON26")
	mustExist(t, filepath.Join(cacheDir, "manifest.json"))
	mustExist(t, filepath.Join(cacheDir, "agent.json"))
	// Memory: prod-host passes (project scope, no env clash); staging-only
	// is dropped (env clash with agent.environments=[prod]); old-rule is
	// archived. Only prod-host should land on disk.
	mustExist(t, filepath.Join(cacheDir, "memory", "prod-host.md"))
	mustNotExist(t, filepath.Join(cacheDir, "memory", "staging-only.md"))
	mustNotExist(t, filepath.Join(cacheDir, "memory", "old-rule.md"))
	// Runbooks: deploy passes (related_agents includes ops); qa-only drops.
	mustExist(t, filepath.Join(cacheDir, "runbooks", "deploy.md"))
	mustNotExist(t, filepath.Join(cacheDir, "runbooks", "qa-only.md"))
	// External systems + related projects always pass when live.
	mustExist(t, filepath.Join(cacheDir, "external_systems", "sentry.md"))
	mustExist(t, filepath.Join(cacheDir, "related_projects", "sister.md"))
	// Guidelines: prod-naming passes; qa-only-rule drops.
	mustExist(t, filepath.Join(cacheDir, "guidelines", "prod-naming.md"))
	mustNotExist(t, filepath.Join(cacheDir, "guidelines", "qa-only-rule.md"))

	// Per-entry file content sanity: frontmatter + body.
	body, err := os.ReadFile(filepath.Join(cacheDir, "memory", "prod-host.md"))
	if err != nil {
		t.Fatalf("read prod-host.md: %v", err)
	}
	if !strings.HasPrefix(string(body), "---json\n") {
		t.Errorf("expected ---json frontmatter prefix, got %q", string(body)[:30])
	}
	if !strings.Contains(string(body), "Use 'imac' not 'laptop'") {
		t.Errorf("expected body to round-trip, got %q", string(body))
	}

	// Manifest sanity.
	manifestBs, err := os.ReadFile(filepath.Join(cacheDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest cacheManifest
	if err := json.Unmarshal(manifestBs, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Project != "BON26" {
		t.Errorf("manifest.Project = %q, want BON26", manifest.Project)
	}
	if manifest.Agent != "ops" {
		t.Errorf("manifest.Agent = %q, want ops", manifest.Agent)
	}
	if manifest.Rev == "" {
		t.Error("manifest.Rev empty")
	}
	if manifest.FetchedAt == "" {
		t.Error("manifest.FetchedAt empty")
	}
	if len(manifest.Entries["memory"]) != 1 || manifest.Entries["memory"][0].Slug != "prod-host" {
		t.Errorf("manifest memory mismatch: %+v", manifest.Entries["memory"])
	}
}

// TestSessionStart_BundleFull_JSONFormat — same call, --format json.
// Asserts the bundle returns one JSON document with all six categories
// post-filter. JSON does NOT short-circuit on cache (always re-fetches).
func TestSessionStart_BundleFull_JSONFormat(t *testing.T) {
	hits := &bundleHits{}
	srv := startBundleAPI(t, hits)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	useTempCWD(t)

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "full",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if got["agent_name"] != "ops" {
		t.Errorf("agent_name=%v, want ops", got["agent_name"])
	}
	if _, ok := got["session_id"].(string); !ok {
		t.Error("session_id missing")
	}
	if _, ok := got["rev"].(string); !ok {
		t.Error("rev missing in json output")
	}
	mustHaveOneEntry(t, got, "memory", "prod-host")
	mustHaveOneEntry(t, got, "runbooks", "deploy")
	mustHaveOneEntry(t, got, "external_systems", "sentry")
	mustHaveOneEntry(t, got, "related_projects", "sister")
	mustHaveOneEntry(t, got, "guidelines", "prod-naming")
}

// TestSessionStart_BundleFull_EnvFormat — --format env (default) writes
// the cache dir AND emits the three export lines, including
// PAIMOS_KNOWLEDGE_DIR which downstream agents read.
func TestSessionStart_BundleFull_EnvFormat(t *testing.T) {
	hits := &bundleHits{}
	srv := startBundleAPI(t, hits)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	tmp := useTempCWD(t)

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "full",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, "export PAIMOS_AGENT_NAME=ops") {
		t.Errorf("missing PAIMOS_AGENT_NAME export: %q", out)
	}
	if !strings.Contains(out, "export PAIMOS_SESSION_ID=") {
		t.Errorf("missing PAIMOS_SESSION_ID export: %q", out)
	}
	wantDir := filepath.Join(tmp, ".paimos", "cache", "BON26")
	if !strings.Contains(out, "export PAIMOS_KNOWLEDGE_DIR="+wantDir) {
		t.Errorf("missing PAIMOS_KNOWLEDGE_DIR export pointing at %q in %q", wantDir, out)
	}
	mustExist(t, filepath.Join(wantDir, "manifest.json"))
}

// TestSessionStart_BundleMinimal_BackwardsCompat — `--bundle minimal`
// is the explicit alias for the no-bundle PAI-327 behaviour. Must
// emit exactly two export lines (no PAIMOS_KNOWLEDGE_DIR) AND must
// NOT touch the cache dir.
func TestSessionStart_BundleMinimal_BackwardsCompat(t *testing.T) {
	hits := &bundleHits{}
	srv := startBundleAPI(t, hits)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	tmp := useTempCWD(t)

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "minimal",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "export PAIMOS_AGENT_NAME=ops") {
		t.Errorf("line 0 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "export PAIMOS_SESSION_ID=") {
		t.Errorf("line 1 = %q", lines[1])
	}
	if strings.Contains(out, "PAIMOS_KNOWLEDGE_DIR") {
		t.Errorf("minimal mode must NOT set PAIMOS_KNOWLEDGE_DIR: %q", out)
	}
	// Cache must not exist.
	cacheDir := filepath.Join(tmp, ".paimos", "cache", "BON26")
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("cache dir created in minimal mode: %v", err)
	}
}

// TestSessionStart_NoBundle_BackwardsCompat — same as minimal: omitting
// --bundle entirely keeps PAI-327 behaviour byte-for-byte. Distinct
// from the minimal test so a regression in either path is attributable.
func TestSessionStart_NoBundle_BackwardsCompat(t *testing.T) {
	hits := &bundleHits{}
	srv := startBundleAPI(t, hits)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	tmp := useTempCWD(t)

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), out)
	}
	if strings.Contains(out, "PAIMOS_KNOWLEDGE_DIR") {
		t.Errorf("no-bundle mode must NOT set PAIMOS_KNOWLEDGE_DIR: %q", out)
	}
	cacheDir := filepath.Join(tmp, ".paimos", "cache", "BON26")
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("cache dir created in no-bundle mode: %v", err)
	}
}

// TestSessionStart_BundleFull_CacheShortCircuit — second call with the
// same parameters does not re-fetch knowledge endpoints. Asserts the
// `--refresh` opt-out forces the API back into play.
func TestSessionStart_BundleFull_CacheShortCircuit(t *testing.T) {
	hits := &bundleHits{}
	srv := startBundleAPI(t, hits)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	useTempCWD(t)

	if _, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "full",
		"--format", "files",
	); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Snapshot which endpoints were hit and how often.
	hits.mu.Lock()
	firstMemoryHits := hits.endpoints["GET /api/projects/42/memory"]
	hits.mu.Unlock()
	if firstMemoryHits != 1 {
		t.Fatalf("first run hit /memory %d times, want 1", firstMemoryHits)
	}

	// Second run: no --refresh. Should NOT re-hit /memory.
	if _, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "full",
		"--format", "files",
	); err != nil {
		t.Fatalf("second (cached) run: %v", err)
	}
	hits.mu.Lock()
	secondMemoryHits := hits.endpoints["GET /api/projects/42/memory"]
	hits.mu.Unlock()
	if secondMemoryHits != 1 {
		t.Errorf("second run re-hit /memory (count=%d, want 1)", secondMemoryHits)
	}

	// Third run: --refresh forces re-fetch.
	if _, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "full",
		"--refresh",
		"--format", "files",
	); err != nil {
		t.Fatalf("third (refresh) run: %v", err)
	}
	hits.mu.Lock()
	thirdMemoryHits := hits.endpoints["GET /api/projects/42/memory"]
	hits.mu.Unlock()
	if thirdMemoryHits != 2 {
		t.Errorf("--refresh did not force re-fetch (count=%d, want 2)", thirdMemoryHits)
	}
}

// TestSessionStart_BundleFull_FilesRequiresFullBundle — `--format files`
// with no bundle (or minimal) is a usage error: there's nothing to
// write.
func TestSessionStart_BundleFull_FilesRequiresFullBundle(t *testing.T) {
	t.Setenv(envURL, "https://example.test")
	t.Setenv(envAPIKey, "test_key")
	useTempCWD(t)

	_, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--format", "files",
	)
	if err == nil {
		t.Fatal("expected usage error for --format files without --bundle full")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("err type=%T want *usageError (%v)", err, err)
	}
}

// TestSessionStart_BundleFull_InvalidBundle — --bundle yaml is a
// usageError; failure must surface BEFORE any network call.
func TestSessionStart_BundleFull_InvalidBundle(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	useTempCWD(t)

	_, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "BON26",
		"--agent", "ops",
		"--bundle", "yaml",
	)
	if err == nil {
		t.Fatal("expected error for invalid --bundle")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("err type=%T want *usageError", err)
	}
	if requests != 0 {
		t.Errorf("network was hit %d times before usage error", requests)
	}
}

// ── helpers ─────────────────────────────────────────────────────

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected %s NOT to exist", path)
	}
}

func mustHaveOneEntry(t *testing.T, doc map[string]any, key, slug string) {
	t.Helper()
	raw, ok := doc[key].([]any)
	if !ok {
		t.Errorf("doc[%q] missing or not a list (%T)", key, doc[key])
		return
	}
	if len(raw) != 1 {
		t.Errorf("doc[%q] len=%d, want 1: %v", key, len(raw), raw)
		return
	}
	entry, ok := raw[0].(map[string]any)
	if !ok {
		t.Errorf("doc[%q][0] not an object", key)
		return
	}
	if entry["slug"] != slug {
		t.Errorf("doc[%q][0].slug = %v, want %q", key, entry["slug"], slug)
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
		if count[s] < 0 {
			return false
		}
	}
	return true
}

// ── PAI-345: cross-scope memory merge ────────────────────────────

// TestMergeMemoryByScope_PrecedenceAndDedup pins the bundle merge:
// project > user > instance precedence on slug collision, scope
// field set on every survivor.
func TestMergeMemoryByScope_PrecedenceAndDedup(t *testing.T) {
	project := []knowledgeEntry{
		{Slug: "imac_rule", Title: "Project iMac"},
		{Slug: "deploy_creds", Title: "Project deploy creds"},
	}
	user := []knowledgeEntry{
		// Collides with project — must drop in favour of project.
		{Slug: "imac_rule", Title: "User iMac"},
		// Unique — survives.
		{Slug: "no_cat_secrets", Title: "User: no cat secrets"},
	}
	instance := []knowledgeEntry{
		// Collides with user — must drop.
		{Slug: "no_cat_secrets", Title: "Instance: no cat"},
		// Unique — survives.
		{Slug: "use_paimos_cli", Title: "Instance: use paimos CLI"},
	}

	merged := mergeMemoryByScope(project, user, instance)
	if len(merged) != 4 {
		t.Fatalf("expected 4 entries after dedup, got %d", len(merged))
	}

	byScope := map[string]string{}
	byTitle := map[string]string{}
	for _, e := range merged {
		byScope[e.Slug] = e.Scope
		byTitle[e.Slug] = e.Title
	}
	if byScope["imac_rule"] != "project" {
		t.Errorf("imac_rule should keep project precedence; got %q", byScope["imac_rule"])
	}
	if byTitle["imac_rule"] != "Project iMac" {
		t.Errorf("imac_rule should retain project body; got %q", byTitle["imac_rule"])
	}
	if byScope["deploy_creds"] != "project" {
		t.Errorf("deploy_creds: %q", byScope["deploy_creds"])
	}
	if byScope["no_cat_secrets"] != "user" {
		t.Errorf("no_cat_secrets should be user-scoped (project absent); got %q", byScope["no_cat_secrets"])
	}
	if byTitle["no_cat_secrets"] != "User: no cat secrets" {
		t.Errorf("no_cat_secrets should keep user body; got %q", byTitle["no_cat_secrets"])
	}
	if byScope["use_paimos_cli"] != "instance" {
		t.Errorf("use_paimos_cli: %q", byScope["use_paimos_cli"])
	}
}

// TestMergeMemoryByScope_EmptyInputs ensures the merge handles all-
// nil / partial inputs without panicking. The bundle resolver feeds
// nil slices for older servers without the user/instance endpoints.
func TestMergeMemoryByScope_EmptyInputs(t *testing.T) {
	if got := mergeMemoryByScope(nil, nil, nil); len(got) != 0 {
		t.Errorf("nil/nil/nil → expected empty, got %d", len(got))
	}
	user := []knowledgeEntry{{Slug: "u1", Title: "u1"}}
	got := mergeMemoryByScope(nil, user, nil)
	if len(got) != 1 || got[0].Scope != "user" {
		t.Errorf("user-only merge: %+v", got)
	}
	instance := []knowledgeEntry{{Slug: "i1", Title: "i1"}}
	got = mergeMemoryByScope(nil, nil, instance)
	if len(got) != 1 || got[0].Scope != "instance" {
		t.Errorf("instance-only merge: %+v", got)
	}
}
