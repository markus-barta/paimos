// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/cmd/paimos/adapters"
	"github.com/markus-barta/paimos/backend/cmd/paimos/adapters/claudecode"
)

// fakeClient is a minimal SyncClient implementation backed by a route
// table. Streaming is not exercised in this file — see watch_test.go.
type fakeClient struct {
	routes      map[string][]byte
	getCalls    []string
	streamRoute string
	streamFn    func(ctx context.Context, onEvent func(Event)) error
}

func (f *fakeClient) Get(path string) ([]byte, error) {
	f.getCalls = append(f.getCalls, path)
	body, ok := f.routes[path]
	if !ok {
		return nil, fmt.Errorf("404: %s", path)
	}
	return body, nil
}

func (f *fakeClient) Stream(ctx context.Context, path string, onEvent func(Event)) error {
	f.streamRoute = path
	if f.streamFn == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.streamFn(ctx, onEvent)
}

func newSkillResourceForTest(t *testing.T) *SkillResource {
	t.Helper()
	reg := adapters.NewRegistry()
	reg.Register(claudecode.New())
	disp := &adapters.Dispatch{Registry: reg}
	res, err := NewSkillResource(disp, "claude-code")
	if err != nil {
		t.Fatalf("NewSkillResource: %v", err)
	}
	return res
}

// canonicalArtifact returns a minimal but valid PAI-329 canonical
// artifact JSON — enough to drive the claude-code adapter.
func canonicalArtifact(projectKey, agentName string) []byte {
	return []byte(fmt.Sprintf(`{
		"project": {"id": 7, "name": "Acme", "key": %q},
		"agent": {
			"name": %q,
			"description": "Test agent.",
			"slash_command_name": %q,
			"lane_tags": ["qa"],
			"metadata": {},
			"body": "Body.",
			"bootstrap_steps": [],
			"non_negotiable_rules": []
		},
		"repos": [],
		"environments": [],
		"deploy_recipes": []
	}`, projectKey, agentName, agentName))
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Lookup("skill"); err == nil {
		t.Fatal("expected error for unknown kind")
	}

	res := newSkillResourceForTest(t)
	reg.Register(res)
	got, err := reg.Lookup("skill")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Kind() != "skill" {
		t.Fatalf("kind = %q, want skill", got.Kind())
	}
	if kinds := reg.Kinds(); len(kinds) != 1 || kinds[0] != "skill" {
		t.Fatalf("Kinds = %v, want [skill]", kinds)
	}
}

func TestRegistry_LookupErrorListsKnownKinds(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newSkillResourceForTest(t))
	_, err := reg.Lookup("memory")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "skill") {
		t.Fatalf("error should list known kinds, got %q", err.Error())
	}
}

func TestEventKind(t *testing.T) {
	cases := map[string]string{
		"agent_changed":           "skill",
		"memory_changed":          "memory",
		"runbook_changed":         "runbook",
		"external_system_changed": "external_system",
		"":                        "",
		"unrelated":               "",
	}
	for in, want := range cases {
		if got := EventKind(in); got != want {
			t.Errorf("EventKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventEndpoint_EncodesQueryParams(t *testing.T) {
	got := EventEndpoint(7, "dev-uuid", "skill", false)
	if !strings.Contains(got, "/api/projects/7/agents/events") {
		t.Errorf("base path missing: %s", got)
	}
	if !strings.Contains(got, "device_id=dev-uuid") {
		t.Errorf("device_id missing: %s", got)
	}
	if !strings.Contains(got, "kind=skill") {
		t.Errorf("kind missing: %s", got)
	}
	if strings.Contains(got, "implement") {
		t.Errorf("implement should be absent when false: %s", got)
	}
	if impl := EventEndpoint(7, "dev-uuid", "", true); !strings.Contains(impl, "implement=1") {
		t.Errorf("implement=1 missing: %s", impl)
	}
}

func TestRevEndpoint_EscapesAgentName(t *testing.T) {
	got := RevEndpoint(7, "ops")
	if got != "/api/projects/7/agents/ops.rev" {
		t.Errorf("got %q", got)
	}
}

func TestSkillResource_SyncWritesAdapterPathAndHeader(t *testing.T) {
	res := newSkillResourceForTest(t)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/agents":         []byte(`[{"name":"qa"},{"name":"ops"}]`),
			"/api/projects/7/agents/qa.json": canonicalArtifact("ACME", "qa"),
			"/api/projects/7/agents/ops.json": canonicalArtifact("ACME", "ops"),
		},
	}
	work := t.TempDir()

	var written []SyncedItem
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "", func(it SyncedItem) {
		written = append(written, it)
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("written = %d, want 2: %+v", len(written), written)
	}

	for _, it := range written {
		if it.Action != "wrote" {
			t.Errorf("first sync should write, got action=%q", it.Action)
		}
		body, err := os.ReadFile(it.Path)
		if err != nil {
			t.Fatalf("read %s: %v", it.Path, err)
		}
		if !strings.HasPrefix(string(body), "<!-- paimos: rendered from ACME/") {
			t.Errorf("missing canonical header in %s: %.80q", it.Path, string(body))
		}
	}
}

func TestSkillResource_SyncSelectName(t *testing.T) {
	res := newSkillResourceForTest(t)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/agents/qa.json": canonicalArtifact("ACME", "qa"),
		},
	}
	work := t.TempDir()

	var written []SyncedItem
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "qa", func(it SyncedItem) {
		written = append(written, it)
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(written) != 1 || written[0].Name != "qa" {
		t.Fatalf("written = %+v, want [qa]", written)
	}
	// Verify list endpoint was NOT called when selectName is set.
	for _, p := range c.getCalls {
		if p == "/api/projects/7/agents" {
			t.Errorf("list endpoint should be skipped on selectName, got call %q", p)
		}
	}
}

