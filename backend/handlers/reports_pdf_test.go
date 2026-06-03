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

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func TestLieferbericht_UngroupedUsesProjectKey(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Bonelio",
		"key":  "BON26",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":  "Ticket without epic",
		"type":   "ticket",
		"status": "backlog",
	})
	assertStatus(t, resp, http.StatusCreated)

	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/reports/lieferbericht?scope=all_open", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	defer resp.Body.Close()

	var body struct {
		Groups []struct {
			EpicKey   string `json:"epic_key"`
			EpicTitle string `json:"epic_title"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Groups) != 1 {
		t.Fatalf("expected one group, got %d", len(body.Groups))
	}
	if body.Groups[0].EpicKey != "BON26" || body.Groups[0].EpicTitle != "BON26" {
		t.Fatalf("ungrouped fallback = %q/%q, want BON26/BON26", body.Groups[0].EpicKey, body.Groups[0].EpicTitle)
	}
}

func TestLieferberichtJSON_ColsParam(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Column Project",
		"key":  "COLS",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":  "Billable ticket",
		"type":   "ticket",
		"status": "backlog",
	})
	assertStatus(t, resp, http.StatusCreated)
	issueID := responseID(t, resp)
	if _, err := db.DB.Exec("UPDATE issues SET ar_lp=1, rate_lp=12.345 WHERE id=?", issueID); err != nil {
		t.Fatalf("set billing fields: %v", err)
	}

	type colsBody struct {
		Cols struct {
			SP    bool `json:"sp"`
			H     bool `json:"h"`
			ARSP  bool `json:"ar_sp"`
			ARH   bool `json:"ar_h"`
			AREUR bool `json:"ar_eur"`
		} `json:"cols"`
		GrandTotal struct {
			AREUR float64 `json:"ar_eur"`
		} `json:"grand_total"`
	}

	decode := func(query string) colsBody {
		t.Helper()
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/reports/lieferbericht?scope=all_open%s", projectID, query), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		defer resp.Body.Close()
		var body colsBody
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return body
	}

	defaultBody := decode("")
	if !defaultBody.Cols.SP || !defaultBody.Cols.H || !defaultBody.Cols.ARSP || !defaultBody.Cols.ARH || !defaultBody.Cols.AREUR {
		t.Fatalf("default cols = %+v, want all visible", defaultBody.Cols)
	}

	filteredBody := decode("&cols=sp,ar_eur")
	if !filteredBody.Cols.SP || !filteredBody.Cols.AREUR || filteredBody.Cols.H || filteredBody.Cols.ARSP || filteredBody.Cols.ARH {
		t.Fatalf("filtered cols = %+v, want only sp + ar_eur", filteredBody.Cols)
	}
	if filteredBody.GrandTotal.AREUR != 12.35 {
		t.Fatalf("grand_total.ar_eur=%v, want rounded 12.35", filteredBody.GrandTotal.AREUR)
	}

	emptyBody := decode("&cols=")
	if emptyBody.Cols.SP || emptyBody.Cols.H || emptyBody.Cols.ARSP || emptyBody.Cols.ARH || emptyBody.Cols.AREUR {
		t.Fatalf("empty cols = %+v, want none visible", emptyBody.Cols)
	}
}

// Regression: AR EUR must be computed from the effective rate hierarchy
// (issue → epic → project → customer). When the rate is only configured on the
// linked customer — the common case — the report query used to stop at the
// epic and render a blank AR EUR column.
func TestLieferberichtJSON_InheritsCustomerRate(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Inherit Rate Project",
		"key":  "INHR",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	// Customer carries the rate; project + issue leave it NULL (inherit).
	res, err := db.DB.Exec(
		"INSERT INTO customers (name, rate_hourly, rate_lp) VALUES ('AVL List GmbH', 148.93, 1200)")
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	customerID, _ := res.LastInsertId()
	if _, err := db.DB.Exec("UPDATE projects SET customer_id=? WHERE id=?", customerID, projectID); err != nil {
		t.Fatalf("link customer: %v", err)
	}

	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":  "Billable, no own rate",
		"type":   "ticket",
		"status": "backlog",
	})
	assertStatus(t, resp, http.StatusCreated)
	issueID := responseID(t, resp)
	if _, err := db.DB.Exec("UPDATE issues SET ar_hours=10, rate_hourly=NULL, rate_lp=NULL WHERE id=?", issueID); err != nil {
		t.Fatalf("set ar_hours: %v", err)
	}

	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/reports/lieferbericht?scope=all_open", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	defer resp.Body.Close()
	var body struct {
		GrandTotal struct {
			AREUR float64 `json:"ar_eur"`
		} `json:"grand_total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// 10h × 148.93 €/h = 1489.30, inherited from the customer.
	if body.GrandTotal.AREUR != 1489.3 {
		t.Fatalf("grand_total.ar_eur=%v, want 1489.3 (inherited customer rate)", body.GrandTotal.AREUR)
	}
}

func TestLieferberichtPDF_BasicRender(t *testing.T) {
	ts := newTestServer(t)

	// Create a project
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "PDF Test Project",
		"key":  "PDF1",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	// Create an epic
	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":  "Epic One",
		"type":   "epic",
		"status": "backlog",
	})
	assertStatus(t, resp, http.StatusCreated)
	epicID := responseID(t, resp)

	// Create tickets under the epic with varying data
	for i := 1; i <= 3; i++ {
		status := "backlog"
		if i == 1 {
			status = "done"
		} else if i == 2 {
			status = "in-progress"
		}
		resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
			"title":       fmt.Sprintf("Ticket %d with a longer summary that tests wrapping behavior", i),
			"description": fmt.Sprintf("Description for ticket %d — this is a detailed description with Ümlauts and special chars", i),
			"type":        "ticket",
			"status":      status,
			"parent_id":   epicID,
		})
		assertStatus(t, resp, http.StatusCreated)
	}

	// Create a ticket where description matches summary (should be skipped)
	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":       "Duplicate description test",
		"description": "Duplicate description test",
		"type":        "ticket",
		"status":      "backlog",
		"parent_id":   epicID,
	})
	assertStatus(t, resp, http.StatusCreated)

	// Set some estimate values directly in DB for richer output
	db.DB.Exec("UPDATE issues SET estimate_lp=5, estimate_hours=40 WHERE title LIKE 'Ticket 1%'")
	db.DB.Exec("UPDATE issues SET estimate_lp=3, estimate_hours=24 WHERE title LIKE 'Ticket 2%'")

	// Request the PDF
	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+fmt.Sprintf("/api/projects/%d/reports/lieferbericht/pdf?scope=all", projectID), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Cookie", ts.adminCookie)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "pm.bytepoets.com")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET pdf: %v", err)
	}
	assertStatus(t, resp, http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// Basic checks
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type: got %q, want application/pdf", ct)
	}
	if len(body) < 1000 {
		t.Errorf("PDF too small: %d bytes", len(body))
	}
	// PDF magic bytes
	if string(body[:5]) != "%PDF-" {
		t.Errorf("not a valid PDF: starts with %q", string(body[:5]))
	}
}

