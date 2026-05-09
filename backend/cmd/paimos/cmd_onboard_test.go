// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

// PAI-352 — `paimos onboard` rendering + drift-check coverage. The
// tests deliberately exercise the renderer over assembled inputs (no
// HTTP) for unit-test attribution, plus an end-to-end CLI test that
// confirms wiring through resolveBundle + the issues endpoint.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureBriefingInput returns a fully-populated briefingInput with
// representative content in every section the renderer cares about:
// description, related projects, external systems, guidelines (mix of
// own + inherited), recent context, agent role (description + body +
// bootstrap_steps + non_negotiable_rules), runbooks, memory reading
// list (mixed confidence). One fixture, two formats — keeps the
// section-coverage assertions consistent across markdown and HTML.
func fixtureBriefingInput(withAgent bool) briefingInput {
	agentRaw := json.RawMessage(`{
		"agent": {
			"name": "ops",
			"description": "Operate the production rig.",
			"body": "The ops agent owns the deploy path and the on-call rotation. Read the runbooks before touching prod.",
			"bootstrap_steps": [
				{"title":"Check on-call","command":"paimos session start --project BON26 --agent ops","rationale":"You inherit the rotation owner's queue."}
			],
			"non_negotiable_rules": [
				{"title":"No prod pushes after 16:00","body":"Cut-off so any incident has daylight to triage.","memory_ref":"deploy_window"}
			]
		}
	}`)

	bundle := &bundlePayload{
		Project: projectSummary{ID: 42, Key: "BON26"},
		Agent:   agentRaw,
		Memory: []knowledgeEntry{
			{Slug: "imac_rule", Title: "Use imac for prod", Body: "Always SSH to imac, not laptop.",
				Metadata: map[string]any{"confidence": "high", "last_referenced_at": "2026-05-08T10:00:00Z"}},
			{Slug: "log_levels", Title: "Use info for noisy steps", Body: "Debug only on demand.",
				Metadata: map[string]any{"confidence": "medium"}},
			{Slug: "inherited_rule", Title: "Inherited from upstream", Body: "From the philosophy project.",
				Metadata: map[string]any{"confidence": "high"},
				Source:   &entrySource{Type: "inherited", FromProject: "PHILO", FromInstance: "https://up.example"}},
		},
		Runbooks: []knowledgeEntry{
			{Slug: "deploy", Title: "Deploy to prod", Body: "Run `paimos session start --bundle full` first.",
				Metadata: map[string]any{}},
		},
		ExternalSystems: []knowledgeEntry{
			{Slug: "sentry", Title: "Sentry", Body: "Errors land here within 60s.",
				Metadata: map[string]any{"url": "https://sentry.example", "purpose": "errors"}},
		},
		RelatedProjects: []knowledgeEntry{
			{Slug: "philo", Title: "Philosophy project", Body: "Upstream rules we inherit.",
				Metadata: map[string]any{"key": "PHILO", "role": "philosophy"}},
		},
		Guidelines: []knowledgeEntry{
			{Slug: "tickets", Title: "Every commit references a PAI ticket", Body: "Even tiny fixes.",
				Metadata: map[string]any{"confidence": "high"}},
			{Slug: "workflow", Title: "Ticket-first workflow", Body: "Always file before coding.",
				Metadata: map[string]any{"confidence": "medium"}},
		},
		FetchedAt: "2026-05-08T13:00:00Z",
	}
	in := briefingInput{
		project: projectDetail{
			ID: 42, Key: "BON26", Name: "Bonelio",
			Description: "Bonelio is the customer-facing accruals app. The team ships fast on a Vue3+Go stack.",
		},
		bundle: bundle,
		recent: []recentIssue{
			{IssueKey: "BON26-100", Title: "Refresh deploy creds", Status: "done", UpdatedAt: "2026-05-07T18:00:00Z"},
			{IssueKey: "BON26-99", Title: "Fix flaky test", Status: "delivered", UpdatedAt: "2026-05-06T11:00:00Z"},
		},
		readingListSize: 10,
	}
	if withAgent {
		in.agentName = "ops"
	}
	return in
}

