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

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/contracts"
	"github.com/markus-barta/paimos/backend/handlers/knowledge"
)

// SchemaVersion is the authoritative version of the API schema payload.
// Any edit to SchemaPayload, SchemaEnums, SchemaTransitions, SchemaEntities,
// or SchemaConventions MUST bump this string. A regression test
// (schema_test.go) hashes the marshaled payload and fails when the hash
// changes — forcing the bump to happen in the same commit as the edit.
//
// The version doubles as cache key: clients refetch when the value changes.
//
// 1.6.0 (PAI-584): added the `parent` relation type — the issue-hierarchy
// edge (epic⊃ticket, ticket⊃task) and SSOT for parentage. `groups` is now
// only cost_unit/release membership; epic→ticket via groups is auto-translated
// to parent. Convention: source=parent, target=child, one parent per child.
// 1.5.0 (PAI-506): added the `agent` entity (create/update shape) and
// the rich `agent` block (entry shape + route map) documenting the
// project-scoped /api/projects/{id}/agents surface, plus the
// `agent_name` convention (lowercase slug, max 32 chars, 'web-ui'
// reserved). Makes first-class agent CRUD discoverable for CLI / MCP /
// HTTP-only agents without reading source — mirrors the PAI-394
// knowledge block.
// 1.4.0 (PAI-494): added `enum_fields`, the field-to-enum binding
// contract used by backend validators and generated client surfaces.
// Also documents lowercase canonical enum wire values, knowledge-status
// values (including proposed drafts), and Idempotency-Key conventions
// for create-style agent writes.
// 1.3.0 (PAI-394): added `knowledge` block (registered types,
// URL segments, default statuses, entry shape, route map) and
// `enums.knowledge_types`. Marks the collapse of the five
// per-type knowledge URLs into one unified `/knowledge` resource
// so agents can discover the new surface without reading source.
// 1.2.2 (PAI-393): added `enums.tag_colors`, the canonical 12-value
// tag color palette. Sourced from handlers.TagColorPalette so the
// schema and the server-side validator can't drift. Closes the
// discoverability gap for HTTP-only and MCP agents who couldn't
// learn the allowed palette without reading source.
// 1.2.1 (PAI-275): added discoverable repo/release/anchor/tag entities
// for project workspace metadata CLI consumers.
// 1.2.0 (PAI-379): added the top-level `scopes` block so agents can
// discover which api-key scopes unlock which endpoints. The scope list
// is populated at init() from auth.ScopeCatalog() — a single source of
// truth shared with the runtime check.
const SchemaVersion = "1.6.0"

// SchemaPayload is the shape returned by GET /api/schema. See PAI-87.
type SchemaPayload struct {
	Version     string                         `json:"version"`
	Enums       map[string][]string            `json:"enums"`
	Transitions map[string]map[string][]string `json:"transitions"`
	Entities    map[string]SchemaEntity        `json:"entities"`
	EnumFields  map[string]string              `json:"enum_fields"`
	Conventions map[string]string              `json:"conventions"`
	Scopes      []auth.ScopeDef                `json:"scopes"`
	Knowledge   *SchemaKnowledge               `json:"knowledge,omitempty"`
	Agent       *SchemaAgent                   `json:"agent,omitempty"`
}

// SchemaEntity describes the create/update shape for a given entity type.
// KeyShape is filled only for entities that have a user-visible key form
// (currently just "issue").
type SchemaEntity struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"`
	KeyShape string   `json:"key_shape,omitempty"`
}

// SchemaKnowledge documents the unified `/api/projects/{id}/knowledge`
// surface introduced by PAI-394. Agents reading this block know
// every knowledge type the server recognises (`Types`), what shape
// to send (`EntryShape`), and which URL each verb hits (`Routes`).
// Populated at init() from the knowledge sub-package so a new
// Module costs zero schema edits.
type SchemaKnowledge struct {
	Types      []SchemaKnowledgeType `json:"types"`
	EntryShape SchemaEntity          `json:"entry_shape"`
	Routes     map[string]string     `json:"routes"`
}

