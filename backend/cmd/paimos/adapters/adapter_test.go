// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

import (
	"strings"
	"testing"
)

// stubAdapter is a minimal Adapter for registry/dispatch tests so we
// don't depend on the claude-code adapter package (avoids a cycle —
// claude-code imports this package).
type stubAdapter struct {
	name     string
	version  string
	supports string
	describe string
	render   func([]byte) (RenderResult, error)
}

func (s *stubAdapter) Name() string                              { return s.name }
func (s *stubAdapter) Version() string                           { return s.version }
func (s *stubAdapter) Supports() string                          { return s.supports }
func (s *stubAdapter) Describe() string                          { return s.describe }
func (s *stubAdapter) Render(c []byte) (RenderResult, error)     { return s.render(c) }

func newStub(name, supports string, content string) *stubAdapter {
	return &stubAdapter{
		name:     name,
		version:  "1.2.3",
		supports: supports,
		describe: "stub adapter for tests",
		render: func(_ []byte) (RenderResult, error) {
			return RenderResult{Content: content, SuggestedPath: ".x/" + name + ".md"}, nil
		},
	}
}

// TestRegistry_GetUnknownLists verifies the missing-adapter error
// names the known adapters so the user can fix a typo.
func TestRegistry_GetUnknownLists(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("alpha", ">=1.0.0 <2.0.0", "x"))
	r.Register(newStub("beta", ">=1.0.0 <2.0.0", "x"))
	_, err := r.Get("gamma")
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Fatalf("expected known adapters in error, got %q", err.Error())
	}
}

// TestRegistry_ListSorted: list-adapters output must be deterministic.
func TestRegistry_ListSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("zoo", "", "x"))
	r.Register(newStub("alpha", "", "x"))
	r.Register(newStub("middle", "", "x"))
	got := r.List()
	want := []string{"alpha", "middle", "zoo"}
	if len(got) != 3 {
		t.Fatalf("got %d adapters, want 3", len(got))
	}
	for i, a := range got {
		if a.Name() != want[i] {
			t.Fatalf("idx %d: got %q want %q", i, a.Name(), want[i])
		}
	}
}

func TestCheckSupports_Match(t *testing.T) {
	cases := []struct {
		rangeExpr string
		version   string
	}{
		{">=1.0.0 <2.0.0", "1.0.0"},
		{">=1.0.0 <2.0.0", "1.5.3"},
		{">=1.0.0 <2.0.0", "1.99.99"},
		{"=1.0.0", "1.0.0"},
		{"!=2.0.0", "1.5.0"},
		{"", "1.0.0"}, // empty range = no constraint
	}
	for _, tc := range cases {
		if err := CheckSupports(tc.rangeExpr, tc.version); err != nil {
			t.Fatalf("CheckSupports(%q, %q) unexpected error: %v", tc.rangeExpr, tc.version, err)
		}
	}
}

func TestCheckSupports_Mismatch(t *testing.T) {
	cases := []struct {
		rangeExpr string
		version   string
	}{
		{">=1.0.0 <2.0.0", "2.0.0"},
		{">=1.0.0 <2.0.0", "0.9.0"},
		{"=1.0.0", "1.0.1"},
	}
	for _, tc := range cases {
		if err := CheckSupports(tc.rangeExpr, tc.version); err == nil {
			t.Fatalf("CheckSupports(%q, %q) expected mismatch error", tc.rangeExpr, tc.version)
		}
	}
}

// TestDispatch_VersionMismatch is the AC requirement: clear error when
// the adapter's supports range rejects the canonical schema. Don't
// silently render through.
func TestDispatch_VersionMismatch(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("v2only", ">=2.0.0 <3.0.0", "body"))
	d := &Dispatch{Registry: r}
	out, err := d.Render(RenderRequest{
		Canonical:   []byte(`{"canonical_schema_version":"1.0.0","project":{"key":"X"},"agent":{"name":"a"}}`),
		HarnessName: "v2only",
		ProjectKey:  "X",
		AgentName:   "a",
	})
	if err == nil {
		t.Fatal("expected version-mismatch error")
	}
	if out != nil {
		t.Fatalf("expected nil output on mismatch, got %+v", out)
	}
	if !strings.Contains(err.Error(), "v2only") {
		t.Fatalf("error should name the adapter, got %q", err.Error())
	}
}

