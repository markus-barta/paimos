// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package hubspot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/handlers/crm"
)

// TestValidateConfig_AcceptsBothTokenFlavours pins the PAI-258 invariant:
// the validator must NOT gate on a `pat-` prefix because HubSpot also
// issues Personal Access Keys with a different opaque format.
func TestValidateConfig_AcceptsBothTokenFlavours(t *testing.T) {
	p := &Provider{}
	cases := []struct {
		name  string
		token string
	}{
		{"private app token", "pat-na1-FAKETOKEN-NOT-REAL-USED-ONLY-IN-TESTS"},
		{"personal access key", "CiRldTEtNxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := p.ValidateConfig(map[string]string{
				"portal_id": "12345678",
				"token":     c.token,
			})
			if err != nil {
				t.Fatalf("expected nil for %s, got %v", c.name, err)
			}
		})
	}
}

// TestTestConnection_HappyPath pins the PAI-259 contract: a successful
// HubSpot smoke test returns OK=true with a portal-aware message and
// includes both request and status lines in the inline log.
func TestTestConnection_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm/v3/objects/companies" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Fatalf("expected Bearer auth, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":"123"}]}`))
	}))
	t.Cleanup(srv.Close)

	prev := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = prev })

	p := &Provider{}
	res := p.TestConnection(context.Background(), crm.ProviderConfig{Values: map[string]string{
		"token":     "valid-token-not-real-just-long-enough-xx",
		"portal_id": "12345678",
	}})
	if !res.OK {
		t.Fatalf("expected OK, got fail: %s", res.Message)
	}
	if !strings.Contains(res.Message, "12345678") {
		t.Fatalf("message should mention the portal id, got %q", res.Message)
	}
	if len(res.Lines) == 0 {
		t.Fatalf("expected log lines, got none")
	}
}

// TestTestConnection_Unauthorised verifies that a 401 from upstream
// surfaces as a clean OK=false with a message that names the failure
// mode (so admins know the token, not the network, is the problem).
func TestTestConnection_Unauthorised(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	prev := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = prev })

	p := &Provider{}
	res := p.TestConnection(context.Background(), crm.ProviderConfig{Values: map[string]string{
		"token":     "valid-token-not-real-just-long-enough-xx",
		"portal_id": "12345678",
	}})
	if res.OK {
		t.Fatalf("expected fail on 401, got OK")
	}
	if !strings.Contains(res.Message, "401") && !strings.Contains(strings.ToLower(res.Message), "rejected") {
		t.Fatalf("message should signal token rejection, got %q", res.Message)
	}
}

// TestTestConnection_EarlyExit covers the no-network paths: empty
// token / empty portal must fail fast without issuing a request.
func TestTestConnection_EarlyExit(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	prev := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = prev })

	p := &Provider{}
	cases := []struct {
		name   string
		values map[string]string
	}{
		{"empty token", map[string]string{"portal_id": "12345678"}},
		{"empty portal", map[string]string{"token": "valid-token-not-real-just-long-enough-xx"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := p.TestConnection(context.Background(), crm.ProviderConfig{Values: c.values})
			if res.OK {
				t.Fatalf("expected fail")
			}
		})
	}
	if called {
		t.Fatalf("upstream must not be hit when required fields are missing")
	}
}

func TestValidateConfig_RejectsBadInput(t *testing.T) {
	p := &Provider{}
	cases := []struct {
		name      string
		portal    string
		token     string
		errSubstr string
	}{
		{"non-numeric portal", "abc", "pat-na1-FAKETOKEN-NOT-REAL-USED-ONLY-IN-TESTS", "portal_id"},
		{"empty token", "12345678", "", "empty"},
		{"whitespace-only token", "12345678", "   ", "empty"},
		{"too-short token", "12345678", "pat-short", "too short"},
		{"contains whitespace", "12345678", "pat-na1-FAKETOKEN NOT-REAL-USED-IN-TESTS", "whitespace"},
		{"includes bearer prefix", "12345678", "Bearer pat-na1-FAKETOKEN-NOT-REAL-USED-ONLY-IN-TESTS", "Bearer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := p.ValidateConfig(map[string]string{
				"portal_id": c.portal,
				"token":     c.token,
			})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.errSubstr)
			}
			if !strings.Contains(err.Error(), c.errSubstr) {
				t.Fatalf("expected error containing %q, got %q", c.errSubstr, err.Error())
			}
		})
	}
}
