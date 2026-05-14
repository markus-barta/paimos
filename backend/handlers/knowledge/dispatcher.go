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

package knowledge

import (
	"errors"
	"sort"
	"strings"
)

// modules is the static registry of knowledge-type Modules — one
// per discriminator value PAI-338 added to issues.type. Lookup is
// O(1) and the registry is read-only once init runs, so it's safe
// to share across goroutines without a mutex.
var modules = map[string]Module{
	memoryModuleInstance.Type():         memoryModuleInstance,
	runbookModuleInstance.Type():        runbookModuleInstance,
	externalSystemModuleInstance.Type(): externalSystemModuleInstance,
	relatedProjectModuleInstance.Type(): relatedProjectModuleInstance,
	guidelineModuleInstance.Type():      guidelineModuleInstance,
}

// RouteByType returns the Module for the given discriminator (the
// raw `issues.type` value as stored in SQL). Returns an error for
// non-knowledge types — callers should treat that as 404, not 500,
// since the caller almost certainly hit the dispatcher with bad
// input.
func RouteByType(typ string) (Module, error) {
	m, ok := modules[typ]
	if !ok {
		return nil, errors.New("unknown knowledge type: " + typ)
	}
	return m, nil
}

// URLSegmentForType maps a SQL discriminator (`external_system`)
// to the kebab-case URL segment (`external-system`) used in the
// unified `/api/projects/{id}/knowledge/{type}/{slug}` surface.
// The translation is mechanical (underscore → hyphen) so adding a
// new Module costs zero URL-routing edits — register the Module
// and the URL segment falls out for free.
func URLSegmentForType(typ string) string {
	return strings.ReplaceAll(typ, "_", "-")
}

// TypeFromURLSegment is the inverse of URLSegmentForType. Returns
// the discriminator if the segment maps to a registered type, or
// an error suitable for a 400 / 404 response.
//
// Edge cases worth noting:
//   - empty segment → error (the caller should treat that as a
//     malformed URL rather than the unified list endpoint).
//   - segments that don't roundtrip — e.g. an attacker probing
//     `external--system` — are rejected even though the
//     hyphen→underscore conversion would produce a known type.
//     We require the segment to equal URLSegmentForType(typ)
//     exactly so there's exactly one canonical form per type.
func TypeFromURLSegment(seg string) (string, error) {
	if seg == "" {
		return "", errors.New("empty type segment")
	}
	candidate := strings.ReplaceAll(seg, "-", "_")
	if _, ok := modules[candidate]; !ok {
		return "", errors.New("unknown knowledge type: " + seg)
	}
	if URLSegmentForType(candidate) != seg {
		return "", errors.New("non-canonical type segment: " + seg)
	}
	return candidate, nil
}

// AllTypes returns the sorted set of registered discriminators.
// Sorted output keeps test assertions deterministic; callers that
// need the original spec order should use a hand-rolled list.
func AllTypes() []string {
	out := make([]string, 0, len(modules))
	for k := range modules {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AllURLSegments returns the sorted set of kebab URL segments the
// unified knowledge surface accepts. Useful for router wiring,
// schema-discovery payloads, and CLI flag completion.
func AllURLSegments() []string {
	out := make([]string, 0, len(modules))
	for typ := range modules {
		out = append(out, URLSegmentForType(typ))
	}
	sort.Strings(out)
	return out
}

// reservedMemorySlugs lists the URL segments that live under
// `/knowledge/memory/...` as named subroutes (PAI-347 decay
// tracking + PAI-349 admin review surface). A memory entry whose
// slug equals one of these would be unreachable through GET — the
// literal route wins over the {slug} wildcard in chi. We reject
// them at insert/rename time so the collision is surfaced loudly
// rather than silently shadowing the data.
var reservedMemorySlugs = map[string]struct{}{
	"references": {},
	"stale":      {},
	"proposed":   {}, // shadows /memory/proposed/stale
}

// IsReservedSlug reports whether (typ, slug) is one of the
// reserved subroute names. Today only the memory type carves out
// subroutes; other types have no reservations so the check is a
// no-op for them.
func IsReservedSlug(typ, slug string) bool {
	if typ != "memory" {
		return false
	}
	_, ok := reservedMemorySlugs[slug]
	return ok
}
