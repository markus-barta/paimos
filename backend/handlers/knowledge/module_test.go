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

// TestURLSegmentRoundTripsAllTypes asserts that the URL segment for
// every registered Module round-trips back to its discriminator via
// TypeFromURLSegment. PAI-394 collapsed the per-alias mapping into
// a mechanical underscore↔hyphen translation; the round-trip test
// keeps the translation honest as Modules come and go.
func TestURLSegmentRoundTripsAllTypes(t *testing.T) {
	cases := []struct {
		seg string
		typ string
	}{
		{"memory", "memory"},
		{"runbook", "runbook"},
		{"external-system", "external_system"},
		{"related-project", "related_project"},
		{"guideline", "guideline"},
	}
	for _, c := range cases {
		t.Run(c.seg, func(t *testing.T) {
			if URLSegmentForType(c.typ) != c.seg {
				t.Fatalf("URLSegmentForType(%q) = %q, want %q",
					c.typ, URLSegmentForType(c.typ), c.seg)
			}
			got, err := TypeFromURLSegment(c.seg)
			if err != nil {
				t.Fatalf("TypeFromURLSegment(%q): %v", c.seg, err)
			}
			if got != c.typ {
				t.Fatalf("TypeFromURLSegment(%q) = %q, want %q", c.seg, got, c.typ)
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

	// Rejection: legacy pluralized alias should NO LONGER match
	// after PAI-394 — `runbooks` is not a canonical segment.
	if _, err := TypeFromURLSegment("runbooks"); err == nil {
		t.Fatal("expected error from TypeFromURLSegment on legacy pluralized alias")
	}
	// Rejection: completely unknown segment.
	if _, err := TypeFromURLSegment("nonsense"); err == nil {
		t.Fatal("expected error from TypeFromURLSegment on unknown segment")
	}
	// Rejection: non-knowledge SQL type.
	if _, err := RouteByType("ticket"); err == nil {
		t.Fatal("expected error from RouteByType on a non-knowledge type")
	}
}

// TestReservedMemorySlugs guards PAI-394's reserved-slug contract:
// memory entries whose slug would shadow a /knowledge/memory/...
// subroute (references, stale, proposed) are rejected at validation
// time. Other types have no reserved slugs.
func TestReservedMemorySlugs(t *testing.T) {
	reserved := []string{"references", "stale", "proposed"}
	for _, slug := range reserved {
		if !IsReservedSlug("memory", slug) {
			t.Errorf("IsReservedSlug(\"memory\", %q) = false, want true", slug)
		}
		// Non-memory types share the namespace but aren't subject
		// to the reservation — a runbook called `references` is
		// not addressable via a literal subroute.
		if IsReservedSlug("runbook", slug) {
			t.Errorf("IsReservedSlug(\"runbook\", %q) = true, want false", slug)
		}
	}
	if IsReservedSlug("memory", "feedback_alpha") {
		t.Error("IsReservedSlug(\"memory\", \"feedback_alpha\") = true, want false")
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
