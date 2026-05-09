// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package knowledge

// PAI-338 unit tests covering the dispatcher + per-type Module
// invariants. End-to-end CRUD lives in the handlers_test
// package alongside the rest of the HTTP-shape suites.

import (
	"strings"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
		want string // "" = valid, otherwise substring of expected error
	}{
		{"valid simple", "feedback", ""},
		{"valid with underscores", "feedback_thread_dump", ""},
		{"valid with hyphens", "deploy-runbook", ""},
		{"valid digits ok", "memory42", ""},
		{"empty", "", "required"},
		{"starts with digit", "1memory", "match"},
		{"starts with uppercase", "Memory", "match"},
		{"contains space", "my memory", "match"},
		{"contains slash", "my/memory", "match"},
		{"too long", strings.Repeat("a", MaxSlugLen+1), "too long"},
		{"max length boundary ok", strings.Repeat("a", MaxSlugLen), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSlug(tc.slug)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestRouteByTypeAndPath(t *testing.T) {
	cases := []struct {
		alias string
		typ   string
	}{
		{"memory", "memory"},
		{"runbooks", "runbook"},
		{"external-systems", "external_system"},
		{"related-projects", "related_project"},
		{"guidelines", "guideline"},
	}
	for _, c := range cases {
		t.Run(c.alias, func(t *testing.T) {
			byPath, err := RouteByPath(c.alias)
			if err != nil {
				t.Fatalf("RouteByPath(%q): %v", c.alias, err)
			}
			if byPath.Type() != c.typ {
				t.Fatalf("RouteByPath(%q).Type() = %q, want %q", c.alias, byPath.Type(), c.typ)
			}
			byType, err := RouteByType(c.typ)
			if err != nil {
				t.Fatalf("RouteByType(%q): %v", c.typ, err)
			}
			if byType.Type() != c.typ {
				t.Fatalf("RouteByType(%q).Type() = %q, want %q", c.typ, byType.Type(), c.typ)
			}
		})
	}
	if _, err := RouteByPath("nonsense"); err == nil {
		t.Fatal("expected error from RouteByPath on unknown alias")
	}
	if _, err := RouteByType("ticket"); err == nil {
		t.Fatal("expected error from RouteByType on a non-knowledge type")
	}
}

func TestExternalSystemValidatesURL(t *testing.T) {
	mod, err := RouteByType("external_system")
	if err != nil {
		t.Fatalf("RouteByType: %v", err)
	}
	bad := Input{
		Slug:     "sentry",
		Title:    "Sentry",
		Metadata: map[string]any{"url": "not a url"},
	}
	if err := mod.ValidateInput(bad); err == nil {
		t.Fatal("expected validation error on bad URL")
	}
	relative := Input{
		Slug:     "sentry",
		Title:    "Sentry",
		Metadata: map[string]any{"url": "/path/only"},
	}
	if err := mod.ValidateInput(relative); err == nil {
		t.Fatal("expected validation error on relative URL")
	}
	good := Input{
		Slug:     "sentry",
		Title:    "Sentry",
		Metadata: map[string]any{"url": "https://sentry.example.com/org/project"},
	}
	if err := mod.ValidateInput(good); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	none := Input{Slug: "sentry", Title: "Sentry"}
	if err := mod.ValidateInput(none); err != nil {
		t.Fatalf("expected metadata-less input to be valid, got %v", err)
	}
}

// TestMemoryValidatesInheritFlag — PAI-348. The `inherit` flag on
// memory.metadata is optional and bool-typed. Missing / nil pass; bool
// values pass unchanged; any other type is rejected so the resolver
// doesn't have to guess at "true"/"1"/etc.
func TestMemoryValidatesInheritFlag(t *testing.T) {
	mod, err := RouteByType("memory")
	if err != nil {
		t.Fatalf("RouteByType: %v", err)
	}
	cases := []struct {
		name    string
		meta    map[string]any
		wantErr bool
	}{
		{"missing", map[string]any{}, false},
		{"true", map[string]any{"inherit": true}, false},
		{"false", map[string]any{"inherit": false}, false},
		{"string is rejected", map[string]any{"inherit": "true"}, true},
		{"number is rejected", map[string]any{"inherit": 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mod.ValidateInput(Input{
				Slug: "ok", Title: "ok", Metadata: c.meta,
			})
			if c.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	mod, err := RouteByType("memory")
	if err != nil {
		t.Fatalf("RouteByType: %v", err)
	}
	original := map[string]any{"category": "feedback", "confidence": "high"}
	raw, err := mod.MarshalMeta(original)
	if err != nil {
		t.Fatalf("MarshalMeta: %v", err)
	}
	out, err := mod.UnmarshalMeta(raw)
	if err != nil {
		t.Fatalf("UnmarshalMeta: %v", err)
	}
	if out["category"] != "feedback" || out["confidence"] != "high" {
		t.Fatalf("round-trip mismatch: %v", out)
	}
}

func TestUnmarshalEmpty(t *testing.T) {
	mod, _ := RouteByType("guideline")
	out, err := mod.UnmarshalMeta("")
	if err != nil {
		t.Fatalf("expected empty input to unmarshal cleanly, got %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %v", out)
	}
}
