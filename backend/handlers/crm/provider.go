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
