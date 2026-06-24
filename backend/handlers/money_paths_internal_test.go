package handlers

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/markus-barta/paimos/backend/models"
)

// Money-path regression suite (PAI-582) — internal package half.
//
// Covers the UNEXPORTED money functions that need no DB:
//   - applyEffectiveRates  (PAI-54 effective rate hierarchy, project↔customer)
//   - computeBudgetHours    (LP→hours conversion + null/zero guards; DB-free paths)
//   - projectReportCustomer{AddressLines,Contact} + hasPostalDetail +
//     compactPostalAddressLines  (PAI-557 PDF party block)
//
// The DB-backed cascade (ResolveRateCascade across cost_unit/project/customer)
// and the booked-hours/export/material integration paths live in the external
// package file money_paths_test.go. See docs/money-paths-tests.md for the map.

func mpFptr(v float64) *float64 { return &v }

func mpFeq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return math.Abs(*a-*b) < 1e-9
}

func mpFstr(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%g", *p)
}

// ── PAI-54: applyEffectiveRates (project override vs customer inherit) ────────

func TestApplyEffectiveRates_MoneyPaths(t *testing.T) {
	cases := []struct {
		name                       string
		projH, projL, custH, custL *float64
		wantEffH, wantEffL         *float64
		wantInherited              bool
	}{
		{
			name: "project override wins over customer",
			projH: mpFptr(120), projL: mpFptr(1200), custH: mpFptr(100), custL: mpFptr(1000),
			wantEffH: mpFptr(120), wantEffL: mpFptr(1200), wantInherited: false,
		},
		{
			name: "customer inherited when project null",
			projH: nil, projL: nil, custH: mpFptr(100), custL: mpFptr(1000),
			wantEffH: mpFptr(100), wantEffL: mpFptr(1000), wantInherited: true,
		},
		{
			name: "independent: hourly from project, lp from customer",
			projH: mpFptr(120), projL: nil, custH: nil, custL: mpFptr(1000),
			wantEffH: mpFptr(120), wantEffL: mpFptr(1000), wantInherited: true,
		},
		{
			name: "independent: hourly from customer, lp from project",
			projH: nil, projL: mpFptr(1200), custH: mpFptr(100), custL: nil,
			wantEffH: mpFptr(100), wantEffL: mpFptr(1200), wantInherited: true,
		},
		{
			name: "all null yields no effective rates, not inherited",
			projH: nil, projL: nil, custH: nil, custL: nil,
			wantEffH: nil, wantEffL: nil, wantInherited: false,
		},
		{
			name: "zero is a real override, not an inherit trigger",
			projH: mpFptr(0), projL: mpFptr(0), custH: mpFptr(100), custL: mpFptr(1000),
			wantEffH: mpFptr(0), wantEffL: mpFptr(0), wantInherited: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &models.Project{RateHourly: tc.projH, RateLp: tc.projL}
			applyEffectiveRates(p, tc.custH, tc.custL)
			if !mpFeq(p.EffectiveRateHourly, tc.wantEffH) {
				t.Errorf("EffectiveRateHourly = %s, want %s", mpFstr(p.EffectiveRateHourly), mpFstr(tc.wantEffH))
			}
			if !mpFeq(p.EffectiveRateLp, tc.wantEffL) {
				t.Errorf("EffectiveRateLp = %s, want %s", mpFstr(p.EffectiveRateLp), mpFstr(tc.wantEffL))
			}
			if p.RateInherited != tc.wantInherited {
				t.Errorf("RateInherited = %v, want %v", p.RateInherited, tc.wantInherited)
			}
		})
	}
}

// ── PAI-54: computeBudgetHours (DB-free paths) ───────────────────────────────
//
// All cases keep ResolveRateCascade off the DB: either they return before the
// cascade, or they pass both issue rates (cascade early-returns), or they leave
// projectID/costUnitID nil (cascade skips its queries). The DB cascade itself
// is covered by TestResolveRateCascade_MoneyPaths.

func TestComputeBudgetHours_MoneyPaths(t *testing.T) {
	cases := []struct {
		name              string
		estHours, estLp   *float64
		rateH, rateL      *float64
		want              *float64
	}{
		{name: "estimate_hours used directly", estHours: mpFptr(8), want: mpFptr(8)},
		{name: "estimate_hours wins over estimate_lp", estHours: mpFptr(8), estLp: mpFptr(5), rateH: mpFptr(100), rateL: mpFptr(1000), want: mpFptr(8)},
		{name: "estimate_hours zero falls through to nil (no lp)", estHours: mpFptr(0), estLp: nil, want: nil},
		{name: "lp converted via rates", estLp: mpFptr(5), rateH: mpFptr(100), rateL: mpFptr(1000), want: mpFptr(50)},
		{name: "lp zero yields nil", estLp: mpFptr(0), rateH: mpFptr(100), rateL: mpFptr(1000), want: nil},
		{name: "lp null yields nil", estLp: nil, want: nil},
		{name: "negative lp yields nil", estLp: mpFptr(-3), rateH: mpFptr(100), rateL: mpFptr(1000), want: nil},
		{name: "zero rate_hourly guards division", estLp: mpFptr(5), rateH: mpFptr(0), rateL: mpFptr(1000), want: nil},
		{name: "null rates with no project yields nil", estLp: mpFptr(5), rateH: nil, rateL: nil, want: nil},
		// Edge behavior locked (not guarded in computeBudgetHours by design):
		{name: "zero rate_lp yields a zero-hour budget (0 LP cost, distinct from nil)", estLp: mpFptr(5), rateH: mpFptr(100), rateL: mpFptr(0), want: mpFptr(0)},
		{name: "negative rate yields a negative budget — rate sign is validated upstream, not here", estLp: mpFptr(5), rateH: mpFptr(100), rateL: mpFptr(-50), want: mpFptr(-2.5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeBudgetHours(tc.estHours, tc.estLp, tc.rateH, tc.rateL, nil, nil)
			if !mpFeq(got, tc.want) {
				t.Errorf("computeBudgetHours = %s, want %s", mpFstr(got), mpFstr(tc.want))
			}
		})
	}
}