// TestRenderBriefing_ProjectLevelMarkdown — without --agent, the
// briefing must render every project-level section (welcome, what
// this project is, external systems, how we work, recent context,
// runbooks, where to look, reading list) but NOT the agent role
// section.
func TestRenderBriefing_ProjectLevelMarkdown(t *testing.T) {
	in := fixtureBriefingInput(false)
	body, err := renderBriefing(in, onboardFormatMarkdown, "abc123def456")
	if err != nil {
		t.Fatalf("renderBriefing: %v", err)
	}
	wantSubstrings := []string{
		"<!-- paimos: onboarded BON26@abc123def456 at ",
		"# Welcome to Bonelio",
		"## What this project is",
		"Bonelio is the customer-facing accruals app",
		"Related projects:",
		"## Key external systems",
		"Sentry",
		"https://sentry.example",
		"## How we work",
		"Every commit references a PAI ticket",
		"## Recent context",
		"BON26-100",
		"## Known runbooks",
		"Deploy to prod",
		"## Where to look",
		".paimos/cache/BON26/",
		"## Reading list",
		"Use imac for prod",
		"_(confidence: high)_",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Errorf("missing substring %q\n--- briefing ---\n%s", want, body)
		}
	}
	// Agent role section MUST be absent at project level.
	if strings.Contains(body, "If you're playing the") {
		t.Errorf("agent role section must NOT render at project level")
	}
}

// TestRenderBriefing_AgentLevelMarkdown — with --agent, the briefing
// also emits the agent role block (description, body excerpt,
// bootstrap_steps, non_negotiable_rules).
func TestRenderBriefing_AgentLevelMarkdown(t *testing.T) {
	in := fixtureBriefingInput(true)
	body, err := renderBriefing(in, onboardFormatMarkdown, "rev123")
	if err != nil {
		t.Fatalf("renderBriefing: %v", err)
	}
	wantSubstrings := []string{
		"[agent=ops]",
		"## If you're playing the ops role",
		"Operate the production rig.",
		"### Excerpt",
		"### Bootstrap steps",
		"Check on-call",
		"paimos session start --project BON26 --agent ops",
		"### Non-negotiable rules",
		"No prod pushes after 16:00",
		"_(memory: `deploy_window`)_",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\n--- briefing ---\n%s", want, body)
		}
	}
}

// TestRenderBriefing_HTMLFormat — same fixture, --format html. The
// HTML output must be self-contained (DOCTYPE + style block) and
// carry the same drift-detection header on the first non-DOCTYPE line
// before the <h1>.
func TestRenderBriefing_HTMLFormat(t *testing.T) {
	in := fixtureBriefingInput(true)
	body, err := renderBriefing(in, onboardFormatHTML, "htmlrev1234")
	if err != nil {
		t.Fatalf("renderBriefing: %v", err)
	}
	wantSubstrings := []string{
		"<!DOCTYPE html>",
		"<style>",
		"<!-- paimos: onboarded BON26@htmlrev1234 [agent=ops]",
		"<h1>Welcome to Bonelio</h1>",
		"<h2>What this project is</h2>",
		"<h2>Key external systems</h2>",
		`<a href="https://sentry.example">`,
		"<h2>How we work</h2>",
		"<h2>Recent context</h2>",
		"BON26-100",
		"<h2>If you're playing the ops role</h2>",
		"<h3>Bootstrap steps</h3>",
		"<h3>Non-negotiable rules</h3>",
		"<h2>Known runbooks</h2>",
		"<h2>Where to look</h2>",
		"<h2>Reading list</h2>",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Errorf("missing HTML substring %q\n--- briefing ---\n%s", want, body)
		}
	}
}

// TestRenderBriefing_InheritedAttribution — the fixture has one
// inherited memory + one inherited-from-PHILO related-project entry.
// The renderer must surface "(inherited from ...)" on the relevant
// rows so the contributor knows which rules are local vs. upstream.
func TestRenderBriefing_InheritedAttribution(t *testing.T) {
	in := fixtureBriefingInput(false)
	body, err := renderBriefing(in, onboardFormatMarkdown, "rev")
	if err != nil {
		t.Fatalf("renderBriefing: %v", err)
	}
	if !strings.Contains(body, "_(from PHILO)_") {
		t.Errorf("expected reading-list inheritance note from PHILO; body=%q", body)
	}
}

