// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/cmd/paimos/sync"
)

// newRunServer serves a canned run detail for GET /api/runs/{id} and records
// the body of every PATCH so a test can assert the status transitions.
func newRunServer(t *testing.T, detail string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	patches := &[]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/runs/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(detail))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/runs/"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			*patches = append(*patches, body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, `{"error":"unmocked"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, patches
}

func implementEvent() sync.Event {
	return sync.Event{Type: "implement_requested", Name: "PAI-5", Rev: "1"}
}

func TestAgentRunnerSuccess(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"running"}`)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp/repo",
		autoConfirm: true,
		spawn: func(_ context.Context, root, _ string, env []string) error {
			spawned = true
			if root != "/tmp/repo" {
				t.Errorf("spawn root=%q, want /tmp/repo", root)
			}
			if strings.Join(env, " ") != "PAIMOS_RUN_ID=1 PAIMOS_ISSUE_KEY=PAI-5" {
				t.Errorf("spawn env=%v", env)
			}
			return nil
		},
	}
	if err := a.handle(context.Background(), implementEvent()); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !spawned {
		t.Error("spawn was not called")
	}
	if len(*patches) != 2 || (*patches)[0]["status"] != "running" || (*patches)[1]["status"] != "tests_passed" {
		t.Fatalf("patches=%+v, want running then tests_passed", *patches)
	}
}

func TestAgentRunnerSpawnFailure(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"running"}`)
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true,
		spawn:       func(_ context.Context, _, _ string, _ []string) error { return errors.New("exit 1") },
	}
	if err := a.handle(context.Background(), implementEvent()); err == nil {
		t.Fatal("expected an error when the spawned command fails")
	}
	if len(*patches) != 2 || (*patches)[0]["status"] != "running" || (*patches)[1]["status"] != "failed" {
		t.Fatalf("patches=%+v, want running then failed", *patches)
	}
}

func TestAgentRunnerDeviceTargetingSkips(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"other-device","status":"queued"}`)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: true,
		spawn:       func(_ context.Context, _, _ string, _ []string) error { spawned = true; return nil },
	}
	if err := a.handle(context.Background(), implementEvent()); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if spawned {
		t.Error("a run targeted at another device must not spawn")
	}
	if len(*patches) != 0 {
		t.Errorf("no patches expected for a skipped run, got %+v", *patches)
	}
}

func TestAgentRunnerDeclineCancels(t *testing.T) {
	srv, patches := newRunServer(t, `{"issue_id":5,"device_id":"","status":"queued"}`)
	spawned := false
	a := &agentRunner{
		client: newClientForTest(srv.URL), deviceID: "dev-1", repoRoot: "/tmp",
		autoConfirm: false,
		confirm:     func(_ string, _ int64, _ string) bool { return false },
		spawn:       func(_ context.Context, _, _ string, _ []string) error { spawned = true; return nil },
	}
	if err := a.handle(context.Background(), implementEvent()); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if spawned {
		t.Error("a declined run must not spawn")
	}
	if len(*patches) != 1 || (*patches)[0]["status"] != "cancelled" {
		t.Fatalf("patches=%+v, want a single cancelled", *patches)
	}
}
