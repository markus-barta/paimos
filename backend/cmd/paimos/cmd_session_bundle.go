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

// PAI-340 — `paimos session start --bundle full` adds a context bundle
// on top of the PAI-327 MVP. The bundle resolves six categories of
// project context client-side (no new backend endpoints) and packages
// them as either eval-friendly env vars + a cache directory (`env`),
// a single JSON document on stdout (`json`), or a directory tree of
// markdown files (`files`).
//
// Filter logic is intentionally conservative — knowledge entries live
// in `category_metadata` as free-form JSON, and the editor (PAI-339)
// does not yet write every taxonomy field. Missing / partial metadata
// must not silently hide entries: the rule of thumb is "include unless
// metadata explicitly excludes the current agent / user / environment".
// That keeps the bundle useful while PAI-339 fills in the schema.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// bundleMode enumerates the user-facing values of `--bundle`. `minimal`
// is an explicit alias for the no-bundle PAI-327 behaviour; `full`
// triggers the six-category fetch + filter pipeline.
type bundleMode string

const (
	bundleModeNone    bundleMode = ""
	bundleModeMinimal bundleMode = "minimal"
	bundleModeFull    bundleMode = "full"
)

// resolveBundleMode validates the `--bundle` flag value. Empty / unset
// returns `bundleModeNone` so the caller can keep PAI-327 behaviour
// without checking explicitly. Any unknown value is a usageError so
// typos surface fast (no silent regression to the no-bundle path).
func resolveBundleMode(raw string) (bundleMode, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	switch bundleMode(trimmed) {
	case bundleModeNone:
		return bundleModeNone, nil
	case bundleModeMinimal:
		return bundleModeMinimal, nil
	case bundleModeFull:
		return bundleModeFull, nil
	default:
		return "", &usageError{
			msg: fmt.Sprintf("invalid --bundle %q (expected minimal or full)", raw),
		}
	}
}

// knowledgeEntry mirrors the on-the-wire shape of the convenience
// endpoints (PAI-338 / PAI-353). The CLI only consumes the fields
// it needs — extra fields the server may add later round-trip
// transparently because we re-encode using a generic map for the
// `files` and `json` outputs.
//
// PAI-345 — the CLI annotates each entry with a `scope` field
// ("project"|"user"|"instance") so downstream agents can
// disambiguate cross-scope merges. The server itself doesn't emit
// this field; the bundle resolver fills it in based on which
// endpoint the entry came from.
type knowledgeEntry struct {
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
	// PAI-345: bundle-resolver-assigned scope discriminator. Empty
	// when the entry pre-dates the cross-scope changes (treated as
	// "project" for backwards-compat).
	Scope string `json:"scope,omitempty"`
}

// bundlePayload is the canonical in-memory shape the resolver builds.
// `Agent` is the canonical artifact JSON (PAI-329) — kept as
// json.RawMessage so we round-trip every field the server returns
// without coupling the CLI to PAI-329's schema. Each knowledge slice
// is post-filter (i.e. exactly the entries the bundle exposes to the
// agent).
type bundlePayload struct {
	Project         projectSummary    `json:"project"`
	Agent           json.RawMessage   `json:"agent"`
	Memory          []knowledgeEntry  `json:"memory"`
	Runbooks        []knowledgeEntry  `json:"runbooks"`
	ExternalSystems []knowledgeEntry  `json:"external_systems"`
	RelatedProjects []knowledgeEntry  `json:"related_projects"`
	Guidelines      []knowledgeEntry  `json:"guidelines"`
	FetchedAt       string            `json:"fetched_at"`
}

// archivedStatus is the on-disk status value the knowledge plane uses
// for the "archived" toggle (matches the frontend's
// projectKnowledge.ts ARCHIVED_STATUS constant). Any other status
// counts as live for v1; that's the same rule the UI applies.
const archivedStatus = "cancelled"

// archivedStatus check kept loose: a missing status is treated as
// live so a server that elides empty fields never produces an empty
// bundle by accident.
func isLiveEntry(e knowledgeEntry) bool {
	return strings.TrimSpace(e.Status) != archivedStatus
}

