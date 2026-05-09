// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

// PAI-332 — adapter conformance test suite.
//
// `paimos skill test-adapter <name>` runs every adapter (in-process or
// external) through this suite. A passing run is the criterion for an
// adapter to be listed in the public registry endpoint.
//
// Cases generated:
//
//  1. Manifest sanity — name + supports range parse; protocol_version
//     is "1"; canonical-shape MarshalJSON round-trip is stable.
//  2. supports-range boundary probes — synthesised canonical artifacts
//     at the lower-inclusive (e.g. 1.0.0), an in-range example
//     (e.g. 1.5.0), and the upper-exclusive (2.0.0). The lower bound +
//     mid-range must Render OK; the upper-exclusive must produce a
//     version-mismatch error from the dispatch layer (NOT a silent
//     render).
//  3. Render produces non-empty content for a representative artifact.
//  4. (External adapters only) `describe` JSON parses as a v1 manifest
//     and matches the on-disk manifest's name + supports range.
//  5. Optional snapshot: when an `expected_output.txt` fixture sits
//     next to the manifest, the suite asserts byte-equality against
//     the rendered output (header-stripped) for the representative
//     artifact.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SnapshotFileName is the conventional filename for the optional
// byte-equality fixture. Sits next to the manifest.
const SnapshotFileName = "expected_output.txt"

// ConformanceCase is one assertion within a conformance run. The
// suite produces a slice; each Pass=false entry is an actionable
// adapter bug.
type ConformanceCase struct {
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Message string `json:"message,omitempty"`
}

// ConformanceReport is the full output of RunConformance.
type ConformanceReport struct {
	Adapter string            `json:"adapter"`
	Cases   []ConformanceCase `json:"cases"`
}

// AllPassed returns true iff every case passed.
func (r *ConformanceReport) AllPassed() bool {
	for _, c := range r.Cases {
		if !c.Pass {
			return false
		}
	}
	return true
}

// FailureCount returns the number of failed cases.
func (r *ConformanceReport) FailureCount() int {
	n := 0
	for _, c := range r.Cases {
		if !c.Pass {
			n++
		}
	}
	return n
}

// ConformanceOptions tunes the run. ManifestPath is set when the
// adapter was discovered on disk — the suite uses the directory to
// look for the optional snapshot fixture. SnapshotOverride lets tests
// supply a snapshot path directly.
type ConformanceOptions struct {
	ManifestPath     string
	SnapshotOverride string
}

// RunConformance exercises an adapter through the standard cases.
// Returns the report; the caller decides exit-code + stdout shape
// (the CLI emits a human table; tests assert on the struct).
func RunConformance(a Adapter, opts ConformanceOptions) *ConformanceReport {
	report := &ConformanceReport{Adapter: a.Name()}

	manifest := ManifestOf(a)

	// Case 1: manifest sanity.
	report.Cases = append(report.Cases, caseManifestSanity(manifest))

	// Case 2: boundary probes against the supports range. Skipped
	// gracefully when the adapter declares no range.
	report.Cases = append(report.Cases, caseSupportsBoundary(a, manifest)...)

	// Case 3: representative-artifact render returns non-empty content.
	report.Cases = append(report.Cases, caseRepresentativeRender(a))

	// Case 4: external adapters only — `describe` shape matches
	// on-disk manifest.
	if ext, ok := a.(*ExternalAdapter); ok {
		report.Cases = append(report.Cases, caseDescribeMatchesManifest(ext))
	}

	// Case 5: optional snapshot.
	if snap := resolveSnapshotPath(opts); snap != "" {
		report.Cases = append(report.Cases, caseSnapshotMatch(a, snap))
	}

	return report
}

// caseManifestSanity verifies the manifest the adapter exposes is a
// well-formed v1 manifest.
func caseManifestSanity(m Manifest) ConformanceCase {
	c := ConformanceCase{Name: "manifest_sanity"}
	if m.ProtocolVersion != ProtocolVersion {
		c.Message = fmt.Sprintf("protocol_version=%q, want %q", m.ProtocolVersion, ProtocolVersion)
		return c
	}
	if strings.TrimSpace(m.Name) == "" {
		c.Message = "name is empty"
		return c
	}
	// Round-trip — paranoia check that MarshalJSON and ParseManifest
	// agree on the canonical shape.
	raw, err := m.MarshalJSON()
	if err != nil {
		c.Message = "marshal: " + err.Error()
		return c
	}
	if _, err := ParseManifest(raw); err != nil {
		c.Message = "manifest fails roundtrip: " + err.Error()
		return c
	}
	c.Pass = true
	return c
}

// caseSupportsBoundary issues three probes derived from the adapter's
// declared supports range. The dispatcher's own version-compat check
// is the assertion target — we want the adapter to delegate it
// faithfully to the shared CheckSupports helper.
func caseSupportsBoundary(a Adapter, m Manifest) []ConformanceCase {
	if strings.TrimSpace(m.Supports) == "" {
		return []ConformanceCase{{
			Name:    "supports_boundary",
			Pass:    true,
			Message: "skipped: adapter declares no supports range",
		}}
	}
	lo, hi, ok := parseRangeForBoundary(m.Supports)
	if !ok {
		return []ConformanceCase{{
			Name:    "supports_boundary",
			Message: fmt.Sprintf("supports range %q has no parseable lower/upper bounds", m.Supports),
		}}
	}
	mid := lo
	mid.minor++ // bump minor to land mid-range when the range is >=N <N+1.
	cases := []ConformanceCase{}

	reg := NewRegistry()
	reg.Register(a)
	disp := &Dispatch{Registry: reg}
	probe := func(label string, version string, wantOK bool) ConformanceCase {
		c := ConformanceCase{Name: "supports_boundary_" + label}
		canonical := []byte(fmt.Sprintf(
			`{"canonical_schema_version":%q,"project":{"key":"P"},"agent":{"name":"a"}}`,
			version))
		_, err := disp.Render(RenderRequest{
			Canonical:   canonical,
			HarnessName: a.Name(),
			ProjectKey:  "P",
			AgentName:   "a",
		})
		if wantOK && err != nil {
			c.Message = fmt.Sprintf("%s (%s) should render but failed: %v", label, version, err)
			return c
		}
		if !wantOK && err == nil {
			c.Message = fmt.Sprintf("%s (%s) should be rejected but rendered", label, version)
			return c
		}
		c.Pass = true
		return c
	}
	cases = append(cases, probe("lower_inclusive", lo.String(), true))
	if cmpSemver(mid, hi) < 0 {
		cases = append(cases, probe("middle", mid.String(), true))
	}
	cases = append(cases, probe("upper_exclusive", hi.String(), false))
	return cases
}

