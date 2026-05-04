// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startFakeAPI serves canned JSON responses keyed by "<METHOD> <path>".
// Returns an httptest.Server scoped to the test (auto-closed via t.Cleanup).
// The router is intentionally tiny — we only need exact path/method
// matches for the helper-level tests.
func startFakeAPI(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		body, ok := routes[key]
		if !ok {
			http.Error(w, `{"error":"unmocked route: `+key+`"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newClientForTest builds a Client pointed at the test server. No
// API key is needed because the fake server doesn't enforce auth.
func newClientForTest(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{},
	}
}

// TestReadMultilineInput covers the file-vs-inline mutual exclusion
// rules that are the whole point of PAI-91: every mutation command
// promises "either --foo or --foo-file, never both, and file wins
// precedence when tests can't infer". Breaking this is how the
// shell-quoted-JSON foot-gun crept back in.
func TestReadMultilineInput(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "desc.md")
	fileContent := "# Heading\n\nBody with **markdown**.\n"
	if err := os.WriteFile(filePath, []byte(fileContent), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	cases := []struct {
		name        string
		inline      string
		file        string
		wantValue   string
		wantSet     bool
		wantErr     bool
		errContains string
	}{
		{
			name:      "neither set",
			wantValue: "",
			wantSet:   false,
		},
		{
			name:      "inline only",
			inline:    "single line",
			wantValue: "single line",
			wantSet:   true,
		},
		{
			name:      "file only",
			file:      filePath,
			wantValue: fileContent,
			wantSet:   true,
		},
		{
			name:        "both set → error",
			inline:      "x",
			file:        filePath,
			wantErr:     true,
			errContains: "mutually exclusive",
		},
		{
			name:        "file points at non-existent path",
			file:        filepath.Join(dir, "missing.md"),
			wantErr:     true,
			errContains: "no such file",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, set, err := readMultilineInput(tc.inline, tc.file, "description")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				if tc.errContains != "" && !containsFold(err.Error(), tc.errContains) {
					t.Errorf("err = %q, want substring %q", err.Error(), tc.errContains)
				}
				return
			}
			if got != tc.wantValue {
				t.Errorf("value = %q, want %q", got, tc.wantValue)
			}
			if set != tc.wantSet {
				t.Errorf("set = %v, want %v", set, tc.wantSet)
			}
		})
	}
}

// TestReadMultilineInput_Stdin verifies the "-" convention for
// file-flag → stdin. Uses a temp pipe since os.Stdin is process-wide.
func TestReadMultilineInput_Stdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	go func() {
		_, _ = w.Write([]byte("from stdin"))
		_ = w.Close()
	}()

	got, set, err := readMultilineInput("", "-", "description")
	if err != nil {
		t.Fatalf("readMultilineInput: %v", err)
	}
	if !set {
		t.Error("set = false, want true for stdin input")
	}
	if got != "from stdin" {
		t.Errorf("value = %q, want %q", got, "from stdin")
	}
}

// containsFold is case-insensitive substring check. "no such file" vs
// "No such file" differ across OSes, so the error-message assertion
// needs a fuzzy compare.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func executeCLIForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer

	oldStdout, oldStderr := stdout, stderr
	oldInstance, oldJSON, oldConfigPath := flagInstance, flagJSON, flagConfigPath
	stdout, stderr = &out, &errOut
	flagInstance, flagJSON, flagConfigPath = "", false, ""
	t.Cleanup(func() {
		stdout, stderr = oldStdout, oldStderr
		flagInstance, flagJSON, flagConfigPath = oldInstance, oldJSON, oldConfigPath
	})

	cmd := rootCmd()
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestParsePositiveInt64Flag(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    int64
		wantErr bool
	}{
		{name: "plain", raw: "2", want: 2},
		{name: "trim", raw: " 42 ", want: 42},
		{name: "zero", raw: "0", wantErr: true},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "text", raw: "mba", wantErr: true},
		{name: "blank", raw: " ", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parsePositiveInt64Flag("assignee", c.raw)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if c.wantErr {
				if !containsFold(err.Error(), "positive numeric id") {
					t.Errorf("err=%q, want positive numeric id", err.Error())
				}
				return
			}
			if got != c.want {
				t.Errorf("got=%d, want=%d", got, c.want)
			}
		})
	}
}

func TestIssueUpdateDryRun_AssigneeIDIsNumeric(t *testing.T) {
	t.Setenv(envURL, "https://example.test")
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t,
		"--json",
		"issue", "update", "PAI-1",
		"--status", "qa",
		"--assignee", "2",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	var got struct {
		Body map[string]any `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode dry-run JSON: %v\n%s", err, out)
	}
	if _, ok := got.Body["assignee_id"].(float64); !ok {
		t.Fatalf("assignee_id type = %T, want JSON number (body=%v)", got.Body["assignee_id"], got.Body)
	}
	if got.Body["assignee_id"].(float64) != 2 {
		t.Errorf("assignee_id=%v, want 2", got.Body["assignee_id"])
	}
	if got.Body["status"] != "qa" {
		t.Errorf("status=%v, want qa", got.Body["status"])
	}
}

func TestIssueUpdateCombinedRequest_AssigneeIDIsNumeric(t *testing.T) {
	var received map[string]any
	var handlerErr string
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPut || r.URL.Path != "/api/issues/PAI-1" {
			handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			handlerErr = fmt.Sprintf("decode request: %v", err)
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issue_key":"PAI-1","title":"x"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	if _, _, err := executeCLIForTest(t,
		"issue", "update", "PAI-1",
		"--status", "qa",
		"--assignee", "2",
	); err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if handlerErr != "" {
		t.Fatal(handlerErr)
	}
	if requests != 1 {
		t.Fatalf("requests=%d, want 1", requests)
	}
	if received["status"] != "qa" {
		t.Errorf("status=%v, want qa", received["status"])
	}
	if _, ok := received["assignee_id"].(float64); !ok {
		t.Fatalf("assignee_id type = %T, want JSON number (body=%v)", received["assignee_id"], received)
	}
	if received["assignee_id"].(float64) != 2 {
		t.Errorf("assignee_id=%v, want 2", received["assignee_id"])
	}
}

func TestIssueUpdateInvalidAssigneeFailsBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":"should not be called"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t,
		"issue", "update", "PAI-1",
		"--status", "qa",
		"--assignee", "mba",
	)
	if err == nil {
		t.Fatal("expected invalid assignee error")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("err type=%T, want *usageError (%v)", err, err)
	}
	if requests != 0 {
		t.Fatalf("requests=%d, want 0", requests)
	}
}

