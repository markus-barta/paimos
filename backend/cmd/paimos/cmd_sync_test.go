// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

// PAI-331. End-to-end CLI tests for `paimos sync init/pull/check` and
// the convenience `paimos skill init/pull/check` wrappers. Watch is
// covered separately because its long-lived loop needs a server-driven
// disconnect to assert termination.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeAgentsListJSON = `[{"name":"qa"},{"name":"ops"}]`

func startFakeSyncAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects":
			_, _ = w.Write([]byte(`[{"id":7,"key":"ACME","name":"Acme Widgets"}]`))
		case "/api/projects/7/agents":
			_, _ = w.Write([]byte(fakeAgentsListJSON))
		case "/api/projects/7/agents/qa.json":
			_, _ = w.Write([]byte(strings.ReplaceAll(fakeArtifactJSON, `"name": "qa"`, `"name": "qa"`)))
		case "/api/projects/7/agents/ops.json":
			body := strings.ReplaceAll(fakeArtifactJSON, `"name": "qa"`, `"name": "ops"`)
			body = strings.ReplaceAll(body, `"slash_command_name": "qa"`, `"slash_command_name": "ops"`)
			_, _ = w.Write([]byte(body))
		default:
			http.Error(w, `{"error":"unexpected route"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSyncInit_PullsAllAgents(t *testing.T) {
	srv := startFakeSyncAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	work := t.TempDir()
	if _, _, err := executeCLIForTest(t,
		"sync", "init",
		"--project", "ACME",
		"--workspace", work,
	); err != nil {
		t.Fatalf("sync init: %v", err)
	}

	for _, name := range []string{"qa", "ops"} {
		path := filepath.Join(work, ".claude", "commands", name+".md")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if !strings.HasPrefix(string(body), "<!-- paimos: rendered from ACME/"+name+"@") {
			t.Errorf("%s missing canonical header: %.80q", path, string(body))
		}
	}
}

func TestSyncPull_KindAndNameNarrow(t *testing.T) {
	srv := startFakeSyncAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	work := t.TempDir()
	if _, _, err := executeCLIForTest(t,
		"sync", "pull",
		"--project", "ACME",
		"--workspace", work,
		"--kind", "skill",
		"--name", "qa",
	); err != nil {
		t.Fatalf("sync pull: %v", err)
	}

	if _, err := os.Stat(filepath.Join(work, ".claude", "commands", "qa.md")); err != nil {
		t.Fatalf("qa.md not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, ".claude", "commands", "ops.md")); !os.IsNotExist(err) {
		t.Errorf("ops.md should NOT be written when --name=qa: %v", err)
	}
}

func TestSyncPull_NameRequiresKind(t *testing.T) {
	srv := startFakeSyncAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t,
		"sync", "pull",
		"--project", "ACME",
		"--name", "qa",
	)
	if err == nil {
		t.Fatal("expected usage error for --name without --kind")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("err type %T, want *usageError", err)
	}
}

func TestSyncCheck_NoDriftExitsZero(t *testing.T) {
	srv := startFakeSyncAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	work := t.TempDir()
	// Seed the local cache.
	if _, _, err := executeCLIForTest(t, "sync", "init",
		"--project", "ACME", "--workspace", work,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out, _, err := executeCLIForTest(t, "sync", "check",
		"--project", "ACME", "--workspace", work,
	)
	if err != nil {
		t.Fatalf("sync check should succeed: %v", err)
	}
	if !strings.Contains(out, "no drift") {
		t.Errorf("stdout should report no drift:\n%s", out)
	}
}

func TestSyncCheck_DriftExits1(t *testing.T) {
	srv := startFakeSyncAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	work := t.TempDir()
	if _, _, err := executeCLIForTest(t, "sync", "init",
		"--project", "ACME", "--workspace", work,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Mutate one of the rendered files to force drift.
	target := filepath.Join(work, ".claude", "commands", "qa.md")
	if err := os.WriteFile(target,
		[]byte("<!-- paimos: rendered from ACME/qa@stale harness=claude-code -->\n\nedited\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := executeCLIForTest(t, "sync", "check",
		"--project", "ACME", "--workspace", work,
	)
	if err == nil {
		t.Fatal("sync check should report drift")
	}
	ce, ok := err.(*checkExitCode)
	if !ok {
		t.Fatalf("err type %T, want *checkExitCode", err)
	}
	if ce.code != 1 {
		t.Errorf("exit = %d, want 1", ce.code)
	}
}

func TestSkillInit_ConvenienceWrapper(t *testing.T) {
	srv := startFakeSyncAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	work := t.TempDir()
	if _, _, err := executeCLIForTest(t, "skill", "init",
		"--project", "ACME", "--workspace", work,
	); err != nil {
		t.Fatalf("skill init: %v", err)
	}
	// Convenience wrapper should produce same effect as `sync init --kind=skill`.
	if _, err := os.Stat(filepath.Join(work, ".claude", "commands", "qa.md")); err != nil {
		t.Fatalf("qa.md not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, ".claude", "commands", "ops.md")); err != nil {
		t.Fatalf("ops.md not written: %v", err)
	}
}

func TestSkillPull_AgentFlagAlias(t *testing.T) {
	srv := startFakeSyncAPI(t)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	work := t.TempDir()
	if _, _, err := executeCLIForTest(t, "skill", "pull",
		"--project", "ACME",
		"--workspace", work,
		"--agent", "qa",
	); err != nil {
		t.Fatalf("skill pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, ".claude", "commands", "qa.md")); err != nil {
		t.Fatalf("qa.md not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, ".claude", "commands", "ops.md")); !os.IsNotExist(err) {
		t.Errorf("ops.md should NOT be written when --agent=qa: %v", err)
	}
}

func TestSyncInit_RequiresProject(t *testing.T) {
	t.Setenv(envURL, "https://example.test")
	t.Setenv(envAPIKey, "test_key")
	_, _, err := executeCLIForTest(t, "sync", "init")
	if err == nil {
		t.Fatal("expected usage error")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("err type %T, want *usageError", err)
	}
}