// ── PAI-557: PDF party block address/contact fallbacks ───────────────────────

func TestProjectReportCustomerAddressLines_MoneyPaths(t *testing.T) {
	cases := []struct {
		name string
		cust *models.Customer
		want []string
	}{
		{
			name: "billing address wins over visit and free-form",
			cust: &models.Customer{
				BillingAddressStreet: "Hauptstr. 1", BillingAddressZip: "1010", BillingAddressCity: "Wien", BillingAddressCountry: "Österreich",
				VisitAddressStreet: "Nebenstr. 2", VisitAddressZip: "1020",
				Address: "free form text", Country: "Austria",
			},
			want: []string{"Hauptstr. 1", "1010, Wien", "Österreich"},
		},
		{
			name: "billing country falls back to customer country",
			cust: &models.Customer{
				BillingAddressStreet: "Hauptstr. 1", BillingAddressZip: "1010", BillingAddressCity: "Wien",
				Country: "Austria",
			},
			want: []string{"Hauptstr. 1", "1010, Wien", "Austria"},
		},
		{
			name: "visit used when billing has no street/zip/city",
			cust: &models.Customer{
				BillingAddressCountry: "ignored-bare-country",
				VisitAddressStreet:    "Nebenstr. 2", VisitAddressZip: "1020",
				Country: "Austria",
			},
			want: []string{"Nebenstr. 2", "1020", "Austria"},
		},
		{
			name: "incomplete billing+no visit skips to free-form",
			cust: &models.Customer{
				Address: "Some free-form address, Wien", Country: "Austria",
			},
			want: []string{"Some free-form address, Wien", "Austria"},
		},
		{
			name: "nil customer yields nil",
			cust: nil,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectReportCustomerAddressLines(tc.cust)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("addressLines = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestHasPostalDetail_MoneyPaths(t *testing.T) {
	cases := []struct {
		name             string
		street, zip, city string
		want             bool
	}{
		{name: "street only", street: "Hauptstr. 1", want: true},
		{name: "zip only", zip: "1010", want: true},
		{name: "city only", city: "Wien", want: true},
		{name: "all empty", want: false},
		{name: "whitespace only", street: "  ", zip: "\t", city: " ", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasPostalDetail(tc.street, tc.zip, tc.city); got != tc.want {
				t.Errorf("hasPostalDetail(%q,%q,%q) = %v, want %v", tc.street, tc.zip, tc.city, got, tc.want)
			}
		})
	}
}

func TestProjectReportCustomerContact_MoneyPaths(t *testing.T) {
	cases := []struct {
		name        string
		cName, cMail string
		want        string
	}{
		{name: "name and email", cName: "Jane Doe", cMail: "jane@x.com", want: "Jane Doe <jane@x.com>"},
		{name: "name only", cName: "Jane Doe", want: "Jane Doe"},
		{name: "email only", cMail: "jane@x.com", want: "jane@x.com"},
		{name: "both empty", want: ""},
		{name: "whitespace only", cName: "  ", cMail: " ", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &models.Customer{ContactName: tc.cName, ContactEmail: tc.cMail}
			if got := projectReportCustomerContact(c); got != tc.want {
				t.Errorf("contact = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompactPostalAddressLines_MoneyPaths(t *testing.T) {
	cases := []struct {
		name                       string
		street, zip, city, country string
		want                       []string
	}{
		{name: "all fields", street: "Hauptstr. 1", zip: "1010", city: "Wien", country: "Austria", want: []string{"Hauptstr. 1", "1010, Wien", "Austria"}},
		{name: "no city", street: "Hauptstr. 1", zip: "1010", country: "Austria", want: []string{"Hauptstr. 1", "1010", "Austria"}},
		{name: "country only", country: "Austria", want: []string{"Austria"}},
		{name: "all empty", want: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compactPostalAddressLines(tc.street, tc.zip, tc.city, tc.country)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("compactPostalAddressLines = %#v, want %#v", got, tc.want)
			}
		})
	}
}
