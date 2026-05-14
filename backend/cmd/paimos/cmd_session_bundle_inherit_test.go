// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

// PAI-348 — bundle inheritance from related_projects[]. The tests here
// pin the four behaviours the ticket lists under "Acceptance":
//
//   1. inherit=true|false on memory round-trips (covered via
//      filterInheritableMemory + memoryInheritsFlag).
//   2. Bundle merges inherited entries in declaration order, with
//      project precedence on slug collision (mergeInherited).
//   3. Source annotation present + correct on inherited entries
//      (annotateInherited + end-to-end resolveBundle test).
//   4. Cross-instance fetch graceful degradation: failure surfaces a
//      warning entry, bundle still returns.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMemoryInheritsFlag pins the default-true semantics: an absent
// flag, a non-bool flag, and a `true` flag all return true; only an
// explicit `false` opts the entry out.
func TestMemoryInheritsFlag(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{name: "missing", meta: map[string]any{}, want: true},
		{name: "nil-map", meta: nil, want: true},
		{name: "true", meta: map[string]any{"inherit": true}, want: true},
		{name: "false", meta: map[string]any{"inherit": false}, want: false},
		{name: "non-bool falls back to true",
			meta: map[string]any{"inherit": "true"}, want: true},
	}
	for _, c := range cases {
		got := memoryInheritsFlag(c.meta)
		if got != c.want {
			t.Errorf("%s: memoryInheritsFlag = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestFilterInheritableMemory_DropsOptOut — the inherit=false flag
// must remove the entry from the upstream's contribution. Archived +
// user-scoped entries also drop. Everything else passes.
func TestFilterInheritableMemory_DropsOptOut(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "a-default", Title: "default", Status: "backlog",
			Metadata: map[string]any{}},
		{Slug: "b-explicit-true", Title: "explicit true", Status: "backlog",
			Metadata: map[string]any{"inherit": true}},
		{Slug: "c-opt-out", Title: "opt out", Status: "backlog",
			Metadata: map[string]any{"inherit": false}},
		{Slug: "d-archived", Title: "archived", Status: "cancelled",
			Metadata: map[string]any{}},
		{Slug: "e-user-scoped", Title: "user scoped", Status: "backlog",
			Metadata: map[string]any{"scope": "user-on-this-project"}},
	}
	got := filterInheritableMemory(entries)
	gotSlugs := []string{}
	for _, e := range got {
		gotSlugs = append(gotSlugs, e.Slug)
	}
	want := []string{"a-default", "b-explicit-true"}
	if !sameStringSet(gotSlugs, want) {
		t.Fatalf("filterInheritableMemory: got %v, want %v", gotSlugs, want)
	}
}

// TestInheritableRefs_RoleFiltering — only related_projects[] entries
// with an inheritance-eligible role surface. Both `role` and the
// editor's `relationship` field are accepted (PAI-338 backwards-compat).
func TestInheritableRefs_RoleFiltering(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "pai-upstream", Status: "backlog",
			Metadata: map[string]any{
				"key":          "PAI",
				"instance_url": "https://pm.barta.cm",
				"role":         "upstream-tool",
			}},
		{Slug: "infra-link", Status: "backlog",
			Metadata: map[string]any{
				"key":          "INFRA",
				"instance_url": "https://pm.barta.cm",
				"relationship": "infra",
			}},
		{Slug: "peer", Status: "backlog",
			Metadata: map[string]any{
				"key":          "ACME",
				"instance_url": "https://pm.example.com",
				"role":         "shared-customer",
			}},
		{Slug: "missing-instance", Status: "backlog",
			Metadata: map[string]any{
				"key":  "BAD",
				"role": "upstream-tool",
			}},
	}
	got := inheritableRefs(entries)
	gotKeys := []string{}
	for _, r := range got {
		gotKeys = append(gotKeys, r.Key)
	}
	want := []string{"PAI", "INFRA"}
	if !sameStringSet(gotKeys, want) {
		t.Fatalf("inheritableRefs: got %v, want %v", gotKeys, want)
	}
}

