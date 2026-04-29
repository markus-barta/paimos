// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

// PAI-273. Pin the load-bearing invariants on the Contact entity:
//   1. CRUD round-trips through the JSON layer with the right shape.
//   2. The "exactly one is_primary per customer" invariant survives
//      every mutation path: create, promote, delete-primary.
//   3. Read-compat: GET /customers/:id returns contact_name +
//      contact_email populated from the primary contact.
//   4. Write-compat: PUT /customers/:id with legacy contact_* fields
//      mirrors into the primary contact (creating one if missing).

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

func urlf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// ── helpers ─────────────────────────────────────────────────────────

func createCustomer(t *testing.T, ts *testServer, name string) int64 {
	t.Helper()
	resp := ts.post(t, "/api/customers", ts.adminCookie, map[string]any{"name": name})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create customer %q: status %d", name, resp.StatusCode)
	}
	return responseID(t, resp)
}

func primaryCount(t *testing.T, customerID int64) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(
		"SELECT COUNT(*) FROM contacts WHERE customer_id=? AND is_primary=1", customerID,
	).Scan(&n); err != nil {
		t.Fatalf("primary count: %v", err)
	}
	return n
}

// ── tests ───────────────────────────────────────────────────────────

func Test_Contacts_CRUDRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	customerID := createCustomer(t, ts, "Acme")

	// Create — first contact becomes primary implicitly.
	resp := ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{
		"name":  "Hannes Lindthaler",
		"email": "hl@acme.com",
		"phone": "+43 660 1234567",
		"role":  "Geschäftsführung",
	})
	assertStatus(t, resp, http.StatusCreated)
	var created models.Contact
	decode(t, resp, &created)
	if !created.IsPrimary {
		t.Errorf("first contact should be primary; got is_primary=false")
	}
	if created.Role != "Geschäftsführung" {
		t.Errorf("role round-trip: got %q", created.Role)
	}

	// Get
	resp = ts.get(t, urlf("/api/contacts/%d", created.ID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var got models.Contact
	decode(t, resp, &got)
	if got.Email != "hl@acme.com" {
		t.Errorf("email round-trip: got %q", got.Email)
	}

	// Put (note: body excludes is_primary — see contactUpdateBody comment).
	resp = ts.put(t, urlf("/api/contacts/%d", created.ID), ts.adminCookie, map[string]any{
		"phone": "+43 660 9999",
	})
	assertStatus(t, resp, http.StatusOK)
	var afterUpdate models.Contact
	decode(t, resp, &afterUpdate)
	if afterUpdate.Phone != "+43 660 9999" {
		t.Errorf("phone update: got %q", afterUpdate.Phone)
	}
	if afterUpdate.Email != "hl@acme.com" {
		t.Errorf("partial update should preserve email; got %q", afterUpdate.Email)
	}

	// List
	resp = ts.get(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var list []models.Contact
	decode(t, resp, &list)
	if len(list) != 1 {
		t.Errorf("list len: got %d, want 1", len(list))
	}

	// Delete (primary deletion auto-promotes none — there's only one).
	resp = ts.del(t, urlf("/api/contacts/%d", created.ID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)
	if primaryCount(t, customerID) != 0 {
		t.Errorf("after deleting only contact, primary count should be 0")
	}
}

func Test_Contacts_AtMostOnePrimary_OnCreate(t *testing.T) {
	ts := newTestServer(t)
	customerID := createCustomer(t, ts, "Acme")

	// First — implicit primary.
	ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{"name": "A"})
	if got := primaryCount(t, customerID); got != 1 {
		t.Fatalf("after first create: primary count = %d, want 1", got)
	}

	// Second with is_primary=true demotes the previous one in the same tx.
	resp := ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{
		"name":       "B",
		"is_primary": true,
	})
	assertStatus(t, resp, http.StatusCreated)
	if got := primaryCount(t, customerID); got != 1 {
		t.Errorf("after explicit-primary create: primary count = %d, want 1 (demote-then-promote in tx)", got)
	}

	// Third with is_primary=false — pre-existing primary stays.
	ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{
		"name":       "C",
		"is_primary": false,
	})
	if got := primaryCount(t, customerID); got != 1 {
		t.Errorf("after non-primary create: primary count = %d, want 1", got)
	}
}

