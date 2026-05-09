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
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"os"
)

// PAI-343 — CLI side of the lesson-capture flow.
//
// Coverage groups:
//
//   1. suggestMemorySlug shape — mirrors the backend's
//      handlers.SuggestMemorySlug + the frontend's TypeScript
//      version. Pure, deterministic, no I/O.
//
//   2. End-to-end --draft-memory headless capture — issues
//      hits the four expected routes (PUT issue, GET ticket,
//      POST memory, POST relation) in the right order with the
//      right payloads.
//
//   3. Hint path — without --draft-memory, a terminal-status
//      transition that the trigger flagged should print a one-
//      line nudge but NOT make any extra writes.

func TestSuggestMemorySlug_CLIVersion(t *testing.T) {
	cases := []struct {
		typ, rule, want string
	}{
		{"feedback", "Use --line-buffered in pipes", "feedback_use_line_buffered_in_pipes"},
		{"feedback", "", "feedback_lesson"},
		{"", "Quick win", "feedback_quick_win"},
		{"feedback", "One two three four five six seven eight", "feedback_one_two_three_four_five_six"},
	}
	for _, c := range cases {
		got := suggestMemorySlug(c.typ, c.rule)
		if got != c.want {
			t.Errorf("suggestMemorySlug(%q, %q) = %q, want %q", c.typ, c.rule, got, c.want)
		}
	}
}

// captured tracks each request the fake API saw, in order. Used by
// the end-to-end --draft-memory test below to assert the call
// sequence + payloads.
type capturedRequest struct {
	method string
	path   string
	body   map[string]any
}

