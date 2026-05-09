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

// PAI-349 — backend coverage for the propose flow:
//
//   • POST /api/projects/:id/memory with status='proposed' lands a
//     row at status=proposed (not the default 'backlog').
//   • Rate-limit fires on the (limit+1)-th proposal in the same
//     (agent, session) and returns 429.
//   • PAIMOS_PROPOSE_DISABLED=1 returns 503.
//   • Stale proposed endpoint surfaces drafts older than N days and
//     omits fresh ones.
//
// The propose-gate is a hook wrapped around the canonical knowledge
// CreateEntryHook (PAI-353), so these tests use the existing
// /api/projects/:id/memory POST surface.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers"
)

// resetProposeLimiter is the bridge into the package-private
// limiter reset. We can't call the unexported helper from the _test
// package, so we reach in via a test-only export.
//
// Implemented by adding ResetProposeLimiterForTest in the handlers
// package; this file just calls it.
func resetProposeLimiter() {
	handlers.ResetProposeLimiterForTest()
}

// TestPropose_StatusLandsProposed asserts the basic flow: POSTing a
// memory entry with status="proposed" and valid attribution headers
// produces a row whose status is exactly "proposed".
func TestPropose_StatusLandsProposed(t *testing.T) {
	resetProposeLimiter()
	t.Setenv("PAIMOS_PROPOSE_DISABLED", "")
	t.Setenv("PAIMOS_PROPOSE_LIMIT_PER_SESSION", "")

	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-349 Propose", "P349P")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		knowledgeURL(projectID, "memory"),
		ts.adminCookie,
		map[string]any{
			"slug":   "feedback_thread_dump_lock",
			"title":  "Thread dump lock",
			"body":   "Bot draft body.",
			"status": "proposed",
			"metadata": map[string]any{
				"originating_tickets": []string{"BON26-492"},
			},
		},
		map[string]string{
			"X-Paimos-Agent-Name": "ops",
			"X-Paimos-Session-Id": "session-test-1",
		},
	)
	assertStatus(t, resp, http.StatusCreated)

	// Status round-trips through the canonical convenience-endpoint
	// shape (id + status + slug). The DB column is the canonical truth;
	// re-read via SQL to make sure no later step (system tags etc.)
	// mutated it.
	var status string
	if err := db.DB.QueryRow(
		`SELECT status FROM issues WHERE project_id=? AND type='memory' AND slug=?`,
		projectID, "feedback_thread_dump_lock").Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "proposed" {
		t.Fatalf("status: got %q, want %q", status, "proposed")
	}
}

// TestPropose_RateLimitReturns429 exercises the rate-limit gate.
// PAIMOS_PROPOSE_LIMIT_PER_SESSION=2 lets us trip the gate without
// flooding the test DB.
func TestPropose_RateLimitReturns429(t *testing.T) {
	resetProposeLimiter()
	t.Setenv("PAIMOS_PROPOSE_DISABLED", "")
	t.Setenv("PAIMOS_PROPOSE_LIMIT_PER_SESSION", "2")

	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-349 Rate", "P349R")

	headers := map[string]string{
		"X-Paimos-Agent-Name": "ops",
		"X-Paimos-Session-Id": "ratelimit-session",
	}
	post := func(slug string) *http.Response {
		return ts.requestWithHeaders(t, http.MethodPost,
			knowledgeURL(projectID, "memory"),
			ts.adminCookie,
			map[string]any{
				"slug":   slug,
				"title":  "Title " + slug,
				"body":   "body",
				"status": "proposed",
			},
			headers,
		)
	}

	// First two proposals: 201.
	for i := 1; i <= 2; i++ {
		resp := post(fmt.Sprintf("rl_%d", i))
		assertStatus(t, resp, http.StatusCreated)
	}

	// Third proposal (over cap): 429.
	resp := post("rl_3")
	assertStatus(t, resp, http.StatusTooManyRequests)

	// A different session sharing the same agent is independent —
	// fresh quota.
	headers2 := map[string]string{
		"X-Paimos-Agent-Name": "ops",
		"X-Paimos-Session-Id": "different-session",
	}
	resp2 := ts.requestWithHeaders(t, http.MethodPost,
		knowledgeURL(projectID, "memory"),
		ts.adminCookie,
		map[string]any{
			"slug":   "rl_other",
			"title":  "other",
			"body":   "body",
			"status": "proposed",
		},
		headers2,
	)
	assertStatus(t, resp2, http.StatusCreated)
}