// TestMergeInherited_ProjectPrecedence — when an inherited entry
// shares a slug with an own entry, the project-own version wins. The
// inherited entry is silently dropped (the project authored a more
// specific replacement). Non-colliding inherited entries land at the
// tail in declaration order.
func TestMergeInherited_ProjectPrecedence(t *testing.T) {
	own := []knowledgeEntry{
		{Slug: "use_paimos_cli", Title: "own version", Status: "backlog"},
		{Slug: "project_only", Title: "project only", Status: "backlog"},
	}
	inherited := []knowledgeEntry{
		// Same slug — must be dropped because project has its own.
		{Slug: "use_paimos_cli", Title: "upstream version", Status: "backlog",
			Source: &entrySource{Type: "inherited", FromProject: "PAI"}},
		{Slug: "upstream_unique", Title: "upstream unique", Status: "backlog",
			Source: &entrySource{Type: "inherited", FromProject: "PAI"}},
	}
	got := mergeInherited(own, inherited)
	if len(got) != 3 {
		t.Fatalf("merged length = %d, want 3 (got %+v)", len(got), got)
	}
	// Project entries first, in input order.
	if got[0].Slug != "use_paimos_cli" || got[0].Title != "own version" {
		t.Errorf("got[0] = %+v, want own use_paimos_cli", got[0])
	}
	if got[0].Source != nil {
		t.Errorf("project-own entry must not carry a Source annotation, got %+v", got[0].Source)
	}
	if got[1].Slug != "project_only" {
		t.Errorf("got[1] = %+v, want project_only", got[1])
	}
	// Inherited tail: only the non-colliding upstream entry.
	if got[2].Slug != "upstream_unique" {
		t.Errorf("got[2] = %+v, want upstream_unique", got[2])
	}
	if got[2].Source == nil || got[2].Source.FromProject != "PAI" {
		t.Errorf("upstream_unique missing inherited annotation: %+v", got[2].Source)
	}
}

// TestAnnotateInherited — every entry must carry the upstream pointer
// once annotated. The original slice is left untouched (defensive
// copy) so a cached upstream is safe to re-use across invocations.
func TestAnnotateInherited(t *testing.T) {
	upstream := relatedProjectRef{
		Key:         "PAI",
		InstanceURL: "https://pm.barta.cm",
		Role:        "upstream-tool",
	}
	originals := []knowledgeEntry{
		{Slug: "a", Title: "A"},
		{Slug: "b", Title: "B"},
	}
	got := annotateInherited(originals, upstream)
	for _, e := range got {
		if e.Source == nil {
			t.Fatalf("missing source on %s", e.Slug)
		}
		if e.Source.Type != "inherited" {
			t.Errorf("%s: source type = %q, want inherited", e.Slug, e.Source.Type)
		}
		if e.Source.FromProject != "PAI" {
			t.Errorf("%s: from_project = %q, want PAI", e.Slug, e.Source.FromProject)
		}
		if e.Source.FromInstance != "https://pm.barta.cm" {
			t.Errorf("%s: from_instance = %q, want pm.barta.cm",
				e.Slug, e.Source.FromInstance)
		}
	}
	for _, e := range originals {
		if e.Source != nil {
			t.Errorf("annotateInherited mutated original entry %s", e.Slug)
		}
	}
}

// TestMakeInheritWarning — failure must produce a warning entry that
// agents can read. The slug embeds the upstream key so multiple
// concurrent failures stay distinguishable.
func TestMakeInheritWarning(t *testing.T) {
	w := makeInheritWarning(relatedProjectRef{
		Key:         "PAI",
		InstanceURL: "https://pm.barta.cm",
	}, "connection refused")
	if w == nil {
		t.Fatal("makeInheritWarning returned nil")
	}
	if w.Source == nil || w.Source.Type != "warning" {
		t.Errorf("expected warning source, got %+v", w.Source)
	}
	if !strings.Contains(w.Body, "connection refused") {
		t.Errorf("warning body missing reason: %q", w.Body)
	}
	if !strings.Contains(w.Slug, "pai") {
		t.Errorf("warning slug should embed upstream key: %q", w.Slug)
	}
}