// agentMetadata is the subset of the canonical artifact's `agent`
// block the filter logic needs. We probe with a tolerant struct so
// the filter survives PAI-329 schema additions.
type agentMetadata struct {
	Name         string         `json:"name"`
	Metadata     map[string]any `json:"metadata"`
}

// agentEnvironments returns the (best-effort) list of environment
// names the agent targets. Reads `metadata.environments` first
// (string array — the tightest match) and falls back to
// `metadata.environment` (single string). Empty list = "agent
// targets every environment".
func agentEnvironments(meta map[string]any) []string {
	if meta == nil {
		return nil
	}
	if raw, ok := meta["environments"]; ok {
		if list, ok := stringSliceFromAny(raw); ok {
			return list
		}
	}
	if raw, ok := meta["environment"]; ok {
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			return []string{strings.TrimSpace(s)}
		}
	}
	return nil
}

// stringSliceFromAny accepts `[]string`, `[]any` of strings, or a
// single string and normalises to a trimmed []string. Non-string
// elements are dropped so a malformed mix doesn't crash filtering.
func stringSliceFromAny(raw any) ([]string, bool) {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out, true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, true
		}
		return []string{s}, true
	}
	return nil, false
}

// containsString reports whether needle (trimmed, case-sensitive) is
// in haystack. Knowledge slugs and agent names share the
// [a-z][a-z0-9_-]* shape (PAI-326 / PAI-346) so case-sensitive match
// is intentional.
func containsString(haystack []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, h := range haystack {
		if strings.TrimSpace(h) == needle {
			return true
		}
	}
	return false
}

// hasAnyOverlap reports whether `a` and `b` share at least one
// trimmed-equal element. Used for the
// memory.applies_to_environments × agent.environments check.
func hasAnyOverlap(a, b []string) bool {
	for _, x := range a {
		if containsString(b, x) {
			return true
		}
	}
	return false
}

// filterMemory applies PAI-340's memory rules:
//
//  1. Drop archived entries.
//  2. Keep entries with no `scope` or scope == "project".
//  3. Keep entries with scope == "user-on-this-project" only when the
//     entry's owning user matches the current user. The metadata
//     field (`user_id` / `user`) is not yet wired by PAI-339, so a
//     missing field falls back to "include" — that keeps user-scoped
//     entries visible to whoever wrote them locally until the editor
//     starts persisting authorship.
//  4. When the entry declares `applies_to_environments` AND the agent
//     declares one or more environments, require at least one overlap.
//     Either side empty / unset = no filtering.
//  5. PAI-347: drop entries with confidence == "low" unless `includeLow`
//     is set. Missing / unknown confidence is treated as "medium" per
//     the ticket's backwards-compat rule, so existing memory
//     (no explicit confidence) continues to flow through.
func filterMemory(entries []knowledgeEntry, currentUserID int64, agentEnvs []string, includeLow bool) []knowledgeEntry {
	out := make([]knowledgeEntry, 0, len(entries))
	for _, e := range entries {
		if !isLiveEntry(e) {
			continue
		}
		scope := strings.TrimSpace(stringFromMeta(e.Metadata, "scope"))
		if scope == "user-on-this-project" {
			if !memoryUserMatches(e.Metadata, currentUserID) {
				continue
			}
		}
		// "project" or empty: always pass the scope gate.

		entryEnvs, _ := stringSliceFromAny(e.Metadata["applies_to_environments"])
		if len(entryEnvs) > 0 && len(agentEnvs) > 0 {
			if !hasAnyOverlap(entryEnvs, agentEnvs) {
				continue
			}
		}

		// PAI-347 — confidence gate. Default exclusion of `low`
		// memories keeps the bundle compact for fresh sessions; the
		// caller flips `--include-low` when debugging or onboarding
		// a project where every working hypothesis matters.
		if !includeLow {
			conf := memoryConfidenceFrom(e.Metadata)
			if conf == "low" {
				continue
			}
		}

		out = append(out, e)
	}
	return out
}

