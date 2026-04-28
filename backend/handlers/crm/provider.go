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

// Package crm defines the in-process plugin layer that lets PAIMOS
// import and re-sync customers from external CRMs (HubSpot, Pipedrive,
// Salesforce, …) without binding the schema or the API surface to any
// particular provider. See PAI-101 for the architectural rationale and
// PAI-28 for the consuming customer-data layer.
//
// New providers register themselves at boot via Register; the generic
// HTTP handlers route to the right Provider based on the request body
// (import) or the customer row's stored external_provider (sync).
package crm

import "context"

// Provider is the contract a CRM-sync plugin satisfies. Implementations
// live in subpackages (e.g. crm/hubspot) and call Register from a package
// init() so the registry is populated before main() boots routes.
//
// The interface is deliberately narrow: PAIMOS owns the customer data
// model and the HTTP surface; providers only translate between PAIMOS
// fields and their CRM's representation.
type Provider interface {
	// ID returns the stable provider identifier persisted in
	// `customers.external_provider` and `provider_configs.provider_id`.
	// Lowercase, no spaces. Once shipped this is effectively part of the
	// public API — never rename.
	ID() string

	// Name returns the human-readable display name used in UIs.
	Name() string

	// LogoURL returns the path or URL to the provider's logo asset.
	// Frontends fall back to a generic globe icon when this is empty.
	LogoURL() string

	// ConfigSchema declares the configuration fields the admin UI
	// renders. Values land in ProviderConfig with the same keys.
	ConfigSchema() ConfigSchema

	// ValidateConfig is called by the config layer before persisting.
	// Return a non-nil error with a user-facing message when the values
	// are unusable (missing required field, malformed token shape, etc).
	ValidateConfig(values map[string]string) error

	// ImportRef takes a raw user-supplied reference (e.g. a HubSpot
	// company URL or bare ID) and fetches the corresponding customer
	// from the external CRM. The returned CustomerImport is mapped 1:1
	// to the PAIMOS customer create payload, with ExternalID /
	// ExternalURL filled in by the provider.
	ImportRef(ctx context.Context, rawRef string, cfg ProviderConfig) (CustomerImport, error)

	// Sync re-fetches the upstream record and returns only the
	// provider-sourced fields. The generic sync handler PATCHes these
	// over the existing customer row so PAIMOS-only fields (rates,
	// notes) are never overwritten by an upstream change.
	Sync(ctx context.Context, externalID string, cfg ProviderConfig) (PartialUpdate, error)

	// DeepLink builds a URL into the external CRM for editing the
	// customer there. Used as the customer's external_url and rendered
	// as the badge link in the customer detail header.
	DeepLink(externalID string, cfg ProviderConfig) string
}

// ConnectionTester is an OPTIONAL provider hook used by the admin
// "Test integration" button (PAI-259). Providers that implement it
// expose a quick, scope-light authenticated read so admins can verify
// the persisted config works before relying on it for real imports.
//
// Why an opt-in side interface instead of a method on Provider
// ------------------------------------------------------------
// Adding a method to Provider would force every existing and future
// in-tree provider to implement it, which is unfair to read-only
// shims and not all CRMs have a clean "verify auth" endpoint. Type-
// assertion keeps the contract minimal: the handler renders the Test
// button only when the provider implements this interface.
//
// Implementations MUST NOT echo the secret in either the returned
// message or the lines slice — the log surface is rendered inline in
// the admin UI and accidentally including the token would defeat the
// secret-hiding model.
type ConnectionTester interface {
	TestConnection(ctx context.Context, cfg ProviderConfig) TestResult
}

// TestResult is the structured response of a connection test. OK is the
// only thing the UI gates on; Message is a one-liner shown next to a
// status pill; Lines is a small log surface (timestamps not included —
// the frontend stamps them on receipt).
type TestResult struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Lines   []string `json:"lines,omitempty"`
}

// Searcher is an OPTIONAL provider hook used by the customer-search field
// (PAI-266). Providers that implement it expose a name-based company
// search so the customer dropdown can fan out to configured CRMs when
// the local DB returns zero hits.
//
// Same opt-in side-interface pattern as ConnectionTester: type-assertion
// in the search handler, so providers without a clean search endpoint
// (or with prohibitive scope requirements) can opt out without breaking
// the Provider contract.
//
// Implementations MUST use the same scope already required for ImportRef
// — adding a new scope here would force every operator to re-grant
// permission. For HubSpot that is `crm.objects.companies.read`, which
// already covers the search endpoint.
type Searcher interface {
	Search(ctx context.Context, query string, limit int, cfg ProviderConfig) ([]SearchHit, error)
}

// SearchHit is one row in a provider search response. The same field set
// the customer dropdown needs to render a row + import on click. Mirrors
// the subset of CustomerImport fields that are useful pre-import; the
// full record is fetched via ImportRef on accept.
type SearchHit struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	Industry    string `json:"industry,omitempty"`
	Address     string `json:"address,omitempty"`
	ExternalURL string `json:"external_url,omitempty"`
	// AlreadyImported + LocalCustomerID are filled in by the search
	// handler after the fan-out, by joining hits against the local
	// `customers` table on (provider_id, external_id). The provider
	// itself never sets these — leave them zero-valued.
	AlreadyImported bool  `json:"already_imported,omitempty"`
	LocalCustomerID int64 `json:"local_customer_id,omitempty"`
}

// ConfigSchema is the set of fields a provider needs from the admin to
// function. Rendered by the admin Integrations UI (PAI-105).
type ConfigSchema struct {
	Fields []ConfigField `json:"fields"`
}

// ConfigField describes one input the admin UI renders. Type drives the
// input element + storage path: secret fields are encrypted at rest and
// never returned in API responses, only flagged with HasValue=true.
type ConfigField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`               // "string" | "secret" | "number" | "select"
	Required    bool     `json:"required"`
	Help        string   `json:"help,omitempty"`     // single-line help text shown under the input
	Placeholder string   `json:"placeholder,omitempty"`
	// Options is non-empty only for Type="select".
	Options []ConfigOption `json:"options,omitempty"`
}

// ConfigOption is one entry in a select-type field's dropdown.
type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ProviderConfig is the merged config (non-secret + decrypted secrets)
// the plugin layer hands to a Provider call. Stringly-typed for
// simplicity — providers parse / coerce as needed in their own scope.
type ProviderConfig struct {
	Values map[string]string
}

// Get returns the value for key, or empty string if unset.
func (c ProviderConfig) Get(key string) string {
	if c.Values == nil {
		return ""
	}
	return c.Values[key]
}

// CustomerImport is the field set a provider can populate when
// importing a customer for the first time. Maps 1:1 to the PAIMOS
// customer create payload — empty strings = leave unset.
type CustomerImport struct {
	Name         string
	ContactName  string
	ContactEmail string
	Address      string
	Country      string
	Industry     string
	// ExternalID + ExternalURL are filled in by the provider; the
	// generic import handler combines them with the provider's ID()
	// before calling the customer-create flow.
	ExternalID  string
	ExternalURL string
}

// PartialUpdate is what Sync returns: only the provider-sourced fields,
// in pointer form so an unset (nil) field is left untouched on the
// existing customer row. PAIMOS-only fields like rate_hourly never
// appear here — the sync handler structurally cannot overwrite them.
type PartialUpdate struct {
	Name         *string
	ContactName  *string
	ContactEmail *string
	Address      *string
	Country      *string
	Industry     *string
	ExternalURL  *string // deep-link can change if the external system migrates IDs
}
