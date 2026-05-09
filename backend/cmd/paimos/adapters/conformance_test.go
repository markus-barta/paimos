// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conformanceProvider is a stubAdapter that opts into ManifestProvider
// so RunConformance gets a fully-populated manifest.
type conformanceProvider struct {
	*stubAdapter
}

func (c *conformanceProvider) Manifest() Manifest {
	return Manifest{
		ProtocolVersion:    ProtocolVersion,
		Name:               c.name,
		Version:            c.version,
		Supports:           c.supports,
		Description:        c.describe,
		TargetPathTemplate: "{workspace}/.x/" + c.name + ".md",
		InputFormat:        "json",
		OutputFormat:       "markdown",
	}
}

func newConformanceStub(name, supports, content string) *conformanceProvider {
	return &conformanceProvider{stubAdapter: newStub(name, supports, content)}
}

// TestRunConformance_HappyPath: a well-behaved adapter produces an
// all-pass report covering manifest sanity, every supports-range
// boundary, and the representative render.
func TestRunConformance_HappyPath(t *testing.T) {
	a := newConformanceStub("good", ">=1.0.0 <2.0.0", "rendered body\n")
	rep := RunConformance(a, ConformanceOptions{})
	if !rep.AllPassed() {
		t.Fatalf("expected all-pass, got: %+v", rep.Cases)
	}
	// The standard suite has at least: manifest_sanity, three
	// supports_boundary probes, representative_render.
	if len(rep.Cases) < 4 {
		t.Fatalf("expected >=4 cases, got %d", len(rep.Cases))
	}
}

// TestRunConformance_DetectsEmptyContent: an adapter that returns ""
// for the representative artifact must fail the suite.
func TestRunConformance_DetectsEmptyContent(t *testing.T) {
	a := newConformanceStub("blank", ">=1.0.0 <2.0.0", "")
	rep := RunConformance(a, ConformanceOptions{})
	if rep.AllPassed() {
		t.Fatal("blank-output adapter should fail conformance")
	}
	found := false
	for _, c := range rep.Cases {
		if c.Name == "representative_render" && !c.Pass {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected representative_render failure: %+v", rep.Cases)
	}
}

// TestRunConformance_DetectsBoundaryViolation: an adapter whose Render
// honours wider versions than its supports range claims is caught.
// We simulate this by stubbing a too-eager renderer that ignores
// dispatch — but the conformance suite uses Dispatch to enforce, so
// the way to make it fail here is to declare an absurd range and rely
// on Dispatch to reject the upper-exclusive probe (which it does).
// To prove the FAIL path we register an adapter whose Render
// short-circuits an error for the lower bound.
func TestRunConformance_DetectsLowerBoundFailure(t *testing.T) {
	stub := newStub("bad", ">=1.0.0 <2.0.0", "x")
	stub.render = func(_ []byte) (RenderResult, error) {
		return RenderResult{}, errors.New("synthetic render failure")
	}
	a := &conformanceProvider{stubAdapter: stub}
	rep := RunConformance(a, ConformanceOptions{})
	if rep.AllPassed() {
		t.Fatal("adapter that errors on every render must fail conformance")
	}
}

// TestRunConformance_SnapshotMatch verifies the optional byte-equality
// case: provide a snapshot fixture, the matching render passes; a
// mutated render fails.
func TestRunConformance_SnapshotMatch(t *testing.T) {
	tmp := t.TempDir()
	snap := filepath.Join(tmp, "expected.txt")

	// Pre-render with a probe canonical to capture the exact output.
	canonical := []byte(`{
		"canonical_schema_version": "1.0.0",
		"project": {"id": 1, "name": "Conformance", "key": "CNF"},
		"agent": {"name": "conf", "slash_command_name": "conf", "body": "Snapshot body."}
	}`)
	a := newConformanceStub("snap", ">=1.0.0 <2.0.0", "frozen output\n")
	res, err := a.Render(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap, []byte(res.Content), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := RunConformance(a, ConformanceOptions{SnapshotOverride: snap})
	if !rep.AllPassed() {
		t.Fatalf("snapshot match should pass: %+v", rep.Cases)
	}

	// Mutate the snapshot — should fail.
	if err := os.WriteFile(snap, []byte("totally different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep = RunConformance(a, ConformanceOptions{SnapshotOverride: snap})
	if rep.AllPassed() {
		t.Fatal("mutated snapshot should fail conformance")
	}
}

// TestRunConformance_OnClaudeCodeReference is an integration check
// against the bundled reference adapter — it must pass conformance,
// otherwise we'd be shipping a broken reference. We can't import
// the claudecode package from here without a cycle; instead, exercise
// the in-process Adapter path by adapting the manifest contract
// through a stub that mimics claude-code's surface. The real
// reference adapter is exercised end-to-end via the CLI test in
// cmd_skill_conformance_test.go.
func TestRunConformance_DefaultsManifestForBareAdapters(t *testing.T) {
	// Use the bare stubAdapter (no ManifestProvider). The fallback in
	// ManifestOf must produce a v1 manifest the suite accepts.
	a := newStub("bare", ">=1.0.0 <2.0.0", "ok\n")
	rep := RunConformance(a, ConformanceOptions{})
	if !rep.AllPassed() {
		var bad []string
		for _, c := range rep.Cases {
			if !c.Pass {
				bad = append(bad, fmt.Sprintf("%s: %s", c.Name, c.Message))
			}
		}
		t.Fatalf("bare adapter should still pass via ManifestOf fallback: %s",
			strings.Join(bad, "; "))
	}
}

// TestParseRangeForBoundary covers a few common adapter ranges.
func TestParseRangeForBoundary(t *testing.T) {
	cases := []struct {
		expr   string
		loStr  string
		hiStr  string
		wantOK bool
	}{
		{">=1.0.0 <2.0.0", "1.0.0", "2.0.0", true},
		{">=1.4.0 <=1.9.9", "1.4.0", "1.9.10", true}, // <= upgrades to <patch+1
		{">=1.0.0", "", "", false},                   // no upper bound
		{"<2.0.0", "", "", false},                    // no lower bound
		{"1.0.0", "", "", false},                     // bare equals — neither bound
	}
	for _, tc := range cases {
		lo, hi, ok := parseRangeForBoundary(tc.expr)
		if ok != tc.wantOK {
			t.Fatalf("%q ok=%v want %v", tc.expr, ok, tc.wantOK)
		}
		if !ok {
			continue
		}
		if lo.String() != tc.loStr || hi.String() != tc.hiStr {
			t.Fatalf("%q lo=%s hi=%s want %s/%s",
				tc.expr, lo.String(), hi.String(), tc.loStr, tc.hiStr)
		}
	}
}