// memoryConfidenceFrom mirrors the backend's confidenceFromMeta —
// missing / unknown values fall back to "medium" so the default
// bundle pipeline includes existing pre-PAI-347 memory entries
// (which have no explicit confidence) by default.
func memoryConfidenceFrom(meta map[string]any) string {
	if meta == nil {
		return "medium"
	}
	if raw, ok := meta["confidence"]; ok {
		if s, ok := raw.(string); ok {
			s = strings.TrimSpace(strings.ToLower(s))
			switch s {
			case "high", "medium", "low":
				return s
			}
		}
	}
	return "medium"
}

// memoryUserMatches checks whether a user-scoped memory entry belongs
// to the current user. Reads `user_id` (number) or `user` (string —
// either a numeric id or a username). Missing field → match: see the
// filterMemory comment for the rationale.
func memoryUserMatches(meta map[string]any, currentUserID int64) bool {
	if meta == nil {
		return true
	}
	if raw, ok := meta["user_id"]; ok {
		switch v := raw.(type) {
		case float64:
			return int64(v) == currentUserID
		case int64:
			return v == currentUserID
		case int:
			return int64(v) == currentUserID
		case string:
			s := strings.TrimSpace(v)
			if s == "" {
				return true
			}
			// Compare as int when possible.
			var asInt int64
			if _, err := fmt.Sscanf(s, "%d", &asInt); err == nil {
				return asInt == currentUserID
			}
			return false
		}
	}
	// No user field declared → preserve visibility (see filterMemory note).
	return true
}

