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
)

// modules is the static registry of knowledge-type Modules — one
// per discriminator value PAI-338 added to issues.type. Lookup is
// O(1) and the registry is read-only once init runs, so it's safe
// to share across goroutines without a mutex.
var modules = map[string]Module{
	memoryModuleInstance.Type():          memoryModuleInstance,
	runbookModuleInstance.Type():         runbookModuleInstance,
	externalSystemModuleInstance.Type():  externalSystemModuleInstance,
	relatedProjectModuleInstance.Type():  relatedProjectModuleInstance,
	guidelineModuleInstance.Type():       guidelineModuleInstance,
}

// pathAliases is the convenience-endpoint URL slug → discriminator
// mapping. Each knowledge type exposes its own resource path under
// /api/projects/:id/<alias> (e.g. /memory, /runbooks). Keeping the
// alias map separate from the Module registry lets us pluralize
// the URL without polluting the SQL discriminator.
var pathAliases = map[string]string{
	"memory":            "memory",
	"runbooks":          "runbook",
	"external-systems":  "external_system",
	"related-projects":  "related_project",
	"guidelines":        "guideline",
}

// RouteByType returns the Module for the given discriminator (the
// raw `issues.type` value). Returns an error for non-knowledge
// types — callers should treat that as 404, not 500, since the
// caller almost certainly hit the dispatcher with bad input.
func RouteByType(typ string) (Module, error) {
	m, ok := modules[typ]
	if !ok {
		return nil, errors.New("unknown knowledge type: " + typ)
	}
	return m, nil
}

// RouteByPath maps a URL alias (e.g. "runbooks") to the
// corresponding Module. Used by the convenience endpoints to keep
// the path → discriminator mapping in one place.
func RouteByPath(alias string) (Module, error) {
	typ, ok := pathAliases[alias]
	if !ok {
		return nil, errors.New("unknown knowledge path: " + alias)
	}
	return RouteByType(typ)
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

// AllPathAliases returns the sorted set of URL aliases the
// convenience endpoints respond to. Helpful for router wiring and
// schema-discovery payloads.
func AllPathAliases() []string {
	out := make([]string, 0, len(pathAliases))
	for k := range pathAliases {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
