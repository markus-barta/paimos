// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// Package adapters defines the harness-adapter interface that
// `paimos skill render` (PAI-330) dispatches against, and the in-memory
// registry of built-in adapters.
//
// An adapter consumes the canonical agent artifact JSON (see
// handlers/project_agent_artifact.go) and produces a harness-specific
// skill file ({content, suggested_path}). The interface is intentionally
// tiny so out-of-tree adapters (PAI-332's SDK) can implement it without
// dragging in CLI-internal types.
//
// Version compatibility uses a Bosun-style SemVer range (`>=1.0.0 <2.0.0`)
// declared by each adapter via Supports(). The canonical artifact carries
// an optional `canonical_schema_version` field at the top level; when
// absent we default to v1.0.0 (the version PAI-329 shipped). A mismatch
// surfaces as a clear error from the dispatch layer, not a silent
// truncation, per the ticket's acceptance criteria.
package adapters

import (
	"fmt"
	"strings"
)

// RenderResult is what an adapter returns from Render(). Content is the
// final file body (the dispatcher injects the drift-detection header
// before writing). SuggestedPath is the harness's conventional location
// relative to the workspace root — the CLI uses it when --out is absent.
type RenderResult struct {
	// Content is the rendered file body, without the paimos header
	// line. The CLI dispatcher prepends the canonical drift-detection
	// header (PAI-331) before writing.
	Content string

	// SuggestedPath is the conventional target path the harness expects
	// (e.g. `.claude/commands/ops.md` for claude-code). The CLI joins
	// it under the workspace root unless --out overrides.
	SuggestedPath string
}

// Adapter is the surface a harness adapter implements. PAI-332
// formalises it as the public SDK boundary; the shape will not change
// in incompatible ways within protocol_version "1".
type Adapter interface {
	// Name is the registry key (matches --harness).
	Name() string

	// Version is the adapter's own semver string. Reported by
	// `paimos skill list-adapters` for diagnostics.
	Version() string

	// Supports declares the canonical-schema version range this adapter
	// consumes (Bosun-style: `>=1.0.0 <2.0.0`). The dispatcher rejects
	// mismatches with a clear error before calling Render.
	Supports() string

	// Render consumes the canonical artifact JSON bytes and returns the
	// rendered content + its suggested path.
	Render(canonical []byte) (RenderResult, error)

	// Describe is a one-line CLI help string.
	Describe() string
}

// ManifestProvider is an optional Adapter extension: adapters that
// expose a full v1 Manifest (PAI-332) implement this so paimos can
// list, serve, and conformance-test them uniformly. The bundled
// claude-code reference adapter implements it; external binary
// adapters expose the same shape via `paimos-adapter-<name> describe`.
type ManifestProvider interface {
	Manifest() Manifest
}

// ManifestOf returns the formal v1 manifest for an adapter, falling
// back to a synthesised one when the adapter has not opted in. The
// fallback ensures every adapter is uniformly describable from
// outside, which `list-adapters --json` and the public registry both
// depend on.
func ManifestOf(a Adapter) Manifest {
	if mp, ok := a.(ManifestProvider); ok {
		m := mp.Manifest()
		if strings.TrimSpace(m.Name) == "" {
			m.Name = a.Name()
		}
		if strings.TrimSpace(m.ProtocolVersion) == "" {
			m.ProtocolVersion = ProtocolVersion
		}
		return m
	}
	return Manifest{
		ProtocolVersion: ProtocolVersion,
		Name:            a.Name(),
		Version:         a.Version(),
		Supports:        a.Supports(),
		Description:     a.Describe(),
		InputFormat:     "json",
	}
}

// Registry is the in-memory map of adapter-name → adapter. The CLI
// constructs one at startup with the built-in claude-code adapter and
// can layer ad-hoc manifests via --harness-from-file (PAI-330).
type Registry struct {
	adapters map[string]Adapter
}

// NewRegistry returns an empty registry. Built-ins are registered by
// the CLI wiring, not the package, so tests can construct minimal
// registries without dragging in every adapter.
func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

// Register adds an adapter. Duplicate names overwrite — explicit and
// last-write-wins so --harness-from-file can shadow a built-in name
// when a user wants to test a fork.
func (r *Registry) Register(a Adapter) {
	r.adapters[a.Name()] = a
}

// Get returns the adapter for `name`, or a clear error if missing.
// The error lists known names so the user gets a useful nudge.
func (r *Registry) Get(name string) (Adapter, error) {
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown harness %q (known: %s)", name, joinNames(r.List()))
	}
	return a, nil
}

// List returns adapters sorted by name. Used by `skill list-adapters`.
func (r *Registry) List() []Adapter {
	out := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	// Stable order so list-adapters output is deterministic across runs.
	sortAdaptersByName(out)
	return out
}

func joinNames(items []Adapter) string {
	if len(items) == 0 {
		return "<none>"
	}
	out := items[0].Name()
	for _, a := range items[1:] {
		out += ", " + a.Name()
	}
	return out
}

// sortAdaptersByName sorts in place by adapter Name() ascending.
func sortAdaptersByName(items []Adapter) {
	// Insertion sort — registry is tiny (<10 adapters), avoid pulling
	// in sort just for this.
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j-1].Name() > items[j].Name() {
			items[j-1], items[j] = items[j], items[j-1]
			j--
		}
	}
}