// stringFromMeta returns the trimmed string value for `key` or "" when
// missing / non-string.
func stringFromMeta(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// filterRunbooks applies PAI-340's runbook rule: keep entries whose
// `related_agents` list contains the current agent name, OR keep
// entries with no `related_agents` field at all (universal runbook).
// Archived entries are dropped.
func filterRunbooks(entries []knowledgeEntry, agentName string) []knowledgeEntry {
	out := make([]knowledgeEntry, 0, len(entries))
	for _, e := range entries {
		if !isLiveEntry(e) {
			continue
		}
		related, ok := stringSliceFromAny(e.Metadata["related_agents"])
		if !ok || len(related) == 0 {
			out = append(out, e)
			continue
		}
		if containsString(related, agentName) {
			out = append(out, e)
		}
	}
	return out
}

// filterGuidelines applies PAI-340's guideline rule: keep entries whose
// `applies_to_agents` list contains the current agent name, OR keep
// entries with no `applies_to_agents` field at all (universal
// guideline). Archived entries are dropped.
func filterGuidelines(entries []knowledgeEntry, agentName string) []knowledgeEntry {
	out := make([]knowledgeEntry, 0, len(entries))
	for _, e := range entries {
		if !isLiveEntry(e) {
			continue
		}
		applies, ok := stringSliceFromAny(e.Metadata["applies_to_agents"])
		if !ok || len(applies) == 0 {
			out = append(out, e)
			continue
		}
		if containsString(applies, agentName) {
			out = append(out, e)
		}
	}
	return out
}

// filterAlwaysLive drops only archived entries — used for external
// systems and related projects which carry no agent / user filter.
func filterAlwaysLive(entries []knowledgeEntry) []knowledgeEntry {
	out := make([]knowledgeEntry, 0, len(entries))
	for _, e := range entries {
		if !isLiveEntry(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// fetchKnowledge fetches one knowledge category and decodes it.
func fetchKnowledge(c *Client, projectID int64, alias string) ([]knowledgeEntry, error) {
	body, err := c.do("GET", fmt.Sprintf("/api/projects/%d/%s", projectID, alias), nil)
	if err != nil {
		return nil, err
	}
	var entries []knowledgeEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("decode %s: %w", alias, err)
	}
	return entries, nil
}

// fetchScopedMemory pulls user-scope or instance-scope memory off
// the matching endpoint. Failure is non-fatal: a server that pre-
// dates PAI-345 returns 404, which we surface as an empty slice so
// the bundle still renders. The caller decides whether to log.
func fetchScopedMemory(c *Client, path string) []knowledgeEntry {
	body, err := c.do("GET", path, nil)
	if err != nil {
		return nil
	}
	var entries []knowledgeEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil
	}
	return entries
}

// mergeMemoryByScope folds project / user / instance memory into a
// single list with project > user > instance precedence on slug
// collision. Each entry carries a `Scope` field so agents can tell
// where a rule came from. Silent dedup: lower-precedence entries
// with a slug already taken at higher precedence are dropped.
//
// The slug is the dedup key even though entries can technically
// share a body/title across scopes — PAI-345's spec is explicit
// that a project memory wins on slug collision and lower-precedence
// duplicates are not surfaced.
func mergeMemoryByScope(project, user, instance []knowledgeEntry) []knowledgeEntry {
	out := make([]knowledgeEntry, 0, len(project)+len(user)+len(instance))
	seen := map[string]bool{}
	tag := func(list []knowledgeEntry, scope string) {
		for _, e := range list {
			if seen[e.Slug] {
				continue
			}
			e.Scope = scope
			out = append(out, e)
			seen[e.Slug] = true
		}
	}
	tag(project, "project")
	tag(user, "user")
	tag(instance, "instance")
	return out
}

// fetchCurrentUserID returns the numeric id of the API-key-bearing
// user. Failures are non-fatal: PAI-340's user-scope filter falls back
// to "include all" when the id is unknown so a CI / unauthenticated
// run still produces a useful bundle.
func fetchCurrentUserID(c *Client) int64 {
	body, err := c.do("GET", "/api/auth/me", nil)
	if err != nil {
		return 0
	}
	var probe struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return 0
	}
	return probe.User.ID
}

// resolveBundle is the orchestration entry point: it fetches the agent
// artifact and all five knowledge categories, applies the per-category
// filters, and returns the assembled payload. The resolver does not
// touch the cache — that's the caller's responsibility (env / files
// formats persist; json prints to stdout).
//
// `includeLow` (PAI-347) flips the confidence gate: by default `low`
// memories are excluded; pass true to opt them back in. The decision
// is plumbed all the way through here (rather than at the CLI flag
// layer) so the helper test surface can pin both paths.
func resolveBundle(c *Client, project projectSummary, agentName string, includeLow bool) (*bundlePayload, error) {
	// Agent artifact is canonical (PAI-329); we keep the full JSON so
	// every field the server emits round-trips into the bundle.
	agentRaw, err := c.do("GET",
		fmt.Sprintf("/api/projects/%d/agents/%s.json", project.ID, url.PathEscape(agentName)), nil)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Agent agentMetadata `json:"agent"`
	}
	if err := json.Unmarshal(agentRaw, &probe); err != nil {
		return nil, fmt.Errorf("decode agent artifact: %w", err)
	}
	agentEnvs := agentEnvironments(probe.Agent.Metadata)

	memRaw, err := fetchKnowledge(c, project.ID, "memory")
	if err != nil {
		return nil, err
	}
	rbRaw, err := fetchKnowledge(c, project.ID, "runbooks")
	if err != nil {
		return nil, err
	}
	exRaw, err := fetchKnowledge(c, project.ID, "external-systems")
	if err != nil {
		return nil, err
	}
	rpRaw, err := fetchKnowledge(c, project.ID, "related-projects")
	if err != nil {
		return nil, err
	}
	glRaw, err := fetchKnowledge(c, project.ID, "guidelines")
	if err != nil {
		return nil, err
	}

	currentUserID := fetchCurrentUserID(c)

	// PAI-345 — pull cross-scope memory layers. These are best-effort:
	// older servers without the new endpoints return 404, which the
	// fetch helper translates to nil (empty list). The merge keeps
	// project > user > instance precedence on slug collision.
	userMemRaw := fetchScopedMemory(c, "/api/users/me/memory")
	instanceMemRaw := fetchScopedMemory(c, "/api/instance/memory")

	projectMem := filterMemory(memRaw, currentUserID, agentEnvs, includeLow)
	userMem := filterMemory(userMemRaw, currentUserID, agentEnvs, includeLow)
	instanceMem := filterMemory(instanceMemRaw, currentUserID, agentEnvs, includeLow)
	mergedMem := mergeMemoryByScope(projectMem, userMem, instanceMem)

	bundle := &bundlePayload{
		Project:         project,
		Agent:           json.RawMessage(agentRaw),
		Memory:          mergedMem,
		Runbooks:        filterRunbooks(rbRaw, agentName),
		ExternalSystems: filterAlwaysLive(exRaw),
		RelatedProjects: filterAlwaysLive(rpRaw),
		Guidelines:      filterGuidelines(glRaw, agentName),
		FetchedAt:       time.Now().UTC().Format(time.RFC3339),
	}

	// PAI-347 — bump reference_count + last_referenced_at on every
	// memory entry that survived filtering. Best-effort: a transient
	// failure here must not break the bundle output (the user-visible
	// payload is far more important than the counter).
	if len(bundle.Memory) > 0 {
		_ = bumpMemoryReferences(c, project.ID, bundle.Memory)
	}
	return bundle, nil
}

