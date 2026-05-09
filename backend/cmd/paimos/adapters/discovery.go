// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package adapters

// PAI-332 — adapter discovery via $PAIMOS_ADAPTER_PATH.
//
// Adapter discovery layers, in order:
//
//  1. Built-in registry (paimos ships claude-code today; PAI-333 will
//     extract). Registered explicitly by the CLI wiring.
//  2. $PAIMOS_ADAPTER_PATH directories. Each colon-separated entry is
//     walked once (one directory deep — adapters live as
//     `<dir>/<name>/paimos-adapter.json`, not nested arbitrarily). Each
//     entry's manifest registers an adapter named after the manifest's
//     `name` field, with execution proxied to the sibling executable
//     `<dir>/<name>/paimos-adapter-<name>` per the PAI-332 contract.
//  3. `--harness-from-file <manifest>` (PAI-330 escape hatch) — loads
//     a single in-process adapter from a manifest with an inline
//     template body. Out-of-tree binary adapters do NOT use this path.
//
// Path-shape rationale: matching `$PATH` semantics (colon-separated
// list, ordered, first-match-wins) keeps the user mental model
// trivial. On Windows the separator is `;` (filepath.ListSeparator);
// we use the runtime-correct value so the same binary works
// cross-platform.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AdapterPathEnv is the env-var name PAI-332 reserves for adapter
// discovery. Frozen here so external docs / shell scripts can rely
// on the constant.
const AdapterPathEnv = "PAIMOS_ADAPTER_PATH"

// DiscoveredAdapter is what the discovery walk returns: the parsed
// manifest plus the on-disk paths needed to execute it. Discovery is
// a planning step — turning a DiscoveredAdapter into a registered
// in-process Adapter is the caller's job (CLI wires it up; conformance
// suite uses the same shape for direct invocation).
type DiscoveredAdapter struct {
	// Manifest is the parsed v1 manifest (post-Validate).
	Manifest Manifest

	// ManifestPath is the absolute path to the manifest file.
	ManifestPath string

	// ExecutablePath is the absolute path to the adapter binary
	// (`paimos-adapter-<name>`). Empty when the discovery walk could
	// not locate one — manifest-only entries are still listed so the
	// user can diagnose a missing binary instead of getting a silent
	// "unknown adapter" later.
	ExecutablePath string

	// Source identifies which discovery layer found this adapter.
	// "$PAIMOS_ADAPTER_PATH" for env walks, "builtin" for in-tree
	// adapters, or "--harness-from-file" for the escape hatch.
	Source string
}

// DiscoverAdapters walks $PAIMOS_ADAPTER_PATH and returns every
// adapter manifest it can parse. Malformed manifests are logged via
// the optional logger and skipped — a single broken adapter must not
// hide its siblings.
//
// pathOverride lets callers (tests; CLI flag) bypass the env var with
// an explicit colon/semicolon-separated string. An empty
// pathOverride uses os.Getenv.
//
// The walk is shallow: one level inside each path entry, looking for
// `<entry>/<name>/paimos-adapter.json`. We don't recursively walk
// arbitrary trees — that would make adapter discovery I/O cost
// unpredictable and surface accidental matches on unrelated json
// files (e.g. node_modules).
func DiscoverAdapters(pathOverride string, logger func(format string, a ...any)) ([]DiscoveredAdapter, error) {
	if logger == nil {
		logger = func(string, ...any) {}
	}
	raw := pathOverride
	if strings.TrimSpace(raw) == "" {
		raw = os.Getenv(AdapterPathEnv)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	sep := string(filepath.ListSeparator)
	entries := strings.Split(raw, sep)

	var out []DiscoveredAdapter
	seen := map[string]struct{}{}
	for _, dir := range entries {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			logger("paimos: skip %s in %s: %v", dir, AdapterPathEnv, err)
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}

		discovered, err := discoverInDirectory(abs, logger)
		if err != nil {
			logger("paimos: walk %s: %v", abs, err)
			continue
		}
		out = append(out, discovered...)
	}
	return out, nil
}

// discoverInDirectory looks for paimos-adapter.json files one level
// deep inside dir. Each matching subdirectory contributes one
// DiscoveredAdapter.
func discoverInDirectory(dir string, logger func(format string, a ...any)) ([]DiscoveredAdapter, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Quiet skip — a stale entry in $PAIMOS_ADAPTER_PATH is
			// expected (cf. $PATH).
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}
	subdirs, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []DiscoveredAdapter
	for _, entry := range subdirs {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, entry.Name(), ManifestFileName)
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			logger("paimos: read %s: %v", manifestPath, err)
			continue
		}
		m, err := ParseManifest(raw)
		if err != nil {
			logger("paimos: parse %s: %v", manifestPath, err)
			continue
		}
		exe := lookupExecutable(filepath.Join(dir, entry.Name()), m.Name)
		out = append(out, DiscoveredAdapter{
			Manifest:       m,
			ManifestPath:   manifestPath,
			ExecutablePath: exe,
			Source:         AdapterPathEnv,
		})
	}
	return out, nil
}

// lookupExecutable returns the absolute path to
// `paimos-adapter-<name>` inside dir, or "" if no such executable is
// found. On Windows we also try the `.exe` suffix.
func lookupExecutable(dir, name string) string {
	candidates := []string{"paimos-adapter-" + name}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "paimos-adapter-"+name+".exe")
	}
	for _, c := range candidates {
		p := filepath.Join(dir, c)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		// Best-effort exec bit check on POSIX. Windows ignores the
		// mode here — `.exe` is the contract.
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			continue
		}
		return p
	}
	return ""
}