// SchemaKnowledgeType is one row of SchemaKnowledge.Types. Type is
// the SQL discriminator stored in `issues.type`; URLSegment is the
// kebab form used in URL paths (mechanical underscore → hyphen).
// Label is the human-readable name surfaced by the SPA; DefaultStatus
// is the value the server stamps when a create request omits status.
type SchemaKnowledgeType struct {
	Type          string `json:"type"`
	URLSegment    string `json:"url_segment"`
	Label         string `json:"label"`
	DefaultStatus string `json:"default_status"`
}

// SchemaAgent documents the project-scoped /api/projects/{id}/agents
// surface introduced by PAI-326 / PAI-329 and exposed as first-class
// CLI / MCP CRUD by PAI-506. Agents reading this block learn what shape
// to send (`EntryShape`) and which URL each verb hits (`Routes`).
// Unlike knowledge there's no dynamic type registry, so this is a
// static literal populated in init().
type SchemaAgent struct {
	EntryShape SchemaEntity      `json:"entry_shape"`
	Routes     map[string]string `json:"routes"`
}

// Schema is the compile-time-constant API schema. Backend enum changes
// (CHECK constraints in migrations, new relation types, new statuses)
// must be reflected here AND mirrored in frontend constants — the
// /api/schema response is what agents and the CLI trust.
var Schema = SchemaPayload{
	Version: SchemaVersion,
	Enums: map[string][]string{
		"status":           append([]string(nil), contracts.IssueStatuses...),
		"knowledge_status": append([]string(nil), contracts.KnowledgeStatuses...),
		"priority":         append([]string(nil), contracts.IssuePriorities...),
		"type":             append([]string(nil), contracts.IssueTypes...),
		"relation":         append([]string(nil), contracts.RelationTypes...),
		// tag_colors is populated in init() from handlers.TagColorPalette
		// so the schema can never drift from the server-side validator.
		"tag_colors": nil,
	},
	// Transitions are RECOMMENDED, not enforced — the backend currently
	// accepts any→any so humans can fix mistakes without a ceremony. The
	// CLI and MCP use this map to offer sensible suggestions and catch
	// typos client-side.
	Transitions: map[string]map[string][]string{
		"status": {
			"new":         {"backlog", "cancelled"},
			"backlog":     {"in-progress", "cancelled", "done"},
			"in-progress": {"qa", "done", "backlog", "cancelled"},
			"qa":          {"done", "in-progress", "backlog", "cancelled"},
			"done":        {"delivered", "in-progress", "qa", "cancelled"},
			"delivered":   {"accepted", "done"},
			"accepted":    {"invoiced", "done"},
			"invoiced":    {},
			"cancelled":   {"backlog"},
		},
	},
	Entities: map[string]SchemaEntity{
		"issue": {
			Required: []string{"title", "type"},
			Optional: []string{
				"description", "acceptance_criteria", "notes",
				"status", "priority",
				"parent_id", "assignee_id",
				"cost_unit", "release",
				"start_date", "end_date",
				"estimate_hours", "estimate_lp",
				"billing_type", "total_budget", "rate_hourly", "rate_lp",
			},
			KeyShape: "{project_key}-{issue_number}",
		},
		"project": {
			Required: []string{"name", "key"},
			Optional: []string{"description"},
		},
		"repo": {
			Required: []string{"url"},
			Optional: []string{"label", "default_branch", "sort_order"},
		},
		"release": {
			Required: []string{"label"},
			Optional: []string{},
		},
		"anchor": {
			Required: []string{"issue_key", "repo_id", "file_path", "line"},
			Optional: []string{"label", "confidence", "symbol_json", "schema_version", "repo_revision"},
		},
		"tag": {
			Required: []string{"name"},
			Optional: []string{"color", "description"},
		},
		"relation": {
			Required: []string{"target_id", "type"},
			Optional: []string{},
		},
		"comment": {
			Required: []string{"body"},
			Optional: []string{},
		},
		"agent": {
			Required: []string{"name"},
			Optional: []string{
				"description", "slash_command_name", "lane_tags",
				"metadata", "body", "bootstrap_steps", "non_negotiable_rules",
			},
		},
	},
	EnumFields: map[string]string{
		"issue.type":       "type",
		"issue.status":     "status",
		"issue.priority":   "priority",
		"relation.type":    "relation",
		"tag.color":        "tag_colors",
		"knowledge.type":   "knowledge_types",
		"knowledge.status": "knowledge_status",
	},
	Conventions: map[string]string{
		"acceptance_criteria":    "markdown checkbox list: `- [ ] ...` / `- [x] ...`",
		"enum_values":            "Enum values are canonical lowercase wire values. Display surfaces may render proper-case labels, but requests must submit the schema value.",
		"idempotency_key":        "Create-style writes may accept `Idempotency-Key: <uuid-or-ulid>`; clients should reuse the same key only when retrying the same logical request.",
		"issue_key":              "{PROJECT_KEY}-{N}, case-sensitive (e.g. PAI-83). `/issues/{id}` accepts either the numeric id or the key since v1.2.5.",
		"agent_name":             "lowercase slug ^[a-z][a-z0-9_-]*$, max 32 chars, 'web-ui' reserved",
		"multiline_inputs":       "description, acceptance_criteria and notes are markdown — prefer file inputs over shell-quoted strings (see paimos CLI).",
		"transitions_permissive": "status transitions are recommendations, not enforced; the backend accepts any→any to keep fix-by-hand flexible. Clients should surface the recommended list but allow override.",
		"relation_direction":     "GET /api/issues/{id}/relations tags each row with direction=outgoing|incoming so clients can render inverse labels (e.g. 'follows up on X' vs 'followed up by Y') without a second DB row.",
		"issue_hierarchy":        "Issue parentage (epic⊃ticket, ticket⊃task) is the `parent` relation edge (source=parent, target=child, at most one parent per child) — the single source of truth. To set a parent, either set parent_id on issue create/update OR add a type=parent relation (source=parent, target=child). The legacy parent_id column is kept in sync and still returned, but reads come from the edge. type=groups is now only cost_unit/release container membership; a type=groups relation with an epic source is auto-translated to a parent edge.",
	},
}

