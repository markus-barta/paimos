// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

// PAI-349 — `paimos memory propose` verb coverage. The verb is a thin
// POST wrapper, so the tests exercise:
//
//   1. Required-flag validation (--project, --title).
//   2. The on-the-wire payload shape (status='proposed', metadata
//      carries originating_tickets when --originating-ticket is set).
//   3. Slug derivation from the title when --suggested-name is omitted.
//   4. The standard PAI-324 attribution header forwarding.
//
// The fake API server captures the POST body so each assertion can
// inspect what reached the wire.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// proposeCapture is the request capture struct shared across the
// table-driven cases. The lock guards a slice of POSTs the fake
// server records so cases that fire multiple POSTs can assert per-call.
type proposeCapture struct {
	mu    sync.Mutex
	posts []proposePOST
}

type proposePOST struct {
	Path    string
	Body    map[string]any
	Headers http.Header
}

// startProposeFakeAPI returns a fake server that handles
// /api/projects (project resolution) and the propose POST. The POST
// returns 201 with a stub knowledge-entry payload so the CLI's
// response-decode path is also exercised.
func startProposeFakeAPI(t *testing.T, cap *proposeCapture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":42,"key":"BON26"}]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/memory"):
			body, _ := io.ReadAll(r.Body)
			var parsed map[string]any
			_ = json.Unmarshal(body, &parsed)
			cap.mu.Lock()
			cap.posts = append(cap.posts, proposePOST{
				Path:    r.URL.Path,
				Body:    parsed,
				Headers: r.Header.Clone(),
			})
			cap.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"id": 1, "project_id": 42, "type": "memory",
				"slug": "stub", "title": "stub", "body": "",
				"status": "proposed", "metadata": {},
				"created_at": "", "updated_at": ""
			}`))
		default:
			http.Error(w, `{"error":"unmocked: `+r.Method+" "+r.URL.Path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMemoryPropose_PayloadShape — the verb POSTs status='proposed',
// builds metadata.originating_tickets[] from --originating-ticket,
// and forwards X-Paimos-Agent-Name on the write.
func TestMemoryPropose_PayloadShape(t *testing.T) {
	cap := &proposeCapture{}
	srv := startProposeFakeAPI(t, cap)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	t.Setenv("PAIMOS_AGENT_NAME", "ops")
	t.Setenv("PAIMOS_SESSION_ID", "session-test")

	_, _, err := executeCLIForTest(t,
		"memory", "propose",
		"--project", "BON26",
		"--type", "feedback",
		"--title", "Thread dump lock signature match",
		"--body", "Bot draft body.",
		"--originating-ticket", "BON26-492",
		"--suggested-name", "feedback_thread_dump_lock",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(cap.posts) != 1 {
		t.Fatalf("expected 1 POST, got %d", len(cap.posts))
	}
	post := cap.posts[0]
	if post.Path != "/api/projects/42/memory" {
		t.Errorf("path = %q, want /api/projects/42/memory", post.Path)
	}
	// Status must be 'proposed' on the wire.
	if status, _ := post.Body["status"].(string); status != "proposed" {
		t.Errorf("status = %v, want proposed", post.Body["status"])
	}
	if slug, _ := post.Body["slug"].(string); slug != "feedback_thread_dump_lock" {
		t.Errorf("slug = %v, want feedback_thread_dump_lock", post.Body["slug"])
	}
	// Metadata.originating_tickets[] holds the value.
	meta, _ := post.Body["metadata"].(map[string]any)
	tickets, _ := meta["originating_tickets"].([]any)
	if len(tickets) != 1 || tickets[0] != "BON26-492" {
		t.Errorf("originating_tickets = %v, want [BON26-492]", tickets)
	}
	// Metadata.type carries the taxonomy.
	if got, _ := meta["type"].(string); got != "feedback" {
		t.Errorf("metadata.type = %v, want feedback", meta["type"])
	}
	// Attribution headers forwarded.
	if got := post.Headers.Get("X-Paimos-Agent-Name"); got != "ops" {
		t.Errorf("X-Paimos-Agent-Name = %q, want ops", got)
	}
	if got := post.Headers.Get("X-Paimos-Session-Id"); got != "session-test" {
		t.Errorf("X-Paimos-Session-Id = %q, want session-test", got)
	}
}

// TestMemoryPropose_AutoSlugFromTitle — when --suggested-name is
// omitted, the slug derives from the title (lowercased, non-ASCII
// collapsed to `_`).
func TestMemoryPropose_AutoSlugFromTitle(t *testing.T) {
	cap := &proposeCapture{}
	srv := startProposeFakeAPI(t, cap)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t,
		"memory", "propose",
		"--project", "BON26",
		"--title", "Deploy needs manual restart of Y",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(cap.posts) != 1 {
		t.Fatalf("expected 1 POST, got %d", len(cap.posts))
	}
	got, _ := cap.posts[0].Body["slug"].(string)
	want := "deploy_needs_manual_restart_of_y"
	if got != want {
		t.Errorf("auto-slug = %q, want %q", got, want)
	}
}

// TestMemoryPropose_RequiresProjectAndTitle — usage errors for the
// two required flags surface as *usageError so main() maps them to
// exit code 2.
func TestMemoryPropose_RequiresProjectAndTitle(t *testing.T) {
	t.Setenv(envURL, "http://localhost:0")
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "memory", "propose")
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("missing flags: got %T %v, want *usageError", err, err)
	}

	_, _, err = executeCLIForTest(t, "memory", "propose", "--project", "BON26")
	if err == nil {
		t.Fatal("expected usage error for missing --title")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("missing title: got %T %v, want *usageError", err, err)
	}
}

// TestSuggestSlugFromTitle — pin the slug derivation rules so the
// CLI and frontend stay aligned (PAI-339's suggestSlug uses the same
// rules: lowercase, non-ASCII → _, trim, prefix `m_` for digit-leading).
func TestSuggestSlugFromTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Deploy needs manual restart of Y", "deploy_needs_manual_restart_of_y"},
		{"  Multiple   Spaces  ", "multiple_spaces"},
		{"123 starts with digit", "m_123_starts_with_digit"},
		{"Already-slug-like", "already-slug-like"},
		{"", ""},
	}
	for _, c := range cases {
		got := suggestSlugFromTitle(c.in)
		if got != c.want {
			t.Errorf("suggestSlugFromTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
