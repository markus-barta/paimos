// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-294: OpenAPI coverage guard.
//
// Policy: /api/openapi.json is the PAIMOS *public / stable* scriptable
// contract — a deliberately curated subset of the canonical resource
// surface. Internal one-off admin tooling (imports, dev test reports,
// branding writes, SSO/TOTP management, AI ops, …) is omitted by design
// and is NOT part of the stability contract.
//
// This guard enforces the half of that policy a machine can: every path
// the published contract claims must resolve to a real registered route,
// so the spec can never silently lie after a route is renamed or removed.
// The complementary half — "new *public* routes get documented" — is a
// review rule (see CONTRIBUTING.md), because the public/internal split is
// a human judgement, not a prefix.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/markus-barta/paimos/backend/handlers"
)

var paramSegment = regexp.MustCompile(`\{[^}]*\}`)

// normalizeAPIPath canonicalizes a path for structural comparison: chi
// regex constraints are stripped and every {param} is collapsed to {} so
// param-name differences (chi {issueID} vs spec {id}) don't matter; a
// trailing slash is trimmed.
func normalizeAPIPath(p string) string {
	p = paramSegment.ReplaceAllString(p, "{}")
	if len(p) > len("/api") && strings.HasSuffix(p, "/") {
		p = strings.TrimRight(p, "/")
	}
	return p
}

// registeredAPIPaths walks the real route tree (built via mountAPI — no DB
// or server needed, since registration never executes handlers).
func registeredAPIPaths(t *testing.T) map[string]bool {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api", mountAPI)
	paths := map[string]bool{}
	err := chi.Walk(r, func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route = normalizeAPIPath(route); strings.HasPrefix(route, "/api") {
			paths[route] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	return paths
}

// documentedAPIPaths reads the published contract through the public handler.
func documentedAPIPaths(t *testing.T) map[string]bool {
	t.Helper()
	rec := httptest.NewRecorder()
	handlers.GetOpenAPI(rec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
	var spec struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("openapi.json unmarshal: %v", err)
	}
	out := map[string]bool{}
	for p := range spec.Paths {
		out[normalizeAPIPath(p)] = true
	}
	return out
}

func TestOpenAPIContractRoutesExist(t *testing.T) {
	registered := registeredAPIPaths(t)
	documented := documentedAPIPaths(t)

	if len(documented) == 0 {
		t.Fatal("openapi.json documents no paths — the embedded contract looks empty")
	}

	var stale []string
	for p := range documented {
		if !registered[p] {
			stale = append(stale, p)
		}
	}
	sort.Strings(stale)
	for _, p := range stale {
		t.Errorf("PAI-294: openapi.json documents %q but no matching route is registered — "+
			"the published contract is stale; update backend/handlers/openapi.json", p)
	}
}