func TestSkillResource_SyncSecondRunMarksUnchanged(t *testing.T) {
	res := newSkillResourceForTest(t)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/agents":         []byte(`[{"name":"qa"}]`),
			"/api/projects/7/agents/qa.json": canonicalArtifact("ACME", "qa"),
		},
	}
	work := t.TempDir()

	if err := res.Sync(context.Background(), c, 7, "ACME", work, "", nil); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	var second []SyncedItem
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "", func(it SyncedItem) {
		second = append(second, it)
	}); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(second) != 1 || second[0].Action != "unchanged" {
		t.Fatalf("second sync action = %+v, want unchanged", second)
	}
}

func TestSkillResource_CheckReportsStates(t *testing.T) {
	res := newSkillResourceForTest(t)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/agents":            []byte(`[{"name":"qa"},{"name":"ops"}]`),
			"/api/projects/7/agents/qa.json":    canonicalArtifact("ACME", "qa"),
			"/api/projects/7/agents/ops.json":   canonicalArtifact("ACME", "ops"),
		},
	}
	work := t.TempDir()

	// Seed-render qa so its on-disk file matches; leave ops absent.
	if err := res.Sync(context.Background(), c, 7, "ACME", work, "qa", nil); err != nil {
		t.Fatalf("seed sync: %v", err)
	}

	records, err := res.Check(context.Background(), c, 7, "ACME", work)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	byName := map[string]CheckRecord{}
	for _, r := range records {
		byName[r.Name] = r
	}
	if byName["qa"].State != "identical" {
		t.Errorf("qa state = %q, want identical", byName["qa"].State)
	}
	if byName["ops"].State != "missing_local" {
		t.Errorf("ops state = %q, want missing_local", byName["ops"].State)
	}

	// Now seed ops with a hand-edited body (no header) to force
	// header_missing.
	target := filepath.Join(work, ".claude", "commands", "ops.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# hand authored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err = res.Check(context.Background(), c, 7, "ACME", work)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.Name == "ops" && r.State != "header_missing" {
			t.Errorf("ops state = %q, want header_missing", r.State)
		}
	}

	// And a header-present-but-stale body to force diff.
	if err := os.WriteFile(target, []byte("<!-- paimos: rendered from ACME/ops@stale harness=claude-code -->\n\nold content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err = res.Check(context.Background(), c, 7, "ACME", work)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.Name == "ops" && r.State != "diff" {
			t.Errorf("ops state = %q, want diff", r.State)
		}
	}
}

func TestSkillResource_KindIsStable(t *testing.T) {
	res := newSkillResourceForTest(t)
	if res.Kind() != "skill" {
		t.Errorf("kind drifted: %q", res.Kind())
	}
}

func TestNewSkillResource_ValidatesArgs(t *testing.T) {
	if _, err := NewSkillResource(nil, "claude-code"); err == nil {
		t.Error("expected error for nil dispatch")
	}
	reg := adapters.NewRegistry()
	disp := &adapters.Dispatch{Registry: reg}
	if _, err := NewSkillResource(disp, ""); err == nil {
		t.Error("expected error for empty harness")
	}
	if _, err := NewSkillResource(disp, "no-such-harness"); err == nil {
		t.Error("expected error for unknown harness")
	}
}

func TestSkillResource_SyncContextCancellation(t *testing.T) {
	res := newSkillResourceForTest(t)
	c := &fakeClient{
		routes: map[string][]byte{
			"/api/projects/7/agents":         []byte(`[{"name":"qa"},{"name":"ops"}]`),
			"/api/projects/7/agents/qa.json": canonicalArtifact("ACME", "qa"),
		},
	}
	work := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := res.Sync(ctx, c, 7, "ACME", work, "", nil)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled (or wrapping it)", err)
	}
}
