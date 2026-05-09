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

// Package sync is the generic "pull canonical artifacts from a paimos
// instance into a local cache" engine. It powers `paimos sync init/pull/
// watch/check` (PAI-331) and is intentionally extensible: PAI-341 will
// register additional Resource implementations (memory, runbook,
// external_system, related_project, guideline) without touching this
// package.
//
// Design constraints (PAI-331 / PAI-341 cross-reference):
//
//   - Resource is the per-kind plug-in surface. Implementations declare
//     their kind name, build server endpoints, choose local cache paths,
//     and detect drift via the paimos-managed header line that PAI-330
//     freezes.
//
//   - Registry is a tiny in-memory map. The CLI registers `skill` here;
//     PAI-341 will register the knowledge-plane kinds. Lookups are by
//     kind name; List() returns a stable-sorted snapshot for pretty
//     output.
//
//   - All sync verbs reuse the same SyncClient interface. Tests pass a
//     fake; production wires the paimos CLI's HTTP client.
//
//   - Drift detection reuses adapters.HasHeader / adapters.BuildHeader
//     so the wire format stays identical to PAI-330's render output.
package sync

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// SyncClient is the minimal HTTP surface the sync verbs need. Implemented
// by the paimos CLI's *Client in production; tests pass a fake. Keeping
// the interface tiny avoids dragging the CLI's auth/config concerns into
// this package.
type SyncClient interface {
	// Get performs an authenticated GET against `path` (server-relative,
	// starts with "/api/...") and returns the response body. Non-2xx
	// responses must surface as an error. Implementations are responsible
	// for headers, auth, and timeouts.
	Get(path string) ([]byte, error)

	// Stream opens an SSE-style long-poll connection to `path` and
	// dispatches each event to onEvent. The implementation owns the
	// connection lifecycle and returns when ctx is done or the server
	// closes the stream. The path includes the device-id query param so
	// the server can scope the subscription per-device.
	Stream(ctx context.Context, path string, onEvent func(Event)) error
}

// Event is the SSE envelope dispatched by Stream. The shape is shared
// across kinds so PAI-341 can extend by registering new Type values
// without changing this package. Per the ticket, today's only Type is
// "agent_changed"; PAI-341 adds "memory_changed", "runbook_changed",
// "external_system_changed", "related_project_changed",
// "guideline_changed".
//
// `Kind` is the registered Resource.Kind() the event affects, derived
// from Type by the dispatch helper EventKind below. `Name` is the slug
// of the affected artifact (agent name, memory slug, …). `Rev` is the
// short content hash matching adapters.canonicalRev. ProjectID is set
// to the project the event was scoped to.
type Event struct {
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	Rev       string `json:"rev,omitempty"`
	ProjectID int64  `json:"project_id,omitempty"`
}

// EventKind maps an event Type ("agent_changed", "memory_changed", …)
// to the registered Resource.Kind() value ("skill", "memory", …). The
// convention is "<kind>_changed" with one exception: agents map to the
// "skill" resource (the rendered output kind) — same naming PAI-330
// shipped (`/api/projects/:id/agents/...`) so the server endpoint name
// stays stable while the on-disk artifact is called a "skill".
//
// PAI-341 implementations register kinds whose event Type follows the
// "<kind>_changed" rule (memory → memory_changed, runbook → runbook_changed,
// …) so EventKind handles them generically; only the skill/agent
// asymmetry is special-cased here.
func EventKind(eventType string) string {
	t := strings.TrimSpace(eventType)
	if t == "" {
		return ""
	}
	if t == "agent_changed" {
		return "skill"
	}
	if strings.HasSuffix(t, "_changed") {
		return strings.TrimSuffix(t, "_changed")
	}
	return ""
}

