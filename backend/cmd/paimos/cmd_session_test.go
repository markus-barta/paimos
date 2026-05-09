// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResolveSessionFormat pins the precedence rules of the format
// resolver. The test matters because PAI-340 will layer a `files`
// constant on top of this resolver — keeping the rules clear here
// stops the future extension from accidentally re-introducing
// mutually-exclusive booleans.
func TestResolveSessionFormat(t *testing.T) {
	cases := []struct {
		name        string
		format      string
		exportFlag  bool
		localJSON   bool
		globalJSON  bool
		want        sessionFormat
		wantErr     bool
		errContains string
	}{
		{name: "default (export=true, no flags) → env", exportFlag: true, want: sessionFormatEnv},
		{name: "explicit --format env wins", format: "env", exportFlag: true, localJSON: true, want: sessionFormatEnv},
		{name: "explicit --format json wins over export", format: "json", exportFlag: true, want: sessionFormatJSON},
		{name: "case-insensitive --format JSON", format: "JSON", want: sessionFormatJSON},
		{name: "trim --format whitespace", format: "  env  ", want: sessionFormatEnv},
		{name: "--json alone → json", localJSON: true, want: sessionFormatJSON},
		{name: "global --json picks json too", globalJSON: true, want: sessionFormatJSON},
		// PAI-340: `files` is now a valid format too.
		{name: "explicit --format files wins", format: "files", want: sessionFormatFiles},
		{name: "case-insensitive --format FILES", format: "FILES", want: sessionFormatFiles},
		{name: "invalid --format with garbage", format: "yaml", wantErr: true, errContains: "expected env, json, or files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveSessionFormat(c.format, c.exportFlag, c.localJSON, c.globalJSON)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if c.wantErr {
				if c.errContains != "" && !containsFold(err.Error(), c.errContains) {
					t.Errorf("err=%q, want substring %q", err.Error(), c.errContains)
				}
				return
			}
			if got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// TestValidateAgentName covers the three branches the user is most
// likely to hit: the happy path, the typo'd-agent path (which has to
// list valid names so the agent can recover), and the empty-project
// path (a dedicated error so users know to declare an agent first).
func TestValidateAgentName(t *testing.T) {
	declared := []string{"ops", "qa", "dev"}

	if err := validateAgentName("ops", declared); err != nil {
		t.Fatalf("ops should be valid: %v", err)
	}

	err := validateAgentName("nope", declared)
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !containsFold(err.Error(), "not declared") {
		t.Errorf("err=%q, want 'not declared'", err.Error())
	}
	for _, n := range declared {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("err=%q must list valid agent %q for recovery", err.Error(), n)
		}
	}

	err = validateAgentName("ops", nil)
	if err == nil {
		t.Fatal("expected error when project has no agents")
	}
	if !containsFold(err.Error(), "no agents declared") {
		t.Errorf("err=%q, want 'no agents declared'", err.Error())
	}

	err = validateAgentName("", declared)
	if err == nil {
		t.Fatal("expected error for empty agent name")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("empty agent should return usageError, got %T", err)
	}
}

// TestNewSessionUUID is a smoke check that session IDs are unique &
// look like v4 UUIDs (8-4-4-4-12 hex with the `4` version nibble).
func TestNewSessionUUID(t *testing.T) {
	a, b := newSessionUUID(), newSessionUUID()
	if a == b {
		t.Fatal("two consecutive UUIDs collided")
	}
	if len(a) != 36 || strings.Count(a, "-") != 4 {
		t.Fatalf("unexpected UUID shape: %q", a)
	}
	// Position 14 is the version nibble; v4 → '4'.
	if a[14] != '4' {
		t.Errorf("UUID version nibble = %c, want 4 (got %q)", a[14], a)
	}
}

// startSessionFakeAPI returns a fake server that wires the two
// endpoints session-start hits: /api/projects (for project resolution)
// and /api/projects/{id}/agents (for the agents list).
func startSessionFakeAPI(t *testing.T, projects, agents string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(projects))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/projects/") && strings.HasSuffix(r.URL.Path, "/agents"):
			_, _ = w.Write([]byte(agents))
		default:
			http.Error(w, `{"error":"unmocked: `+r.Method+" "+r.URL.Path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSessionStart_ExportFormat — the AC line: "emits two `export` lines
// on stdout". Pin the exact prefix shape so a future refactor doesn't
// accidentally break shell `eval $(...)` consumers.
func TestSessionStart_ExportFormat(t *testing.T) {
	srv := startSessionFakeAPI(t,
		`[{"id":6,"key":"PAI"}]`,
		`[{"name":"ops"},{"name":"qa"}]`,
	)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "PAI",
		"--agent", "ops",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "export PAIMOS_AGENT_NAME=ops") {
		t.Errorf("line 0 = %q, want export PAIMOS_AGENT_NAME=ops", lines[0])
	}
	if !strings.HasPrefix(lines[1], "export PAIMOS_SESSION_ID=") {
		t.Errorf("line 1 = %q, want export PAIMOS_SESSION_ID=…", lines[1])
	}
	// session id part should look like a UUID.
	parts := strings.SplitN(lines[1], "=", 2)
	if len(parts) != 2 || len(parts[1]) != 36 {
		t.Errorf("session id part = %q, want 36-char UUID", lines[1])
	}
}

// TestSessionStart_ProjectByID — accepts numeric DB id alongside the
// project key. Same fake server, just pass the id instead.
func TestSessionStart_ProjectByID(t *testing.T) {
	srv := startSessionFakeAPI(t,
		`[{"id":6,"key":"PAI"}]`,
		`[{"name":"ops"}]`,
	)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "6",
		"--agent", "ops",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, "export PAIMOS_AGENT_NAME=ops") {
		t.Errorf("expected agent name in output, got %q", out)
	}
}

