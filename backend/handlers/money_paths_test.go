package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/handlers"
	"github.com/inspr-at/paimos/backend/models"
)

// Money-path regression suite (PAI-582) — external package half.
//
// Covers the DB-backed effective-rate cascade ResolveRateCascade, which since
// PAI-599 resolves the cost_unit container by edge id. The function does
// issue → cost_unit → project(OWN rate); the customer level is applied
// separately (applyEffectiveRates / report SQL COALESCE) and is locked by
// TestLieferberichtJSON_InheritsCustomerRate + the internal-package suite.
// See docs/money-paths-tests.md for the full map.

func TestResolveRateCascade_MoneyPaths(t *testing.T) {
	ts := newTestServer(t)

	mkProject := func(name, key string, rateH, rateL *float64) int64 {
		r := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{"name": name, "key": key})
		assertStatus(t, r, http.StatusCreated)
		id := responseID(t, r)
		if rateH != nil || rateL != nil {
			if _, err := db.DB.Exec("UPDATE projects SET rate_hourly=?, rate_lp=? WHERE id=?", rateH, rateL, id); err != nil {
				t.Fatalf("set project rates: %v", err)
			}
		}
		return id
	}
	mkIssueWithRates := func(projectID int64, title string, rateH, rateL *float64) int64 {
		r := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": title, "type": "ticket", "status": "new",
		})
		assertStatus(t, r, http.StatusCreated)
		id := responseID(t, r)
		if rateH != nil || rateL != nil {
			if _, err := db.DB.Exec("UPDATE issues SET rate_hourly=?, rate_lp=? WHERE id=?", rateH, rateL, id); err != nil {
				t.Fatalf("set issue rates: %v", err)
			}
		}
		return id
	}
	fp := func(v float64) *float64 { return &v }

	projWithRates := mkProject("Rated", "RATE", fp(80), fp(800))
	projNoRates := mkProject("Bare", "BARE", nil, nil)
	costUnit := mkIssueWithRates(projWithRates, "Cost Unit", fp(120), fp(1200))
	costUnitHourlyOnly := mkIssueWithRates(projWithRates, "CU hourly only", fp(150), nil)
	danglingCostUnitID := int64(99999999)

	cases := []struct {
		name             string
		projectID        int64
		issH, issL       *float64
		costUnitID       *int64
		wantH, wantL     *float64
	}{
		{
			name: "issue rates dominate (cost_unit + project ignored)",
			projectID: projWithRates, issH: fp(90), issL: fp(900), costUnitID: &costUnit,
			wantH: fp(90), wantL: fp(900),
		},
		{
			name: "null issue falls to cost_unit container",
			projectID: projWithRates, issH: nil, issL: nil, costUnitID: &costUnit,
			wantH: fp(120), wantL: fp(1200),
		},
		{
			name: "null issue, no cost_unit, falls to project own rate",
			projectID: projWithRates, issH: nil, issL: nil, costUnitID: nil,
			wantH: fp(80), wantL: fp(800),
		},
		{
			name: "cost_unit supplies hourly, project supplies lp (independent)",
			projectID: projWithRates, issH: nil, issL: nil, costUnitID: &costUnitHourlyOnly,
			wantH: fp(150), wantL: fp(800),
		},
		{
			name: "issue lp set, cost_unit fills hourly only",
			projectID: projWithRates, issH: nil, issL: fp(900), costUnitID: &costUnit,
			wantH: fp(120), wantL: fp(900),
		},
		{
			name: "all null (no customer fallback in this function)",
			projectID: projNoRates, issH: nil, issL: nil, costUnitID: nil,
			wantH: nil, wantL: nil,
		},
		{
			name: "dangling cost_unit edge falls through to project",
			projectID: projWithRates, issH: nil, issL: nil, costUnitID: &danglingCostUnitID,
			wantH: fp(80), wantL: fp(800),
		},
	}

	eq := func(a, b *float64) bool {
		if a == nil || b == nil {
			return a == b
		}
		return *a == *b
	}
	str := func(p *float64) string {
		if p == nil {
			return "nil"
		}
		return fmt.Sprintf("%g", *p)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pid := tc.projectID
			iss := models.Issue{ProjectID: &pid, RateHourly: tc.issH, RateLp: tc.issL}
			if tc.costUnitID != nil {
				iss.CostUnit = &models.LabelRef{ID: *tc.costUnitID}
			}
			handlers.ResolveRateCascade(&iss)
			if !eq(iss.RateHourly, tc.wantH) {
				t.Errorf("RateHourly = %s, want %s", str(iss.RateHourly), str(tc.wantH))
			}
			if !eq(iss.RateLp, tc.wantL) {
				t.Errorf("RateLp = %s, want %s", str(iss.RateLp), str(tc.wantL))
			}
		})
	}
}
