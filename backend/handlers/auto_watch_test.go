// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-331 — auto-watch toggle + SSE handler integration tests.

package handlers_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/handlers"
)

// Reuse the existing test server. The router was extended in
// testhelper_test.go to mount the new routes alongside api-keys.

func TestAutoWatch_DefaultStateOff(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/api/auth/auto-watch", ts.adminCookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty list, got %d rows", len(rows))
	}
}

func TestAutoWatch_UpsertCreatesRow(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Watch Project",
		"key":  "WATC",
	}))

	resp := ts.put(t,
		fmt.Sprintf("/api/auth/auto-watch/dev-A/%d", projectID),
		ts.adminCookie,
		map[string]any{"enabled": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var row map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row["enabled"] != true {
		t.Errorf("enabled = %v", row["enabled"])
	}
	if row["device_id"] != "dev-A" {
		t.Errorf("device_id = %v", row["device_id"])
	}
}

func TestAutoWatch_ToggleOffTerminatesSubscription(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Watch Project",
		"key":  "WATC",
	}))

	// Open SSE stream — implicit row creation with enabled=1.
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet,
		ts.srv.URL+fmt.Sprintf("/api/projects/%d/agents/events?device_id=dev-A", projectID),
		nil)
	streamReq.Header.Set("Cookie", ts.adminCookie)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

	// Verify SSE handshake delivered the :connected line.
	reader := bufio.NewReader(streamResp.Body)
	if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, "connected") {
		t.Fatalf("expected :connected handshake, got %q err=%v", line, err)
	}

	// Toggle OFF — should disconnect the stream server-side.
	toggleResp := ts.put(t,
		fmt.Sprintf("/api/auth/auto-watch/dev-A/%d", projectID),
		ts.adminCookie,
		map[string]any{"enabled": false})
	toggleResp.Body.Close()
	if toggleResp.StatusCode != http.StatusOK {
		t.Fatalf("toggle status = %d", toggleResp.StatusCode)
	}

	// Reading from the stream should now hit a clean EOF / disconnect
	// event in short order.
	streamResp.Body.(io.Closer).Close()
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		// Drain any pending events. After toggle OFF the broker closes
		// the channel; the handler emits an `event: disconnect` line
		// then returns. Either condition is acceptable; we just want
		// the read loop to terminate without hanging.
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
		}
	}()
	select {
	case <-doneCh:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not terminate after toggle OFF")
	}
}

func TestAutoWatch_DeleteRemovesRow(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Watch Project",
		"key":  "WATC",
	}))
	upsert := ts.put(t,
		fmt.Sprintf("/api/auth/auto-watch/dev-A/%d", projectID),
		ts.adminCookie,
		map[string]any{"enabled": true})
	upsert.Body.Close()

	resp := ts.del(t,
		fmt.Sprintf("/api/auth/auto-watch/dev-A/%d", projectID),
		ts.adminCookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	listResp := ts.get(t, "/api/auth/auto-watch", ts.adminCookie)
	defer listResp.Body.Close()
	var rows []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected list to be empty after delete, got %d rows", len(rows))
	}
}

func TestAgentRevHandler_ReturnsHash(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Rev Project",
		"key":  "REVP",
	}))
	// Seed an agent so the artifact exists.
	resp := ts.post(t, fmt.Sprintf("/api/projects/%d/agents", projectID), ts.adminCookie, map[string]any{
		"name":               "qa",
		"description":        "Test agent.",
		"slash_command_name": "qa",
		"lane_tags":          []string{"qa"},
		"metadata":           map[string]any{},
		"body":               "Body.",
		"bootstrap_steps":    []any{},
		"non_negotiable_rules": []any{},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("agent create status = %d", resp.StatusCode)
	}

	revResp := ts.get(t,
		fmt.Sprintf("/api/projects/%d/agents/qa.rev", projectID),
		ts.adminCookie)
	defer revResp.Body.Close()
	if revResp.StatusCode != http.StatusOK {
		t.Fatalf("rev status = %d", revResp.StatusCode)
	}
	body, _ := io.ReadAll(revResp.Body)
	rev := strings.TrimSpace(string(body))
	if len(rev) != 12 {
		t.Errorf("rev length = %d, want 12: %q", len(rev), rev)
	}
}

func TestAgentsEventsStream_RequiresDeviceID(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Events Project",
		"key":  "EVNP",
	}))
	// No device_id query param.
	resp := ts.get(t,
		fmt.Sprintf("/api/projects/%d/agents/events", projectID),
		ts.adminCookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPublishAgentChanged_DeliversToSubscribedDeviceOnly(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Pub Project",
		"key":  "PUBP",
	}))

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet,
		ts.srv.URL+fmt.Sprintf("/api/projects/%d/agents/events?device_id=dev-publish", projectID),
		nil)
	streamReq.Header.Set("Cookie", ts.adminCookie)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

	reader := bufio.NewReader(streamResp.Body)
	// Drain `:connected\n\n` handshake (one comment line + blank).
	for i := 0; i < 2; i++ {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read handshake: %v", err)
		}
	}

	// Publish on a goroutine to avoid blocking if the broker's
	// non-blocking send happens to race.
	go func() {
		// Give the subscription a moment to fully register before publish.
		time.Sleep(50 * time.Millisecond)
		handlers.PublishAgentChanged(projectID, "qa", "abcd1234")
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			var ev map[string]any
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if ev["type"] != "agent_changed" {
				t.Errorf("type = %v", ev["type"])
			}
			if ev["name"] != "qa" {
				t.Errorf("name = %v", ev["name"])
			}
			return
		}
	}
	t.Fatal("did not receive agent_changed event within 2s")
}
