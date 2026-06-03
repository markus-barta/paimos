package handlers_test

import (
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-579 / PAI-580 / PAI-581: end-to-end money-path coverage for the
// time-booked project report and the booked-hours time-report. Exercises
// window selection + exclusion, the canonical hours formula, per-window
// material aggregation, Time & Material AR EUR (hours×rate + material×lpRate),
// flat vs epic grouping, the dynamic state filter, and reconciliation between
// the two reports.
func TestTimeBookedReport_MoneyPaths(t *testing.T) {
	ts := newTestServer(t)

	// Project + customer carrying the effective rates (hourly + LP).
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{"name": "T&M", "key": "TMR"})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)
	res, err := db.DB.Exec("INSERT INTO customers (name, rate_hourly, rate_lp) VALUES ('Cust', 100, 1000)")
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	custID, _ := res.LastInsertId()
	if _, err := db.DB.Exec("UPDATE projects SET customer_id=? WHERE id=?", custID, projectID); err != nil {
		t.Fatalf("link customer: %v", err)
	}

	mkIssue := func(title, status string) int64 {
		r := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": title, "type": "ticket", "status": status,
		})
		assertStatus(t, r, http.StatusCreated)
		return responseID(t, r)
	}
	mkEpic := func(title string) int64 {
		r := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": title, "type": "epic", "status": "in-progress",
		})
		assertStatus(t, r, http.StatusCreated)
		return responseID(t, r)
	}
	postTE := func(issueID int64, started string, override float64, material *float64) {
		body := map[string]any{"started_at": started, "stopped_at": started, "override": override}
		if material != nil {
			body["material_lp"] = *material
		}
		r := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, body)
		assertStatus(t, r, http.StatusCreated)
		r.Body.Close()
	}

	epicID := mkEpic("Epic")
	aID := mkIssue("A", "delivered")
	if _, err := db.DB.Exec("UPDATE issues SET parent_id=? WHERE id=?", epicID, aID); err != nil {
		t.Fatalf("link epic: %v", err)
	}
	bID := mkIssue("B", "delivered")
	cID := mkIssue("C", "new")

	mat := 2.5
	postTE(aID, "2026-05-10T09:00:00Z", 10, &mat) // in window: 10h + 2.5 LP
	postTE(bID, "2026-05-15T09:00:00Z", 5, nil)   // in window: 5h
	postTE(bID, "2026-06-02T09:00:00Z", 40, nil)  // OUT of window: must be excluded
	postTE(cID, "2026-05-20T09:00:00Z", 3, nil)   // in window: 3h

	const win = "from=2026-05-01&to=2026-05-31"

	// ── time-report (PAI-579) ───────────────────────────────────────────────
	type trIssue struct {
		IssueKey   string  `json:"issue_key"`
		Hours      float64 `json:"hours"`
		MaterialLp float64 `json:"material_lp"`
	}
	var tr struct {
		TotalHours      float64   `json:"total_hours"`
		TotalMaterialLp float64   `json:"total_material_lp"`
		TotalEntries    int       `json:"total_entries"`
		ByIssue         []trIssue `json:"by_issue"`
		ByDay           []struct {
			Date  string  `json:"date"`
			Hours float64 `json:"hours"`
		} `json:"by_day"`
	}
	r := ts.get(t, fmt.Sprintf("/api/projects/%d/time-report?%s", projectID, win), ts.adminCookie)
	assertStatus(t, r, http.StatusOK)
	decode(t, r, &tr)
	// 10 + 5 + 3 = 18; the 40h June entry is excluded by the window.
	if !approx(tr.TotalHours, 18) {
		t.Fatalf("time-report total_hours=%v, want 18 (June 40h must be excluded)", tr.TotalHours)
	}
	if !approx(tr.TotalMaterialLp, 2.5) {
		t.Fatalf("time-report total_material_lp=%v, want 2.5", tr.TotalMaterialLp)
	}
	if tr.TotalEntries != 3 {
		t.Fatalf("time-report total_entries=%d, want 3", tr.TotalEntries)
	}
	if len(tr.ByIssue) != 3 || len(tr.ByDay) != 3 {
		t.Fatalf("time-report breakdown by_issue=%d by_day=%d, want 3/3", len(tr.ByIssue), len(tr.ByDay))
	}

	// ── time_booked report, flat, all states ────────────────────────────────
	all := "new,backlog,in-progress,qa,done,delivered,accepted,invoiced,cancelled"
	flat := getLBReport(t, ts, projectID, fmt.Sprintf("scope=time_booked&%s&group=flat&statuses=%s", win, all))
	if len(flat.Groups) != 1 {
		t.Fatalf("flat grouping: groups=%d, want 1", len(flat.Groups))
	}
	// AR h = window hours = 18; AR LP (material) = 2.5; AR EUR = T&M:
	// A: 10×100 + 2.5×1000 = 3500; B: 5×100 = 500; C: 3×100 = 300 → 4300.
	if !approx(flat.GrandTotal.ArHours, 18) || !approx(flat.GrandTotal.ArLp, 2.5) || !approx(flat.GrandTotal.ArEur, 4300) {
		t.Fatalf("flat grand total = h:%v lp:%v eur:%v, want 18 / 2.5 / 4300",
			flat.GrandTotal.ArHours, flat.GrandTotal.ArLp, flat.GrandTotal.ArEur)
	}
	// Reconciliation: window booked hours match the time-report total.
	if !approx(flat.GrandTotal.ArHours, tr.TotalHours) {
		t.Fatalf("reconciliation: report AR h %v != time-report hours %v", flat.GrandTotal.ArHours, tr.TotalHours)
	}

	// ── epic grouping: A under epic, B+C ungrouped → 2 groups ────────────────
	epic := getLBReport(t, ts, projectID, fmt.Sprintf("scope=time_booked&%s&group=epic&statuses=%s", win, all))
	if len(epic.Groups) != 2 {
		t.Fatalf("epic grouping: groups=%d, want 2", len(epic.Groups))
	}

	// ── state filter: only "new" → just C ────────────────────────────────────
	onlyNew := getLBReport(t, ts, projectID, fmt.Sprintf("scope=time_booked&%s&group=flat&statuses=new", win))
	if len(onlyNew.Groups) != 1 || len(onlyNew.Groups[0].Issues) != 1 {
		t.Fatalf("state filter new: groups/issues = %d/%v, want 1/1", len(onlyNew.Groups), onlyNew.Groups)
	}
	if !approx(onlyNew.GrandTotal.ArHours, 3) || !approx(onlyNew.GrandTotal.ArEur, 300) {
		t.Fatalf("state filter new grand = h:%v eur:%v, want 3 / 300", onlyNew.GrandTotal.ArHours, onlyNew.GrandTotal.ArEur)
	}
}

type lbTestReport struct {
	Groups []struct {
		EpicKey string `json:"epic_key"`
		Issues  []struct {
			IssueKey string  `json:"issue_key"`
			ArHours  float64 `json:"ar_hours"`
			ArLp     float64 `json:"ar_lp"`
			ArEur    float64 `json:"ar_eur"`
		} `json:"issues"`
	} `json:"groups"`
	GrandTotal struct {
		ArHours float64 `json:"ar_hours"`
		ArLp    float64 `json:"ar_lp"`
		ArEur   float64 `json:"ar_eur"`
	} `json:"grand_total"`
}

func getLBReport(t *testing.T, ts *testServer, projectID int64, query string) lbTestReport {
	t.Helper()
	r := ts.get(t, fmt.Sprintf("/api/projects/%d/reports/lieferbericht?%s", projectID, query), ts.adminCookie)
	assertStatus(t, r, http.StatusOK)
	var out lbTestReport
	decode(t, r, &out)
	return out
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
