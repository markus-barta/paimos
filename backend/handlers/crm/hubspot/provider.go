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

// Package hubspot is the first reference Provider implementation against
// the crm plugin layer (PAI-56 → PAI-101). Imports a HubSpot company by
// URL or bare ID, supports manual re-sync, builds deep-link URLs back
// into the HubSpot UI for editing.
package hubspot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/handlers/crm"
)

// Register on package import. main.go does a blank import of this
// package so the provider lights up at boot.
func init() {
	crm.Register(&Provider{})
}

// apiBase is the upstream HubSpot REST host. Exposed as a package var
// (not a const) so tests can swap in an httptest server URL — there
// is no production code path that mutates it.
var apiBase = "https://api.hubapi.com"

// Provider implements crm.Provider for HubSpot. Stateless — the API
// token + portal_id come in via crm.ProviderConfig on every call.
type Provider struct{}

func (p *Provider) ID() string      { return "hubspot" }
func (p *Provider) Name() string    { return "HubSpot" }
// LogoURL is suffixed with `-v2` so a future re-skin can bump the suffix
// instead of relying on cache-control to invalidate. The original
// `/assets/crm/hubspot.svg` URL got a 404 on first deploy and that 404
// was strongly cached by the static handler (now fixed in main.go).
func (p *Provider) LogoURL() string { return "/assets/crm/hubspot-v2.svg" }

func (p *Provider) ConfigSchema() crm.ConfigSchema {
	return crm.ConfigSchema{Fields: []crm.ConfigField{
		{
			Key:         "token",
			Label:       "Access Token",
			Type:        "secret",
			Required:    true,
			Help:        "Use a HubSpot Private App token (pat-na1-…) — that is the only format we have seen authenticate reliably. Personal Access Keys and Service Account keys (Service-Schlüssel) are accepted by this form but have failed against HubSpot in practice; prefer a Private App. Only one scope is required: crm.objects.companies.read — tick that single box and leave everything else off (PAIMOS does not write to HubSpot and does not read contacts, deals, schemas, or sensitive-classified fields).",
			Placeholder: "pat-na1-…",
		},
		{
			Key:         "portal_id",
			Label:       "Portal ID",
			Type:        "string",
			Required:    true,
			Help:        "Numeric HubSpot account ID — used to build deep-link URLs back into the HubSpot UI.",
			Placeholder: "12345678",
		},
	}}
}

var portalIDRe = regexp.MustCompile(`^\d+$`)

// minTokenLen is a generous floor that catches obvious paste-truncation
// without being prescriptive about HubSpot's actual format. Real Private
// App tokens are ~70 chars, real Personal Access Keys ~50 chars; 20 is
// short enough that anything below it is almost certainly a mistake.
const minTokenLen = 20

func (p *Provider) ValidateConfig(values map[string]string) error {
	if !portalIDRe.MatchString(values["portal_id"]) {
		return errors.New("portal_id must be a numeric HubSpot account id")
	}
	token := strings.TrimSpace(values["token"])
	if token == "" {
		return errors.New("access token must not be empty")
	}
	// Paste-of-Bearer-header is the most common copy-paste mistake;
	// surface it explicitly instead of failing the length / format
	// check with a misleading message.
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return errors.New("paste the token only — drop the leading 'Bearer '")
	}
	if strings.ContainsAny(token, " \t\n\r") {
		return errors.New("access token must not contain whitespace")
	}
	if len(token) < minTokenLen {
		return errors.New("access token looks too short — paste the full HubSpot key")
	}
	// PAI-258: HubSpot ships at least two valid token formats — the old
	// Private App Token (`pat-na1-…`) and the newer Personal Access Key
	// (opaque base64-looking string with no stable prefix, e.g.
	// `CiRldTEtN…`). Both authenticate the same Bearer-token endpoints,
	// so we accept either rather than gating on a prefix that HubSpot
	// already proved willing to change.
	return nil
}

// hubspotCompanyURLRe matches HubSpot's company URL shape and captures
// the numeric company id. Three known variants:
//   https://app.hubspot.com/contacts/<portal>/company/<id>
//   https://app.hubspot.com/contacts/<portal>/companies/<id>
//   https://app.hubspot.com/contacts/<portal>/record/0-2/<id>   (object-id form)
var hubspotCompanyURLRe = regexp.MustCompile(`hubspot\.com/contacts/\d+/(?:company|companies|record/0-2)/(\d+)`)
var bareIDRe = regexp.MustCompile(`^\d+$`)