// TestPropose_DisabledReturns503 asserts the operator opt-out via
// env var blocks every propose write at 503 — including the first
// one, even with a fresh limiter.
func TestPropose_DisabledReturns503(t *testing.T) {
	resetProposeLimiter()
	t.Setenv("PAIMOS_PROPOSE_DISABLED", "1")
	t.Setenv("PAIMOS_PROPOSE_LIMIT_PER_SESSION", "")

	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-349 Disabled", "P349D")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		knowledgeURL(projectID, "memory"),
		ts.adminCookie,
		map[string]any{
			"slug":   "shouldnt_land",
			"title":  "blocked",
			"body":   "body",
			"status": "proposed",
		},
		map[string]string{
			"X-Paimos-Agent-Name": "ops",
			"X-Paimos-Session-Id": "disabled-session",
		},
	)
	assertStatus(t, resp, http.StatusServiceUnavailable)

	// And the row must not exist.
	var n int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM issues WHERE project_id=? AND type='memory' AND slug=?`,
		projectID, "shouldnt_land").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected no row written when disabled; got %d", n)
	}
}

// TestPropose_NonProposedBypassesGate asserts that regular memory
// writes (status='backlog' or default) sail through even when
// PAIMOS_PROPOSE_DISABLED is set — the gate only fires on proposals.
func TestPropose_NonProposedBypassesGate(t *testing.T) {
	resetProposeLimiter()
	t.Setenv("PAIMOS_PROPOSE_DISABLED", "1")

	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-349 Bypass", "P349B")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		knowledgeURL(projectID, "memory"),
		ts.adminCookie,
		map[string]any{
			"slug":  "regular_memory",
			"title": "regular",
			"body":  "body",
			// no status -> default backlog
		},
		map[string]string{
			"X-Paimos-Agent-Name": "ops",
			"X-Paimos-Session-Id": "bypass-session",
		},
	)
	assertStatus(t, resp, http.StatusCreated)
}

// TestStaleProposed_SurfacesOldDrafts seeds two proposed rows — one
// fresh, one back-dated — then asserts the stale endpoint returns
// only the back-dated one with `?days=14`.
func TestStaleProposed_SurfacesOldDrafts(t *testing.T) {
	resetProposeLimiter()
	t.Setenv("PAIMOS_PROPOSE_DISABLED", "")
	t.Setenv("PAIMOS_PROPOSE_LIMIT_PER_SESSION", "")

	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "PAI-349 Stale", "P349S")

	// Seed a fresh proposal via the normal path (updated_at = now).
	freshResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie,
		map[string]any{
			"slug":   "fresh_proposal",
			"title":  "fresh",
			"body":   "body",
			"status": "proposed",
		})
	assertStatus(t, freshResp, http.StatusCreated)

	// Seed a stale proposal directly into the DB so we can pin
	// updated_at far in the past — the API doesn't expose updated_at
	// for back-dating. The slug + project + type uniqueness is still
	// enforced by the partial UNIQUE INDEX.
	if _, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, description,
		                   status, priority, slug, category_metadata, updated_at)
		VALUES(?, 9999, 'memory', 'stale', 'old body', 'proposed', 'medium',
		       'stale_proposal', '{}', date('now', '-30 days'))
	`, projectID); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	// Pull the stale list with a 14-day threshold.
	resp := ts.get(t, fmt.Sprintf("/api/projects/%d/memory/proposed/stale?days=14", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	resp.Body.Close()

	var list []map[string]any
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode stale list: %v (body=%s)", err, body)
	}
	// Expect exactly the stale_proposal row.
	if len(list) != 1 {
		t.Fatalf("expected 1 stale proposal, got %d (%s)", len(list), body)
	}
	gotSlug, _ := list[0]["slug"].(string)
	if gotSlug != "stale_proposal" {
		t.Errorf("got slug %q, want %q", gotSlug, "stale_proposal")
	}
	// And the days_since_update should be ≥ 14.
	if d, ok := list[0]["days_since_update"].(float64); !ok || d < 14 {
		t.Errorf("days_since_update: got %v, want ≥14", list[0]["days_since_update"])
	}
}

// _ keep tests-imports honest with a string-using assertion the
// linter doesn't flag if no other helper does.
var _ = strings.TrimSpace
