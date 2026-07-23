// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// moveMockServer stands in for the API: it resolves the OPS project and the
// issue refs, and records the body posted to the move endpoint(s).
func moveMockServer(t *testing.T, capture *map[string]any, capturePath *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":6,"key":"OPS"},{"id":1,"key":"PAI"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-1":
			_, _ = w.Write([]byte(`{"id":101,"issue_key":"PAI-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-2":
			_, _ = w.Write([]byte(`{"id":202,"issue_key":"PAI-2"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/202/move":
			*capturePath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(capture)
			_, _ = w.Write([]byte(`{"issue_id":202,"old_key":"PAI-2","new_key":"OPS-1","project_id":6,"detached":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/move":
			*capturePath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(capture)
			_, _ = w.Write([]byte(`{"moved":2,"failed":0,"results":[{"issue_id":101,"ok":true,"result":{"old_key":"PAI-1","new_key":"OPS-1","detached":[]}},{"issue_id":202,"ok":true,"result":{"old_key":"PAI-2","new_key":"OPS-2","detached":[]}}]}`))
		default:
			http.Error(w, fmt.Sprintf(`{"error":"unexpected %s %s"}`, r.Method, r.URL.Path), http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestIssueMoveResolvesRefsAndPostsToMoveEndpoint(t *testing.T) {
	var body map[string]any
	var path string
	srv := moveMockServer(t, &body, &path)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	if _, _, err := executeCLIForTest(t, "issue", "move", "PAI-2", "--to", "OPS"); err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if path != "/api/issues/202/move" {
		t.Fatalf("posted to %q, want single move endpoint", path)
	}
	pid, ok := body["project_id"].(float64)
	if !ok || pid != 6 {
		t.Errorf("project_id = %v, want 6", body["project_id"])
	}
}

func TestIssueUpdateProjectRoutesToMove(t *testing.T) {
	var body map[string]any
	var path string
	srv := moveMockServer(t, &body, &path)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	// `issue update <ref> --project` must go to the move endpoint, not PUT.
	if _, _, err := executeCLIForTest(t, "issue", "update", "PAI-2", "--project", "OPS"); err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if path != "/api/issues/202/move" {
		t.Fatalf("update --project posted to %q, want the move endpoint", path)
	}
	if pid, _ := body["project_id"].(float64); pid != 6 {
		t.Errorf("project_id = %v, want 6", body["project_id"])
	}
}

func TestIssueUpdateProjectRejectsCombinedFlags(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":"should not be called"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "issue", "update", "PAI-2", "--project", "OPS", "--status", "done")
	if err == nil {
		t.Fatal("expected a usage error combining --project with --status")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("error type = %T, want *usageError", err)
	}
	if requests != 0 {
		t.Errorf("made %d API calls, want 0 (should fail before any request)", requests)
	}
}

func TestIssueMoveBulkUsesBulkEndpoint(t *testing.T) {
	var body map[string]any
	var path string
	srv := moveMockServer(t, &body, &path)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	if _, _, err := executeCLIForTest(t, "issue", "move", "PAI-1", "PAI-2", "--to", "OPS"); err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if path != "/api/issues/move" {
		t.Fatalf("posted to %q, want bulk move endpoint", path)
	}
	ids, ok := body["issue_ids"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("issue_ids = %v, want two ids", body["issue_ids"])
	}
	if ids[0].(float64) != 101 || ids[1].(float64) != 202 {
		t.Errorf("issue_ids = %v, want [101 202]", ids)
	}
}
