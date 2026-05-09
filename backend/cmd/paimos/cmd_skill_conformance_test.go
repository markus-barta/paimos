// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

// PAI-332 — end-to-end conformance suite tests for `paimos skill
// test-adapter`. The reference claude-code adapter must pass
// conformance; this is the gating criterion for the public registry
// listing.

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSkillTestAdapter_ClaudeCodePasses is the AC requirement: the
// reference claude-code adapter must pass the full conformance suite.
// If this test ever fails the registry endpoint should not list it.
func TestSkillTestAdapter_ClaudeCodePasses(t *testing.T) {
	t.Setenv("PAIMOS_ADAPTER_PATH", "")
	stdoutS, _, err := executeCLIForTest(t, "skill", "test-adapter", "claude-code")
	if err != nil {
		t.Fatalf("conformance against claude-code should pass: %v\nstdout:\n%s", err, stdoutS)
	}
	if !strings.Contains(stdoutS, "all cases passed") {
		t.Fatalf("expected 'all cases passed' in stdout, got:\n%s", stdoutS)
	}
}

func TestSkillTestAdapter_JSONShape(t *testing.T) {
	t.Setenv("PAIMOS_ADAPTER_PATH", "")
	stdoutS, _, err := executeCLIForTest(t, "--json", "skill", "test-adapter", "claude-code")
	if err != nil {
		t.Fatalf("conformance: %v", err)
	}
	var rep struct {
		Adapter string `json:"adapter"`
		Cases   []struct {
			Name string `json:"name"`
			Pass bool   `json:"pass"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdoutS)), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdoutS)
	}
	if rep.Adapter != "claude-code" {
		t.Fatalf("adapter name: %q", rep.Adapter)
	}
	if len(rep.Cases) == 0 {
		t.Fatal("expected at least one case")
	}
	for _, c := range rep.Cases {
		if !c.Pass {
			t.Fatalf("case %q failed", c.Name)
		}
	}
}

// TestSkillTestAdapter_UnknownAdapter is a usability check — the
// error must name the missing adapter and list known ones (driven by
// the registry's own message).
func TestSkillTestAdapter_UnknownAdapter(t *testing.T) {
	t.Setenv("PAIMOS_ADAPTER_PATH", "")
	_, _, err := executeCLIForTest(t, "skill", "test-adapter", "no-such-thing")
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
	if !strings.Contains(err.Error(), "no-such-thing") {
		t.Fatalf("error should name the missing adapter: %q", err.Error())
	}
}

// TestSkillTestAdapter_OpencodeFixtureViaPATH proves the full PAI-332
// pipeline end-to-end through the user-facing CLI: an external
// adapter on $PAIMOS_ADAPTER_PATH is discovered, wrapped, and the
// conformance suite passes. Mirrors what an external adapter author
// would do to confirm their submission qualifies for registry
// listing.
func TestSkillTestAdapter_OpencodeFixtureViaPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("opencode fixture is a POSIX shell script")
	}
	fixturesDir, err := filepath.Abs(filepath.Join("adapters", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PAIMOS_ADAPTER_PATH", fixturesDir)

	stdoutS, _, err := executeCLIForTest(t, "skill", "test-adapter", "opencode")
	if err != nil {
		t.Fatalf("opencode conformance via PATH should pass: %v\nstdout:\n%s", err, stdoutS)
	}
	if !strings.Contains(stdoutS, "all cases passed") {
		t.Fatalf("expected 'all cases passed', got:\n%s", stdoutS)
	}
}