// TestDispatch_HeaderInjected: the rendered body must start with the
// canonical paimos header. PAI-331 reads it.
func TestDispatch_HeaderInjected(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("h", ">=1.0.0 <2.0.0", "Hello world\n"))
	d := &Dispatch{Registry: r}
	out, err := d.Render(RenderRequest{
		Canonical:   []byte(`{"project":{"key":"X"},"agent":{"name":"a"}}`),
		HarnessName: "h",
		ProjectKey:  "X",
		AgentName:   "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Body, HeaderPrefix) {
		t.Fatalf("body did not start with header: %q", out.Body[:min(120, len(out.Body))])
	}
	if !strings.Contains(out.Body, "X/a@") {
		t.Fatalf("header missing project/agent ref: %q", out.Body)
	}
	if !strings.Contains(out.Body, "harness=h") {
		t.Fatalf("header missing harness= field: %q", out.Body)
	}
	if !strings.Contains(out.Body, "Hello world") {
		t.Fatal("body content lost")
	}
	if out.Rev == "" {
		t.Fatal("rev should be populated")
	}
}

// TestDispatch_DefaultsToV1: when canonical_schema_version is absent,
// dispatch uses 1.0.0 as the assumed version (PAI-329 shipped that).
func TestDispatch_DefaultsToV1(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("h", ">=1.0.0 <2.0.0", "x"))
	d := &Dispatch{Registry: r}
	_, err := d.Render(RenderRequest{
		Canonical:   []byte(`{"project":{"key":"P"},"agent":{"name":"a"}}`),
		HarnessName: "h",
		ProjectKey:  "P",
		AgentName:   "a",
	})
	if err != nil {
		t.Fatalf("expected default v1 to satisfy >=1.0.0 <2.0.0, got %v", err)
	}
}

// TestCompare_Identical / Diff / HeaderMissing pin the --check exit
// classification.
func TestCompare_Cases(t *testing.T) {
	rendered := "<!-- paimos: rendered from P/a@abc harness=h -->\n\nbody\n"

	if got := Compare(rendered, rendered); got != CheckIdentical {
		t.Fatalf("identical: got %d", got)
	}

	mutated := "<!-- paimos: rendered from P/a@abc harness=h -->\n\nbody changed\n"
	if got := Compare(rendered, mutated); got != CheckDiff {
		t.Fatalf("diff: got %d", got)
	}

	noHeader := "body without header\n"
	if got := Compare(rendered, noHeader); got != CheckHeaderMissing {
		t.Fatalf("missing header: got %d", got)
	}
}

func TestCanonicalRev_StableAcrossWhitespace(t *testing.T) {
	a := []byte(`{"project":{"key":"P"},"agent":{"name":"a"}}`)
	b := []byte(`{
		"project": {"key": "P"},
		"agent": {"name": "a"}
	}`)
	if canonicalRev(a) != canonicalRev(b) {
		t.Fatalf("rev should be whitespace-insensitive: %s vs %s",
			canonicalRev(a), canonicalRev(b))
	}
}

func TestBuildHeader_Format(t *testing.T) {
	h := BuildHeader("BON26", "ops", "abc123def456", "claude-code")
	want := "<!-- paimos: rendered from BON26/ops@abc123def456 harness=claude-code -->"
	if h != want {
		t.Fatalf("got %q want %q", h, want)
	}
}

func TestHasHeader_TolerantToBOM(t *testing.T) {
	body := "\xef\xbb\xbf<!-- paimos: rendered from X/a@abc harness=h -->\n"
	if !HasHeader(body) {
		t.Fatal("BOM-prefixed header should still match")
	}
}

