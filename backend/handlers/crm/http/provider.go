// PAIMOS -- Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// Package httpcrm is the HTTP sidecar CRM provider. It adapts the in-process
// crm.Provider contract to a signed JSON wire contract so non-Go integrators
// can bridge their CRM without compiling code into PAIMOS.
package httpcrm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/handlers/crm"
)

const (
	providerID       = "http"
	defaultTimeout   = 15 * time.Second
	minTimeout       = 1 * time.Second
	maxTimeout       = 60 * time.Second
	maxResponseBytes = 1 << 20
	maxAttempts      = 4
)

var retryBaseDelay = 100 * time.Millisecond

func init() {
	crm.Register(&Provider{})
}

type Provider struct{}

func (p *Provider) ID() string      { return providerID }
func (p *Provider) Name() string    { return "HTTP CRM sidecar" }
func (p *Provider) LogoURL() string { return "" }

func (p *Provider) ConfigSchema() crm.ConfigSchema {
	return crm.ConfigSchema{Fields: []crm.ConfigField{
		{
			Key:         "base_url",
			Label:       "Sidecar URL",
			Type:        "string",
			Required:    true,
			Help:        "Root URL of the CRM sidecar. PAIMOS calls signed /v1 endpoints below this URL.",
			Placeholder: "https://crm-bridge.example.com",
		},
		{
			Key:         "hmac_secret",
			Label:       "HMAC Secret",
			Type:        "secret",
			Required:    true,
			Help:        "Shared signing secret. Stored encrypted and never shown again after save.",
			Placeholder: "paste shared secret",
		},
		{
			Key:         "timeout_seconds",
			Label:       "Timeout",
			Type:        "number",
			Required:    false,
			Help:        "Optional per-request timeout in seconds. Defaults to 15; allowed range is 1-60.",
			Placeholder: "15",
		},
	}}
}

func (p *Provider) ValidateConfig(values map[string]string) error {
	_, err := configFromValues(values)
	return err
}

func (p *Provider) ImportRef(ctx context.Context, rawRef string, cfg crm.ProviderConfig) (crm.CustomerImport, error) {
	rawRef = strings.TrimSpace(rawRef)
	if rawRef == "" {
		return crm.CustomerImport{}, &crm.ProviderError{Kind: crm.ErrProviderBadRequest, Msg: "ref must not be empty"}
	}
	c, err := configFromValues(cfg.Values)
	if err != nil {
		return crm.CustomerImport{}, &crm.ProviderError{Kind: crm.ErrProviderBadRequest, Msg: err.Error()}
	}
	var out customerImportPayload
	if err := p.callJSON(ctx, c, stdhttp.MethodPost, "/v1/import", nil, importRequest{Ref: rawRef}, &out); err != nil {
		return crm.CustomerImport{}, err
	}
	imp := out.toCRM()
	if strings.TrimSpace(imp.ExternalID) == "" {
		return crm.CustomerImport{}, invalidPayloadError("/v1/import", "external_id is required")
	}
	if strings.TrimSpace(imp.Name) == "" {
		return crm.CustomerImport{}, invalidPayloadError("/v1/import", "name is required")
	}
	if imp.ExternalURL == "" {
		imp.ExternalURL = p.DeepLink(imp.ExternalID, cfg)
	}
	return imp, nil
}

func (p *Provider) Sync(ctx context.Context, externalID string, cfg crm.ProviderConfig) (crm.PartialUpdate, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return crm.PartialUpdate{}, &crm.ProviderError{Kind: crm.ErrProviderBadRequest, Msg: "external_id must not be empty"}
	}
	c, err := configFromValues(cfg.Values)
	if err != nil {
		return crm.PartialUpdate{}, &crm.ProviderError{Kind: crm.ErrProviderBadRequest, Msg: err.Error()}
	}
	var out partialUpdatePayload
	if err := p.callJSON(ctx, c, stdhttp.MethodPost, "/v1/sync", nil, syncRequest{ExternalID: externalID}, &out); err != nil {
		return crm.PartialUpdate{}, err
	}
	upd := out.toCRM()
	if upd.ExternalURL == nil {
		if link := p.DeepLink(externalID, cfg); link != "" {
			upd.ExternalURL = &link
		}
	}
	return upd, nil
}

func (p *Provider) DeepLink(externalID string, cfg crm.ProviderConfig) string {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return ""
	}
	c, err := configFromValues(cfg.Values)
	if err != nil {
		return ""
	}
	q := url.Values{"id": []string{externalID}}
	var out deepLinkResponse
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	if err := p.callJSON(ctx, c, stdhttp.MethodGet, "/v1/deep-link", q, nil, &out); err != nil {
		return ""
	}
	return strings.TrimSpace(out.URL)
}