// TestRenderBriefing_EmptyEdgeCases — a project with no memory, no
// runbooks, no agents must produce a graceful skeleton (welcome +
// description + where-to-look) rather than an error.
func TestRenderBriefing_EmptyEdgeCases(t *testing.T) {
	in := briefingInput{
		project: projectDetail{ID: 1, Key: "EMPTY", Name: "Empty"},
		bundle: &bundlePayload{
			Project:   projectSummary{ID: 1, Key: "EMPTY"},
			Agent:     json.RawMessage(`{}`),
			FetchedAt: "2026-05-08T13:00:00Z",
		},
		readingListSize: 10,
	}
	body, err := renderBriefing(in, onboardFormatMarkdown, "emptyrev")
	if err != nil {
		t.Fatalf("renderBriefing: %v", err)
	}
	wantSubstrings := []string{
		"<!-- paimos: onboarded EMPTY@emptyrev",
		"# Welcome to Empty",
		"## What this project is",
		"_No project description on file yet",
		"## Where to look",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Errorf("empty briefing missing %q\nbody=%s", want, body)
		}
	}
	// Sections that have no data must NOT render their headings (we
	// only emit a heading when there's at least one row to show).
	if strings.Contains(body, "## Key external systems") {
		t.Errorf("empty briefing should not render external-systems heading")
	}
	if strings.Contains(body, "## Reading list") {
		t.Errorf("empty briefing should not render reading-list heading")
	}
	if strings.Contains(body, "## Recent context") {
		t.Errorf("empty briefing should not render recent-context heading")
	}
}

// TestParseOnboardHeader — round-trip the header builder with both
// agent / project-only variants and confirm parser extracts the rev
// + agent fields. Pin malformed inputs as ok=false so --check exits
// 2 (not 1) when the file isn't paimos-managed.
func TestParseOnboardHeader(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantRev   string
		wantAgent string
		wantOK    bool
	}{
		{
			name:    "project-only",
			body:    "<!-- paimos: onboarded BON26@abc123def456 at 2026-05-08T13:00:00Z -->\n\n# Welcome",
			wantRev: "abc123def456", wantOK: true,
		},
		{
			name:      "with-agent",
			body:      "<!-- paimos: onboarded BON26@deadbeef1234 [agent=ops] at 2026-05-08T13:00:00Z -->\n\n# Welcome",
			wantRev:   "deadbeef1234",
			wantAgent: "ops", wantOK: true,
		},
		{
			name:   "no-header",
			body:   "# A briefing without a paimos header\n",
			wantOK: false,
		},
		{
			name:   "malformed-no-rev",
			body:   "<!-- paimos: onboarded BON26 at 2026 -->",
			wantOK: false,
		},
		{
			name:    "leading-bom-and-whitespace",
			body:    "\xef\xbb\xbf  \n<!-- paimos: onboarded P@xyz at t -->",
			wantRev: "xyz", wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev, agent, ok := parseOnboardHeader(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if rev != tc.wantRev {
				t.Errorf("rev=%q, want %q", rev, tc.wantRev)
			}
			if agent != tc.wantAgent {
				t.Errorf("agent=%q, want %q", agent, tc.wantAgent)
			}
		})
	}
}

// TestRunOnboardCheck_Identical — the same rev in the header and on
// disk → exit 0, "identical" message.
func TestRunOnboardCheck_Identical(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "briefing.md")
	body := "<!-- paimos: onboarded BON26@samerev1234 at 2026-05-08T13:00:00Z -->\n\n# Welcome\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := runOnboardCheck(path, "samerev1234", onboardFormatMarkdown); err != nil {
		t.Errorf("expected nil (identical), got %v", err)
	}
}

// TestRunOnboardCheck_Drift — header rev differs from current rev →
// exit 1, drift message on stderr.
func TestRunOnboardCheck_Drift(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "briefing.md")
	body := "<!-- paimos: onboarded BON26@oldrev123 at 2026-05-08T13:00:00Z -->\n\n# Welcome\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := runOnboardCheck(path, "newrev456", onboardFormatMarkdown)
	ce, ok := err.(*checkExitCode)
	if !ok {
		t.Fatalf("err type=%T, want *checkExitCode (%v)", err, err)
	}
	if ce.code != 1 {
		t.Errorf("exit code=%d, want 1 (drift)", ce.code)
	}
}