// TestManifest_ParseValidatesProtocolVersion: PAI-332 v1 manifests
// pass; future-version manifests fail with a clear error rather than
// being silently truncated. Missing protocol_version is tolerated for
// backward compat with PAI-330 manifests and filled in.
func TestManifest_ParseValidatesProtocolVersion(t *testing.T) {
	// Missing — tolerated, filled in.
	m, err := ParseManifest([]byte(`{"name":"a","version":"1.0.0"}`))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if m.ProtocolVersion != ProtocolVersion {
		t.Fatalf("missing protocol_version should default to %q, got %q", ProtocolVersion, m.ProtocolVersion)
	}

	// Match — pass.
	if _, err := ParseManifest([]byte(`{"protocol_version":"1","name":"a"}`)); err != nil {
		t.Fatalf("v1 should pass: %v", err)
	}

	// Mismatch — reject.
	_, err = ParseManifest([]byte(`{"protocol_version":"2","name":"a"}`))
	if err == nil {
		t.Fatal("expected error for unsupported protocol_version")
	}
	if !strings.Contains(err.Error(), "protocol_version") {
		t.Fatalf("error should mention protocol_version: %q", err.Error())
	}
}

// TestManifest_RequiresName ensures the SDK rejects nameless manifests
// — a registry without unique names is meaningless.
func TestManifest_RequiresName(t *testing.T) {
	if _, err := ParseManifest([]byte(`{"protocol_version":"1"}`)); err == nil {
		t.Fatal("expected error for missing name")
	}
}

// TestManifest_RejectsInvalidSemver ensures malformed adapter versions
// are caught at manifest-load time, not at dispatch time.
func TestManifest_RejectsInvalidSemver(t *testing.T) {
	_, err := ParseManifest([]byte(`{"name":"a","version":"not-a-version"}`))
	if err == nil {
		t.Fatal("expected version validation error")
	}
}

// TestManifest_LegacyAliasesAccepted: PAI-330 used `describe` and
// `suggested_path`; PAI-332's canonical names are `description` and
// `target_path_template`. Loaders accept both for back-compat.
func TestManifest_LegacyAliasesAccepted(t *testing.T) {
	m, err := ParseManifest([]byte(`{
		"name": "old",
		"describe": "legacy describe",
		"suggested_path": ".x/{{.agent.name}}.md"
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := m.effectiveDescription(); got != "legacy describe" {
		t.Fatalf("legacy describe alias not honoured: %q", got)
	}
	if got := m.effectiveTargetPath(); got != ".x/{{.agent.name}}.md" {
		t.Fatalf("legacy suggested_path alias not honoured: %q", got)
	}
}

// TestManifest_MarshalEmitsCanonicalShape: when paimos serves a
// manifest (registry endpoint) or an adapter binary emits one via
// `describe`, the output must use the canonical names so external
// tooling has one shape to parse.
func TestManifest_MarshalEmitsCanonicalShape(t *testing.T) {
	m := Manifest{
		Name:          "x",
		Version:       "1.0.0",
		Describe:      "legacy describe",
		SuggestedPath: ".x.md",
	}
	raw, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, `"description":"legacy describe"`) {
		t.Fatalf("legacy describe should be promoted to description: %s", got)
	}
	if !strings.Contains(got, `"target_path_template":".x.md"`) {
		t.Fatalf("legacy suggested_path should be promoted to target_path_template: %s", got)
	}
	if strings.Contains(got, `"describe"`) || strings.Contains(got, `"suggested_path"`) {
		t.Fatalf("legacy field names must not appear in canonical output: %s", got)
	}
	if !strings.Contains(got, `"protocol_version":"1"`) {
		t.Fatalf("canonical output must declare protocol_version: %s", got)
	}
}

// TestManifestOf_FallsBackForBareAdapters: an adapter that doesn't
// implement ManifestProvider still produces a uniform v1 manifest so
// the registry endpoint can list it.
func TestManifestOf_FallsBackForBareAdapters(t *testing.T) {
	a := newStub("bare", ">=1.0.0 <2.0.0", "x")
	got := ManifestOf(a)
	if got.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol_version: %q", got.ProtocolVersion)
	}
	if got.Name != "bare" {
		t.Fatalf("name: %q", got.Name)
	}
	if got.Supports != ">=1.0.0 <2.0.0" {
		t.Fatalf("supports: %q", got.Supports)
	}
	if got.InputFormat != "json" {
		t.Fatalf("input_format default should be json, got %q", got.InputFormat)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