// schemaJSON + schemaETag are precomputed once in init(). Marshaling the
// Schema literal is deterministic (encoding/json sorts map keys) so the
// ETag is stable across requests and only changes when SchemaVersion /
// Schema contents change — which is exactly what clients cache on.
var (
	schemaJSON []byte
	schemaETag string
)

func init() {
	// PAI-379: populate the scopes block from the auth catalog so the
	// runtime check and the discoverable schema can never drift.
	Schema.Scopes = auth.ScopeCatalog()
	// PAI-393: same trick for the tag-color palette. The slice is the
	// SSoT; we copy it so a downstream mutation of Schema.Enums can't
	// reach back and clobber the validator's palette.
	Schema.Enums["tag_colors"] = append([]string{}, TagColorPalette...)
	// PAI-394: enumerate knowledge types in `enums.knowledge_types`
	// and build the rich `knowledge` block sourced from the
	// registered Modules. The block documents the unified
	// /knowledge surface so agents can discover everything they
	// need without reading the source — closes the OpenAPI gap
	// noted in PAI-394's filing.
	types := knowledge.AllTypes()
	Schema.Enums["knowledge_types"] = append([]string{}, types...)
	rows := make([]SchemaKnowledgeType, 0, len(types))
	for _, typ := range types {
		mod, err := knowledge.RouteByType(typ)
		if err != nil {
			// Should not happen — AllTypes() returns only
			// registered discriminators. Skip defensively rather
			// than panic; the binary booting is more important
			// than a single missing schema row.
			continue
		}
		rows = append(rows, SchemaKnowledgeType{
			Type:          mod.Type(),
			URLSegment:    knowledge.URLSegmentForType(mod.Type()),
			Label:         mod.Label(),
			DefaultStatus: mod.DefaultStatus(),
		})
	}
	Schema.Knowledge = &SchemaKnowledge{
		Types: rows,
		EntryShape: SchemaEntity{
			Required: []string{"type", "slug", "title"},
			Optional: []string{"body", "status", "metadata"},
		},
		Routes: map[string]string{
			"list":   "GET /api/projects/{id}/knowledge",
			"filter": "GET /api/projects/{id}/knowledge?type={url_segment}",
			"get":    "GET /api/projects/{id}/knowledge/{type}/{slug}",
			"rev":    "GET /api/projects/{id}/knowledge/{type}/{slug}.rev",
			"create": "POST /api/projects/{id}/knowledge",
			"update": "PUT /api/projects/{id}/knowledge/{type}/{slug}",
			"delete": "DELETE /api/projects/{id}/knowledge/{type}/{slug}",
		},
	}
	// PAI-506: the project-scoped agent surface. Static literal (no
	// dynamic type registry like knowledge) — documents the entry shape
	// and the route map so the single-agent read (.json artifact, peel
	// `.agent`) is discoverable. The `get` route is the .json artifact
	// since there is no plain GET /agents/{name}.
	Schema.Agent = &SchemaAgent{
		EntryShape: SchemaEntity{
			Required: []string{"name"},
			Optional: []string{
				"description", "slash_command_name", "lane_tags",
				"metadata", "body", "bootstrap_steps", "non_negotiable_rules",
			},
		},
		Routes: map[string]string{
			"list":   "GET /api/projects/{id}/agents",
			"get":    "GET /api/projects/{id}/agents/{name}.json",
			"rev":    "GET /api/projects/{id}/agents/{name}.rev",
			"create": "POST /api/projects/{id}/agents",
			"update": "PUT /api/projects/{id}/agents/{name}",
			"delete": "DELETE /api/projects/{id}/agents/{name}",
		},
	}
	b, err := json.MarshalIndent(&Schema, "", "  ")
	if err != nil {
		// Unreachable in practice — Schema is a literal with only maps,
		// strings, and slices. A failure here means the binary can't boot;
		// that's the correct response since every agent relies on this.
		panic("schema marshal: " + err.Error())
	}
	schemaJSON = append(b, '\n')
	h := sha256.Sum256(schemaJSON)
	// Weak ETag (W/"...") rather than strong. chi's Compress middleware
	// gzip-rewrites the response body, which legitimately changes bytes;
	// emitting a strong ETag in that setup is a lie and RFC-7232
	// violating. Weak is what we can truthfully assert here — the
	// payload is semantically equivalent across compressions.
	schemaETag = `W/"` + hex.EncodeToString(h[:16]) + `"`
}

// etagMatches does RFC 7232 §2.3.2 weak comparison: W/"x" and "x" and W/"x"
// are all considered equivalent. Needed because compression middleware
// can legitimately add the W/ prefix without our code's knowledge.
func etagMatches(got, want string) bool {
	return strings.TrimPrefix(got, "W/") == strings.TrimPrefix(want, "W/")
}

// GetAPISchema serves the schema payload. Public — no auth required,
// since the schema is not secret and the CLI / MCP fetch it before
// any session exists. Cacheable for 5 minutes with a strong ETag so
// agents can use If-None-Match to avoid re-downloading unchanged schemas.
//
// This handler cannot 500 by construction: the payload is a compile-time
// constant marshaled at init. If marshaling ever fails the binary refuses
// to start, so reaching runtime at all means schemaJSON is valid.
func GetAPISchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", schemaETag)
	w.Header().Set("X-Schema-Version", SchemaVersion)
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, schemaETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(schemaJSON)
}
