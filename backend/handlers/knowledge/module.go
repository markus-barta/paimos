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

// Package knowledge implements PAI-338's knowledge plane. Per PAI-346
// the plane is layered on top of the existing `issues` table by
// extending the `type` enum with five new values
// (memory, runbook, external_system, related_project, guideline).
// Every entry is a first-class issue and reuses the existing history
// snapshots, comments, tags, FTS, parent-child, soft-delete and undo
// machinery for free.
//
// Each knowledge type ships as a Module: a small unit that knows
// its discriminator, the per-type validation rules, and how to
// marshal/unmarshal the optional `category_metadata` tail field for
// API responses. The dispatcher (RouteByType) picks the right
// Module given a type discriminator. Convenience endpoints in
// handlers.go funnel each of the five resource paths
// (/api/projects/:id/{memory|runbooks|external-systems|...})
// through the same code.
package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// MaxSlugLen mirrors PAI-346's spec: long enough to hold descriptive
// memory names like `feedback_thread_dump_lock_signature_match`
// without truncation.
const MaxSlugLen = 64

// slugPattern is the canonical [a-z][a-z0-9_-]* shape used for
// agent names (PAI-326) and now reused for knowledge slugs. The
// length cap is enforced separately in ValidateSlug so we can
// surface a precise error message.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ValidateSlug returns nil when slug matches the pattern and is
// within length bounds, otherwise an actionable error. Empty slug
// is rejected — knowledge entries are looked up by slug, so a NULL
// or blank slug would be unaddressable through the convenience
// endpoints. (The DB column itself is nullable to keep
// non-knowledge issues untouched; the validation only runs on
// knowledge writes.)
func ValidateSlug(slug string) error {
	if slug == "" {
		return errors.New("slug required")
	}
	if len(slug) > MaxSlugLen {
		return fmt.Errorf("slug too long (max %d chars)", MaxSlugLen)
	}
	if !slugPattern.MatchString(slug) {
		return errors.New("slug must match [a-z][a-z0-9_-]*")
	}
	return nil
}

// Input is the per-request payload a Module receives from the
// dispatcher. Title and Body map onto the issue's `title` and
// `description` columns; Slug is the addressable identifier;
// Metadata holds the per-type tail fields persisted into
// `category_metadata` as JSON-as-text.
//
// Status is optional — when blank the dispatcher applies a
// type-appropriate default (knowledge entries default to
// 'backlog', PAI-346 §"Status values"). Tags and parent_id are
// preserved so future fields can be added without breaking the
// payload contract.
type Input struct {
	Slug     string         `json:"slug"`
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Status   string         `json:"status"`
	Metadata map[string]any `json:"metadata"`
}

// Output is the canonical JSON shape the convenience endpoints
// return. It deliberately mirrors Input so round-trips are
// symmetric, with the addition of read-only fields the server
// owns (id, project_id, type, timestamps).
//
// PAI-347 — `ReferenceCount` and `LastReferencedAt` are decay-
// tracking fields surfaced for memory entries. They're maintained
// server-side (bundle include + auto-suggest) and round-trip
// through the convenience endpoints so the UI can sort + filter
// on them. Default 0 / "" for non-memory rows so the JSON shape
// stays uniform across types; the fields are still serialised
// (omitempty applies to LastReferencedAt only since 0 is a
// meaningful "never referenced" value for the counter).
type Output struct {
	ID               int64          `json:"id"`
	ProjectID        int64          `json:"project_id"`
	Type             string         `json:"type"`
	Slug             string         `json:"slug"`
	Title            string         `json:"title"`
	Body             string         `json:"body"`
	Status           string         `json:"status"`
	Metadata         map[string]any `json:"metadata"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	ReferenceCount   int64          `json:"reference_count"`
	LastReferencedAt string         `json:"last_referenced_at,omitempty"`
}

// Module is the per-type contract. Each of memory / runbook /
// external_system / related_project / guideline ships an
// implementation. The Type() method is the SQL discriminator the
// dispatcher writes into `issues.type`; ValidateInput runs on
// every POST/PUT (after the shared slug/title checks); MarshalMeta
// turns the runtime Metadata map into the on-disk JSON-as-text
// stored in `issues.category_metadata`; UnmarshalMeta is the
// inverse for read paths.
type Module interface {
	Type() string
	Label() string
	DefaultStatus() string
	ValidateInput(in Input) error
	MarshalMeta(meta map[string]any) (string, error)
	UnmarshalMeta(raw string) (map[string]any, error)
}

// MarshalMetaDefault is the standard implementation Modules without
// per-type tail fields can delegate to. nil → "{}" so an empty
// map round-trips cleanly through the API.
func MarshalMetaDefault(meta map[string]any) (string, error) {
	if meta == nil {
		meta = map[string]any{}
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalMetaDefault is the inverse. Empty / blank input → an
// empty map (never nil) so JSON serializers always emit `{}`.
func UnmarshalMetaDefault(raw string) (map[string]any, error) {
	out := map[string]any{}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
