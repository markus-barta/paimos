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

// PAI-341 — shared knowledge-plane Resource implementation.
//
// PAI-338 ships five knowledge kinds (memory, runbook, external_system,
// related_project, guideline). They share an HTTP shape (the convenience
// endpoints under /api/projects/:id/{alias}) and a render shape (a
// markdown file with the canonical paimos drift-detection header). The
// per-kind files (memory_resource.go, runbook_resource.go, …) wrap this
// generic implementation with their own Kind() / Endpoint() / cache
// directory so the registry holds one Resource per kind exactly like
// the spec PAI-331 froze.
//
// Header format mirrors adapters.BuildHeader for skills, with one
// difference: the harness slot is replaced by the literal `kind=<kind>`
// so a polling fallback can spot the kind without parsing the body. The
// rev hash is computed from the canonical JSON shape returned by the
// server, so it stays stable across re-fetches.

package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/inspr-at/paimos/backend/cmd/paimos/adapters"
)

// KnowledgeEntry is the canonical shape the server's knowledge
// convenience endpoints return. We unmarshal only the fields that
// influence the on-disk render — the API may grow new fields without
// disturbing the sync output.
type KnowledgeEntry struct {
	ID        int64          `json:"id"`
	ProjectID int64          `json:"project_id"`
	Type      string         `json:"type"`
	Slug      string         `json:"slug"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

// knowledgeResource is the per-kind plug-in shared across the five
// knowledge kinds. Each kind file constructs one of these with its
// own Kind() name + URL alias.
//
// The struct is unexported so consumers always go through the
// kind-specific constructors (NewMemoryResource, NewRunbookResource, …).
// Those constructors validate inputs and return *knowledgeResource as
// a Resource, mirroring NewSkillResource's shape.
type knowledgeResource struct {
	// kind is the registry key (e.g. "memory"). Stable lower_snake_case.
	kind string

	// urlSegment is the kebab-singular URL segment the unified
	// /knowledge surface accepts (PAI-394). Examples: "memory",
	// "runbook", "external-system", "related-project", "guideline".
	// Distinct from kind only when the discriminator contains an
	// underscore (external_system ↔ external-system).
	urlSegment string

	// cacheSubdir is the directory name under .paimos/cache/<project>/
	// where the kind's slugs land on disk. Kept on the legacy
	// plural form so existing on-disk caches don't get orphaned by
	// the URL collapse.
	cacheSubdir string
}

// Kind returns the registry key.
func (k *knowledgeResource) Kind() string { return k.kind }

// Endpoint returns the filtered-list endpoint for the kind on the
// unified /knowledge surface (PAI-394). Used by the .rev polling
// fallback's project-scoped enumeration; Sync also hits this to
// get the slug list.
func (k *knowledgeResource) Endpoint(projectID int64) string {
	return fmt.Sprintf("/api/projects/%d/knowledge?type=%s", projectID, k.urlSegment)
}

// LocalPath returns the cache target. Layout is
// `.paimos/cache/<project-key>/<kind-subdir>/<slug>.md` so:
//
//   - all knowledge kinds live under one parent (`.paimos/cache/<project>`)
//     making it trivial to scope a `.gitignore` block.
//
//   - per-kind subdirectories prevent cross-kind slug collisions (memory
//     and runbook can both have a `deploy` slug without overwriting).
//
//   - the project key is in the path so a single workspace can hold
//     caches for multiple projects without merging them.
//
// The skill resource keeps its existing `.claude/commands/<name>.md`
// path because that's what the harness expects. Knowledge entries are
// reference material the agent reads, not commands the harness
// executes, so the read-only cache layout is appropriate.
func (k *knowledgeResource) LocalPath(projectKey, slug string) string {
	pk := strings.TrimSpace(projectKey)
	if pk == "" {
		pk = "_"
	}
	return filepath.ToSlash(filepath.Join(".paimos", "cache", pk, k.cacheSubdir, slug+".md"))
}

// HeaderRev compares an in-memory rendered body against the file on
// disk. Byte-equality of the rendered body implies identical rev
// because the canonical header line and content payload are both
// rebuilt from the same KnowledgeEntry source.
func (k *knowledgeResource) HeaderRev(rendered, existing []byte) bool {
	return string(rendered) == string(existing)
}

// Sync pulls every entry of this kind for the project (or just the
// selected slug when selectName is non-empty), renders each into the
// canonical markdown shape, and writes (or skips) them.
//
// Listing uses the convenience endpoint's GET-list response. The
// per-slug GET could be issued for parity with the skill resource's
// /agents/:name.json fetch pattern, but the list endpoint already
// returns the full Output payload — round-tripping through individual
// fetches would just double the request count.
func (k *knowledgeResource) Sync(
	ctx context.Context,
	c SyncClient,
	projectID int64,
	projectKey, workspaceRoot, selectName string,
	onWritten func(SyncedItem),
) error {
	if c == nil {
		return fmt.Errorf("%s sync: nil client", k.kind)
	}
	if onWritten == nil {
		onWritten = func(SyncedItem) {}
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return fmt.Errorf("%s sync: workspace root required", k.kind)
	}

	entries, err := k.fetchEntries(c, projectID, selectName)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := k.syncOne(projectKey, workspaceRoot, entry)
		if err != nil {
			return fmt.Errorf("sync %s %q: %w", k.kind, entry.Slug, err)
		}
		onWritten(item)
	}
	return nil
}

// fetchEntries returns the entries to sync. With selectName set, fetches
// the single slug; without, lists everything. Both shapes funnel
// through KnowledgeEntry so the caller path stays identical.
func (k *knowledgeResource) fetchEntries(c SyncClient, projectID int64, selectName string) ([]KnowledgeEntry, error) {
	if name := strings.TrimSpace(selectName); name != "" {
		body, err := c.Get(fmt.Sprintf("/api/projects/%d/knowledge/%s/%s", projectID, k.urlSegment, url.PathEscape(name)))
		if err != nil {
			return nil, fmt.Errorf("fetch %s %q: %w", k.kind, name, err)
		}
		var entry KnowledgeEntry
		if err := json.Unmarshal(body, &entry); err != nil {
			return nil, fmt.Errorf("decode %s %q: %w", k.kind, name, err)
		}
		return []KnowledgeEntry{entry}, nil
	}
	body, err := c.Get(k.Endpoint(projectID))
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", k.kind, err)
	}
	var entries []KnowledgeEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("decode %s list: %w", k.kind, err)
	}
	// Drop entries with blank slugs defensively (the API never returns
	// these, but a future refactor could).
	out := entries[:0]
	for _, e := range entries {
		if strings.TrimSpace(e.Slug) != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// syncOne renders a KnowledgeEntry and writes the result if drift is
// detected. Mirrors SkillResource.syncOne so the two paths report the
// same SyncedItem shape.
func (k *knowledgeResource) syncOne(projectKey, workspaceRoot string, entry KnowledgeEntry) (SyncedItem, error) {
	rendered := k.renderEntry(projectKey, entry)
	suggested := k.LocalPath(projectKey, entry.Slug)
	target, err := joinWorkspacePath(workspaceRoot, suggested)
	if err != nil {
		return SyncedItem{}, err
	}
	action := "wrote"
	// #nosec G304 -- target is contained in workspaceRoot by joinWorkspacePath.
	if existing, readErr := os.ReadFile(target); readErr == nil && k.HeaderRev(rendered, existing) {
		action = "unchanged"
	}
	if action == "wrote" {
		if err := WriteFileAtomic(target, rendered); err != nil {
			return SyncedItem{}, err
		}
	}
	return SyncedItem{
		Kind:   k.kind,
		Name:   entry.Slug,
		Path:   target,
		Rev:    ExtractRevFromHeader(rendered),
		Action: action,
	}, nil
}

// renderEntry builds the canonical markdown body for a knowledge entry.
// Format:
//
//	<!-- paimos: rendered from <project>/<slug>@<rev> kind=<kind> -->
//
//	# <title>
//
//	- Type: <kind>
//	- Status: <status>
//	- Slug: <slug>
//	- Updated: <updated_at>
//
//	## Metadata
//
//	```json
//	{...}
//	```
//
//	## Body
//
//	<body>
//
// The body section holds the entry's free-form markdown verbatim. The
// metadata block is included even when empty so the file shape stays
// stable across re-renders.
func (k *knowledgeResource) renderEntry(projectKey string, entry KnowledgeEntry) []byte {
	rev := KnowledgeRev(entry)
	header := fmt.Sprintf("%s%s/%s@%s kind=%s -->", adapters.HeaderPrefix, projectKey, entry.Slug, rev, k.kind)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\n")
	if title := strings.TrimSpace(entry.Title); title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	b.WriteString("- Type: ")
	b.WriteString(k.kind)
	b.WriteString("\n")
	b.WriteString("- Status: ")
	b.WriteString(strings.TrimSpace(entry.Status))
	b.WriteString("\n")
	b.WriteString("- Slug: ")
	b.WriteString(entry.Slug)
	b.WriteString("\n")
	if updated := strings.TrimSpace(entry.UpdatedAt); updated != "" {
		b.WriteString("- Updated: ")
		b.WriteString(updated)
		b.WriteString("\n")
	}
	b.WriteString("\n## Metadata\n\n```json\n")
	b.WriteString(canonicaliseMetadataJSON(entry.Metadata))
	b.WriteString("\n```\n\n## Body\n\n")
	body := strings.TrimRight(entry.Body, "\n")
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// Check enumerates the kind's entries and compares each against the
// local copy. Mirrors SkillResource.Check so `paimos sync check`
// aggregates uniformly across kinds.
func (k *knowledgeResource) Check(
	ctx context.Context,
	c SyncClient,
	projectID int64,
	projectKey, workspaceRoot string,
) ([]CheckRecord, error) {
	if c == nil {
		return nil, fmt.Errorf("%s check: nil client", k.kind)
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, fmt.Errorf("%s check: workspace root required", k.kind)
	}
	entries, err := k.fetchEntries(c, projectID, "")
	if err != nil {
		return nil, err
	}
	out := make([]CheckRecord, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		rendered := k.renderEntry(projectKey, entry)
		suggested := k.LocalPath(projectKey, entry.Slug)
		target, err := joinWorkspacePath(workspaceRoot, suggested)
		if err != nil {
			return out, fmt.Errorf("check %s %q: %w", k.kind, entry.Slug, err)
		}
		rec := CheckRecord{
			Kind: k.kind,
			Name: entry.Slug,
			Path: target,
			Rev:  ExtractRevFromHeader(rendered),
		}
		// #nosec G304 -- target is contained in workspaceRoot by joinWorkspacePath.
		existing, readErr := os.ReadFile(target)
		switch {
		case readErr != nil && os.IsNotExist(readErr):
			rec.State = "missing_local"
		case readErr != nil:
			return out, fmt.Errorf("read %s: %w", target, readErr)
		case adapters.Compare(string(rendered), string(existing)) == adapters.CheckIdentical:
			rec.State = "identical"
		case adapters.Compare(string(rendered), string(existing)) == adapters.CheckHeaderMissing:
			rec.State = "header_missing"
		default:
			rec.State = "diff"
		}
		out = append(out, rec)
	}
	return out, nil
}

// KnowledgeRev returns the short canonical hash for a KnowledgeEntry.
// Stable across re-fetches as long as the underlying record hasn't
// changed; flips on any title / body / status / slug / metadata edit.
//
// The hash space matches adapters.canonicalRev (sha256 truncated to 12
// hex chars) so the rev format is consistent between skills and
// knowledge entries — readers parsing the header don't need to special-
// case kind.
//
// Public so server-side handlers (Publish<Kind>Changed, the .rev
// polling endpoint) can compute the rev without re-rendering.
func KnowledgeRev(entry KnowledgeEntry) string {
	// Marshal a stable subset rather than the whole struct so adding
	// new fields to KnowledgeEntry doesn't accidentally bump every
	// existing rev.
	probe := struct {
		Slug     string         `json:"slug"`
		Title    string         `json:"title"`
		Body     string         `json:"body"`
		Status   string         `json:"status"`
		Metadata map[string]any `json:"metadata"`
		Type     string         `json:"type"`
	}{
		Slug:     entry.Slug,
		Title:    entry.Title,
		Body:     entry.Body,
		Status:   entry.Status,
		Metadata: entry.Metadata,
		Type:     entry.Type,
	}
	body, err := json.Marshal(probe)
	if err != nil {
		// Defensive — shouldn't happen for the shape above.
		body = []byte(entry.Slug + "|" + entry.Title)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:12]
}

// canonicaliseMetadataJSON returns a sorted-key JSON encoding of the
// metadata map so the rendered file is byte-stable across map iteration
// reorderings. Empty / nil → `{}`.
func canonicaliseMetadataJSON(meta map[string]any) string {
	if len(meta) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]json.RawMessage, len(keys))
	for _, k := range keys {
		v, err := json.Marshal(meta[k])
		if err != nil {
			v = []byte(`null`)
		}
		ordered[k] = v
	}
	// Re-encode with sorted keys preserved by writing them in order.
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		kBytes, _ := json.Marshal(k)
		b.WriteString("  ")
		b.Write(kBytes)
		b.WriteString(": ")
		b.Write(ordered[k])
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}
