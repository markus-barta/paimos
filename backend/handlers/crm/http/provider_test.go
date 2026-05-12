// PAIMOS -- Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package httpcrm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/handlers/crm"
)

func TestProviderSignedCoreFlows(t *testing.T) {
	const secret = "shared-secret"
	var signedRequests atomic.Int32

	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := VerifyRequest(r, secret, time.Now(), raw); err != nil {
			stdhttp.Error(w, "bad signature", stdhttp.StatusUnauthorized)
			return
		}
		signedRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/schema":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":      "crm-http-v1",
				"name":         "Acme bridge",
				"capabilities": []string{"import", "sync", "search", "deep_link"},
			})
		case "/v1/import":
			var in importRequest
			if err := json.Unmarshal(raw, &in); err != nil || in.Ref != "crm://acme" {
				stdhttp.Error(w, "bad import request", stdhttp.StatusBadRequest)
				return
			}
			employeeCount := int64(42)
			_ = json.NewEncoder(w).Encode(customerImportPayload{
				Name:               "Acme GmbH",
				Industry:           "Security",
				ExternalID:         "ext-1",
				ExternalURL:        "https://crm.example/customers/ext-1",
				EmployeeCount:      &employeeCount,
				VisitAddressStreet: "Main Street 1",
				Contacts: []contactImportPayload{{
					Name:       "Ada Admin",
					Email:      "ada@example.com",
					IsPrimary:  true,
					ExternalID: "contact-1",
				}},
			})
		case "/v1/sync":
			var in syncRequest
			if err := json.Unmarshal(raw, &in); err != nil || in.ExternalID != "ext-1" {
				stdhttp.Error(w, "bad sync request", stdhttp.StatusBadRequest)
				return
			}
			name := "Acme GmbH Updated"
			externalURL := "https://crm.example/customers/ext-1"
			_ = json.NewEncoder(w).Encode(partialUpdatePayload{
				Name:        &name,
				ExternalURL: &externalURL,
				Contacts: []contactImportPayload{{
					Name:       "Ada Admin",
					Email:      "ada.new@example.com",
					ExternalID: "contact-1",
				}},
			})
		case "/v1/search":
			var in searchRequest
			if err := json.Unmarshal(raw, &in); err != nil || in.Query != "acme" || in.Limit != 7 {
				stdhttp.Error(w, "bad search request", stdhttp.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(searchResponsePayload{Hits: []searchHitPayload{{
				ExternalID:  "ext-1",
				Name:        "Acme GmbH",
				Industry:    "Security",
				ExternalURL: "https://crm.example/customers/ext-1",
			}}})
		case "/v1/deep-link":
			if r.URL.Query().Get("id") != "ext-1" {
				stdhttp.Error(w, "bad id", stdhttp.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(deepLinkResponse{URL: "https://crm.example/customers/ext-1"})
		default:
			stdhttp.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := &Provider{}
	cfg := crm.ProviderConfig{Values: map[string]string{
		"base_url":        ts.URL,
		"hmac_secret":     secret,
		"timeout_seconds": "2",
	}}

	test := p.TestConnection(context.Background(), cfg)
	if !test.OK || !strings.Contains(test.Message, "Acme bridge") {
		t.Fatalf("TestConnection: %+v", test)
	}

	imp, err := p.ImportRef(context.Background(), "crm://acme", cfg)
	if err != nil {
		t.Fatalf("ImportRef: %v", err)
	}
	if imp.Name != "Acme GmbH" || imp.ExternalID != "ext-1" || imp.EmployeeCount == nil || *imp.EmployeeCount != 42 {
		t.Fatalf("import payload: %+v", imp)
	}
	if len(imp.Contacts) != 1 || !imp.Contacts[0].IsPrimary {
		t.Fatalf("import contacts: %+v", imp.Contacts)
	}

	upd, err := p.Sync(context.Background(), "ext-1", cfg)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if upd.Name == nil || *upd.Name != "Acme GmbH Updated" {
		t.Fatalf("sync name: %+v", upd.Name)
	}
	if upd.Contacts == nil || len(upd.Contacts) != 1 || upd.Contacts[0].Email != "ada.new@example.com" {
		t.Fatalf("sync contacts: %+v", upd.Contacts)
	}

	hits, err := p.Search(context.Background(), "acme", 7, cfg)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ExternalID != "ext-1" {
		t.Fatalf("hits: %+v", hits)
	}

	if got := p.DeepLink("ext-1", cfg); got != "https://crm.example/customers/ext-1" {
		t.Fatalf("DeepLink: got %q", got)
	}
	if signedRequests.Load() != 5 {
		t.Fatalf("signed requests: got %d, want 5", signedRequests.Load())
	}
}

func TestProviderRetriesTransientStatus(t *testing.T) {
	oldDelay := retryBaseDelay
	retryBaseDelay = 0
	defer func() { retryBaseDelay = oldDelay }()

	const secret = "shared-secret"
	var attempts atomic.Int32
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := VerifyRequest(r, secret, time.Now(), raw); err != nil {
			stdhttp.Error(w, "bad signature", stdhttp.StatusUnauthorized)
			return
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			stdhttp.Error(w, "try again", stdhttp.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(customerImportPayload{
			Name:        "Retry Inc",
			ExternalID:  "retry-1",
			ExternalURL: "https://crm.example/customers/retry-1",
		})
	}))
	defer ts.Close()

	p := &Provider{}
	cfg := crm.ProviderConfig{Values: map[string]string{
		"base_url":    ts.URL,
		"hmac_secret": secret,
	}}
	imp, err := p.ImportRef(context.Background(), "retry-1", cfg)
	if err != nil {
		t.Fatalf("ImportRef: %v", err)
	}
	if imp.ExternalID != "retry-1" {
		t.Fatalf("import: %+v", imp)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts: got %d, want 2", attempts.Load())
	}
}

func TestProviderStatusMappingDoesNotLeakResponseBody(t *testing.T) {
	const secret = "shared-secret"
	var requestSignature string
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		requestSignature = r.Header.Get(HeaderSignature)
		w.WriteHeader(stdhttp.StatusUnauthorized)
		_, _ = w.Write([]byte("signature was " + requestSignature))
	}))
	defer ts.Close()

	p := &Provider{}
	cfg := crm.ProviderConfig{Values: map[string]string{
		"base_url":    ts.URL,
		"hmac_secret": secret,
	}}
	_, err := p.ImportRef(context.Background(), "leak-check", cfg)
	if err == nil {
		t.Fatalf("ImportRef succeeded")
	}
	var pe *crm.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("error type: got %T %[1]v", err)
	}
	if pe.Kind != crm.ErrProviderAuth {
		t.Fatalf("kind: got %d, want ErrProviderAuth", pe.Kind)
	}
	if requestSignature == "" {
		t.Fatalf("server did not capture request signature")
	}
	if strings.Contains(err.Error(), requestSignature) || strings.Contains(err.Error(), "signature was") {
		t.Fatalf("error leaked sidecar body or signature: %q", err.Error())
	}
}