// resolveCompanyID accepts a URL or a bare ID and returns the numeric
// HubSpot company id.
func resolveCompanyID(rawRef string) (string, error) {
	rawRef = strings.TrimSpace(rawRef)
	if bareIDRe.MatchString(rawRef) {
		return rawRef, nil
	}
	if m := hubspotCompanyURLRe.FindStringSubmatch(rawRef); m != nil {
		return m[1], nil
	}
	// Try parsing as a URL anyway — in case someone pasted an
	// unexpected variant we can still pull a trailing-segment numeric id.
	if u, err := url.Parse(rawRef); err == nil && u.Path != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) > 0 {
			if last := parts[len(parts)-1]; bareIDRe.MatchString(last) {
				return last, nil
			}
		}
	}
	return "", &crm.ProviderError{
		Kind: crm.ErrProviderBadRequest,
		Msg:  "could not extract HubSpot company id from input — paste a HubSpot company URL or numeric id",
	}
}

// hubspotCompany is the slice of HubSpot's company response we read.
type hubspotCompany struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
}

// companyProps is the property set we ask HubSpot for on every
// company fetch. Kept in one place (vs duplicated across fetchCompany
// + Search) so adding a column happens once. PAI-273 extended this
// from the original 6-property set to the full Companies object slice
// PAIMOS now stores.
const companyProps = "name,domain,website,industry,numberofemployees,annualrevenue," +
	"description,phone,address,address2,city,state,zip,country"

// fetchCompany calls HubSpot's CRM API for the named properties.
func (p *Provider) fetchCompany(ctx context.Context, companyID, token string) (*hubspotCompany, error) {
	endpoint := apiBase + "/crm/v3/objects/companies/" + url.PathEscape(companyID) +
		"?properties=" + url.QueryEscape(companyProps)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "HubSpot API unreachable: " + err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusOK:
		// fall through
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &crm.ProviderError{Kind: crm.ErrProviderAuth, Msg: "HubSpot rejected the token"}
	case resp.StatusCode == http.StatusNotFound:
		return nil, &crm.ProviderError{Kind: crm.ErrProviderNotFound, Msg: "HubSpot company not found"}
	default:
		return nil, &crm.ProviderError{
			Kind: crm.ErrProviderUnknown,
			Msg:  fmt.Sprintf("HubSpot API status %d: %s", resp.StatusCode, truncateForLog(body, 200)),
		}
	}
	var out hubspotCompany
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnknown, Msg: "decode HubSpot response: " + err.Error()}
	}
	return &out, nil
}

func (p *Provider) ImportRef(ctx context.Context, rawRef string, cfg crm.ProviderConfig) (crm.CustomerImport, error) {
	companyID, err := resolveCompanyID(rawRef)
	if err != nil {
		return crm.CustomerImport{}, err
	}
	token := cfg.Get("token")
	c, err := p.fetchCompany(ctx, companyID, token)
	if err != nil {
		return crm.CustomerImport{}, err
	}
	imp := crm.CustomerImport{
		Name:               propString(c.Properties, "name"),
		Industry:           propString(c.Properties, "industry"),
		Address:            joinAddress(propString(c.Properties, "city"), propString(c.Properties, "country")),
		Country:            propString(c.Properties, "country"),
		Website:            propString(c.Properties, "website"),
		Domain:             propString(c.Properties, "domain"),
		Description:        propString(c.Properties, "description"),
		Phone:              propString(c.Properties, "phone"),
		VisitAddressStreet: joinStreet(
			propString(c.Properties, "address"),
			propString(c.Properties, "address2"),
		),
		VisitAddressZip:    propString(c.Properties, "zip"),
		EmployeeCount:      propInt(c.Properties, "numberofemployees"),
		AnnualRevenueCents: propMoneyCents(c.Properties, "annualrevenue"),
		ExternalID:         c.ID,
		ExternalURL:        p.DeepLink(c.ID, cfg),
	}
	// Pull associated contacts. Soft-fail: if the contacts call fails
	// (rate limit, transient 5xx) we still return the company import —
	// the user can re-sync later to populate contacts. Logging happens
	// upstream in the import handler when it sees an empty Contacts
	// slice on a HubSpot-linked customer.
	if contacts, err := p.fetchAssociatedContacts(ctx, c.ID, token); err == nil {
		imp.Contacts = contacts
	}
	return imp, nil
}