// bumpMemoryReferences POSTs the included memory ids to the
// `/memory/references` endpoint so the server can update the
// reference_count + last_referenced_at columns. The endpoint is
// idempotent enough — a partial / dropped request just under-counts
// for this run, which is preferable to blocking the bundle on a
// counter-update failure.
func bumpMemoryReferences(c *Client, projectID int64, entries []knowledgeEntry) error {
	if c == nil || projectID <= 0 || len(entries) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		if e.ID > 0 {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	body := map[string]any{
		"ids":    ids,
		"source": "bundle",
	}
	_, err := c.do("POST",
		fmt.Sprintf("/api/projects/%d/memory/references", projectID), body)
	return err
}

// cacheManifest is the on-disk JSON written under
// `<cache-dir>/<project-key>/manifest.json`. `Rev` is a stable hash of
// the entries (excluding fetched_at, which would defeat the
// invalidation cheapness) so PAI-341's sync verb can ask "did
// anything change?" with one SELECT.
type cacheManifest struct {
	Project   string                      `json:"project"`
	Agent     string                      `json:"agent"`
	FetchedAt string                      `json:"fetched_at"`
	Rev       string                      `json:"rev"`
	Entries   map[string][]knowledgeEntry `json:"entries"`
}

// computeBundleRev returns a stable sha256 over the filtered entries
// (sorted by category alias, then by slug within each category). The
// agent artifact bytes are folded in too so a no-op knowledge edit
// that re-renders the agent still bumps the rev. Excludes timestamps
// so two fetches of an unchanged corpus return the same rev.
func computeBundleRev(b *bundlePayload) string {
	h := sha256.New()
	// Stable category order — the manifest uses the same key names.
	cats := []struct {
		name string
		list []knowledgeEntry
	}{
		{"memory", b.Memory},
		{"runbooks", b.Runbooks},
		{"external_systems", b.ExternalSystems},
		{"related_projects", b.RelatedProjects},
		{"guidelines", b.Guidelines},
	}
	for _, c := range cats {
		fmt.Fprintf(h, "[%s]", c.name)
		// Sort by slug so insertion order doesn't perturb the hash.
		sorted := make([]knowledgeEntry, len(c.list))
		copy(sorted, c.list)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Slug < sorted[j].Slug
		})
		for _, e := range sorted {
			// Title + body are the load-bearing fields; the metadata
			// is hashed via JSON marshal so map ordering doesn't matter.
			metaJSON, _ := json.Marshal(e.Metadata)
			fmt.Fprintf(h, "%s\x1f%s\x1f%s\x1f%s\x1e",
				e.Slug, e.Title, e.Body, string(metaJSON))
		}
	}
	// Fold the agent artifact in last — its bytes are already JSON-
	// marshalled by the server so ordering is whatever the server picks
	// (stable enough for invalidation purposes).
	h.Write(b.Agent)
	return hex.EncodeToString(h.Sum(nil))
}

