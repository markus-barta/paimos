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
)

// SchemaVersion is the authoritative version of the API schema payload.
// Any edit to SchemaPayload, SchemaEnums, SchemaTransitions, SchemaEntities,
// or SchemaConventions MUST bump this string. A regression test
// (schema_test.go) hashes the marshaled payload and fails when the hash
// changes — forcing the bump to happen in the same commit as the edit.
//
// The version doubles as cache key: clients refetch when the value changes.
const SchemaVersion = "1.0.0"

// SchemaPayload is the shape returned by GET /api/schema. See PAI-87.
type SchemaPayload struct {
	Version     string                         `json:"version"`
	Enums       map[string][]string            `json:"enums"`
	Transitions map[string]map[string][]string `json:"transitions"`
	Entities    map[string]SchemaEntity        `json:"entities"`
	Conventions map[string]string              `json:"conventions"`
}

// SchemaEntity describes the create/update shape for a given entity type.
// KeyShape is filled only for entities that have a user-visible key form
// (currently just "issue").
type SchemaEntity struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"`
	KeyShape string   `json:"key_shape,omitempty"`
}

// Schema is the compile-time-constant API schema. Backend enum changes
// (CHECK constraints in migrations, new relation types, new statuses)
// must be reflected here AND mirrored in frontend constants — the
// /api/schema response is what agents and the CLI trust.
var Schema = SchemaPayload{
	Version: SchemaVersion,
	Enums: map[string][]string{
		"status":   {"new", "backlog", "in-progress", "qa", "done", "delivered", "accepted", "invoiced", "cancelled"},
		"priority": {"low", "medium", "high"},
		"type":     {"epic", "cost_unit", "release", "sprint", "ticket", "task"},
		"relation": {"groups", "sprint", "depends_on", "impacts"},
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
		"relation": {
			Required: []string{"target_id", "type"},
			Optional: []string{},
		},
		"comment": {
			Required: []string{"body"},
			Optional: []string{},
		},
	},
	Conventions: map[string]string{
		"acceptance_criteria":   "markdown checkbox list: `- [ ] ...` / `- [x] ...`",
		"issue_key":             "{PROJECT_KEY}-{N}, case-sensitive (e.g. PAI-83). `/issues/{id}` accepts either the numeric id or the key since v1.2.5.",
		"multiline_inputs":      "description, acceptance_criteria and notes are markdown — prefer file inputs over shell-quoted strings (see paimos CLI).",
		"transitions_permissive": "status transitions are recommendations, not enforced; the backend accepts any→any to keep fix-by-hand flexible. Clients should surface the recommended list but allow override.",
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
	b, err := json.MarshalIndent(&Schema, "", "  ")
	if err != nil {
		// Unreachable in practice — Schema is a literal with only maps,
		// strings, and slices. A failure here means the binary can't boot;
		// that's the correct response since every agent relies on this.
		panic("schema marshal: " + err.Error())
	}
	schemaJSON = append(b, '\n')
	h := sha256.Sum256(schemaJSON)
	schemaETag = `"` + hex.EncodeToString(h[:16]) + `"`
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
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == schemaETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(schemaJSON)
}