func TestIssueUpdate_DraftMemoryFlow(t *testing.T) {
	// Stage write-bodies + ticket fetch + memory create + relation
	// create. The fake server records every request; the test asserts
	// the four-call sequence and the memory creation body shape.
	dir := t.TempDir()
	whyPath := filepath.Join(dir, "why.md")
	howPath := filepath.Join(dir, "how.md")
	if err := os.WriteFile(whyPath, []byte("Cause: stale cache."), 0o600); err != nil {
		t.Fatalf("seed why: %v", err)
	}
	if err := os.WriteFile(howPath, []byte("Apply when: stale cache symptom."), 0o600); err != nil {
		t.Fatalf("seed how: %v", err)
	}

	var (
		mu     sync.Mutex
		seen   []capturedRequest
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		mu.Lock()
		seen = append(seen, capturedRequest{method: r.Method, path: r.URL.Path, body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/PAI-1":
			_, _ = w.Write([]byte(`{"id":42,"issue_key":"PAI-1","status":"done","project_id":6}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-1":
			_, _ = w.Write([]byte(`{"id":42,"issue_key":"PAI-1","project_id":6}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/6/memory":
			_, _ = w.Write([]byte(`{"id":555,"slug":"feedback_use_line_buffered_in_pipes"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/PAI-1/relations":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"source_id":42,"target_id":555,"type":"applies_to_memory"}`))
		default:
			http.Error(w, `{"error":"unmocked"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	if _, _, err := executeCLIForTest(t,
		"issue", "update", "PAI-1",
		"--status", "done",
		"--draft-memory",
		"--memory-rule", "Use --line-buffered in pipes",
		"--memory-why-file", whyPath,
		"--memory-how-file", howPath,
		"--memory-tags", "cli, logging",
	); err != nil {
		t.Fatalf("CLI: %v", err)
	}

	// Verify the four calls fired in the expected order.
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 4 {
		t.Fatalf("call count = %d, want 4: %+v", len(seen), seen)
	}
	want := []struct{ method, path string }{
		{http.MethodPut, "/api/issues/PAI-1"},
		{http.MethodGet, "/api/issues/PAI-1"},
		{http.MethodPost, "/api/projects/6/memory"},
		{http.MethodPost, "/api/issues/PAI-1/relations"},
	}
	for i, w := range want {
		if seen[i].method != w.method || seen[i].path != w.path {
			t.Errorf("call %d: got %s %s, want %s %s",
				i, seen[i].method, seen[i].path, w.method, w.path)
		}
	}

	// Memory POST body shape — title = rule, slug = auto, body
	// contains both sections, metadata.tags + originating_tickets.
	mb := seen[2].body
	if mb["title"] != "Use --line-buffered in pipes" {
		t.Errorf("memory.title=%v, want rule", mb["title"])
	}
	if mb["slug"] != "feedback_use_line_buffered_in_pipes" {
		t.Errorf("memory.slug=%v, want auto-suggested", mb["slug"])
	}
	bodyStr, _ := mb["body"].(string)
	if !strings.Contains(bodyStr, "## Why") || !strings.Contains(bodyStr, "## How to apply") {
		t.Errorf("memory.body missing Why / How sections: %q", bodyStr)
	}
	meta, _ := mb["metadata"].(map[string]any)
	if meta == nil {
		t.Fatalf("memory.metadata missing")
	}
	if meta["type"] != "feedback" {
		t.Errorf("metadata.type=%v, want feedback", meta["type"])
	}
	tags, _ := meta["tags"].([]any)
	if len(tags) != 2 || tags[0] != "cli" || tags[1] != "logging" {
		t.Errorf("metadata.tags=%v, want [cli, logging]", tags)
	}
	orig, _ := meta["originating_tickets"].([]any)
	if len(orig) != 1 {
		t.Fatalf("metadata.originating_tickets=%v, want 1 entry", orig)
	}
	first, _ := orig[0].(map[string]any)
	if first["key"] != "PAI-1" {
		t.Errorf("originating_tickets[0].key=%v, want PAI-1", first["key"])
	}

	// Relation POST body — target_id = memory id (555), type =
	// applies_to_memory.
	rb := seen[3].body
	if rb["type"] != "applies_to_memory" {
		t.Errorf("relation.type=%v, want applies_to_memory", rb["type"])
	}
	if rb["target_id"].(float64) != 555 {
		t.Errorf("relation.target_id=%v, want 555", rb["target_id"])
	}
}

func TestIssueUpdate_LessonCaptureHint_PrintsAndDoesNotWrite(t *testing.T) {
	// When the trigger fires but --draft-memory was NOT passed, the
	// CLI prints a one-line hint and stops. Two server calls only:
	// the PUT itself + the GET to /lesson-capture-prompt.
	var (
		mu   sync.Mutex
		seen []capturedRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, capturedRequest{method: r.Method, path: r.URL.Path})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/PAI-2":
			_, _ = w.Write([]byte(`{"id":43,"issue_key":"PAI-2","status":"done"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-2/lesson-capture-prompt":
			_, _ = w.Write([]byte(`{"should_prompt":true,"reason":"tag:bug","suggested_name":"feedback_crash","ticket_key":"PAI-2"}`))
		default:
			http.Error(w, `{"error":"unmocked: `+r.Method+` `+r.URL.Path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t,
		"issue", "update", "PAI-2",
		"--status", "done",
	)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(out, "lesson") || !strings.Contains(out, "--draft-memory") {
		t.Errorf("hint not printed; stdout=%q", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("call count = %d, want 2 (PUT + GET prompt): %+v", len(seen), seen)
	}
}

func TestRunDraftMemoryFlow_RequiresRule(t *testing.T) {
	c := newClientForTest("http://example.test")
	err := runDraftMemoryFlow(c, "PAI-1", "", "", "", "", "", "feedback", "", "")
	if err == nil {
		t.Fatal("expected error when --memory-rule is empty")
	}
	if !strings.Contains(err.Error(), "memory-rule") {
		t.Errorf("err=%q, want mention of memory-rule", err.Error())
	}
}

func TestRunDraftMemoryFlow_RejectsBadType(t *testing.T) {
	c := newClientForTest("http://example.test")
	err := runDraftMemoryFlow(c, "PAI-1", "rule", "x", "", "y", "", "wat", "", "")
	if err == nil {
		t.Fatal("expected error on bad memory-type")
	}
	if !strings.Contains(err.Error(), "memory-type") {
		t.Errorf("err=%q, want mention of memory-type", err.Error())
	}
}