// writeBundleManifest serialises the bundle into the canonical
// manifest path under `<cacheRoot>/<project-key>/manifest.json`. The
// directory is created on demand (0o755) and the manifest is written
// via tmp+rename so a concurrent reader never sees a half-written
// JSON document.
func writeBundleManifest(cacheRoot string, b *bundlePayload, agentName string) (string, string, error) {
	rev := computeBundleRev(b)
	manifest := cacheManifest{
		Project:   b.Project.Key,
		Agent:     agentName,
		FetchedAt: b.FetchedAt,
		Rev:       rev,
		Entries: map[string][]knowledgeEntry{
			"memory":           b.Memory,
			"runbooks":         b.Runbooks,
			"external_systems": b.ExternalSystems,
			"related_projects": b.RelatedProjects,
			"guidelines":       b.Guidelines,
		},
	}
	dir := filepath.Join(cacheRoot, b.Project.Key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir cache: %w", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	bs, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal manifest: %w", err)
	}
	tmp := manifestPath + ".tmp"
	if err := os.WriteFile(tmp, append(bs, '\n'), 0o644); err != nil {
		return "", "", fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmp, manifestPath); err != nil {
		return "", "", fmt.Errorf("rename manifest: %w", err)
	}
	return dir, rev, nil
}

// readBundleManifest loads an existing manifest (or returns nil when
// missing / corrupt — both are "no cache" from the caller's
// perspective). Used to honour `--bundle full` without `--refresh`
// when a fresh-enough cache already exists; PAI-341 will harden the
// freshness check, today the rule is "any well-formed manifest
// counts" so a developer iterating with `--bundle full` doesn't beat
// up the API on every command.
func readBundleManifest(cacheRoot, projectKey string) (*cacheManifest, error) {
	manifestPath := filepath.Join(cacheRoot, projectKey, "manifest.json")
	bs, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m cacheManifest
	if err := json.Unmarshal(bs, &m); err != nil {
		// Treat corrupt JSON as a cache miss — refusing the command
		// over a stale cache file would be hostile.
		return nil, nil
	}
	return &m, nil
}

