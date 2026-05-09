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

// writeManifest dumps a v1 manifest into a fresh subdir of root and
// (when execContent is non-empty) emits a sibling executable with
// the right naming convention.
func writeManifest(t *testing.T, root, adapterName, manifestJSON, execContent string) string {
	t.Helper()
	dir := filepath.Join(root, adapterName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(dir, ManifestFileName)
	if err := os.WriteFile(mp, []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if execContent != "" {
		exe := filepath.Join(dir, "paimos-adapter-"+adapterName)
		if runtime.GOOS == "windows" {
			exe += ".exe"
		}
		if err := os.WriteFile(exe, []byte(execContent), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return mp
}

// TestDiscover_FindsManifestAndExec covers the happy path: a single
// $PAIMOS_ADAPTER_PATH entry holding one adapter is found, parsed,
// and resolved to its executable.
func TestDiscover_FindsManifestAndExec(t *testing.T) {
	root := t.TempDir()
	manifest := `{
		"protocol_version": "1",
		"name": "fake",
		"version": "0.1.0",
		"supports": ">=1.0.0 <2.0.0",
		"description": "fake test adapter",
		"target_path_template": "{workspace}/.fake/{slash_command_name}.txt",
		"input_format": "json",
		"output_format": "text"
	}`
	writeManifest(t, root, "fake", manifest, "#!/bin/sh\nexit 0\n")

	got, err := DiscoverAdapters(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(got), got)
	}
	d := got[0]
	if d.Manifest.Name != "fake" {
		t.Fatalf("name: %q", d.Manifest.Name)
	}
	if d.Manifest.ProtocolVersion != "1" {
		t.Fatalf("protocol_version: %q", d.Manifest.ProtocolVersion)
	}
	if d.ExecutablePath == "" {
		t.Fatal("expected executable path to resolve")
	}
	if d.Source != AdapterPathEnv {
		t.Fatalf("source: %q", d.Source)
	}
}

// TestDiscover_SkipsBrokenManifest: a malformed manifest in one
// subdirectory must not hide siblings. The bad entry is logged, the
// good entry surfaces.
func TestDiscover_SkipsBrokenManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "good",
		`{"protocol_version":"1","name":"good","version":"1.0.0"}`,
		"#!/bin/sh\nexit 0\n")
	writeManifest(t, root, "bad",
		`{not json`, "")

	var logs []string
	got, err := DiscoverAdapters(root, func(format string, a ...any) {
		logs = append(logs, format)
	})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, d := range got {
		names = append(names, d.Manifest.Name)
	}
	if len(names) != 1 || names[0] != "good" {
		t.Fatalf("got %v, want [good]", names)
	}
	if len(logs) == 0 {
		t.Fatal("expected log line for the broken manifest")
	}
}

// TestDiscover_HonoursPathListSeparator: multi-entry walks process
// each directory in order and de-dupe absolute paths.
func TestDiscover_HonoursPathListSeparator(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	writeManifest(t, root1, "a", `{"protocol_version":"1","name":"a"}`, "")
	writeManifest(t, root2, "b", `{"protocol_version":"1","name":"b"}`, "")

	combined := root1 + string(filepath.ListSeparator) + root2
	got, err := DiscoverAdapters(combined, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	names := []string{got[0].Manifest.Name, got[1].Manifest.Name}
	if names[0] != "a" && names[0] != "b" {
		t.Fatalf("unexpected names: %v", names)
	}
}

// TestDiscover_QuietOnMissingDir: a stale entry in
// $PAIMOS_ADAPTER_PATH is expected (cf. $PATH) — it must not produce
// an error or noise.
func TestDiscover_QuietOnMissingDir(t *testing.T) {
	got, err := DiscoverAdapters("/nonexistent/path/probably", func(format string, a ...any) {
		t.Errorf("unexpected log: "+format, a...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

// TestDiscover_EmptyEnvIsNoOp: nil env, no path override → nil result,
// no error.
func TestDiscover_EmptyEnvIsNoOp(t *testing.T) {
	t.Setenv(AdapterPathEnv, "")
	got, err := DiscoverAdapters("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

// TestDiscover_RejectsFutureProtocolVersion: a manifest declaring a
// protocol_version this paimos doesn't know is skipped (and logged)
// instead of silently truncated.
func TestDiscover_RejectsFutureProtocolVersion(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "future",
		`{"protocol_version":"2","name":"future"}`, "")

	logs := []string{}
	got, err := DiscoverAdapters(root, func(format string, a ...any) {
		logs = append(logs, format)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
	joined := strings.Join(logs, "|")
	if !strings.Contains(joined, "parse") {
		t.Fatalf("expected parse-error log, got %v", logs)
	}
}

// TestNewExternalAdapter_RejectsMissingExec ensures we surface a clear
// error when discovery found a manifest without a matching executable
// (typical user mistake: forgot chmod +x).
func TestNewExternalAdapter_RejectsMissingExec(t *testing.T) {
	d := DiscoveredAdapter{
		Manifest:     Manifest{Name: "x", ProtocolVersion: "1"},
		ManifestPath: "/tmp/x/" + ManifestFileName,
	}
	_, err := NewExternalAdapter(d)
	if err == nil {
		t.Fatal("expected error for missing executable")
	}
	if !strings.Contains(err.Error(), "paimos-adapter-x") {
		t.Fatalf("error should name the expected binary: %q", err.Error())
	}
}

// TestSubstituteTargetPath: every documented token is honoured; an
// empty template returns "".
func TestSubstituteTargetPath(t *testing.T) {
	canonical := []byte(`{
		"project": {"key": "ACME", "name": "Acme"},
		"agent": {"name": "qa", "slash_command_name": "qa-slash"}
	}`)
	got, slug := SubstituteTargetPath(
		"{workspace}/.adapter/{project_key}/{agent_name}_{slash_command_name}.md",
		canonical, "/ws")
	want := "/ws/.adapter/ACME/qa_qa-slash.md"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if slug != "qa-slash" {
		t.Fatalf("slug: %q", slug)
	}

	// Slug falls back to agent.name when slash_command_name is absent.
	canonical = []byte(`{"agent":{"name":"qa"}}`)
	if _, slug := SubstituteTargetPath("{slash_command_name}", canonical, ""); slug != "qa" {
		t.Fatalf("slug fallback: got %q", slug)
	}

	// Empty template yields empty.
	if got, _ := SubstituteTargetPath("", canonical, ""); got != "" {
		t.Fatalf("empty template: got %q", got)
	}
}