// PAI-418 / PAI-425. Exercise the text_source param end-to-end:
// "tech" renders the technical description, "report" reads
// report_summary (with a "[keine Kundenfassung]" fallback tag when
// the summary is empty), and unknown values default to "tech". The
// PDF magic-bytes check is enough — the renderer's row layout is
// covered by the basic-render test above; here we only need to
// prove the param is wired without breaking output.
func TestLieferberichtPDF_TextSourceParam(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Text Source Project",
		"key":  "TSRC",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":          "Filled summary",
		"description":    "Technical description here.",
		"report_summary": "Wir haben die Anmeldung stabiler gemacht.",
		"type":           "ticket",
		"status":         "done",
	})
	assertStatus(t, resp, http.StatusCreated)

	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":       "Missing summary",
		"description": "Another technical body without a customer-facing summary yet.",
		"type":        "ticket",
		"status":      "done",
	})
	assertStatus(t, resp, http.StatusCreated)

	for _, src := range []string{"tech", "report", "bogus"} {
		req, err := http.NewRequest(http.MethodGet, ts.srv.URL+fmt.Sprintf("/api/projects/%d/reports/lieferbericht/pdf?scope=all&text_source=%s", projectID, src), nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Cookie", ts.adminCookie)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "pm.bytepoets.com")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET pdf %s: %v", src, err)
		}
		assertStatus(t, resp, http.StatusOK)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(body) < 1000 || string(body[:5]) != "%PDF-" {
			t.Errorf("text_source=%s: invalid PDF (len=%d)", src, len(body))
		}
	}
}