func (p *Provider) TestConnection(ctx context.Context, cfg crm.ProviderConfig) crm.TestResult {
	c, err := configFromValues(cfg.Values)
	if err != nil {
		return crm.TestResult{OK: false, Message: err.Error()}
	}
	start := time.Now()
	var out schemaResponse
	if err := p.callJSON(ctx, c, stdhttp.MethodGet, "/v1/schema", nil, nil, &out); err != nil {
		return crm.TestResult{
			OK:      false,
			Message: providerErrorMessage(err, "HTTP sidecar test failed"),
			Lines: []string{
				"GET " + endpointForLog(c.BaseURL, "/v1/schema"),
				"error: " + providerErrorMessage(err, "request failed"),
			},
		}
	}
	version := strings.TrimSpace(out.Version)
	if version != "crm-http-v1" {
		return crm.TestResult{
			OK:      false,
			Message: "HTTP sidecar returned unsupported schema version",
			Lines: []string{
				"GET " + endpointForLog(c.BaseURL, "/v1/schema"),
				"version=" + version,
			},
		}
	}
	name := strings.TrimSpace(out.Name)
	if name == "" {
		name = "HTTP sidecar"
	}
	return crm.TestResult{
		OK:      true,
		Message: fmt.Sprintf("%s reachable (%s)", name, version),
		Lines: []string{
			"GET " + endpointForLog(c.BaseURL, "/v1/schema"),
			fmt.Sprintf("schema OK in %dms", time.Since(start).Milliseconds()),
		},
	}
}

func (p *Provider) Search(ctx context.Context, query string, limit int, cfg crm.ProviderConfig) ([]crm.SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	c, err := configFromValues(cfg.Values)
	if err != nil {
		return nil, &crm.ProviderError{Kind: crm.ErrProviderBadRequest, Msg: err.Error()}
	}
	var out searchResponsePayload
	if err := p.callJSON(ctx, c, stdhttp.MethodPost, "/v1/search", nil, searchRequest{Query: query, Limit: limit}, &out); err != nil {
		return nil, err
	}
	hits := make([]crm.SearchHit, 0, len(out.Hits))
	for i, h := range out.Hits {
		hit := h.toCRM()
		if strings.TrimSpace(hit.ExternalID) == "" {
			return nil, invalidPayloadError("/v1/search", fmt.Sprintf("hits[%d].external_id is required", i))
		}
		if strings.TrimSpace(hit.Name) == "" {
			return nil, invalidPayloadError("/v1/search", fmt.Sprintf("hits[%d].name is required", i))
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

type providerConfig struct {
	BaseURL string
	Secret  string
	Timeout time.Duration
}

func configFromValues(values map[string]string) (providerConfig, error) {
	baseURL := strings.TrimSpace(values["base_url"])
	if baseURL == "" {
		return providerConfig{}, errors.New("base_url must not be empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return providerConfig{}, errors.New("base_url must be an absolute http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return providerConfig{}, errors.New("base_url must use http or https")
	}
	if u.User != nil {
		return providerConfig{}, errors.New("base_url must not include credentials")
	}
	u.RawQuery = ""
	u.Fragment = ""
	secret := strings.TrimSpace(values["hmac_secret"])
	if secret == "" {
		return providerConfig{}, errors.New("hmac_secret must not be empty")
	}
	timeout := defaultTimeout
	if raw := strings.TrimSpace(values["timeout_seconds"]); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return providerConfig{}, errors.New("timeout_seconds must be a whole number")
		}
		timeout = time.Duration(seconds) * time.Second
		if timeout < minTimeout || timeout > maxTimeout {
			return providerConfig{}, errors.New("timeout_seconds must be between 1 and 60")
		}
	}
	return providerConfig{BaseURL: u.String(), Secret: secret, Timeout: timeout}, nil
}

func (p *Provider) callJSON(ctx context.Context, cfg providerConfig, method, path string, query url.Values, requestBody any, responseBody any) error {
	body := []byte{}
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = encoded
	}
	endpoint, err := endpointURL(cfg.BaseURL, path, query)
	if err != nil {
		return &crm.ProviderError{Kind: crm.ErrProviderBadRequest, Msg: err.Error()}
	}
	client := &stdhttp.Client{Timeout: cfg.Timeout}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := stdhttp.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "paimos-crm-http/1")
		if requestBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		SignRequest(req, cfg.Secret, time.Now(), body)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "HTTP sidecar unreachable: " + err.Error()}
			if attempt < maxAttempts-1 {
				if err := waitBeforeRetry(ctx, "", attempt); err != nil {
					return lastErr
				}
				continue
			}
			return lastErr
		}

		raw, readErr := readLimitedAndClose(resp.Body)
		if readErr != nil {
			return &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "read HTTP sidecar response failed"}
		}
		if isRetryableStatus(resp.StatusCode) && attempt < maxAttempts-1 {
			if err := waitBeforeRetry(ctx, resp.Header.Get("Retry-After"), attempt); err != nil {
				return statusError(resp.StatusCode, method, path)
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return statusError(resp.StatusCode, method, path)
		}
		if responseBody == nil {
			return nil
		}
		if len(raw) == 0 {
			return &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "HTTP sidecar returned an empty JSON response for " + path}
		}
		if err := json.Unmarshal(raw, responseBody); err != nil {
			return &crm.ProviderError{Kind: crm.ErrProviderUnreachable, Msg: "HTTP sidecar returned invalid JSON for " + path}
		}
		return nil
	}
	return lastErr
}

