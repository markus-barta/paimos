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

package crm

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// HTTP handlers for the plugin layer:
//   - Admin Integrations endpoints (PAI-104): list providers, get/put
//     config, toggle enabled.
//   - Generic customer import + sync endpoints (PAI-103): route to the
//     right Provider via the registry.
//
// All handlers are admin-gated by the routing layer (see backend/main.go).
// Tokens / secrets never appear in any response body or log line — the
// only path that touches plaintext secrets is the in-memory ProviderConfig
// passed to a Provider call.

// ── Admin: list registered providers + per-provider state ───────────

type providerListItem struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	LogoURL       string       `json:"logo_url"`
	Enabled       bool         `json:"enabled"`
	Configured    bool         `json:"configured"` // true when all required fields have a value
	Schema        ConfigSchema `json:"schema"`
	TestSupported bool         `json:"test_supported"` // PAI-259: provider implements ConnectionTester
}

func ListProviders(w http.ResponseWriter, r *http.Request) {
	out := []providerListItem{}
	for _, p := range List() {
		rec, err := LoadConfig(p.ID())
		if err != nil {
			jsonError(w, "config load failed for "+p.ID(), http.StatusInternalServerError)
			return
		}
		merged := rec.MergedValues()
		schema := p.ConfigSchema()
		_, testable := p.(ConnectionTester)
		out = append(out, providerListItem{
			ID:            p.ID(),
			Name:          p.Name(),
			LogoURL:       p.LogoURL(),
			Enabled:       rec.Enabled,
			Configured:    schemaSatisfied(schema, merged),
			Schema:        schema,
			TestSupported: testable,
		})
	}
	jsonOK(w, out)
}

// TestProviderConnection is POST /api/integrations/crm/{id}/test (PAI-259).
// Admin-only. Loads the persisted config, defers to the provider's
// ConnectionTester implementation, and returns a structured result the
// admin UI surfaces inline. The endpoint never sees nor logs the secret
// itself — it round-trips through the same merged config that powers
// real imports.
func TestProviderConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, ok := Get(id)
	if !ok {
		jsonError(w, "unknown provider", http.StatusNotFound)
		return
	}
	tester, ok := p.(ConnectionTester)
	if !ok {
		jsonError(w, "this provider does not support connection testing", http.StatusNotImplemented)
		return
	}
	rec, err := LoadConfig(id)
	if err != nil {
		jsonError(w, "config load failed", http.StatusInternalServerError)
		return
	}
	merged := rec.MergedValues()
	if !schemaSatisfied(p.ConfigSchema(), merged) {
		jsonOK(w, TestResult{
			OK:      false,
			Message: "configure required fields and save before testing",
		})
		return
	}
	// Bound the test so a stuck upstream can't pin an admin tab. The
	// provider's own client may apply a tighter timeout — this is the
	// outer cap.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result := tester.TestConnection(ctx, ProviderConfig{Values: merged})
	jsonOK(w, result)
}

// ── Admin: get one provider's config (no secret values) ─────────────

type providerConfigResponse struct {
	ProviderID string                       `json:"provider_id"`
	Enabled    bool                         `json:"enabled"`
	Fields     []providerConfigFieldValue   `json:"fields"`
}

type providerConfigFieldValue struct {
	ConfigField
	// For Type="secret": HasValue indicates a stored value exists. The
	// actual value is NEVER returned. For other types, Value is the
	// stored string (or "" when unset).
	Value    string `json:"value,omitempty"`
	HasValue bool   `json:"has_value"`
}

func GetProviderConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, ok := Get(id)
	if !ok {
		jsonError(w, "unknown provider", http.StatusNotFound)
		return
	}
	rec, err := LoadConfig(id)
	if err != nil {
		jsonError(w, "config load failed", http.StatusInternalServerError)
		return
	}
	out := providerConfigResponse{ProviderID: id, Enabled: rec.Enabled}
	for _, f := range p.ConfigSchema().Fields {
		fv := providerConfigFieldValue{ConfigField: f}
		if f.Type == "secret" {
			fv.HasValue = rec.Secret[f.Key] != ""
			// Never echo the secret value — even to the admin who set it.
		} else {
			v := rec.NonSecret[f.Key]
			fv.Value = v
			fv.HasValue = v != ""
		}
		out.Fields = append(out.Fields, fv)
	}
	jsonOK(w, out)
}