// caseRepresentativeRender confirms the adapter renders something
// non-empty for a fully-populated canonical artifact. The fixture is
// project-agnostic so unrelated adapters (opencode etc.) can succeed
// without claude-code-shaped fields.
func caseRepresentativeRender(a Adapter) ConformanceCase {
	c := ConformanceCase{Name: "representative_render"}
	canonical := []byte(`{
		"canonical_schema_version": "1.0.0",
		"project": {"id": 1, "name": "Conformance", "key": "CNF"},
		"agent": {
			"name": "conf",
			"description": "conformance probe",
			"slash_command_name": "conf",
			"body": "Conformance body.",
			"bootstrap_steps": [{"title": "Step", "command": "echo ok"}],
			"non_negotiable_rules": [{"title": "Rule", "body": "Be deterministic."}]
		},
		"repos": [],
		"environments": [],
		"deploy_recipes": []
	}`)
	res, err := a.Render(canonical)
	if err != nil {
		c.Message = "render: " + err.Error()
		return c
	}
	if strings.TrimSpace(res.Content) == "" {
		c.Message = "render returned empty content"
		return c
	}
	c.Pass = true
	return c
}

// caseDescribeMatchesManifest is external-adapter-only: the `describe`
// stdout must parse as a v1 manifest and match the on-disk one on the
// load-bearing fields.
func caseDescribeMatchesManifest(e *ExternalAdapter) ConformanceCase {
	c := ConformanceCase{Name: "describe_matches_manifest"}
	raw, err := e.DescribeJSON()
	if err != nil {
		c.Message = "describe failed: " + err.Error()
		return c
	}
	got, err := ParseManifest(raw)
	if err != nil {
		c.Message = "describe stdout did not parse as v1 manifest: " + err.Error()
		return c
	}
	on := e.Manifest()
	if got.Name != on.Name {
		c.Message = fmt.Sprintf("describe.name=%q on-disk=%q", got.Name, on.Name)
		return c
	}
	if strings.TrimSpace(on.Supports) != "" && got.Supports != on.Supports {
		c.Message = fmt.Sprintf("describe.supports=%q on-disk=%q", got.Supports, on.Supports)
		return c
	}
	c.Pass = true
	return c
}

// caseSnapshotMatch reads SnapshotFileName next to the manifest and
// asserts byte-equality against the representative-artifact render
// (header-stripped — the dispatch layer's header is non-deterministic
// because of the rev hash).
func caseSnapshotMatch(a Adapter, snapPath string) ConformanceCase {
	c := ConformanceCase{Name: "snapshot_byte_equality"}
	raw, err := os.ReadFile(snapPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.Pass = true
			c.Message = "skipped: no snapshot fixture"
			return c
		}
		c.Message = "read snapshot: " + err.Error()
		return c
	}
	canonical := []byte(`{
		"canonical_schema_version": "1.0.0",
		"project": {"id": 1, "name": "Conformance", "key": "CNF"},
		"agent": {"name": "conf", "slash_command_name": "conf", "body": "Snapshot body."}
	}`)
	res, err := a.Render(canonical)
	if err != nil {
		c.Message = "render: " + err.Error()
		return c
	}
	if string(raw) != res.Content {
		c.Message = fmt.Sprintf("rendered output != snapshot at %s", snapPath)
		return c
	}
	c.Pass = true
	return c
}

// resolveSnapshotPath picks the fixture the suite will compare
// against. Explicit override wins; otherwise look for SnapshotFileName
// next to the manifest.
func resolveSnapshotPath(opts ConformanceOptions) string {
	if opts.SnapshotOverride != "" {
		return opts.SnapshotOverride
	}
	if opts.ManifestPath == "" {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(opts.ManifestPath), SnapshotFileName)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// parseRangeForBoundary extracts the lower-inclusive + upper-exclusive
// SemVers from a canonical Bosun-style range like ">=1.0.0 <2.0.0".
// Returns false when the range doesn't have both bounds (the
// conformance suite needs both to probe boundary behaviour).
func parseRangeForBoundary(rangeExpr string) (lo, hi semver, ok bool) {
	haveLo, haveHi := false, false
	for _, clause := range strings.Fields(rangeExpr) {
		op, v, err := parseClause(clause)
		if err != nil {
			return semver{}, semver{}, false
		}
		switch op {
		case ">=":
			lo = v
			haveLo = true
		case ">":
			lo = v
			lo.patch++
			haveLo = true
		case "<":
			hi = v
			haveHi = true
		case "<=":
			hi = v
			hi.patch++
			haveHi = true
		}
	}
	return lo, hi, haveLo && haveHi
}

// String returns the M.m.p formatting of a semver. Used for probe
// versions in conformance logs.
func (s semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.major, s.minor, s.patch)
}
