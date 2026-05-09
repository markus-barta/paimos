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

import "errors"

// errInheritNotBool surfaces a mis-typed `inherit` field on memory
// metadata. PAI-348 makes the flag optional — but when it is set, the
// JSON value must be a real boolean so the resolver doesn't have to
// guess what `"true"` (a string) or `1` (a number) means.
var errInheritNotBool = errors.New("metadata.inherit must be a boolean")

// memoryModule implements PAI-338's `memory` knowledge type — the
// declarative side of the knowledge plane. Memory entries hold
// rules learned from incidents, project-state facts, references,
// and user-specific notes (PAI-337's taxonomy). Validation here
// is intentionally lax: the body is markdown, free-form, and the
// taxonomy fields (e.g. memory.type, scope, applies_to_environments)
// are tracked in `category_metadata` without server-side schema
// enforcement so PAI-339's editor can iterate freely.
//
// PAI-329's `agents[].non_negotiable_rules[].memory_ref` resolves
// against this module: SELECT * FROM issues WHERE type='memory'
// AND slug=? AND project_id=?. The slug uniqueness is enforced
// by the partial UNIQUE INDEX (M96) — no extra check needed here.
//
// PAI-348 — `category_metadata.inherit` (bool) controls whether this
// memory entry is exposed to downstream projects that declare this
// project via `related_projects[]`. Default `true` (most rules ARE
// general). The flag is enforced by the bundle resolver
// (cmd_session_bundle.go) and by the inheritance pull endpoint; no
// schema change is needed because `category_metadata` is JSON-as-text.
type memoryModule struct{}

func (memoryModule) Type() string          { return "memory" }
func (memoryModule) Label() string         { return "Memory" }
func (memoryModule) DefaultStatus() string { return "backlog" }

func (memoryModule) ValidateInput(in Input) error {
	// Slug + title are checked centrally in the handler. Memory has
	// no per-type required tail fields for v1; richer constraints
	// (e.g. memory.type ∈ {feedback,project,reference,user}) ship
	// with PAI-339 once the editor surface stabilizes.
	//
	// PAI-348 — when `inherit` is present, it must be a bool. We
	// don't require it (default = inherit) so existing entries
	// round-trip unchanged.
	if raw, ok := in.Metadata["inherit"]; ok {
		if _, isBool := raw.(bool); !isBool {
			return errInheritNotBool
		}
	}
	return nil
}

func (memoryModule) MarshalMeta(meta map[string]any) (string, error) {
	return MarshalMetaDefault(meta)
}

func (memoryModule) UnmarshalMeta(raw string) (map[string]any, error) {
	return UnmarshalMetaDefault(raw)
}

var memoryModuleInstance Module = memoryModule{}