func (p *Provider) Sync(ctx context.Context, externalID string, cfg crm.ProviderConfig) (crm.PartialUpdate, error) {
	token := cfg.Get("token")
	c, err := p.fetchCompany(ctx, externalID, token)
	if err != nil {
		return crm.PartialUpdate{}, err
	}
	// Wrap each provider-sourced field in a *string so the generic
	// PATCH handler knows to overwrite it. Empty strings from HubSpot
	// still clear the field — that matches "the upstream is the source
	// of truth for these specific fields".
	name := propString(c.Properties, "name")
	industry := propString(c.Properties, "industry")
	address := joinAddress(propString(c.Properties, "city"), propString(c.Properties, "country"))
	country := propString(c.Properties, "country")
	website := propString(c.Properties, "website")
	domain := propString(c.Properties, "domain")
	description := propString(c.Properties, "description")
	phone := propString(c.Properties, "phone")
	visitStreet := joinStreet(propString(c.Properties, "address"), propString(c.Properties, "address2"))
	visitZip := propString(c.Properties, "zip")
	deepLink := p.DeepLink(c.ID, cfg)
	upd := crm.PartialUpdate{
		Name:               &name,
		Industry:           &industry,
		Address:            &address,
		Country:            &country,
		Website:            &website,
		Domain:             &domain,
		Description:        &description,
		Phone:              &phone,
		VisitAddressStreet: &visitStreet,
		VisitAddressZip:    &visitZip,
		EmployeeCount:      propInt(c.Properties, "numberofemployees"),
		AnnualRevenueCents: propMoneyCents(c.Properties, "annualrevenue"),
		ExternalURL:        &deepLink,
	}
	if contacts, err := p.fetchAssociatedContacts(ctx, c.ID, token); err == nil {
		upd.Contacts = contacts
	}
	return upd, nil
}

func (p *Provider) DeepLink(externalID string, cfg crm.ProviderConfig) string {
	portal := cfg.Get("portal_id")
	if portal == "" || externalID == "" {
		return ""
	}
	return fmt.Sprintf("https://app.hubspot.com/contacts/%s/company/%s", portal, externalID)
}