// Resource is the per-kind plug-in surface. PAI-331 ships the skill
// implementation; PAI-341 will register knowledge-plane kinds.
//
// All methods are pure / side-effect-free except Sync, which owns the
// fetch + write. Splitting Sync out (rather than baking it into the
// engine) lets a Resource make multiple HTTP calls (e.g. list-then-fetch
// for kinds that have no single canonical endpoint per slug) without
// the engine knowing the wire shape.
type Resource interface {
	// Kind is the registry key (e.g. "skill", "memory"). MUST be a
	// stable, lower_snake_case identifier — used in CLI flags
	// (`--kind=memory`), event types ("<kind>_changed"), and on-disk
	// cache paths.
	Kind() string

	// Endpoint returns the server-relative URL the engine fetches the
	// canonical artifact list for `projectID` from. Used by the .rev
	// polling fallback and by Sync implementations that consume a list.
	// May return "" if the kind has no list endpoint (sync-by-watch only).
	Endpoint(projectID int64) string

	// LocalPath returns the on-disk cache target for a single artifact
	// named `name` under the given project key. Engine joins with the
	// workspace root before writing. Slash-separated; resolve via
	// filepath.Join at the call site.
	LocalPath(projectKey, name string) string

	// HeaderRev compares an in-memory rendered body against an existing
	// local file body and reports whether they refer to the same rev.
	// Returns true when in sync (no rewrite needed). Reuses
	// adapters.HasHeader for the present-but-not-paimos-managed case.
	HeaderRev(rendered, existing []byte) bool

	// Sync fetches the canonical artifact(s) for `projectID` and applies
	// them under `workspaceRoot`. The Resource owns rendering — for the
	// skill kind this means dispatching through PAI-330's adapter
	// registry. Implementations report each artifact through onWritten
	// (path, rev, kind) so the engine can drive a uniform CLI summary.
	//
	// `selectName`, when non-empty, restricts Sync to a single artifact
	// (used by `paimos sync pull --kind=skill --name=ops`).
	Sync(ctx context.Context, c SyncClient, projectID int64, projectKey, workspaceRoot, selectName string, onWritten func(SyncedItem)) error

	// Check verifies the local cache for `projectID` against canonical.
	// Returns the per-artifact CheckRecord slice; the engine's `paimos
	// sync check` aggregates across all registered Resources.
	Check(ctx context.Context, c SyncClient, projectID int64, projectKey, workspaceRoot string) ([]CheckRecord, error)
}

// SyncedItem is what Resource.Sync reports for each artifact it wrote.
// `Action` is one of "wrote", "unchanged", or "skipped" so the CLI can
// emit a precise summary line.
type SyncedItem struct {
	Kind   string
	Name   string
	Path   string
	Rev    string
	Action string
}

// CheckRecord is the per-artifact result Check returns. State mirrors
// adapters.CheckResult but is duplicated here as strings so the CLI can
// emit JSON without importing adapters.
type CheckRecord struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"` // "identical" | "diff" | "header_missing" | "missing_local"
	Rev   string `json:"rev,omitempty"`
}

// Registry holds the registered Resource implementations.
type Registry struct {
	// resources is keyed by Resource.Kind().
	resources map[string]Resource
}

// NewRegistry returns an empty registry. PAI-331 registers skill at
// startup; PAI-341 will layer on its kinds via the same Register call.
func NewRegistry() *Registry {
	return &Registry{resources: map[string]Resource{}}
}

// Register adds a Resource. Duplicate Kind() overrides — explicit and
// last-write-wins so out-of-tree extensions can shadow built-ins (we
// don't expect this in practice but the policy mirrors adapters.Registry
// for symmetry).
func (r *Registry) Register(res Resource) {
	r.resources[res.Kind()] = res
}

// Lookup returns the Resource for `kind`. Returns a clear error
// containing the list of known kinds when the lookup misses, so the user
// gets an actionable nudge.
func (r *Registry) Lookup(kind string) (Resource, error) {
	res, ok := r.resources[kind]
	if !ok {
		known := r.kindsList()
		if len(known) == 0 {
			return nil, fmt.Errorf("unknown sync kind %q (no kinds registered)", kind)
		}
		return nil, fmt.Errorf("unknown sync kind %q (known: %s)", kind, strings.Join(known, ", "))
	}
	return res, nil
}

// List returns all registered Resources sorted by Kind() ascending.
// Used by the CLI's `paimos sync init` (which iterates every kind) and
// by the JSON shape of `paimos sync check`.
func (r *Registry) List() []Resource {
	out := make([]Resource, 0, len(r.resources))
	for _, res := range r.resources {
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind() < out[j].Kind() })
	return out
}

// Kinds returns the kind names sorted ascending. Convenience for the
// CLI's `--kind` autocompletion / help text.
func (r *Registry) Kinds() []string {
	return r.kindsList()
}

func (r *Registry) kindsList() []string {
	out := make([]string, 0, len(r.resources))
	for k := range r.resources {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EventEndpoint builds the SSE subscribe URL for `projectID`, embedding
// the local device-id and an optional kind filter as query params.
// Centralised here so the wire shape stays consistent across the CLI
// (subscriber) and the server handler (publisher).
func EventEndpoint(projectID int64, deviceID string, kindFilter string) string {
	q := url.Values{}
	if strings.TrimSpace(deviceID) != "" {
		q.Set("device_id", deviceID)
	}
	if strings.TrimSpace(kindFilter) != "" {
		q.Set("kind", kindFilter)
	}
	path := fmt.Sprintf("/api/projects/%d/agents/events", projectID)
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path
}

// RevEndpoint builds the cheap-poll URL the polling fallback hits when
// SSE is unavailable. Mirrors the path shape PAI-330 froze for the
// canonical artifact (`/api/projects/{id}/agents/{name}.json`) so users
// reading Cloudflare access logs see consistent prefixes.
func RevEndpoint(projectID int64, agentName string) string {
	return fmt.Sprintf("/api/projects/%d/agents/%s.rev", projectID, url.PathEscape(strings.TrimSpace(agentName)))
}