// TestUpstreamClientFor — same-instance pulls reuse `c`; cross-
// instance pulls return a fresh client at the upstream URL; bad URLs
// surface as errors so the resolver can degrade gracefully.
func TestUpstreamClientFor(t *testing.T) {
	c := &Client{baseURL: "https://pm.bytepoets.com", http: http.DefaultClient}

	same, err := upstreamClientFor(c, "https://pm.bytepoets.com")
	if err != nil {
		t.Fatalf("same-instance err: %v", err)
	}
	if same != c {
		t.Errorf("expected same-instance pull to reuse client; got fresh")
	}

	cross, err := upstreamClientFor(c, "https://pm.barta.cm")
	if err != nil {
		t.Fatalf("cross-instance err: %v", err)
	}
	if cross == c {
		t.Errorf("cross-instance pull must return a fresh client")
	}
	if cross.baseURL != "https://pm.barta.cm" {
		t.Errorf("cross.baseURL = %q, want pm.barta.cm", cross.baseURL)
	}

	// Trailing slash normalisation.
	same2, _ := upstreamClientFor(c, "https://pm.bytepoets.com/")
	if same2 != c {
		t.Errorf("trailing slash should still match same-instance")
	}

	if _, err := upstreamClientFor(c, ""); err == nil {
		t.Error("expected error for empty URL")
	}
	if _, err := upstreamClientFor(c, "not-a-url"); err == nil {
		t.Error("expected error for non-absolute URL")
	}
}

// ── end-to-end inheritance through resolveBundle ────────────────────

