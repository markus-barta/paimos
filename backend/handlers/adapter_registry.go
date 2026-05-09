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

// PAI-332 — public adapter registry endpoint.
//
// `GET /api/registry/adapters` returns the static list of published
// adapters with their PAI-332 v1 manifests. External hosts (e.g.
// `paimos-adapter-opencode`) register themselves via PR to the
// `backend/handlers/adapter_registry.json` index — a static file in
// v1, a separate registry service in the future.
//
// The endpoint is public (no auth) so:
//   - The CLI can discover available adapters during `paimos skill
//     list-adapters --remote` (future).
//   - External tooling can browse without holding a paimos session.
//   - Adapter authors can self-link from their own README.
//
// On every request the file is parsed (cheap; <1KB today) so a
// hot-reload of the JSON during development just works without a
// process restart.

package handlers

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

//go:embed adapter_registry.json
var adapterRegistryRaw []byte

// adapterRegistryEntry is one published adapter. The manifest field
// holds a PAI-332 v1 manifest verbatim (so the on-the-wire shape is a
// strict superset of `paimos-adapter-<name> describe` output).
type adapterRegistryEntry struct {
	Manifest    json.RawMessage           `json:"manifest"`
	Homepage    string                    `json:"homepage,omitempty"`
	Source      string                    `json:"source,omitempty"`
	Maintainer  string                    `json:"maintainer,omitempty"`
	Conformance *adapterRegistryConfRecord `json:"conformance,omitempty"`
}

type adapterRegistryConfRecord struct {
	Passes       bool   `json:"passes"`
	LastVerified string `json:"last_verified,omitempty"`
	Note         string `json:"note,omitempty"`
}

// adapterRegistryFile is the on-disk shape of adapter_registry.json.
type adapterRegistryFile struct {
	ProtocolVersion string                 `json:"protocol_version"`
	GeneratedBy     string                 `json:"generated_by,omitempty"`
	Doc             string                 `json:"doc,omitempty"`
	Adapters        []adapterRegistryEntry `json:"adapters"`
}

// adapterRegistryResponse is the public API shape — the file plus a
// `count` summary (cheap convenience for CLI/script consumers) and a
// generated-at timestamp so caches downstream can de-duplicate
// identical responses without parsing.
type adapterRegistryResponse struct {
	ProtocolVersion string                 `json:"protocol_version"`
	GeneratedAt     string                 `json:"generated_at"`
	Count           int                    `json:"count"`
	Adapters        []adapterRegistryEntry `json:"adapters"`
}

// GetAdapterRegistry serves GET /api/registry/adapters. Public — no
// session required. Filters out entries whose conformance field
// reports a non-passing run (so the public registry never advertises
// known-broken adapters), but keeps stub/conformance-pending entries
// addressable via the optional ?include=pending query param so
// adapter authors can verify their submission landed.
func GetAdapterRegistry(w http.ResponseWriter, r *http.Request) {
	var file adapterRegistryFile
	if err := json.Unmarshal(adapterRegistryRaw, &file); err != nil {
		jsonError(w, "registry: parse static file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	include := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("include")))
	includePending := include == "pending" || include == "all"

	out := make([]adapterRegistryEntry, 0, len(file.Adapters))
	for _, e := range file.Adapters {
		if !includePending && (e.Conformance == nil || !e.Conformance.Passes) {
			continue
		}
		out = append(out, e)
	}
	// Stable sort by manifest.name so external consumers can rely on
	// deterministic ordering when caching by ETag.
	sort.SliceStable(out, func(i, j int) bool {
		return manifestName(out[i].Manifest) < manifestName(out[j].Manifest)
	})

	resp := adapterRegistryResponse{
		ProtocolVersion: file.ProtocolVersion,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Count:           len(out),
		Adapters:        out,
	}
	jsonOK(w, resp)
}

// manifestName pulls the `name` field out of the embedded manifest
// JSON without a full parse — used for the sort key.
func manifestName(raw json.RawMessage) string {
	var probe struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &probe)
	return probe.Name
}