func TestProjektberichtSnapshotAcceptanceFlow(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Snapshot Project",
		"key":  "PBS",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	createIssue := func(title, status string) int64 {
		t.Helper()
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
			"title":  title,
			"type":   "ticket",
			"status": status,
		})
		assertStatus(t, resp, http.StatusCreated)
		return responseID(t, resp)
	}
	doneID := createIssue("Ready for acceptance", "done")
	deliveredID := createIssue("Already delivered", "delivered")
	backlogID := createIssue("Not yet ready", "backlog")

	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+fmt.Sprintf("/api/projects/%d/reports/projektbericht/pdf?scope=date_range&statuses=done,delivered,backlog&snapshot=1", projectID), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Cookie", ts.adminCookie)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "pm.bytepoets.com")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET projektbericht pdf: %v", err)
	}
	assertStatus(t, resp, http.StatusOK)
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "PB-PBS") {
		t.Fatalf("Content-Disposition=%q, want PB-PBS filename", cd)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/projektberichte", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	defer resp.Body.Close()
	var snaps []struct {
		Code              string         `json:"code"`
		Status            string         `json:"status"`
		AcceptanceURL     string         `json:"acceptance_url"`
		EligibleCount     int            `json:"eligible_count"`
		SkippedCount      int            `json:"skipped_count"`
		AlreadyFinalCount int            `json:"already_final_count"`
		AcceptSummary     map[string]int `json:"accept_summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snaps); err != nil {
		t.Fatalf("decode snapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("snapshot count=%d, want 1", len(snaps))
	}
	if snaps[0].Code == "" || !strings.Contains(snaps[0].AcceptanceURL, "/accept/"+snaps[0].Code) {
		t.Fatalf("bad acceptance URL for snapshot: code=%q url=%q", snaps[0].Code, snaps[0].AcceptanceURL)
	}
	if snaps[0].EligibleCount != 2 || snaps[0].SkippedCount != 1 || snaps[0].AlreadyFinalCount != 0 {
		t.Fatalf("counts eligible/skipped/final=%d/%d/%d, want 2/1/0", snaps[0].EligibleCount, snaps[0].SkippedCount, snaps[0].AlreadyFinalCount)
	}

	resp = ts.post(t, "/api/projektberichte/accept/"+snaps[0].Code, ts.adminCookie, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	defer resp.Body.Close()
	var accepted struct {
		Status        string         `json:"status"`
		AcceptSummary map[string]int `json:"accept_summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode accepted snapshot: %v", err)
	}
	if accepted.Status != "accepted" || accepted.AcceptSummary["accepted"] != 2 || accepted.AcceptSummary["skipped"] != 1 {
		t.Fatalf("accept result status=%q summary=%v, want accepted with 2 accepted / 1 skipped", accepted.Status, accepted.AcceptSummary)
	}

	assertIssueStatus := func(id int64, want string) {
		t.Helper()
		var got string
		if err := db.DB.QueryRow(`SELECT status FROM issues WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatalf("issue %d status query: %v", id, err)
		}
		if got != want {
			t.Fatalf("issue %d status=%q, want %q", id, got, want)
		}
	}
	assertIssueStatus(doneID, "accepted")
	assertIssueStatus(deliveredID, "accepted")
	assertIssueStatus(backlogID, "backlog")

	resp = ts.get(t, "/api/projektberichte/"+snaps[0].Code+"/pdf", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

func TestLieferberichtPDF_ManyIssues(t *testing.T) {
	ts := newTestServer(t)

	// Create project
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Large PDF Project",
		"key":  "LPD",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	// Create epic
	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":  "Large Epic",
		"type":   "epic",
		"status": "backlog",
	})
	assertStatus(t, resp, http.StatusCreated)
	epicID := responseID(t, resp)

	// Create 60 tickets to test multi-page rendering
	for i := 1; i <= 60; i++ {
		ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
			"title":       fmt.Sprintf("Issue %d — testing pagination with a reasonably long summary text", i),
			"description": fmt.Sprintf("Detailed description for issue %d that should wrap nicely in the PDF output", i),
			"type":        "ticket",
			"status":      "done",
			"parent_id":   epicID,
		})
	}

	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/reports/lieferbericht/pdf?scope=all", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if len(body) < 2000 {
		t.Errorf("PDF for 60 issues too small: %d bytes", len(body))
	}
	if string(body[:5]) != "%PDF-" {
		t.Errorf("not a valid PDF")
	}
}