// ── PAI-260: issue tag add/rm helpers ────────────────────────────────

// TestRequireTagSelector pins the "exactly one of --tag / --tag-id"
// rule. Cobra's MarkFlagsMutuallyExclusive handles "both set" at parse
// time; the "neither set" case has to come from us so the user sees a
// helpful message instead of a silent no-op.
func TestRequireTagSelector(t *testing.T) {
	cases := []struct {
		name    string
		tagKey  string
		tagID   int64
		wantErr bool
	}{
		{"key only", "dev", 0, false},
		{"id only", "", 99, false},
		{"key with whitespace counts as set", "  dev  ", 0, false},
		{"neither", "", 0, true},
		{"only whitespace key", "   ", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := requireTagSelector(c.tagKey, c.tagID)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if c.wantErr && !containsFold(err.Error(), "--tag") {
				t.Errorf("expected the error to mention --tag, got %q", err.Error())
			}
		})
	}
}

// TestResolveTagSelector exercises the /api/tags lookup against an
// httptest server. The CLI uses this both for --tag <key> resolution
// (the common path) and for --tag-id validation (so a typo'd id fails
// here rather than as a silent no-op against the idempotent upstream
// DELETE endpoint).
func TestResolveTagSelector(t *testing.T) {
	srv := startFakeAPI(t, map[string]string{
		"GET /api/tags": `[
		  {"id": 99,  "name": "dev",  "color": "blue"},
		  {"id": 100, "name": "ops",  "color": "green"},
		  {"id": 200, "name": "lane:special", "color": "purple"}
		]`,
	})
	client := newClientForTest(srv.URL)

	cases := []struct {
		name    string
		tagKey  string
		tagID   int64
		wantID  int64
		wantNm  string
		wantErr string
	}{
		{name: "by key (dev)", tagKey: "dev", wantID: 99, wantNm: "dev"},
		{name: "by key with surrounding whitespace", tagKey: "  ops  ", wantID: 100, wantNm: "ops"},
		{name: "by id", tagID: 200, wantID: 200, wantNm: "lane:special"},
		{name: "id wins when both supplied (no precedence test in code, but id branch fires first)", tagID: 99, tagKey: "ignored", wantID: 99, wantNm: "dev"},
		{name: "unknown key 404s", tagKey: "nonexistent", wantErr: "not found"},
		{name: "unknown id 404s", tagID: 99999, wantErr: "not found"},
		{name: "case-sensitive (DEV != dev)", tagKey: "DEV", wantErr: "not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveTagSelector(client, c.tagKey, c.tagID)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("expected err containing %q, got nil", c.wantErr)
				}
				if !containsFold(err.Error(), c.wantErr) {
					t.Fatalf("err = %q, want substring %q", err.Error(), c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.ID != c.wantID || got.Name != c.wantNm {
				t.Errorf("got {%d, %q}, want {%d, %q}", got.ID, got.Name, c.wantID, c.wantNm)
			}
		})
	}
}
