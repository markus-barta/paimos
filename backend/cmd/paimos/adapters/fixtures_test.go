// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

// PAI-332 — end-to-end test against the bundled external-adapter stub
// (`fixtures/opencode/paimos-adapter-opencode`). This is the
// "non-paimos-bundled adapter" acceptance criterion proven against an
// actual on-disk adapter that paimos discovers via $PAIMOS_ADAPTER_PATH
// and dispatches through the formal PAI-332 contract.

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestExternalFixture_OpencodeStubPassesProtocol exercises the full
// discovery → wrap → conformance pipeline against the opencode stub
// at fixtures/opencode/. The stub passes manifest_sanity,
// supports_boundary_*, representative_render, and
// describe_matches_manifest — proving paimos can dispatch to a
// non-bundled adapter end-to-end.
func TestExternalFixture_OpencodeStubPassesProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("opencode fixture is a POSIX shell script")
	}
	root := filepath.Join("fixtures") // relative to package dir
	got, err := DiscoverAdapters(root, func(format string, a ...any) {
		t.Logf(format, a...)
	})
	if err != nil {
		t.Fatal(err)
	}
	var found *DiscoveredAdapter
	for i := range got {
		if got[i].Manifest.Name == "opencode" {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("opencode fixture not discovered; got %+v", got)
	}
	if found.ExecutablePath == "" {
		t.Fatal("opencode fixture has no resolved executable")
	}

	a, err := NewExternalAdapter(*found)
	if err != nil {
		t.Fatalf("wrap external adapter: %v", err)
	}

	rep := RunConformance(a, ConformanceOptions{ManifestPath: found.ManifestPath})
	if !rep.AllPassed() {
		var failed []string
		for _, c := range rep.Cases {
			if !c.Pass {
				failed = append(failed, c.Name+": "+c.Message)
			}
		}
		t.Fatalf("opencode fixture failed conformance: %v", failed)
	}
}
