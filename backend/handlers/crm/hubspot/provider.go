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
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/handlers/crm"
)

// Register on package import. main.go does a blank import of this
// package so the provider lights up at boot.
func init() {
	crm.Register(&Provider{})
}

// Provider implements crm.Provider for HubSpot. Stateless — the API
// token + portal_id come in via crm.ProviderConfig on every call.
type Provider struct{}

func (p *Provider) ID() string      { return "hubspot" }
func (p *Provider) Name() string    { return "HubSpot" }
func (p *Provider) LogoURL() string { return "/assets/crm/hubspot.svg" }

func (p *Provider) ConfigSchema() crm.ConfigSchema {
	return crm.ConfigSchema{Fields: []crm.ConfigField{
		{
			Key:         "token",
			Label:       "Private App Token",
			Type:        "secret",
			Required:    true,
			Help:        "HubSpot Private App access token with crm.objects.companies.read scope.",
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

func (p *Provider) ValidateConfig(values map[string]string) error {
	if !portalIDRe.MatchString(values["portal_id"]) {
		return errors.New("portal_id must be a numeric HubSpot account id")
	}
	if !strings.HasPrefix(values["token"], "pat-") {
		return errors.New("token must be a HubSpot Private App token (pat-…)")
	}
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

// fetchCompany calls HubSpot's CRM API for the named properties.
func (p *Provider) fetchCompany(ctx context.Context, companyID, token string) (*hubspotCompany, error) {
	props := "name,domain,industry,city,country,phone"
	endpoint := "https://api.hubapi.com/crm/v3/objects/companies/" + url.PathEscape(companyID) +
		"?properties=" + url.QueryEscape(props)

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
	c, err := p.fetchCompany(ctx, companyID, cfg.Get("token"))
	if err != nil {
		return crm.CustomerImport{}, err
	}
	return crm.CustomerImport{
		Name:        propString(c.Properties, "name"),
		Industry:    propString(c.Properties, "industry"),
		Address:     joinAddress(propString(c.Properties, "city"), propString(c.Properties, "country")),
		Country:     propString(c.Properties, "country"),
		ExternalID:  c.ID,
		ExternalURL: p.DeepLink(c.ID, cfg),
	}, nil
}

func (p *Provider) Sync(ctx context.Context, externalID string, cfg crm.ProviderConfig) (crm.PartialUpdate, error) {
	c, err := p.fetchCompany(ctx, externalID, cfg.Get("token"))
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
	deepLink := p.DeepLink(c.ID, cfg)
	return crm.PartialUpdate{
		Name:        &name,
		Industry:    &industry,
		Address:     &address,
		Country:     &country,
		ExternalURL: &deepLink,
	}, nil
}

func (p *Provider) DeepLink(externalID string, cfg crm.ProviderConfig) string {
	portal := cfg.Get("portal_id")
	if portal == "" || externalID == "" {
		return ""
	}
	return fmt.Sprintf("https://app.hubspot.com/contacts/%s/company/%s", portal, externalID)
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

func truncateForLog(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
