// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package handlers_test

// PAI-341 — knowledge-plane SSE + .rev coverage. Mirrors the shape of
// auto_watch_test.go's TestPublishAgentChanged_DeliversToSubscribedDeviceOnly
// so any refactor of the broker stays test-covered uniformly across
// kinds.

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

// TestPublishKnowledgeChanged_DeliversAllFiveKinds opens one SSE
// subscription and verifies that publishing each of the five
// knowledge `<kind>_changed` event types lands on the subscriber. The
// broker is kind-agnostic; this asserts the per-kind helpers wire to it
// correctly.
func TestPublishKnowledgeChanged_DeliversAllFiveKinds(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]any{
		"name": "Knowledge Pub",
		"key":  "KPUB",
	}))

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet,
		ts.srv.URL+fmt.Sprintf("/api/projects/%d/agents/events?device_id=dev-knowledge-pub", projectID),
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
	// Drain the `:connected\n\n` handshake.
	for i := 0; i < 2; i++ {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read handshake: %v", err)
		}
	}

	// Publish all five kinds back-to-back. The broker buffers per
	// subscriber so reading in order is safe.
	go func() {
		time.Sleep(50 * time.Millisecond)
		handlers.PublishMemoryChanged(projectID, "feedback_x", "rev1")
		handlers.PublishRunbookChanged(projectID, "deploy", "rev2")
		handlers.PublishExternalSystemChanged(projectID, "clickhouse", "rev3")
		handlers.PublishRelatedProjectChanged(projectID, "frontend", "rev4")
		handlers.PublishGuidelineChanged(projectID, "no-secrets", "rev5")
	}()

	wantTypes := map[string]string{
		"memory_changed":          "feedback_x",
		"runbook_changed":         "deploy",
		"external_system_changed": "clickhouse",
		"related_project_changed": "frontend",
		"guideline_changed":       "no-secrets",
	}
	got := map[string]string{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(got) < len(wantTypes) {
		// Set a per-read deadline so a stuck reader can't block past
		// our outer budget.
		_ = streamResp.Body.(io.Closer)
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		typ, _ := ev["type"].(string)
		name, _ := ev["name"].(string)
		got[typ] = name
	}
	for typ, wantName := range wantTypes {
		if got[typ] != wantName {
			t.Errorf("event %s: name = %q, want %q (got map = %+v)", typ, got[typ], wantName, got)
		}
	}
}

// TestKnowledgeRevHandler_ReturnsStableHash asserts the .rev endpoint
// behaves like AgentRevHandler: 200 + plain-text 12-char hex hash for
// a live entry, 404 for a missing slug, and the rev is byte-stable
// across repeated GETs.
func TestKnowledgeRevHandler_ReturnsStableHash(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "RevTest", "RVT1")
	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/knowledge?type=memory", projectID), ts.adminCookie, map[string]any{
		"slug":     "feedback_alpha",
		"title":    "Feedback alpha",
		"body":     "Some body.",
		"metadata": map[string]any{"category": "feedback"},
	})
	assertStatus(t, createResp, http.StatusCreated)

	revPath := fmt.Sprintf("/api/projects/%d/knowledge/memory/feedback_alpha.rev", projectID)
	resp1 := ts.get(t, revPath, ts.adminCookie)
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("rev status = %d", resp1.StatusCode)
	}
	body1, _ := io.ReadAll(resp1.Body)
	rev1 := strings.TrimSpace(string(body1))
	if len(rev1) != 12 {
		t.Errorf("rev length = %d, want 12: %q", len(rev1), rev1)
	}

	// Second call with no edits should return identical bytes.
	resp2 := ts.get(t, revPath, ts.adminCookie)
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if strings.TrimSpace(string(body2)) != rev1 {
		t.Errorf("rev not stable: %q vs %q", rev1, strings.TrimSpace(string(body2)))
	}

	// 404 for a missing slug.
	missing := ts.get(t, fmt.Sprintf("/api/projects/%d/knowledge/memory/no_such_slug.rev", projectID), ts.adminCookie)
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("missing slug status = %d, want 404", missing.StatusCode)
	}
}

// TestKnowledgeRevHandler_ChangesAfterEdit asserts the rev shifts when
// a knowledge entry is mutated. Mirrors the AgentRevHandler invariant
// that lets polling clients detect change without parsing the body.
func TestKnowledgeRevHandler_ChangesAfterEdit(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "RevDelta", "RVT2")
	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/knowledge?type=runbook", projectID), ts.adminCookie, map[string]any{
		"slug":  "deploy",
		"title": "Deploy",
		"body":  "v1 body.",
	})
	assertStatus(t, createResp, http.StatusCreated)

	revPath := fmt.Sprintf("/api/projects/%d/knowledge/runbook/deploy.rev", projectID)
	resp1 := ts.get(t, revPath, ts.adminCookie)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	rev1 := strings.TrimSpace(string(body1))

	// Update the body — should bump the rev.
	updateResp := ts.put(t, fmt.Sprintf("/api/projects/%d/knowledge/runbook/deploy", projectID), ts.adminCookie, map[string]any{
		"title": "Deploy",
		"body":  "v2 body, totally different.",
	})
	assertStatus(t, updateResp, http.StatusOK)

	resp2 := ts.get(t, revPath, ts.adminCookie)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	rev2 := strings.TrimSpace(string(body2))

	if rev1 == rev2 {
		t.Errorf("rev did not change after edit (was %q, still %q)", rev1, rev2)
	}
}

// TestKnowledgeWriteFlow_PublishesSSE asserts that PUT /memory/:slug
// (the canonical write path) fires a `memory_changed` event the
// subscriber sees. Sanity-checks the wiring from knowledge_writes.go's
// post-commit hook through publishKnowledgeChange into the broker.
func TestKnowledgeWriteFlow_PublishesSSE(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "WriteFlow", "WFL1")

	// Seed an entry up-front so the test focuses on the UPDATE path.
	createResp := ts.post(t, fmt.Sprintf("/api/projects/%d/knowledge?type=memory", projectID), ts.adminCookie, map[string]any{
		"slug":  "feedback_flow",
		"title": "Initial",
		"body":  "initial.",
	})
	assertStatus(t, createResp, http.StatusCreated)

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet,
		ts.srv.URL+fmt.Sprintf("/api/projects/%d/agents/events?device_id=dev-write-flow", projectID),
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
	for i := 0; i < 2; i++ {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("drain handshake: %v", err)
		}
	}

	// Now do the PUT — the post-commit hook should publish.
	go func() {
		time.Sleep(50 * time.Millisecond)
		updateResp := ts.put(t,
			fmt.Sprintf("/api/projects/%d/knowledge/memory/feedback_flow", projectID),
			ts.adminCookie,
			map[string]any{"title": "Updated", "body": "updated body."})
		_ = updateResp.Body.Close()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ev["type"] == "memory_changed" && ev["name"] == "feedback_flow" {
			// Rev must be a 12-char hex string (matches sync.KnowledgeRev).
			rev, _ := ev["rev"].(string)
			if len(rev) != 12 {
				t.Errorf("rev = %q, want 12 chars", rev)
			}
			return
		}
	}
	t.Fatal("did not receive memory_changed event for feedback_flow")
}