// TestSessionStart_JSONFormat — `--json` prints a JSON record with both
// fields populated, no `export` prefix anywhere.
func TestSessionStart_JSONFormat(t *testing.T) {
	srv := startSessionFakeAPI(t,
		`[{"id":6,"key":"PAI"}]`,
		`[{"name":"ops"}]`,
	)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "PAI",
		"--agent", "ops",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if strings.Contains(out, "export ") {
		t.Errorf("--format json should not emit export lines, got %q", out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out)
	}
	if got["agent_name"] != "ops" {
		t.Errorf("agent_name=%q, want ops", got["agent_name"])
	}
	if len(got["session_id"]) != 36 {
		t.Errorf("session_id=%q, want 36-char UUID", got["session_id"])
	}
}

// TestSessionStart_AgentNotDeclared — the validation error path. Must
// list the declared agents so the user can correct the typo without a
// follow-up paimos call.
func TestSessionStart_AgentNotDeclared(t *testing.T) {
	srv := startSessionFakeAPI(t,
		`[{"id":6,"key":"PAI"}]`,
		`[{"name":"ops"},{"name":"qa"}]`,
	)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "PAI",
		"--agent", "ghost",
	)
	if err == nil {
		t.Fatal("expected error for undeclared agent")
	}
	msg := err.Error()
	if !containsFold(msg, "not declared") {
		t.Errorf("err=%q, want 'not declared'", msg)
	}
	if !strings.Contains(msg, "ops") || !strings.Contains(msg, "qa") {
		t.Errorf("err must list valid agents (ops, qa); got %q", msg)
	}
}

// TestSessionStart_ProjectNotFound — a typo'd project key bails out
// before validating the agent (one network call, clean error).
func TestSessionStart_ProjectNotFound(t *testing.T) {
	srv := startSessionFakeAPI(t,
		`[{"id":6,"key":"PAI"}]`,
		`[{"name":"ops"}]`,
	)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "NOPE",
		"--agent", "ops",
	)
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
	if !containsFold(err.Error(), "not found") {
		t.Errorf("err=%q, want 'not found'", err.Error())
	}
}

// TestSessionStart_MissingFlags — usage errors must surface as
// *usageError so main() exits with code 2.
func TestSessionStart_MissingFlags(t *testing.T) {
	t.Setenv(envURL, "https://example.test")
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "session", "start")
	if err == nil {
		t.Fatal("expected usage error for missing flags")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("err type=%T, want *usageError (%v)", err, err)
	}
}

// TestSessionStart_InvalidFormat — bad --format value should be a
// usage error and must NOT make a network call.
func TestSessionStart_InvalidFormat(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":"should not be called"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t,
		"session", "start",
		"--project", "PAI",
		"--agent", "ops",
		"--format", "yaml",
	)
	if err == nil {
		t.Fatal("expected usage error for invalid --format")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("err type=%T, want *usageError", err)
	}
	if requests != 0 {
		t.Errorf("network was hit %d times; should be 0", requests)
	}
}

// TestSessionShow — when env is set, prints the values; when unset,
// prints "(unset)" placeholders so the user can see at a glance.
func TestSessionShow(t *testing.T) {
	t.Setenv("PAIMOS_AGENT_NAME", "ops")
	t.Setenv("PAIMOS_SESSION_ID", "abc-123")

	out, _, err := executeCLIForTest(t, "session", "show")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, "PAIMOS_AGENT_NAME=ops") {
		t.Errorf("out=%q, want PAIMOS_AGENT_NAME=ops", out)
	}
	if !strings.Contains(out, "PAIMOS_SESSION_ID=abc-123") {
		t.Errorf("out=%q, want PAIMOS_SESSION_ID=abc-123", out)
	}

	t.Setenv("PAIMOS_AGENT_NAME", "")
	t.Setenv("PAIMOS_SESSION_ID", "")
	out, _, err = executeCLIForTest(t, "session", "show")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, "(unset)") {
		t.Errorf("out=%q, want (unset) markers", out)
	}
}

// TestSessionEnd — emits unset lines by default; respects --format json.
func TestSessionEnd(t *testing.T) {
	out, _, err := executeCLIForTest(t, "session", "end")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, "unset PAIMOS_AGENT_NAME") {
		t.Errorf("out=%q, want unset PAIMOS_AGENT_NAME", out)
	}
	if !strings.Contains(out, "unset PAIMOS_SESSION_ID") {
		t.Errorf("out=%q, want unset PAIMOS_SESSION_ID", out)
	}

	out, _, err = executeCLIForTest(t, "session", "end", "--format", "json")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("decode JSON end output: %v\n%s", err, out)
	}
	if rec["ended"] != true {
		t.Errorf("expected ended=true, got %v", rec)
	}
}
