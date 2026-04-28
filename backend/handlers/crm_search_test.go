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

// PAI-266 — search handler fan-out + per-provider error isolation +
// (provider_id, external_id) → already_imported dedup. The HubSpot-side
// behaviour is covered in handlers/crm/hubspot/provider_test.go;
// this file pins the cross-provider handler contract.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers/crm"
)

// fakeSearchProvider is a minimal Searcher used only in handler tests.
// Two flavours via the same struct: hits-returning + error-returning,
// driven by the fields the test sets at construction.
type fakeSearchProvider struct {
	id, name string
	hits     []crm.SearchHit
	err      error
}

func (p *fakeSearchProvider) ID() string                          { return p.id }
func (p *fakeSearchProvider) Name() string                        { return p.name }
func (p *fakeSearchProvider) LogoURL() string                     { return "/assets/" + p.id + ".svg" }
func (p *fakeSearchProvider) ConfigSchema() crm.ConfigSchema      { return crm.ConfigSchema{} }
func (p *fakeSearchProvider) ValidateConfig(_ map[string]string) error {
	return nil
}
func (p *fakeSearchProvider) ImportRef(_ context.Context, _ string, _ crm.ProviderConfig) (crm.CustomerImport, error) {
	return crm.CustomerImport{}, nil
}
func (p *fakeSearchProvider) Sync(_ context.Context, _ string, _ crm.ProviderConfig) (crm.PartialUpdate, error) {
	return crm.PartialUpdate{}, nil
}
func (p *fakeSearchProvider) DeepLink(externalID string, _ crm.ProviderConfig) string {
	return "https://" + p.id + "/co/" + externalID
}
func (p *fakeSearchProvider) Search(_ context.Context, _ string, _ int, _ crm.ProviderConfig) ([]crm.SearchHit, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.hits, nil
}

func Test_CRMSearch_FanOutAndDedup(t *testing.T) {
	// Side-effect: opens the in-memory DB and runs migrations. We don't
	// use the returned HTTP server (the search route lives outside the
	// test router); we drive SearchProviders directly with httptest
	// recorders so the fan-out + DB join are exercised end-to-end
	// without touching unrelated handler glue.
	_ = newTestServer(t)

	// One provider that returns two hits — one of which is already
	// imported locally — and a second provider that fails authentication
	// so we can pin per-provider error isolation.
	good := &fakeSearchProvider{
		id:   "fake-good",
		name: "Fake Good",
		hits: []crm.SearchHit{
			{ExternalID: "g-1", Name: "Acme G1", Industry: "Software", Address: "Vienna, AT", ExternalURL: "https://fake-good/co/g-1"},
			{ExternalID: "g-2", Name: "Acme G2", Industry: "Hardware", Address: "Linz, AT", ExternalURL: "https://fake-good/co/g-2"},
		},
	}
	bad := &fakeSearchProvider{
		id:   "fake-bad",
		name: "Fake Bad",
		err:  &crm.ProviderError{Kind: crm.ErrProviderAuth, Msg: "fake-bad: token rejected"},
	}
	crm.Register(good)
	crm.Register(bad)

	// Seed enabled+configured config rows — handler skips disabled
	// providers, so without these the fan-out is empty.
	if _, err := db.DB.Exec(
		`INSERT INTO provider_configs(provider_id, enabled, config_json, config_secret_json, updated_at) VALUES (?, 1, '{}', NULL, datetime('now'))`,
		good.ID(),
	); err != nil {
		t.Fatalf("seed config %s: %v", good.ID(), err)
	}
	if _, err := db.DB.Exec(
		`INSERT INTO provider_configs(provider_id, enabled, config_json, config_secret_json, updated_at) VALUES (?, 1, '{}', NULL, datetime('now'))`,
		bad.ID(),
	); err != nil {
		t.Fatalf("seed config %s: %v", bad.ID(), err)
	}

	// Pre-import g-1 locally so dedup flags it AlreadyImported.
	res, err := db.DB.Exec(
		`INSERT INTO customers(name, external_provider, external_id) VALUES (?, ?, ?)`,
		"Acme G1 (local)", good.ID(), "g-1",
	)
	if err != nil {
		t.Fatalf("seed local customer: %v", err)
	}
	localID, _ := res.LastInsertId()

	// Drive the handler directly.
	req := httptest.NewRequest(http.MethodGet, "/api/integrations/crm/search?q=Acme", nil)
	rec := httptest.NewRecorder()
	crm.SearchProviders(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Providers []struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Hits  []crm.SearchHit `json:"hits"`
			Error string          `json:"error,omitempty"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v — body: %s", err, rec.Body.String())
	}

	// Locate each provider by id (List() returns sorted-by-name order,
	// which is "Fake Bad" before "Fake Good", but we don't rely on
	// alphabetic order in the contract).
	var goodGroup, badGroup *struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Hits  []crm.SearchHit `json:"hits"`
		Error string          `json:"error,omitempty"`
	}
	for i := range body.Providers {
		switch body.Providers[i].ID {
		case good.ID():
			goodGroup = &body.Providers[i]
		case bad.ID():
			badGroup = &body.Providers[i]
		}
	}
	if goodGroup == nil {
		t.Fatalf("missing %s in response", good.ID())
	}
	if badGroup == nil {
		t.Fatalf("missing %s in response", bad.ID())
	}

	// Good provider: 2 hits, g-1 already imported with the local id stamped, g-2 not.
	if len(goodGroup.Hits) != 2 {
		t.Fatalf("good hits: got %d, want 2", len(goodGroup.Hits))
	}
	if goodGroup.Error != "" {
		t.Errorf("good provider should have no error, got %q", goodGroup.Error)
	}
	var g1, g2 *crm.SearchHit
	for i := range goodGroup.Hits {
		switch goodGroup.Hits[i].ExternalID {
		case "g-1":
			g1 = &goodGroup.Hits[i]
		case "g-2":
			g2 = &goodGroup.Hits[i]
		}
	}
	if g1 == nil || g2 == nil {
		t.Fatalf("expected hits g-1 and g-2 in response, got %+v", goodGroup.Hits)
	}
	if !g1.AlreadyImported {
		t.Errorf("g-1 should be flagged AlreadyImported")
	}
	if g1.LocalCustomerID != localID {
		t.Errorf("g-1 LocalCustomerID: got %d, want %d", g1.LocalCustomerID, localID)
	}
	if g2.AlreadyImported {
		t.Errorf("g-2 should NOT be flagged AlreadyImported")
	}
	if g2.LocalCustomerID != 0 {
		t.Errorf("g-2 LocalCustomerID should be zero, got %d", g2.LocalCustomerID)
	}

	// Bad provider: error populated, hits empty — broken integration
	// must not leak into the good provider's slot.
	if badGroup.Error == "" {
		t.Errorf("bad provider should have an error string")
	}
	if len(badGroup.Hits) != 0 {
		t.Errorf("bad provider hits should be empty, got %d", len(badGroup.Hits))
	}
}

// Test_CRMSearch_EmptyQuery pins the cheap-rejection contract: a missing
// query short-circuits to an empty Providers list without contacting
// any upstream. Saves the dropdown a useless round-trip on every keystroke
// before the input has content.
func Test_CRMSearch_EmptyQuery(t *testing.T) {
	_ = newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/crm/search?q=", nil)
	rec := httptest.NewRecorder()
	crm.SearchProviders(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var body struct {
		Providers []any `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Providers) != 0 {
		t.Errorf("empty query should yield no providers, got %d", len(body.Providers))
	}
}
