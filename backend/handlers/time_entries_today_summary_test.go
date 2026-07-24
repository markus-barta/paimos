// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// PAI-495 — regression coverage for the timer panel's today-total
// footer. The handler at GET /api/time-entries/today-summary is
// browser-local-day aware: the client sends explicit [from, to)
// timestamps and the server sums hours for stopped entries whose
// stopped_at falls inside that window. Running entries are excluded
// — the frontend adds their live elapsed seconds on top.

package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

type todaySummaryResp struct {
	TotalHours float64 `json:"total_hours"`
	Count      int     `json:"count"`
}

func decodeTodaySummary(t *testing.T, resp *http.Response) todaySummaryResp {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var out todaySummaryResp
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode today-summary: %v — body: %s", err, body)
	}
	return out
}

// TestTodaySummary_StoppedInWindow_Sums asserts the happy path:
// stopped entries inside [from, to) are summed via override-or-derived
// hours; entries outside the window are skipped; running entries are
// skipped; entries belonging to a different user are skipped.
func TestTodaySummary_StoppedInWindow_Sums(t *testing.T) {
	ts := newTestServer(t)
	issueID := seedTestProjectAndIssue(t, ts)
	memberID := userIDByUsername(t, "member")
	adminID := userIDByUsername(t, "admin")

	// today: 2026-05-26 in the test fixture's clock (matches CLAUDE.md
	// currentDate). The handler doesn't care what "today" is — only that
	// stopped_at falls inside the [from, to) window the client sends.
	from := "2026-05-26T00:00:00Z"
	to := "2026-05-27T00:00:00Z"

	// inside window — 2h via override
	mustExec(t, `INSERT INTO time_entries (issue_id, user_id, started_at, stopped_at, override)
		VALUES (?, ?, '2026-05-26T08:00:00Z', '2026-05-26T10:00:00Z', 2.0)`, issueID, memberID)
	// inside window — 1.5h derived from start/stop (no override)
	mustExec(t, `INSERT INTO time_entries (issue_id, user_id, started_at, stopped_at)
		VALUES (?, ?, '2026-05-26T13:00:00Z', '2026-05-26T14:30:00Z')`, issueID, memberID)
	// before window — must not count
	mustExec(t, `INSERT INTO time_entries (issue_id, user_id, started_at, stopped_at, override)
		VALUES (?, ?, '2026-05-25T08:00:00Z', '2026-05-25T17:00:00Z', 9.0)`, issueID, memberID)
	// after window — must not count
	mustExec(t, `INSERT INTO time_entries (issue_id, user_id, started_at, stopped_at, override)
		VALUES (?, ?, '2026-05-27T08:00:00Z', '2026-05-27T09:00:00Z', 1.0)`, issueID, memberID)
	// running (no stopped_at) — must not count
	mustExec(t, `INSERT INTO time_entries (issue_id, user_id, started_at)
		VALUES (?, ?, '2026-05-26T15:00:00Z')`, issueID, memberID)
	// other user inside window — must not count
	mustExec(t, `INSERT INTO time_entries (issue_id, user_id, started_at, stopped_at, override)
		VALUES (?, ?, '2026-05-26T08:00:00Z', '2026-05-26T09:00:00Z', 1.0)`, issueID, adminID)

	resp := ts.get(t, summaryPath(from, to), ts.memberCookie)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("today-summary returned %d — body: %s", resp.StatusCode, body)
	}
	out := decodeTodaySummary(t, resp)

	const want = 3.5 // 2h override + 1.5h derived
	if !approxEqual(out.TotalHours, want, 0.001) {
		t.Errorf("total_hours = %v, want %v", out.TotalHours, want)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
}

// TestTodaySummary_EmptyDay_ReturnsZero asserts the empty-day shape:
// 200 with total_hours = 0 and count = 0. The footer renders "0m"
// in this case rather than hiding the row.
func TestTodaySummary_EmptyDay_ReturnsZero(t *testing.T) {
	ts := newTestServer(t)
	_ = seedTestProjectAndIssue(t, ts)

	resp := ts.get(t, summaryPath("2026-05-26T00:00:00Z", "2026-05-27T00:00:00Z"), ts.memberCookie)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("today-summary returned %d — body: %s", resp.StatusCode, body)
	}
	out := decodeTodaySummary(t, resp)
	if out.TotalHours != 0 || out.Count != 0 {
		t.Errorf("empty day: got total=%v count=%d, want 0/0", out.TotalHours, out.Count)
	}
}

// TestTodaySummary_BadInputs_Reject400 asserts the input-validation
// posture. Missing or malformed bounds must surface as 400, not 500
// or a silent empty sum.
func TestTodaySummary_BadInputs_Reject400(t *testing.T) {
	ts := newTestServer(t)
	cases := []struct {
		name, path string
	}{
		{"missing", "/api/time-entries/today-summary"},
		{"missing-to", "/api/time-entries/today-summary?from=2026-05-26T00:00:00Z"},
		{"bad-from", "/api/time-entries/today-summary?from=nope&to=2026-05-27T00:00:00Z"},
		{"inverted", summaryPath("2026-05-27T00:00:00Z", "2026-05-26T00:00:00Z")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := ts.get(t, tc.path, ts.memberCookie)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", tc.name, resp.StatusCode)
			}
		})
	}
}

// TestTodaySummary_Unauthenticated_Returns401 asserts that an
// anonymous caller can't read another user's daily total.
func TestTodaySummary_Unauthenticated_Returns401(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, summaryPath("2026-05-26T00:00:00Z", "2026-05-27T00:00:00Z"), "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", resp.StatusCode)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func summaryPath(from, to string) string {
	v := url.Values{}
	v.Set("from", from)
	v.Set("to", to)
	return "/api/time-entries/today-summary?" + v.Encode()
}

func mustExec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := db.DB.Exec(query, args...); err != nil {
		t.Fatalf("exec: %v — query: %s", err, query)
	}
}

func approxEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

