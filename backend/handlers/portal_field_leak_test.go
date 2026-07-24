// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// PAI-474: regression test confirming the portal API does NOT expose
// internal effort, cost, or pricing fields. Before this commit the
// frontend showed those columns as "—" but the JSON wire format still
// carried the values — visible in DevTools, in any cached response, in
// any proxy log. This test fails if any of those fields leak back.

package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

// fieldsThatMustNotLeak lists the JSON keys that the customer must
// never see on the portal browse path. Add to this list — never
// remove — as new internal fields appear on the issues table.
var fieldsThatMustNotLeak = []string{
	"cost_unit",
	"release",
	"estimate_hours",
	"estimate_lp",
	"ar_hours",
	"ar_lp",
	"estimate_eur",
	"ar_eur",
	"total_estimate_eur",
	"total_ar_eur",
	"ai_work_status",
}

func assertNoLeakedFields(t *testing.T, label string, payload []byte) {
	t.Helper()
	for _, key := range fieldsThatMustNotLeak {
		// Match `"key":` — JSON-encoded property names are always quoted
		// and followed by a colon. Avoids false positives on substrings
		// embedded inside string values.
		needle := fmt.Sprintf("%q:", key)
		if containsBytes(payload, []byte(needle)) {
			t.Errorf("%s: response leaks %q — body: %s", label, key, payload)
		}
	}
}

// containsBytes is a tiny strings.Contains for []byte without pulling in
// bytes — keeps the test file dependency-free.
func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
outer:
	for i := 0; i <= len(haystack)-len(needle); i++ {
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				continue outer
			}
		}
		return true
	}
	return false
}

func TestQuick_PortalIssueResponseHasNoCostFields(t *testing.T) {
	ts := newTestServer(t)
	projectID, issueID := seedVisibilityIssue(t)
	tagAllIssuesAsCustomerPortal(t, projectID)
	grantPortalAccess(t, projectID, "external")

	// Stuff the issue with values for every leakable column so any
	// regression that re-introduces them lands a non-zero value into the
	// payload (easier to spot in CI logs than a literal "null").
	if _, err := db.DB.Exec(`UPDATE issues
		SET estimate_hours=9.5, estimate_lp=12, ar_hours=7, ar_lp=8,
		    rate_hourly=150, rate_lp=80
		WHERE id=?`, issueID); err != nil {
		t.Fatalf("stuff issue: %v", err)
	}
	// PAI-599: cost_unit/release are edges now — attach secret-labelled
	// containers so these dimensions are still exercised by the leak check.
	seedLabelEdge(t, projectID, issueID, "cost_unit", "secret-cu")
	seedLabelEdge(t, projectID, issueID, "release", "secret-rel")

	// List endpoint
	resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues", projectID), ts.externalCookie)
	assertStatus(t, resp, http.StatusOK)
	listBody := []byte(readBody(t, resp))
	assertNoLeakedFields(t, "list issues", listBody)

	// Detail endpoint
	resp = ts.get(t,
		fmt.Sprintf("/api/portal/projects/%d/issues/%d", projectID, issueID),
		ts.externalCookie)
	assertStatus(t, resp, http.StatusOK)
	detailBody := []byte(readBody(t, resp))
	assertNoLeakedFields(t, "issue detail", detailBody)

	// Belt-and-braces: confirm the customer-allowed payload IS what we
	// expect — title, status, etc., should still be there.
	var detail map[string]any
	if err := json.Unmarshal(detailBody, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	for _, key := range []string{"id", "issue_key", "title", "status", "priority"} {
		if _, ok := detail[key]; !ok {
			t.Errorf("expected key %q in detail payload — body: %s", key, detailBody)
		}
	}
}

func TestQuick_PortalSummaryHasNoCostTotals(t *testing.T) {
	ts := newTestServer(t)
	projectID, _ := seedVisibilityIssue(t)
	tagAllIssuesAsCustomerPortal(t, projectID)
	grantPortalAccess(t, projectID, "external")

	// Stuff totals into every issue on this project so the SUM aggregate
	// would land non-zero values if it still queried them.
	if _, err := db.DB.Exec(`UPDATE issues
		SET estimate_hours=10, estimate_lp=20, ar_hours=15, ar_lp=12,
		    rate_hourly=200, rate_lp=100
		WHERE project_id=?`, projectID); err != nil {
		t.Fatalf("stuff issues: %v", err)
	}

	resp := ts.get(t,
		fmt.Sprintf("/api/portal/projects/%d/summary", projectID),
		ts.externalCookie)
	assertStatus(t, resp, http.StatusOK)
	assertNoLeakedFields(t, "summary", []byte(readBody(t, resp)))
}