func endpointURL(baseURL, path string, query url.Values) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func endpointForLog(baseURL, path string) string {
	endpoint, err := endpointURL(baseURL, path, nil)
	if err != nil {
		return path
	}
	return endpoint
}

func readLimitedAndClose(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxResponseBytes {
		return nil, errors.New("response too large")
	}
	return raw, nil
}

func isRetryableStatus(status int) bool {
	return status == stdhttp.StatusTooManyRequests || status >= 500
}

func waitBeforeRetry(ctx context.Context, retryAfter string, attempt int) error {
	delay := retryDelay(retryAfter, attempt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	retryAfter = strings.TrimSpace(retryAfter)
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
		if when, err := stdhttp.ParseTime(retryAfter); err == nil {
			if delay := time.Until(when); delay > 0 {
				return delay
			}
			return 0
		}
	}
	return retryBaseDelay * time.Duration(1<<attempt)
}

func statusError(status int, method, path string) error {
	kind := crm.ErrProviderUnknown
	switch {
	case status == stdhttp.StatusBadRequest:
		kind = crm.ErrProviderBadRequest
	case status == stdhttp.StatusUnauthorized || status == stdhttp.StatusForbidden:
		kind = crm.ErrProviderAuth
	case status == stdhttp.StatusNotFound:
		kind = crm.ErrProviderNotFound
	case status == stdhttp.StatusTooManyRequests || status >= 500:
		kind = crm.ErrProviderUnreachable
	}
	return &crm.ProviderError{
		Kind: kind,
		Msg:  fmt.Sprintf("HTTP sidecar returned status %d for %s %s", status, method, path),
	}
}

func invalidPayloadError(path, msg string) error {
	return &crm.ProviderError{
		Kind: crm.ErrProviderUnreachable,
		Msg:  "HTTP sidecar returned invalid payload for " + path + ": " + msg,
	}
}

func providerErrorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

type importRequest struct {
	Ref string `json:"ref"`
}

type syncRequest struct {
	ExternalID string `json:"external_id"`
}

type searchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type schemaResponse struct {
	Version      string   `json:"version"`
	Name         string   `json:"name,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type deepLinkResponse struct {
	URL string `json:"url"`
}

type searchResponsePayload struct {
	Hits []searchHitPayload `json:"hits"`
}

type searchHitPayload struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	Industry    string `json:"industry,omitempty"`
	Address     string `json:"address,omitempty"`
	ExternalURL string `json:"external_url,omitempty"`
}

func (p searchHitPayload) toCRM() crm.SearchHit {
	return crm.SearchHit{
		ExternalID:  p.ExternalID,
		Name:        p.Name,
		Industry:    p.Industry,
		Address:     p.Address,
		ExternalURL: p.ExternalURL,
	}
}

type customerImportPayload struct {
	Name                  string                 `json:"name"`
	ContactName           string                 `json:"contact_name,omitempty"`
	ContactEmail          string                 `json:"contact_email,omitempty"`
	Address               string                 `json:"address,omitempty"`
	Country               string                 `json:"country,omitempty"`
	Industry              string                 `json:"industry,omitempty"`
	Website               string                 `json:"website,omitempty"`
	Domain                string                 `json:"domain,omitempty"`
	VATID                 string                 `json:"vat_id,omitempty"`
	TaxID                 string                 `json:"tax_id,omitempty"`
	CompanyRegisterNumber string                 `json:"company_register_number,omitempty"`
	EmployeeCount         *int64                 `json:"employee_count,omitempty"`
	AnnualRevenueCents    *int64                 `json:"annual_revenue_cents,omitempty"`
	Description           string                 `json:"description,omitempty"`
	Phone                 string                 `json:"phone,omitempty"`
	VisitAddressStreet    string                 `json:"visit_address_street,omitempty"`
	VisitAddressZip       string                 `json:"visit_address_zip,omitempty"`
	ExternalID            string                 `json:"external_id"`
	ExternalURL           string                 `json:"external_url,omitempty"`
	Contacts              []contactImportPayload `json:"contacts,omitempty"`
}

func (p customerImportPayload) toCRM() crm.CustomerImport {
	return crm.CustomerImport{
		Name:                  p.Name,
		ContactName:           p.ContactName,
		ContactEmail:          p.ContactEmail,
		Address:               p.Address,
		Country:               p.Country,
		Industry:              p.Industry,
		Website:               p.Website,
		Domain:                p.Domain,
		VATID:                 p.VATID,
		TaxID:                 p.TaxID,
		CompanyRegisterNumber: p.CompanyRegisterNumber,
		EmployeeCount:         p.EmployeeCount,
		AnnualRevenueCents:    p.AnnualRevenueCents,
		Description:           p.Description,
		Phone:                 p.Phone,
		VisitAddressStreet:    p.VisitAddressStreet,
		VisitAddressZip:       p.VisitAddressZip,
		ExternalID:            p.ExternalID,
		ExternalURL:           p.ExternalURL,
		Contacts:              contactsToCRM(p.Contacts),
	}
}

type contactImportPayload struct {
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone,omitempty"`
	Role        string `json:"role,omitempty"`
	IsPrimary   bool   `json:"is_primary,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
	ExternalURL string `json:"external_url,omitempty"`
}

func (p contactImportPayload) toCRM() crm.ContactImport {
	return crm.ContactImport{
		Name:        p.Name,
		Email:       p.Email,
		Phone:       p.Phone,
		Role:        p.Role,
		IsPrimary:   p.IsPrimary,
		ExternalID:  p.ExternalID,
		ExternalURL: p.ExternalURL,
	}
}

func contactsToCRM(in []contactImportPayload) []crm.ContactImport {
	if in == nil {
		return nil
	}
	out := make([]crm.ContactImport, 0, len(in))
	for _, c := range in {
		out = append(out, c.toCRM())
	}
	return out
}

type partialUpdatePayload struct {
	Name                  *string                `json:"name,omitempty"`
	ContactName           *string                `json:"contact_name,omitempty"`
	ContactEmail          *string                `json:"contact_email,omitempty"`
	Address               *string                `json:"address,omitempty"`
	Country               *string                `json:"country,omitempty"`
	Industry              *string                `json:"industry,omitempty"`
	Website               *string                `json:"website,omitempty"`
	Domain                *string                `json:"domain,omitempty"`
	VATID                 *string                `json:"vat_id,omitempty"`
	TaxID                 *string                `json:"tax_id,omitempty"`
	CompanyRegisterNumber *string                `json:"company_register_number,omitempty"`
	EmployeeCount         *int64                 `json:"employee_count,omitempty"`
	AnnualRevenueCents    *int64                 `json:"annual_revenue_cents,omitempty"`
	Description           *string                `json:"description,omitempty"`
	Phone                 *string                `json:"phone,omitempty"`
	VisitAddressStreet    *string                `json:"visit_address_street,omitempty"`
	VisitAddressZip       *string                `json:"visit_address_zip,omitempty"`
	ExternalURL           *string                `json:"external_url,omitempty"`
	Contacts              []contactImportPayload `json:"contacts,omitempty"`
}

func (p partialUpdatePayload) toCRM() crm.PartialUpdate {
	return crm.PartialUpdate{
		Name:                  p.Name,
		ContactName:           p.ContactName,
		ContactEmail:          p.ContactEmail,
		Address:               p.Address,
		Country:               p.Country,
		Industry:              p.Industry,
		Website:               p.Website,
		Domain:                p.Domain,
		VATID:                 p.VATID,
		TaxID:                 p.TaxID,
		CompanyRegisterNumber: p.CompanyRegisterNumber,
		EmployeeCount:         p.EmployeeCount,
		AnnualRevenueCents:    p.AnnualRevenueCents,
		Description:           p.Description,
		Phone:                 p.Phone,
		VisitAddressStreet:    p.VisitAddressStreet,
		VisitAddressZip:       p.VisitAddressZip,
		ExternalURL:           p.ExternalURL,
		Contacts:              contactsToCRM(p.Contacts),
	}
}