// TestConnection (PAI-259) verifies the stored token authenticates
// against HubSpot without needing a specific company id. Hits the
// scope-light `crm/v3/objects/companies?limit=1` endpoint — that's the
// same scope (`crm.objects.companies.read`) the import flow uses, so
// an OK here means the integration is genuinely usable.
//
// We deliberately do NOT call the `/account-info/v3/details` endpoint
// even though it works without scopes: a token that authenticates but
// lacks `crm.objects.companies.read` would pass that test and still
// fail the first import. Picking the same endpoint as the real flow
// makes "Test integration" a faithful smoke test.
func (p *Provider) TestConnection(ctx context.Context, cfg crm.ProviderConfig) crm.TestResult {
	token := strings.TrimSpace(cfg.Get("token"))
	portal := strings.TrimSpace(cfg.Get("portal_id"))
	if token == "" {
		return crm.TestResult{OK: false, Message: "no access token configured"}
	}
	if portal == "" {
		return crm.TestResult{OK: false, Message: "no portal_id configured"}
	}

	endpoint := apiBase + "/crm/v3/objects/companies?limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return crm.TestResult{OK: false, Message: "request build failed: " + err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	cli := &http.Client{Timeout: 10 * time.Second}
	t0 := time.Now()
	resp, err := cli.Do(req)
	latency := time.Since(t0)
	lines := []string{
		"GET " + endpoint,
		fmt.Sprintf("portal_id=%s", portal),
	}
	if err != nil {
		lines = append(lines, "transport error: "+err.Error())
		return crm.TestResult{OK: false, Message: "HubSpot unreachable", Lines: lines}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lines = append(lines, fmt.Sprintf("status %d in %dms", resp.StatusCode, latency.Milliseconds()))

	switch {
	case resp.StatusCode == http.StatusOK:
		// Don't try to parse the body — the count alone is meaningful.
		// Truncate just in case the response is huge or empty so the log
		// stays readable.
		var preview struct {
			Results []struct {
				ID string `json:"id"`
			} `json:"results"`
		}
		_ = json.Unmarshal(body, &preview)
		count := len(preview.Results)
		lines = append(lines, fmt.Sprintf("companies endpoint OK · %d row(s) returned", count))
		return crm.TestResult{
			OK:      true,
			Message: fmt.Sprintf("Authenticated to HubSpot portal %s · companies scope verified", portal),
			Lines:   lines,
		}
	case resp.StatusCode == http.StatusUnauthorized:
		lines = append(lines, "401 Unauthorized — token rejected by HubSpot")
		return crm.TestResult{OK: false, Message: "HubSpot rejected the token (401)", Lines: lines}
	case resp.StatusCode == http.StatusForbidden:
		lines = append(lines, "403 Forbidden — token authenticates but lacks crm.objects.companies.read")
		return crm.TestResult{OK: false, Message: "Token is missing the crm.objects.companies.read scope (403)", Lines: lines}
	default:
		lines = append(lines, "response: "+truncateForLog(body, 160))
		return crm.TestResult{
			OK:      false,
			Message: fmt.Sprintf("HubSpot returned status %d", resp.StatusCode),
			Lines:   lines,
		}
	}
}

// Search (PAI-266) implements crm.Searcher against HubSpot's
// /crm/v3/objects/companies/search endpoint. Same `crm.objects.companies.read`
// scope as ImportRef — no new permission for operators to grant. The
// `query` field does HubSpot-side full-text search across the default
// company properties (name, domain, phone, website); we explicitly ask
// for the props we render in the dropdown so a single request answers
// the search.
func (p *Provider) Search(ctx context.Context, query string, limit int, cfg crm.ProviderConfig) ([]crm.SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	token := strings.TrimSpace(cfg.Get("token"))
	if token == "" {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderBadRequest, Msg: "no access token configured"}
	}
	body := map[string]any{
		"query":      query,
		"limit":      limit,
		"properties": []string{"name", "domain", "industry", "city", "country"},
	}
	enc, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint := apiBase + "/crm/v3/objects/companies/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(enc)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	cli := &http.Client{Timeout: 10 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "HubSpot API unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusOK:
		// fall through
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &crm.ProviderError{Kind: crm.ErrProviderAuth, Msg: "HubSpot rejected the token"}
	default:
		return nil, &crm.ProviderError{
			Kind: crm.ErrProviderUnknown,
			Msg:  fmt.Sprintf("HubSpot search status %d: %s", resp.StatusCode, truncateForLog(raw, 200)),
		}
	}
	var out struct {
		Total   int              `json:"total"`
		Results []hubspotCompany `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnknown, Msg: "decode HubSpot search response: " + err.Error()}
	}
	hits := make([]crm.SearchHit, 0, len(out.Results))
	for _, c := range out.Results {
		hits = append(hits, crm.SearchHit{
			ExternalID:  c.ID,
			Name:        propString(c.Properties, "name"),
			Industry:    propString(c.Properties, "industry"),
			Address:     joinAddress(propString(c.Properties, "city"), propString(c.Properties, "country")),
			ExternalURL: p.DeepLink(c.ID, cfg),
		})
	}
	return hits, nil
}

// ── helpers ─────────────────────────────────────────────────────────

func propString(props map[string]interface{}, key string) string {
	if props == nil {
		return ""
	}
	if v, ok := props[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// propInt parses HubSpot's stringly-typed integer properties (e.g.
// numberofemployees) into a *int64. Returns nil for empty / unparseable
// — the customer column is nullable so unset stays unset.
func propInt(props map[string]interface{}, key string) *int64 {
	s := strings.TrimSpace(propString(props, key))
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// propMoneyCents reads a HubSpot money string (e.g. "1500000.00") and
// returns the value in cents. Returns nil for empty / unparseable so
// the customer column stays NULL.
func propMoneyCents(props map[string]interface{}, key string) *int64 {
	s := strings.TrimSpace(propString(props, key))
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	cents := int64(f * 100)
	return &cents
}

func joinAddress(city, country string) string {
	switch {
	case city != "" && country != "":
		return city + ", " + country
	case city != "":
		return city
	default:
		return country
	}
}

// joinStreet handles HubSpot's split address1 / address2 properties.
// Both empty → "", one empty → the populated one, both populated →
// joined with ", ".
func joinStreet(line1, line2 string) string {
	switch {
	case line1 != "" && line2 != "":
		return line1 + ", " + line2
	case line1 != "":
		return line1
	default:
		return line2
	}
}

// fetchAssociatedContacts pulls the contact-id list associated with a
// HubSpot company, then batch-reads the contact properties we render.
// Two upstream calls in series — the associations endpoint returns IDs
// only and HubSpot's batch-read endpoint accepts up to 100 ids per
// request. We page in chunks of 100 so a Fortune-500 company with 200
// contacts doesn't hit a 500 from a fan-out that's too big.
//
// Soft-fails on any upstream error: callers (ImportRef / Sync) treat
// an error here as "leave contacts alone" rather than failing the
// whole company fetch. This matches the AC's "degrade gracefully if
// HubSpot is slow".
func (p *Provider) fetchAssociatedContacts(ctx context.Context, companyID, token string) ([]crm.ContactImport, error) {
	ids, err := p.fetchCompanyContactIDs(ctx, companyID, token)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	contacts := make([]crm.ContactImport, 0, len(ids))
	const batchSize = 100
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch, err := p.fetchContactBatch(ctx, ids[start:end], token)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, batch...)
	}
	// The first associated contact takes the primary flag if HubSpot
	// didn't tell us which one is primary — we have no signal to do
	// better. The customer-import handler can override this if the
	// customer already has a primary on the PAIMOS side.
	if len(contacts) > 0 {
		contacts[0].IsPrimary = true
	}
	return contacts, nil
}

func (p *Provider) fetchCompanyContactIDs(ctx context.Context, companyID, token string) ([]string, error) {
	endpoint := apiBase + "/crm/v3/objects/companies/" + url.PathEscape(companyID) + "/associations/contacts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "associations endpoint unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// 404 here means "company has no associated contacts" in
		// some edge cases; treat as empty rather than failure.
		if resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, &crm.ProviderError{
			Kind: crm.ErrProviderUnknown,
			Msg:  fmt.Sprintf("associations status %d: %s", resp.StatusCode, truncateForLog(body, 160)),
		}
	}
	var out struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnknown, Msg: "decode associations: " + err.Error()}
	}
	ids := make([]string, 0, len(out.Results))
	for _, r := range out.Results {
		if r.ID != "" {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

func (p *Provider) fetchContactBatch(ctx context.Context, ids []string, token string) ([]crm.ContactImport, error) {
	type inputItem struct {
		ID string `json:"id"`
	}
	inputs := make([]inputItem, 0, len(ids))
	for _, id := range ids {
		inputs = append(inputs, inputItem{ID: id})
	}
	body := map[string]any{
		"properties": []string{"firstname", "lastname", "email", "phone", "jobtitle"},
		"inputs":     inputs,
	}
	enc, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint := apiBase + "/crm/v3/objects/contacts/batch/read"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(enc)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "contacts batch unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, &crm.ProviderError{
			Kind: crm.ErrProviderUnknown,
			Msg:  fmt.Sprintf("contacts batch status %d: %s", resp.StatusCode, truncateForLog(raw, 160)),
		}
	}
	var out struct {
		Results []hubspotCompany `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderUnknown, Msg: "decode contacts batch: " + err.Error()}
	}
	contacts := make([]crm.ContactImport, 0, len(out.Results))
	for _, c := range out.Results {
		first := propString(c.Properties, "firstname")
		last := propString(c.Properties, "lastname")
		name := strings.TrimSpace(first + " " + last)
		if name == "" {
			// Fall back to email if no name — better than a blank
			// row in the UI.
			name = propString(c.Properties, "email")
		}
		contacts = append(contacts, crm.ContactImport{
			Name:       name,
			Email:      propString(c.Properties, "email"),
			Phone:      propString(c.Properties, "phone"),
			Role:       propString(c.Properties, "jobtitle"),
			ExternalID: c.ID,
		})
	}
	return contacts, nil
}

func truncateForLog(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