// writeBundleFiles materialises the bundle as a directory tree of
// markdown files (one per entry, frontmatter + body). The agent
// artifact is written as `agent.json` so consumers that prefer the
// canonical artifact don't have to reconstruct it. Returns the
// directory path written.
//
// Layout:
//
//	<cacheRoot>/<project-key>/manifest.json
//	<cacheRoot>/<project-key>/agent.json
//	<cacheRoot>/<project-key>/memory/<slug>.md
//	<cacheRoot>/<project-key>/runbooks/<slug>.md
//	... (one folder per category)
func writeBundleFiles(cacheRoot string, b *bundlePayload) (string, error) {
	dir := filepath.Join(cacheRoot, b.Project.Key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir cache: %w", err)
	}
	// Agent artifact — pretty-print so a human inspecting the cache
	// can read it without `jq`.
	var pretty any
	_ = json.Unmarshal(b.Agent, &pretty)
	agentBs, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		// Fall back to the raw bytes — never block the bundle write
		// over a cosmetic re-encode failure.
		agentBs = b.Agent
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), append(agentBs, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write agent.json: %w", err)
	}
	cats := []struct {
		alias   string
		entries []knowledgeEntry
	}{
		{"memory", b.Memory},
		{"runbooks", b.Runbooks},
		{"external_systems", b.ExternalSystems},
		{"related_projects", b.RelatedProjects},
		{"guidelines", b.Guidelines},
	}
	for _, c := range cats {
		if err := writeCategoryFiles(dir, c.alias, c.entries); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// writeCategoryFiles writes one frontmatter+body markdown file per
// entry. Empty categories still create the directory so a consumer
// listing the tree gets a stable layout.
func writeCategoryFiles(rootDir, alias string, entries []knowledgeEntry) error {
	catDir := filepath.Join(rootDir, alias)
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", catDir, err)
	}
	for _, e := range entries {
		path := filepath.Join(catDir, e.Slug+".md")
		if err := os.WriteFile(path, []byte(renderEntryMarkdown(e)), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// renderEntryMarkdown serialises one knowledge entry as
// frontmatter + markdown body. The frontmatter is JSON-shaped (rather
// than YAML) so consumers can decode it without a YAML parser; tools
// that read TOML/YAML frontmatter can still handle JSON via the
// generic frontmatter prefix `---json`.
func renderEntryMarkdown(e knowledgeEntry) string {
	header := map[string]any{
		"id":         e.ID,
		"type":       e.Type,
		"slug":       e.Slug,
		"title":      e.Title,
		"status":     e.Status,
		"metadata":   e.Metadata,
		"created_at": e.CreatedAt,
		"updated_at": e.UpdatedAt,
	}
	hb, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		// Should never happen with the values above, but if marshal
		// somehow fails we still want a usable file: write a minimal
		// header and the body.
		hb = []byte(fmt.Sprintf(`{"slug": %q, "title": %q}`, e.Slug, e.Title))
	}
	var b strings.Builder
	b.WriteString("---json\n")
	b.Write(hb)
	b.WriteString("\n---\n\n")
	body := strings.TrimRight(e.Body, "\n")
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

// emitBundleEnv is the eval-friendly emitter: it writes the bundle to
// a cache directory (so subsequent agent reads don't have to re-fetch)
// and prints `export` lines that point at the cache plus the usual
// PAI-327 attribution exports. The cache layout matches `--format
// files` so a downstream tool can mix and match.
func emitBundleEnv(b *bundlePayload, agentName, sessionID, cacheRoot string) error {
	dir, err := writeBundleFiles(cacheRoot, b)
	if err != nil {
		return err
	}
	if _, _, err := writeBundleManifest(cacheRoot, b, agentName); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "export PAIMOS_AGENT_NAME=%s\n", agentName)
	fmt.Fprintf(stdout, "export PAIMOS_SESSION_ID=%s\n", sessionID)
	fmt.Fprintf(stdout, "export PAIMOS_KNOWLEDGE_DIR=%s\n", dir)
	return nil
}

// emitBundleJSON dumps the full bundle (including the agent artifact
// and every filtered entry) to stdout as a single JSON document. The
// rev is included so downstream tools can detect drift without
// re-running the resolver.
func emitBundleJSON(b *bundlePayload, agentName, sessionID string) error {
	rev := computeBundleRev(b)
	out := map[string]any{
		"agent_name":       agentName,
		"session_id":       sessionID,
		"project":          b.Project,
		"agent":            json.RawMessage(b.Agent),
		"memory":           b.Memory,
		"runbooks":         b.Runbooks,
		"external_systems": b.ExternalSystems,
		"related_projects": b.RelatedProjects,
		"guidelines":       b.Guidelines,
		"fetched_at":       b.FetchedAt,
		"rev":              rev,
	}
	return emitJSON(out)
}

// emitBundleFiles writes the bundle as a directory tree (manifest +
// agent.json + per-entry markdown) and prints a small confirmation
// line so the caller knows where to look. Eval-friendliness is not a
// goal of this format — `--format env` covers that.
func emitBundleFiles(b *bundlePayload, agentName string, cacheRoot string) error {
	dir, err := writeBundleFiles(cacheRoot, b)
	if err != nil {
		return err
	}
	manifestDir, rev, err := writeBundleManifest(cacheRoot, b, agentName)
	if err != nil {
		return err
	}
	// Confirmation line — sent to stdout so callers can capture it
	// programmatically. Two lines stay terse (consumer-friendly).
	fmt.Fprintf(stdout, "wrote bundle to %s (rev=%s)\n", dir, rev)
	if manifestDir != dir {
		fmt.Fprintf(stdout, "manifest at %s/manifest.json\n", manifestDir)
	}
	return nil
}

// defaultCacheRoot returns the cache directory the CLI uses when the
// caller doesn't pass `--cache-dir`. Defaults to `.paimos/cache` under
// the current working directory — matches the ticket's spec and keeps
// the cache in-tree so it shows up in `git status` for opt-in tracking.
func defaultCacheRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, ".paimos", "cache"), nil
}