func Test_Contacts_PromotePrimary_Atomic(t *testing.T) {
	ts := newTestServer(t)
	customerID := createCustomer(t, ts, "Acme")

	resp := ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{"name": "First"})
	var first models.Contact
	decode(t, resp, &first)
	resp = ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{"name": "Second"})
	var second models.Contact
	decode(t, resp, &second)
	resp = ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{"name": "Third"})
	var third models.Contact
	decode(t, resp, &third)

	// Promote the third — invariant must hold after.
	resp = ts.post(t, urlf("/api/contacts/%d/promote-primary", third.ID), ts.adminCookie, nil)
	assertStatus(t, resp, http.StatusOK)
	if got := primaryCount(t, customerID); got != 1 {
		t.Errorf("after promote: primary count = %d, want 1", got)
	}
	// Idempotent re-promote.
	resp = ts.post(t, urlf("/api/contacts/%d/promote-primary", third.ID), ts.adminCookie, nil)
	assertStatus(t, resp, http.StatusOK)
	if got := primaryCount(t, customerID); got != 1 {
		t.Errorf("after re-promote: primary count = %d, want 1", got)
	}
	// And the primary really is `third`.
	resp = ts.get(t, urlf("/api/contacts/%d", third.ID), ts.adminCookie)
	var got models.Contact
	decode(t, resp, &got)
	if !got.IsPrimary {
		t.Errorf("third should be primary after promote")
	}
}

func Test_Contacts_DeletePrimary_AutoPromotesNext(t *testing.T) {
	ts := newTestServer(t)
	customerID := createCustomer(t, ts, "Acme")

	resp := ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{"name": "First"})
	var first models.Contact
	decode(t, resp, &first)
	resp = ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{"name": "Second"})
	// First is primary. Delete it — Second should auto-promote so the
	// customer never sits in a "has contacts but no primary" state.
	resp = ts.del(t, urlf("/api/contacts/%d", first.ID), ts.adminCookie)
	assertStatus(t, resp, http.StatusNoContent)
	if got := primaryCount(t, customerID); got != 1 {
		t.Errorf("after deleting primary with one survivor: primary count = %d, want 1", got)
	}
}

func Test_Customer_ReadCompat_PopulatesLegacyFromPrimary(t *testing.T) {
	ts := newTestServer(t)
	customerID := createCustomer(t, ts, "Acme")
	ts.post(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie, map[string]any{
		"name":  "Hannes Lindthaler",
		"email": "hl@acme.com",
	})

	resp := ts.get(t, urlf("/api/customers/%d", customerID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var c models.Customer
	decode(t, resp, &c)
	if c.ContactName != "Hannes Lindthaler" {
		t.Errorf("read-compat: contact_name = %q, want primary contact's name", c.ContactName)
	}
	if c.ContactEmail != "hl@acme.com" {
		t.Errorf("read-compat: contact_email = %q, want primary contact's email", c.ContactEmail)
	}
}

func Test_Customer_WriteCompat_LegacyFieldsMirrorToPrimary(t *testing.T) {
	ts := newTestServer(t)
	customerID := createCustomer(t, ts, "Acme")

	// PUT with legacy fields on a customer that has zero contacts —
	// must create a primary.
	resp := ts.put(t, urlf("/api/customers/%d", customerID), ts.adminCookie, map[string]any{
		"contact_name":  "Hannes",
		"contact_email": "h@acme.com",
	})
	assertStatus(t, resp, http.StatusOK)
	if got := primaryCount(t, customerID); got != 1 {
		t.Fatalf("after legacy-field PUT (no prior contacts): primary count = %d, want 1", got)
	}
	resp = ts.get(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie)
	var list []models.Contact
	decode(t, resp, &list)
	if len(list) != 1 || list[0].Name != "Hannes" || list[0].Email != "h@acme.com" {
		t.Errorf("legacy-field PUT didn't populate primary correctly: %+v", list)
	}

	// PUT with only contact_email — existing primary's name must be preserved.
	resp = ts.put(t, urlf("/api/customers/%d", customerID), ts.adminCookie, map[string]any{
		"contact_email": "new@acme.com",
	})
	assertStatus(t, resp, http.StatusOK)
	resp = ts.get(t, urlf("/api/customers/%d/contacts", customerID), ts.adminCookie)
	decode(t, resp, &list)
	if list[0].Name != "Hannes" || list[0].Email != "new@acme.com" {
		t.Errorf("partial legacy update overwrote name: %+v", list[0])
	}
}

func Test_Customer_NewMetadataFields_RoundTrip(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/customers", ts.adminCookie, map[string]any{
		"name":                    "MXP",
		"website":                 "https://mxp.com",
		"vat_id":                  "ATU12345678",
		"description":             "long description here",
		"phone":                   "+43 660 1111",
		"billing_address_street":  "Hauptplatz 1",
		"billing_address_city":    "Graz",
		"billing_address_zip":     "8010",
		"billing_address_country": "Austria",
	})
	assertStatus(t, resp, http.StatusCreated)
	var c models.Customer
	decode(t, resp, &c)
	if c.Website != "https://mxp.com" || c.VATID != "ATU12345678" || c.BillingAddressStreet != "Hauptplatz 1" {
		t.Errorf("new metadata fields didn't round-trip: %+v", c)
	}
}