func TestProviderTestConnectionRejectsUnsupportedSchemaVersion(t *testing.T) {
	const secret = "shared-secret"
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := VerifyRequest(r, secret, time.Now(), raw); err != nil {
			stdhttp.Error(w, "bad signature", stdhttp.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(schemaResponse{Version: "crm-http-v2"})
	}))
	defer ts.Close()

	p := &Provider{}
	res := p.TestConnection(context.Background(), crm.ProviderConfig{Values: map[string]string{
		"base_url":    ts.URL,
		"hmac_secret": secret,
	}})
	if res.OK || !strings.Contains(res.Message, "unsupported schema version") {
		t.Fatalf("TestConnection: %+v", res)
	}
}

func TestProviderRejectsInvalidSuccessPayload(t *testing.T) {
	const secret = "shared-secret"
	ts := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := VerifyRequest(r, secret, time.Now(), raw); err != nil {
			stdhttp.Error(w, "bad signature", stdhttp.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "Missing external id"})
	}))
	defer ts.Close()

	p := &Provider{}
	cfg := crm.ProviderConfig{Values: map[string]string{
		"base_url":    ts.URL,
		"hmac_secret": secret,
	}}
	_, err := p.ImportRef(context.Background(), "bad-payload", cfg)
	if err == nil {
		t.Fatalf("ImportRef accepted invalid payload")
	}
	var pe *crm.ProviderError
	if !errors.As(err, &pe) || pe.Kind != crm.ErrProviderUnreachable {
		t.Fatalf("error: got %T %[1]v", err)
	}
}