// ── Admin: write one provider's config (merge) ──────────────────────

type providerConfigUpdate struct {
	// Map of field key → value. Secret fields: pass the new value to set,
	// the empty string to clear, or omit the key to leave unchanged.
	Values map[string]*string `json:"values"`
}

func PutProviderConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, ok := Get(id)
	if !ok {
		jsonError(w, "unknown provider", http.StatusNotFound)
		return
	}
	var body providerConfigUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	rec, err := LoadConfig(id)
	if err != nil {
		jsonError(w, "config load failed", http.StatusInternalServerError)
		return
	}

	schema := p.ConfigSchema()
	fieldByKey := map[string]ConfigField{}
	for _, f := range schema.Fields {
		fieldByKey[f.Key] = f
	}

	// Apply the patch. Unknown keys are rejected so a typo doesn't
	// silently drift config — providers can grow new fields, but the
	// admin's old client should still get 400 if it sends an unknown key.
	for key, valPtr := range body.Values {
		f, known := fieldByKey[key]
		if !known {
			jsonError(w, "unknown field: "+key, http.StatusBadRequest)
			return
		}
		if valPtr == nil {
			continue
		}
		v := *valPtr
		target := rec.NonSecret
		if f.Type == "secret" {
			target = rec.Secret
		}
		if v == "" {
			delete(target, key)
		} else {
			target[key] = v
		}
	}

	// Defer validation to the provider so it can apply provider-specific
	// rules (e.g. token shape, portal_id is digits, …).
	if err := p.ValidateConfig(rec.MergedValues()); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	user := auth.GetUser(r)
	var uid int64
	if user != nil {
		uid = user.ID
	}
	if err := SaveConfig(rec, uid); err != nil {
		log.Printf("crm: SaveConfig %s: %v", id, err)
		jsonError(w, "save failed", http.StatusInternalServerError)
		return
	}
	GetProviderConfig(w, r)
}

// ── Admin: toggle enabled ───────────────────────────────────────────

func PutProviderEnabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, ok := Get(id)
	if !ok {
		jsonError(w, "unknown provider", http.StatusNotFound)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	rec, err := LoadConfig(id)
	if err != nil {
		jsonError(w, "config load failed", http.StatusInternalServerError)
		return
	}
	// Refuse to enable a misconfigured provider — better to fail loudly
	// here than silently route imports to a provider that's missing a
	// token.
	if body.Enabled && !schemaSatisfied(p.ConfigSchema(), rec.MergedValues()) {
		jsonError(w, "provider is missing required configuration", http.StatusBadRequest)
		return
	}
	rec.Enabled = body.Enabled
	user := auth.GetUser(r)
	var uid int64
	if user != nil {
		uid = user.ID
	}
	if err := SaveConfig(rec, uid); err != nil {
		jsonError(w, "save failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]bool{"enabled": rec.Enabled})
}

// ── Customer: import via provider ───────────────────────────────────

type importRequest struct {
	Provider string `json:"provider"`
	Ref      string `json:"ref"`
}

// ImportCustomer handles POST /api/customers/import — looks up the named
// provider, calls ImportRef, persists the resulting customer row.
func ImportCustomer(w http.ResponseWriter, r *http.Request) {
	var body importRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" || body.Ref == "" {
		jsonError(w, "provider and ref required", http.StatusBadRequest)
		return
	}
	p, ok := Get(body.Provider)
	if !ok {
		jsonError(w, "unknown provider: "+body.Provider, http.StatusNotFound)
		return
	}
	rec, err := LoadConfig(body.Provider)
	if err != nil {
		jsonError(w, "config load failed", http.StatusInternalServerError)
		return
	}
	if !rec.Enabled {
		jsonError(w, "provider is disabled", http.StatusConflict)
		return
	}
	imp, err := p.ImportRef(r.Context(), body.Ref, ProviderConfig{Values: rec.MergedValues()})
	if err != nil {
		mapProviderError(w, p.ID(), err)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		INSERT INTO customers(
			name, external_id, external_url, external_provider, synced_at,
			contact_name, contact_email, address, country, industry
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		imp.Name, nullableStr(imp.ExternalID), nullableStr(imp.ExternalURL), nullableStr(p.ID()), now,
		imp.ContactName, imp.ContactEmail, imp.Address, imp.Country, imp.Industry,
	)
	if err != nil {
		log.Printf("crm: insert imported customer (%s): %v", p.ID(), err)
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]int64{"id": id})
}

// ── Customer: re-sync via provider ──────────────────────────────────

// SyncCustomer handles POST /api/customers/:id/sync. Routes to the
// stored external_provider; PATCHes only provider-sourced fields so
// PAIMOS-only fields (rates, notes) are preserved.
func SyncCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var (
		extID, extProv *string
	)
	if err := db.DB.QueryRow(
		"SELECT external_id, external_provider FROM customers WHERE id=?", id,
	).Scan(&extID, &extProv); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if extProv == nil || extID == nil {
		jsonError(w, "customer is not linked to an external CRM", http.StatusBadRequest)
		return
	}
	p, ok := Get(*extProv)
	if !ok {
		jsonError(w, "provider no longer compiled in: "+*extProv, http.StatusConflict)
		return
	}
	rec, err := LoadConfig(p.ID())
	if err != nil {
		jsonError(w, "config load failed", http.StatusInternalServerError)
		return
	}
	if !rec.Enabled {
		jsonError(w, "provider is disabled", http.StatusConflict)
		return
	}
	upd, err := p.Sync(r.Context(), *extID, ProviderConfig{Values: rec.MergedValues()})
	if err != nil {
		mapProviderError(w, p.ID(), err)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	// COALESCE-style update: any field the provider didn't return stays
	// as it was. Crucially, rate_hourly / rate_lp / notes / contact_*
	// (when nil in upd) are NEVER touched — this is the "PAIMOS-only
	// fields preserved" guarantee.
	_, err = db.DB.Exec(`
		UPDATE customers SET
			name          = COALESCE(?, name),
			contact_name  = COALESCE(?, contact_name),
			contact_email = COALESCE(?, contact_email),
			address       = COALESCE(?, address),
			country       = COALESCE(?, country),
			industry      = COALESCE(?, industry),
			external_url  = CASE WHEN ? IS NOT NULL THEN ? ELSE external_url END,
			synced_at     = ?,
			updated_at    = ?
		WHERE id=?
	`, upd.Name, upd.ContactName, upd.ContactEmail, upd.Address, upd.Country, upd.Industry,
		upd.ExternalURL, upd.ExternalURL, now, now, id)
	if err != nil {
		log.Printf("crm: sync update %d (%s): %v", id, p.ID(), err)
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]int64{"id": id})
}

// ── Provider error handling ─────────────────────────────────────────

// ProviderError lets a provider classify its failure for the generic
// HTTP layer to map cleanly. Unknown errors become 500.
type ProviderError struct {
	Kind ProviderErrorKind
	Msg  string
}

type ProviderErrorKind int

const (
	ErrProviderUnknown ProviderErrorKind = iota
	ErrProviderUnreachable
	ErrProviderAuth
	ErrProviderNotFound
	ErrProviderBadRequest
)

func (e *ProviderError) Error() string { return e.Msg }

func mapProviderError(w http.ResponseWriter, providerID string, err error) {
	var pe *ProviderError
	msg := providerID + ": " + err.Error()
	if errors.As(err, &pe) {
		switch pe.Kind {
		case ErrProviderUnreachable:
			jsonError(w, msg, http.StatusBadGateway)
		case ErrProviderAuth:
			jsonError(w, msg, http.StatusUnauthorized)
		case ErrProviderNotFound:
			jsonError(w, msg, http.StatusNotFound)
		case ErrProviderBadRequest:
			jsonError(w, msg, http.StatusBadRequest)
		default:
			log.Printf("crm: provider %s error: %v", providerID, err)
			jsonError(w, msg, http.StatusInternalServerError)
		}
		return
	}
	log.Printf("crm: provider %s untyped error: %v", providerID, err)
	jsonError(w, msg, http.StatusInternalServerError)
}

// ── small utilities ─────────────────────────────────────────────────

func schemaSatisfied(schema ConfigSchema, values map[string]string) bool {
	for _, f := range schema.Fields {
		if f.Required && values[f.Key] == "" {
			return false
		}
	}
	return true
}

func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