// startBundleAPIWithRelated returns the same fake API as the PAI-340
// tests but injects a related_projects[] entry pointing at an external
// upstream (handled by the test). Used by the e2e tests below.
func startBundleAPIWithRelated(t *testing.T, hits *bundleHits, upstreamURL, upstreamKey string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.record(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":42,"key":"BON26"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/agents":
			_, _ = w.Write([]byte(`[{"name":"ops"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/agents/ops.json":
			_, _ = w.Write([]byte(`{
				"project": {"id":42,"key":"BON26","name":"Bonelio"},
				"agent": {"name": "ops", "metadata": {}},
				"repos": [], "environments": [], "deploy_recipes": []
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/knowledge" && r.URL.Query().Get("type") == "memory":
			_, _ = w.Write([]byte(`[
				{"id":1,"project_id":42,"type":"memory","slug":"own_rule","title":"Own rule","body":"BON26 own","status":"backlog","metadata":{},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/knowledge" && r.URL.Query().Get("type") == "runbook":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/knowledge" && r.URL.Query().Get("type") == "external-system":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/knowledge" && r.URL.Query().Get("type") == "related-project":
			body := `[{
				"id":7,"project_id":42,"type":"related_project","slug":"upstream_pai",
				"title":"Upstream paimos","body":"","status":"backlog",
				"metadata":{"key":"` + upstreamKey + `","instance_url":"` + upstreamURL + `","role":"upstream-tool"},
				"created_at":"","updated_at":""
			}]`
			_, _ = w.Write([]byte(body))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/42/knowledge" && r.URL.Query().Get("type") == "guideline":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":7,"username":"mba"}}`))
		default:
			http.Error(w, `{"error":"unmocked: `+r.Method+" "+r.URL.Path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startUpstreamAPI returns a fake upstream that exposes one project
// (PAI / id=99) with one inheritable memory entry, one runbook, one
// guideline, and one inherit=false entry that must NOT propagate.
func startUpstreamAPI(t *testing.T, projectKey string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":99,"key":"` + projectKey + `"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/99/knowledge" && r.URL.Query().Get("type") == "memory":
			_, _ = w.Write([]byte(`[
				{"id":100,"project_id":99,"type":"memory","slug":"use_paimos_cli","title":"Use paimos CLI not curl","body":"Always go through paimos CLI.","status":"backlog","metadata":{},"created_at":"","updated_at":""},
				{"id":101,"project_id":99,"type":"memory","slug":"private_lesson","title":"Project-internal","body":"don't share","status":"backlog","metadata":{"inherit":false},"created_at":"","updated_at":""},
				{"id":102,"project_id":99,"type":"memory","slug":"explicit_inherit","title":"Explicit","body":"Yes inherit","status":"backlog","metadata":{"inherit":true},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/99/knowledge" && r.URL.Query().Get("type") == "runbook":
			_, _ = w.Write([]byte(`[
				{"id":110,"project_id":99,"type":"runbook","slug":"deploy_paimos","title":"Deploy paimos","body":"step 1","status":"backlog","metadata":{},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/99/knowledge" && r.URL.Query().Get("type") == "guideline":
			_, _ = w.Write([]byte(`[
				{"id":120,"project_id":99,"type":"guideline","slug":"prod_not_live","title":"Prod naming","body":"Use prod","status":"backlog","metadata":{},"created_at":"","updated_at":""}
			]`))
		default:
			http.Error(w, `{"error":"upstream unmocked: `+r.Method+" "+r.URL.Path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSessionStart_BundleFull_InheritsFromUpstream — the smoke case
// from PAI-348's acceptance list: BON26 → related_projects → PAI;
// PAI's "use_paimos_cli" memory + runbook + guideline land in the
// bundle with inherited annotation. inherit=false drops.
func TestSessionStart_BundleFull_InheritsFromUpstream(t *testing.T) {
	upstream := startUpstreamAPI(t, "PAI")
	hits := &bundleHits{}
	srv := startBundleAPIWithRelated(t, hits, upstream.URL, "PAI")

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
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	memories, _ := doc["memory"].([]any)
	if len(memories) == 0 {
		t.Fatalf("memory empty: %v", doc["memory"])
	}
	gotSlugs := []string{}
	for _, raw := range memories {
		entry, _ := raw.(map[string]any)
		slug, _ := entry["slug"].(string)
		gotSlugs = append(gotSlugs, slug)
	}
	wantSlugs := []string{"own_rule", "use_paimos_cli", "explicit_inherit"}
	if !sameStringSet(gotSlugs, wantSlugs) {
		t.Errorf("memory slugs = %v, want %v (private_lesson must NOT inherit)",
			gotSlugs, wantSlugs)
	}

	// inherited annotation present + correct.
	for _, raw := range memories {
		entry, _ := raw.(map[string]any)
		slug, _ := entry["slug"].(string)
		switch slug {
		case "own_rule":
			if _, has := entry["source"]; has {
				t.Errorf("own_rule must NOT carry a source annotation: %+v", entry)
			}
		case "use_paimos_cli", "explicit_inherit":
			source, ok := entry["source"].(map[string]any)
			if !ok {
				t.Errorf("%s missing source: %+v", slug, entry)
				continue
			}
			if source["type"] != "inherited" {
				t.Errorf("%s source.type = %v, want inherited", slug, source["type"])
			}
			if source["from_project"] != "PAI" {
				t.Errorf("%s source.from_project = %v, want PAI", slug, source["from_project"])
			}
			if source["from_instance"] != upstream.URL {
				t.Errorf("%s source.from_instance = %v, want %s",
					slug, source["from_instance"], upstream.URL)
			}
		}
	}

	// Runbooks + guidelines also inherit.
	runbooks, _ := doc["runbooks"].([]any)
	if len(runbooks) != 1 {
		t.Errorf("runbooks: got %d, want 1 inherited", len(runbooks))
	}
	guidelines, _ := doc["guidelines"].([]any)
	if len(guidelines) != 1 {
		t.Errorf("guidelines: got %d, want 1 inherited", len(guidelines))
	}
}

// TestSessionStart_BundleFull_CrossInstanceFailureDegradesGracefully —
// when the upstream refuses connections, the bundle still returns and
// surfaces a `source: warning` entry. Project's own memory remains
// intact.
func TestSessionStart_BundleFull_CrossInstanceFailureDegradesGracefully(t *testing.T) {
	// Upstream URL points at a closed port — the client will fail to
	// connect when the resolver tries to fetch the project list.
	deadUpstream := "http://127.0.0.1:1" // privileged port, refused
	hits := &bundleHits{}
	srv := startBundleAPIWithRelated(t, hits, deadUpstream, "PAI")

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
		t.Fatalf("bundle resolution must NOT fail on upstream outage: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	memories, _ := doc["memory"].([]any)

	hasOwn := false
	hasWarning := false
	for _, raw := range memories {
		entry, _ := raw.(map[string]any)
		if entry["slug"] == "own_rule" {
			hasOwn = true
		}
		source, ok := entry["source"].(map[string]any)
		if ok && source["type"] == "warning" {
			hasWarning = true
			if source["from_project"] != "PAI" {
				t.Errorf("warning source.from_project = %v, want PAI", source["from_project"])
			}
			if msg, _ := source["message"].(string); msg == "" {
				t.Error("warning message should explain the failure")
			}
		}
	}
	if !hasOwn {
		t.Error("project's own memory must remain even on inheritance failure")
	}
	if !hasWarning {
		t.Error("expected synthetic warning entry on inheritance failure")
	}
}

// TestSessionStart_BundleFull_DeclarationOrder — when multiple
// upstream projects are declared, inherited entries appear in the
// declaration (slug-sort) order. mergeInherited preserves that.
func TestSessionStart_BundleFull_DeclarationOrder(t *testing.T) {
	// Two upstream servers, distinguishable by the slug they expose.
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects":
			_, _ = w.Write([]byte(`[{"id":50,"key":"AAA"}]`))
		case "/api/projects/50/knowledge":
			// PAI-394 — unified surface; type rides on ?type=.
			switch r.URL.Query().Get("type") {
			case "memory":
				_, _ = w.Write([]byte(`[
					{"id":1,"project_id":50,"type":"memory","slug":"from_aaa","title":"From AAA","body":"","status":"backlog","metadata":{},"created_at":"","updated_at":""}
				]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		default:
			http.Error(w, "unmocked", http.StatusNotFound)
		}
	}))
	t.Cleanup(upA.Close)
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects":
			_, _ = w.Write([]byte(`[{"id":60,"key":"BBB"}]`))
		case "/api/projects/60/knowledge":
			switch r.URL.Query().Get("type") {
			case "memory":
				_, _ = w.Write([]byte(`[
					{"id":2,"project_id":60,"type":"memory","slug":"from_bbb","title":"From BBB","body":"","status":"backlog","metadata":{},"created_at":"","updated_at":""}
				]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		default:
			http.Error(w, "unmocked", http.StatusNotFound)
		}
	}))
	t.Cleanup(upB.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects":
			_, _ = w.Write([]byte(`[{"id":42,"key":"BON26"}]`))
		case "/api/projects/42/agents":
			_, _ = w.Write([]byte(`[{"name":"ops"}]`))
		case "/api/projects/42/agents/ops.json":
			_, _ = w.Write([]byte(`{"agent":{"name":"ops","metadata":{}},"repos":[],"environments":[],"deploy_recipes":[]}`))
		case "/api/projects/42/knowledge":
			switch r.URL.Query().Get("type") {
			case "related-project":
				body := `[
					{"id":1,"project_id":42,"type":"related_project","slug":"a_aaa","title":"AAA","body":"","status":"backlog","metadata":{"key":"AAA","instance_url":"` + upA.URL + `","role":"upstream-tool"},"created_at":"","updated_at":""},
					{"id":2,"project_id":42,"type":"related_project","slug":"b_bbb","title":"BBB","body":"","status":"backlog","metadata":{"key":"BBB","instance_url":"` + upB.URL + `","role":"philosophy"},"created_at":"","updated_at":""}
				]`
				_, _ = w.Write([]byte(body))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		case "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":7}}`))
		default:
			http.Error(w, "unmocked", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
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
	var doc map[string]any
	_ = json.Unmarshal([]byte(out), &doc)
	memories, _ := doc["memory"].([]any)
	gotOrder := []string{}
	for _, raw := range memories {
		entry, _ := raw.(map[string]any)
		gotOrder = append(gotOrder, entry["slug"].(string))
	}
	// related_projects[] entries are sorted by slug ASC server-side
	// (loadByType in handlers/knowledge/handlers.go), so a_aaa
	// precedes b_bbb in declaration order, and the inherited slugs
	// follow the same order.
	want := []string{"from_aaa", "from_bbb"}
	if len(gotOrder) != 2 || gotOrder[0] != want[0] || gotOrder[1] != want[1] {
		t.Errorf("inherited memory order = %v, want %v", gotOrder, want)
	}
}