// TestRunOnboardCheck_HeaderMissing — file exists but has no paimos
// header → exit 2 (not paimos-managed).
func TestRunOnboardCheck_HeaderMissing(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "briefing.md")
	body := "# A hand-written briefing without a paimos header\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := runOnboardCheck(path, "anyrev", onboardFormatMarkdown)
	ce, ok := err.(*checkExitCode)
	if !ok {
		t.Fatalf("err type=%T, want *checkExitCode (%v)", err, err)
	}
	if ce.code != 2 {
		t.Errorf("exit code=%d, want 2 (header missing)", ce.code)
	}
}

// TestRunOnboardCheck_FileMissing — passing a path to a non-existent
// file returns exit 1 (would be created on render).
func TestRunOnboardCheck_FileMissing(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nope.md")
	err := runOnboardCheck(path, "anyrev", onboardFormatMarkdown)
	ce, ok := err.(*checkExitCode)
	if !ok {
		t.Fatalf("err type=%T, want *checkExitCode (%v)", err, err)
	}
	if ce.code != 1 {
		t.Errorf("exit code=%d, want 1 (missing file)", ce.code)
	}
}

// TestResolveOnboardFormat — md / html / "" accepted, anything else
// is a usageError so the CLI fails fast with exit 2.
func TestResolveOnboardFormat(t *testing.T) {
	cases := []struct {
		raw       string
		want      onboardFormat
		wantUsage bool
	}{
		{raw: "", want: onboardFormatMarkdown},
		{raw: "md", want: onboardFormatMarkdown},
		{raw: "MD", want: onboardFormatMarkdown},
		{raw: "html", want: onboardFormatHTML},
		{raw: " HTML ", want: onboardFormatHTML},
		{raw: "pdf", wantUsage: true},
	}
	for _, tc := range cases {
		got, err := resolveOnboardFormat(tc.raw)
		if tc.wantUsage {
			if _, ok := err.(*usageError); !ok {
				t.Errorf("raw=%q: want usageError, got %v", tc.raw, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("raw=%q: unexpected err %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("raw=%q: got %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestSortByConfidenceAndRecency — high beats medium beats low; on
// confidence tie, most-recent last_referenced_at wins; on full tie,
// slug alphabetical for determinism.
func TestSortByConfidenceAndRecency(t *testing.T) {
	entries := []knowledgeEntry{
		{Slug: "z-low", Metadata: map[string]any{"confidence": "low"}},
		{Slug: "a-high-old", Metadata: map[string]any{"confidence": "high", "last_referenced_at": "2026-01-01T00:00:00Z"}},
		{Slug: "b-high-new", Metadata: map[string]any{"confidence": "high", "last_referenced_at": "2026-05-01T00:00:00Z"}},
		{Slug: "c-med-norec", Metadata: map[string]any{"confidence": "medium"}},
		{Slug: "d-no-confidence", Metadata: map[string]any{}}, // → medium
	}
	sortByConfidenceAndRecency(entries)
	wantOrder := []string{"b-high-new", "a-high-old", "c-med-norec", "d-no-confidence", "z-low"}
	for i, e := range entries {
		if e.Slug != wantOrder[i] {
			t.Errorf("index %d: got %q, want %q (full=%v)", i, e.Slug, wantOrder[i], slugsOf(entries))
		}
	}
}

func slugsOf(entries []knowledgeEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Slug
	}
	return out
}

// TestExcerpt — caps at n chars, prefers a word boundary, suffixes
// with ellipsis on truncation. Below the cap the body round-trips.
func TestExcerpt(t *testing.T) {
	if got := excerpt("short", 50); got != "short" {
		t.Errorf("short body should round-trip; got %q", got)
	}
	if got := excerpt(strings.Repeat("a", 200), 100); !strings.HasSuffix(got, "…") {
		t.Errorf("long body should be truncated with ellipsis; got %q", got)
	}
	long := "The quick brown fox jumps over the lazy dog and then runs away because of the noise."
	got := excerpt(long, 30)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix; got %q", got)
	}
	if len(got) > 31 { // 30 + ellipsis (3-byte UTF-8) — give a little slack
		t.Errorf("excerpt longer than expected: %q (%d bytes)", got, len(got))
	}
}

// ── end-to-end CLI ──────────────────────────────────────────────────

// startOnboardAPI returns a fake server wired with every endpoint the
// onboard verb hits: project list, project detail, agents list, agent
// artifact, the five knowledge endpoints, /api/auth/me, and the
// projects/:id/issues query for "Recent context".
func startOnboardAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":42,"key":"BON26","name":"Bonelio"}]`))
		case r.Method == http.MethodGet && path == "/api/projects/42":
			_, _ = w.Write([]byte(`{"id":42,"key":"BON26","name":"Bonelio","description":"Bonelio is the customer-facing accruals app."}`))
		case r.Method == http.MethodGet && path == "/api/projects/42/agents":
			_, _ = w.Write([]byte(`[{"name":"ops"}]`))
		case r.Method == http.MethodGet && path == "/api/projects/42/agents/ops.json":
			_, _ = w.Write([]byte(`{
				"project": {"id":42,"key":"BON26","name":"Bonelio"},
				"agent": {
					"name": "ops",
					"description": "Operate the production rig.",
					"body": "Long body here for excerpt rendering.",
					"bootstrap_steps": [{"title":"On-call","command":"echo hi","rationale":"because"}],
					"non_negotiable_rules": [{"title":"No prod pushes after 16:00","body":"cut-off","memory_ref":"deploy_window"}],
					"metadata": {"environments": ["prod"]}
				}
			}`))
		case r.Method == http.MethodGet && path == "/api/projects/42/memory":
			_, _ = w.Write([]byte(`[
				{"id":1,"project_id":42,"type":"memory","slug":"imac_rule","title":"Use imac for prod","body":"SSH imac.","status":"backlog","metadata":{"scope":"project","confidence":"high"},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && path == "/api/projects/42/runbooks":
			_, _ = w.Write([]byte(`[
				{"id":2,"project_id":42,"type":"runbook","slug":"deploy","title":"Deploy","body":"step 1","status":"backlog","metadata":{},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && path == "/api/projects/42/external-systems":
			_, _ = w.Write([]byte(`[
				{"id":3,"project_id":42,"type":"external_system","slug":"sentry","title":"Sentry","body":"errors","status":"backlog","metadata":{"url":"https://sentry.example"},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && path == "/api/projects/42/related-projects":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && path == "/api/projects/42/guidelines":
			_, _ = w.Write([]byte(`[
				{"id":4,"project_id":42,"type":"guideline","slug":"tickets","title":"Every commit references PAI","body":"yes","status":"backlog","metadata":{"confidence":"high"},"created_at":"","updated_at":""}
			]`))
		case r.Method == http.MethodGet && path == "/api/projects/42/issues":
			_, _ = w.Write([]byte(`[
				{"issue_key":"BON26-100","title":"Refresh deploy creds","status":"done","updated_at":"2026-05-07T18:00:00Z"},
				{"issue_key":"BON26-99","title":"Fix flaky test","status":"delivered","updated_at":"2026-05-06T11:00:00Z"}
			]`))
		case r.Method == http.MethodGet && path == "/api/auth/me":
			_, _ = w.Write([]byte(`{"user":{"id":7,"username":"mba"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/memory/references"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, `{"error":"unmocked: `+r.Method+" "+path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestOnboard_ProjectLevel_E2E — end-to-end: drives `paimos onboard`
// against the fake server, captures stdout, and asserts the rendered
// briefing carries the required sections (no agent role).
func TestOnboard_ProjectLevel_E2E(t *testing.T) {
	srv := startOnboardAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	useTempCWD(t)

	out, _, err := executeCLIForTest(t, "onboard", "--project", "BON26")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	wantSubstrings := []string{
		"<!-- paimos: onboarded BON26@",
		"# Welcome to Bonelio",
		"## What this project is",
		"Bonelio is the customer-facing accruals app",
		"## Key external systems",
		"https://sentry.example",
		"## How we work",
		"Every commit references PAI",
		"## Recent context",
		"BON26-100",
		"## Where to look",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n--- stdout ---\n%s", want, out)
		}
	}
	// No agent role section without --agent.
	if strings.Contains(out, "If you're playing the") {
		t.Errorf("project-only briefing should not include agent role section")
	}
}

// TestOnboard_AgentLevel_E2E — same, but with --agent ops. Asserts
// the agent role section + the [agent=ops] header tag.
func TestOnboard_AgentLevel_E2E(t *testing.T) {
	srv := startOnboardAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	useTempCWD(t)

	out, _, err := executeCLIForTest(t, "onboard", "--project", "BON26", "--agent", "ops")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	wantSubstrings := []string{
		"[agent=ops]",
		"## If you're playing the ops role",
		"Operate the production rig.",
		"### Bootstrap steps",
		"### Non-negotiable rules",
		"No prod pushes after 16:00",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n--- stdout ---\n%s", want, out)
		}
	}
}

// TestOnboard_HTMLFormat_E2E — --format html writes a self-contained
// HTML document to --out. Asserts DOCTYPE, embedded styles, and the
// drift-detection header.
func TestOnboard_HTMLFormat_E2E(t *testing.T) {
	srv := startOnboardAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	tmp := useTempCWD(t)
	out := filepath.Join(tmp, "brief.html")

	stdout, _, err := executeCLIForTest(t,
		"onboard", "--project", "BON26", "--format", "html", "--out", out)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(stdout, "wrote ") {
		t.Errorf("expected confirmation line, got %q", stdout)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}
	wantSubstrings := []string{
		"<!DOCTYPE html>",
		"<style>",
		"<!-- paimos: onboarded BON26@",
		"<h1>Welcome to Bonelio</h1>",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %q in HTML\n%s", want, string(body))
		}
	}
}

// TestOnboard_CheckMode_E2E — render once, then `--check` against the
// same file: expect exit 0 (identical). Mutate the rev and expect 1
// (drift); strip the header and expect 2 (out of management surface).
func TestOnboard_CheckMode_E2E(t *testing.T) {
	srv := startOnboardAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	tmp := useTempCWD(t)
	briefPath := filepath.Join(tmp, "briefing.md")

	// Render.
	if _, _, err := executeCLIForTest(t,
		"onboard", "--project", "BON26", "--out", briefPath); err != nil {
		t.Fatalf("render: %v", err)
	}

	// Identical: exit 0.
	if _, _, err := executeCLIForTest(t,
		"onboard", "--project", "BON26", "--out", briefPath, "--check"); err != nil {
		t.Errorf("identical check: expected nil err, got %v", err)
	}

	// Mutate the file's rev → drift.
	body, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	mutated := strings.Replace(string(body), "<!-- paimos: onboarded BON26@",
		"<!-- paimos: onboarded BON26@deadrev00000 oldat=", 1)
	if err := os.WriteFile(briefPath, []byte(mutated), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	_, _, err = executeCLIForTest(t,
		"onboard", "--project", "BON26", "--out", briefPath, "--check")
	ce, ok := err.(*checkExitCode)
	if !ok {
		t.Fatalf("drift: err type=%T (%v)", err, err)
	}
	if ce.code != 1 {
		t.Errorf("drift: exit %d, want 1", ce.code)
	}

	// Strip header → exit 2.
	if err := os.WriteFile(briefPath, []byte("# Hand-written, no paimos header\n"), 0o644); err != nil {
		t.Fatalf("strip header: %v", err)
	}
	_, _, err = executeCLIForTest(t,
		"onboard", "--project", "BON26", "--out", briefPath, "--check")
	ce, ok = err.(*checkExitCode)
	if !ok {
		t.Fatalf("header-missing: err type=%T (%v)", err, err)
	}
	if ce.code != 2 {
		t.Errorf("header-missing: exit %d, want 2", ce.code)
	}
}

// TestOnboard_InvalidFormat — --format pdf is a usageError before any
// network call (no project list / agent fetch).
func TestOnboard_InvalidFormat(t *testing.T) {
	t.Setenv(envURL, "https://example.test")
	t.Setenv(envAPIKey, "test_key")
	useTempCWD(t)

	_, _, err := executeCLIForTest(t, "onboard", "--project", "BON26", "--format", "pdf")
	if err == nil {
		t.Fatal("expected usage error for --format pdf")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("err type=%T, want *usageError (%v)", err, err)
	}
}
