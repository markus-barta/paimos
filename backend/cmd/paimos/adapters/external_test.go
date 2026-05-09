// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeAdapterScript is a minimal POSIX shell adapter implementing the
// PAI-332 contract. Used by the external_test fixtures so the
// ExternalAdapter wrapper exercises a real subprocess on each Render
// / Validate / Describe call.
const fakeAdapterScript = `#!/bin/sh
set -e
case "$1" in
  render)
    body="$(cat)"
    printf 'fake-render: %s\n' "$body"
    ;;
  validate)
    body="$(cat)"
    case "$body" in
      *FAIL*) echo "fake adapter rejects this input" >&2; exit 1 ;;
      *) exit 0 ;;
    esac
    ;;
  describe)
    cat <<'EOF'
{"protocol_version":"1","name":"fake","version":"0.2.0","supports":">=1.0.0 <2.0.0","description":"shell-script fake","input_format":"json","output_format":"text","target_path_template":"{workspace}/.fake/out.txt"}
EOF
    ;;
  *)
    echo "unknown verb: $1" >&2
    exit 64
    ;;
esac
`

// installFakeBinaryAdapter writes the fake shell adapter + manifest
// into a fresh subdirectory of root, returns the path.
func installFakeBinaryAdapter(t *testing.T, root string) DiscoveredAdapter {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script adapter fixture is POSIX-only")
	}
	dir := filepath.Join(root, "fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(`{
		"protocol_version": "1",
		"name": "fake",
		"version": "0.2.0",
		"supports": ">=1.0.0 <2.0.0",
		"description": "shell-script fake",
		"target_path_template": "{workspace}/.fake/out.txt",
		"input_format": "json",
		"output_format": "text"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(dir, "paimos-adapter-fake")
	if err := os.WriteFile(exePath, []byte(fakeAdapterScript), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverAdapters(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("discovery: %d, want 1", len(got))
	}
	return got[0]
}

func TestExternalAdapter_Render(t *testing.T) {
	d := installFakeBinaryAdapter(t, t.TempDir())
	a, err := NewExternalAdapter(d)
	if err != nil {
		t.Fatal(err)
	}
	canonical := []byte(`{"project":{"key":"P"},"agent":{"name":"qa"}}`)
	res, err := a.Render(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "fake-render:") {
		t.Fatalf("render output should be the fake's shape: %q", res.Content)
	}
	if res.SuggestedPath != "/.fake/out.txt" && !strings.Contains(res.SuggestedPath, "out.txt") {
		// {workspace} substituted with empty string when not passed
		// through the wrapper — that's expected; the dispatch layer
		// supplies it when known.
		t.Fatalf("suggested path: %q", res.SuggestedPath)
	}
}

func TestExternalAdapter_ValidatePassThrough(t *testing.T) {
	d := installFakeBinaryAdapter(t, t.TempDir())
	a, err := NewExternalAdapter(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Validate([]byte(`{"agent":{"name":"qa"}}`)); err != nil {
		t.Fatalf("happy path validate should pass: %v", err)
	}
	err = a.Validate([]byte(`{"agent":{"name":"FAIL"}}`))
	if err == nil {
		t.Fatal("expected validate failure")
	}
	if !strings.Contains(err.Error(), "fake adapter rejects") {
		t.Fatalf("stderr summary should be folded into the error: %q", err.Error())
	}
}

func TestExternalAdapter_DescribeMatchesManifest(t *testing.T) {
	d := installFakeBinaryAdapter(t, t.TempDir())
	a, err := NewExternalAdapter(d)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := a.DescribeJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseManifest(raw)
	if err != nil {
		t.Fatalf("describe stdout must parse as a v1 manifest: %v", err)
	}
	if got.Name != d.Manifest.Name {
		t.Fatalf("describe name=%q on-disk=%q", got.Name, d.Manifest.Name)
	}
	if got.Version != d.Manifest.Version {
		t.Fatalf("describe version=%q on-disk=%q", got.Version, d.Manifest.Version)
	}
}

// TestExternalAdapter_RegistersThroughDispatch confirms that an
// ExternalAdapter slots into the same dispatch path the in-process
// adapters use — including header injection and version-compat
// enforcement.
func TestExternalAdapter_RegistersThroughDispatch(t *testing.T) {
	d := installFakeBinaryAdapter(t, t.TempDir())
	a, err := NewExternalAdapter(d)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Register(a)
	disp := &Dispatch{Registry: reg}
	out, err := disp.Render(RenderRequest{
		Canonical:   []byte(`{"project":{"key":"P"},"agent":{"name":"qa"}}`),
		HarnessName: "fake",
		ProjectKey:  "P",
		AgentName:   "qa",
	})
	if err != nil {
		t.Fatalf("dispatch through external adapter: %v", err)
	}
	if !strings.HasPrefix(out.Body, HeaderPrefix) {
		t.Fatal("external adapter output should still get the canonical header")
	}
	if !strings.Contains(out.Body, "harness=fake") {
		t.Fatalf("header should name the external adapter: %q", out.Body)
	}
}
