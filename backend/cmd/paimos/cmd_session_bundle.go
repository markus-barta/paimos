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
// PAI-345 + PAI-348 — both add CLI-side annotations:
//
//	Scope  (PAI-345): "project"|"user"|"instance" — set by the
//	  bundle resolver based on which endpoint the entry came from
//	  so downstream agents can disambiguate cross-scope merges.
//	Source (PAI-348): provenance for inherited entries (from
//	  related_projects[]). Never persisted server-side; the resolver
//	  fills it in when pulling cross-project inheritance.
//
// Both use `omitempty` so own / project-scope entries stay free of
// empty annotation noise.
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
	Scope     string         `json:"scope,omitempty"`  // PAI-345
	Source    *entrySource   `json:"source,omitempty"` // PAI-348
}

// entrySource is the PAI-348 provenance annotation. `Type` is one of:
//
//   - "inherited": entry was pulled from a project declared in
//     related_projects[]. FromProject + FromInstance identify the
//     upstream.
//   - "warning": cross-instance inheritance failed; Message describes
//     the cause. Surfaced so the bundle is never silently incomplete.
//
// The struct is intentionally narrow — the inheritance contract is
// "carry enough info for the agent / UI to render a click-through to
// the source"; richer provenance (rev, fetched_at) layers on PAI-341.
type entrySource struct {
	Type         string `json:"type"`
	FromProject  string `json:"from_project,omitempty"`
	FromInstance string `json:"from_instance,omitempty"`
	Message      string `json:"message,omitempty"`
}

// inheritableRoles is the set of related_projects[] roles that pull
// memory / runbooks / guidelines into the downstream bundle. PAI-348
// limits inheritance to "this project depends on / extends Q"
// relationships — a peer relationship like "shared-customer" must
// stay opaque to the bundle resolver.
var inheritableRoles = map[string]bool{
	"upstream-tool": true,
	"philosophy":    true,
	"infra":         true,
}

// inheritableCategories enumerates the knowledge kinds PAI-348 inherits.
// `external_systems` and `related_projects` are project-specific by
// nature (PAI-348 §"Out-of-scope") so they never inherit.
var inheritableCategories = []string{"memory", "runbook", "guideline"}

// bundlePayload is the canonical in-memory shape the resolver builds.
// `Agent` is the canonical artifact JSON (PAI-329) — kept as
// json.RawMessage so we round-trip every field the server returns
// without coupling the CLI to PAI-329's schema. Each knowledge slice
// is post-filter (i.e. exactly the entries the bundle exposes to the
// agent).
type bundlePayload struct {
	Project         projectSummary   `json:"project"`
	Agent           json.RawMessage  `json:"agent"`
	Memory          []knowledgeEntry `json:"memory"`
	Runbooks        []knowledgeEntry `json:"runbooks"`
	ExternalSystems []knowledgeEntry `json:"external_systems"`
	RelatedProjects []knowledgeEntry `json:"related_projects"`
	Guidelines      []knowledgeEntry `json:"guidelines"`
	FetchedAt       string           `json:"fetched_at"`
}

// archivedStatus is the on-disk status value the knowledge plane uses
// for the "archived" toggle (matches the frontend's
// projectKnowledge.ts ARCHIVED_STATUS constant). Any other status
// counts as live for v1; that's the same rule the UI applies.
const archivedStatus = "cancelled"

// proposedStatusEntry is the status value PAI-349 uses for bot-authored
// memory drafts pending operator review. Excluded from the bundle by
// default; the agent that drafted opts back in via --include-proposed.
const proposedStatusEntry = "proposed"

// archivedStatus check kept loose: a missing status is treated as
// live so a server that elides empty fields never produces an empty
// bundle by accident.
func isLiveEntry(e knowledgeEntry) bool {
	return strings.TrimSpace(e.Status) != archivedStatus
}

// isProposedEntry reports whether the entry is in the PAI-349 draft
// state. Only memory entries can be 'proposed' for v1; runbooks /
// guidelines have no propose UX yet, so the check still returns false
// for them.
func isProposedEntry(e knowledgeEntry) bool {
	return strings.TrimSpace(e.Status) == proposedStatusEntry
}

// agentMetadata is the subset of the canonical artifact's `agent`
// block the filter logic needs. We probe with a tolerant struct so
// the filter survives PAI-329 schema additions.
type agentMetadata struct {
	Name     string         `json:"name"`
	Metadata map[string]any `json:"metadata"`
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
//  6. PAI-349: drop entries with status == "proposed" unless
//     `includeProposed` is set. Bot-authored drafts await operator
//     review before they affect downstream agents; the agent that
//     drafted them opts back in via --include-proposed to verify its
//     own pending work.
func filterMemory(entries []knowledgeEntry, currentUserID int64, agentEnvs []string, includeLow, includeProposed bool) []knowledgeEntry {
	out := make([]knowledgeEntry, 0, len(entries))
	for _, e := range entries {
		if !isLiveEntry(e) {
			continue
		}
		// PAI-349 — proposed entries are filtered before the scope /
		// environment / confidence checks; the gate is independent and
		// short-circuiting here keeps the rest of the pipeline simple.
		if !includeProposed && isProposedEntry(e) {
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
// PAI-394 unified surface — the legacy `alias` parameter is now
// the URL segment (kebab singular) that travels as the `?type=`
// query on the single /knowledge route.
func fetchKnowledge(c *Client, projectID int64, alias string) ([]knowledgeEntry, error) {
	body, err := c.do("GET", fmt.Sprintf("/api/projects/%d/knowledge?type=%s", projectID, alias), nil)
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

// memoryInheritsFlag returns whether a memory entry should be exposed
// to downstream projects via PAI-348's inheritance. Default is true
// (most rules generalise); a typed-false `inherit` flag opts out. A
// non-bool value falls back to true so a corrupt metadata map never
// silently hides the entry — the editor / API enforce the type.
func memoryInheritsFlag(meta map[string]any) bool {
	if meta == nil {
		return true
	}
	raw, ok := meta["inherit"]
	if !ok {
		return true
	}
	if b, isBool := raw.(bool); isBool {
		return b
	}
	return true
}

// inheritedSources is the resolver's intermediate output for one
// related_projects[] declaration: the entries pulled from the upstream
// per category, plus an optional warning entry when the cross-instance
// fetch failed. Per PAI-348 §"Backwards-compat" the bundle never fails
// because of an upstream outage — the warning surfaces the cause and
// resolution continues with the project's own entries.
type inheritedSources struct {
	memory     []knowledgeEntry
	runbooks   []knowledgeEntry
	guidelines []knowledgeEntry
	warning    *knowledgeEntry // synthetic memory-typed warning, if any
}

// PAI-348 §"Cross-instance inheritance" calls for caching the
// resolved inherited set per (project, related-project) pair, with
// invalidation tied to PAI-341's rev mechanism. v1 piggy-backs on the
// existing PAI-340 manifest: inherited entries land in the manifest's
// `memory` / `runbooks` / `guidelines` slices alongside own entries
// (carrying their `Source` annotation), so the bundle-level rev hash
// already invalidates inherited content the moment any upstream entry
// changes the merged shape. PAI-341 will layer per-upstream `?since=rev`
// lookups on top once the server endpoint exists; the manifest format
// is forward-compatible because `Source` already carries the upstream
// pointer.

// fetchInheritedFromUpstream pulls memory / runbooks / guidelines from
// the upstream project (cross-instance when `instance_url` differs
// from the caller's own URL). On failure the returned `warning` is
// non-nil and the category slices are empty — matching PAI-348's
// "graceful degradation" rule. On success entries carry an
// `entrySource` annotation so downstream code (and agents reading the
// bundle JSON) can attribute the rule to its origin.
//
// Same-instance pulls reuse `c` directly (auth, baseURL, headers).
// Cross-instance pulls build a fresh unauthenticated client at the
// upstream URL — the inherited memory plane is read-only here, and
// PAI-348 v1 deliberately doesn't propagate API keys across instances
// (PAI-341's sync infrastructure layers richer auth on top).
func fetchInheritedFromUpstream(c *Client, upstream relatedProjectRef) inheritedSources {
	out := inheritedSources{}
	upstreamClient, err := upstreamClientFor(c, upstream.InstanceURL)
	if err != nil {
		out.warning = makeInheritWarning(upstream, fmt.Sprintf("resolve instance: %v", err))
		return out
	}
	upstreamID, err := resolveProjectKeyToIDOnInstance(upstreamClient, upstream.Key)
	if err != nil {
		out.warning = makeInheritWarning(upstream, fmt.Sprintf("project lookup: %v", err))
		return out
	}
	for _, alias := range inheritableCategories {
		entries, err := fetchKnowledge(upstreamClient, upstreamID, alias)
		if err != nil {
			// Partial failure — surface a warning and stop pulling
			// from this upstream. The caller still merges what we
			// have so far (all-or-nothing would punish a transient
			// blip on one endpoint).
			out.warning = makeInheritWarning(upstream, fmt.Sprintf("fetch %s: %v", alias, err))
			return out
		}
		// inherit-flag filter applies only to memory; runbooks /
		// guidelines inherit unconditionally for v1 (PAI-348's opt-out
		// surface focused on memory; runbooks are procedural and
		// almost always universal).
		if alias == "memory" {
			entries = filterInheritableMemory(entries)
		} else {
			entries = filterAlwaysLive(entries)
		}
		annotated := annotateInherited(entries, upstream)
		switch alias {
		case "memory":
			out.memory = annotated
		case "runbook":
			out.runbooks = annotated
		case "guideline":
			out.guidelines = annotated
		}
	}
	return out
}

// relatedProjectRef is the parsed shape of a related_projects[] entry
// the resolver hands to the inheritance pipeline. `Key` is the
// upstream project key, `InstanceURL` is the absolute URL of the
// upstream instance (required by PAI-338 schema), `Role` is one of
// the inheritance-eligible roles (see inheritableRoles).
type relatedProjectRef struct {
	Key         string
	InstanceURL string
	Role        string
}

// inheritableRefs filters the related_projects[] knowledge entries to
// those whose role triggers inheritance (PAI-348 §"Inheritance
// resolution"). Iteration order matches the on-disk slug order so the
// resulting precedence is stable per-project. Entries missing key or
// instance_url are skipped — bundle resolution is best-effort.
func inheritableRefs(entries []knowledgeEntry) []relatedProjectRef {
	out := []relatedProjectRef{}
	for _, e := range entries {
		role := strings.TrimSpace(stringFromMeta(e.Metadata, "role"))
		// PAI-338 stored the relationship under "relationship" in the
		// editor, but the inheritance contract uses "role". Accept
		// both so existing entries don't need a content migration.
		if role == "" {
			role = strings.TrimSpace(stringFromMeta(e.Metadata, "relationship"))
		}
		if !inheritableRoles[role] {
			continue
		}
		key := strings.TrimSpace(stringFromMeta(e.Metadata, "key"))
		instanceURL := strings.TrimSpace(stringFromMeta(e.Metadata, "instance_url"))
		if key == "" || instanceURL == "" {
			continue
		}
		out = append(out, relatedProjectRef{Key: key, InstanceURL: instanceURL, Role: role})
	}
	return out
}

// filterInheritableMemory drops archived entries AND entries whose
// `inherit` flag is explicitly false. The memory.scope filter is NOT
// applied here — user-on-this-project entries are inherently
// upstream-private and the inherit flag covers the opt-out, but
// scoping is a different concern (the upstream user might not exist
// on the downstream instance).
func filterInheritableMemory(entries []knowledgeEntry) []knowledgeEntry {
	out := make([]knowledgeEntry, 0, len(entries))
	for _, e := range entries {
		if !isLiveEntry(e) {
			continue
		}
		if !memoryInheritsFlag(e.Metadata) {
			continue
		}
		// User-scoped memory does not propagate to other projects —
		// the user-id namespace is per-instance and we don't carry a
		// user mapping across instances. Project-scoped + missing
		// scope both flow through.
		if scope := strings.TrimSpace(stringFromMeta(e.Metadata, "scope")); scope == "user-on-this-project" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// annotateInherited tags each entry with the PAI-348 source
// annotation. The slice is copied (rather than mutated in place) so
// the upstream cache stays clean for re-use across invocations.
func annotateInherited(entries []knowledgeEntry, upstream relatedProjectRef) []knowledgeEntry {
	out := make([]knowledgeEntry, len(entries))
	for i, e := range entries {
		clone := e
		clone.Source = &entrySource{
			Type:         "inherited",
			FromProject:  upstream.Key,
			FromInstance: upstream.InstanceURL,
		}
		out[i] = clone
	}
	return out
}

// makeInheritWarning synthesises a marker memory-typed entry the
// bundle surfaces in place of failed-inheritance content. The slug
// embeds the upstream key so multiple warnings (rare) stay
// distinguishable in the bundle JSON. PAI-348 §"Backwards-compat"
// requires the bundle to keep working — this is the visible signal.
func makeInheritWarning(upstream relatedProjectRef, reason string) *knowledgeEntry {
	return &knowledgeEntry{
		Type:  "memory",
		Slug:  fmt.Sprintf("inherit_warning_%s", strings.ToLower(strings.ReplaceAll(upstream.Key, "-", "_"))),
		Title: fmt.Sprintf("inheritance from %s failed", upstream.Key),
		Body:  fmt.Sprintf("Bundle resolution could not pull memory from %s (%s): %s", upstream.Key, upstream.InstanceURL, reason),
		Source: &entrySource{
			Type:         "warning",
			FromProject:  upstream.Key,
			FromInstance: upstream.InstanceURL,
			Message:      reason,
		},
	}
}

// upstreamClientFor returns a Client configured to talk to the
// upstream instance. Same-instance pulls return `c` unchanged so
// auth + headers + custom transport carry over. Cross-instance pulls
// get a fresh unauthenticated client — PAI-348 v1 doesn't propagate
// credentials, the inheritance contract assumes the upstream's
// memory plane is publicly readable for the configured projects (or
// equivalent auth has been granted out-of-band).
func upstreamClientFor(c *Client, upstreamURL string) (*Client, error) {
	upstreamURL = strings.TrimRight(strings.TrimSpace(upstreamURL), "/")
	if upstreamURL == "" {
		return nil, fmt.Errorf("instance_url is empty")
	}
	if upstreamURL == strings.TrimRight(c.baseURL, "/") {
		return c, nil
	}
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("parse instance_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("instance_url must be absolute (scheme + host)")
	}
	return &Client{
		baseURL: upstreamURL,
		http:    c.http,
	}, nil
}

// resolveProjectKeyToIDOnInstance fetches the project list from `c`
// and returns the numeric id matching `key`. Mirrors the existing
// per-instance lookup in cmd_session.go (resolveProjectKeyFromID) but
// keyed by key instead of id — the cross-instance pull path knows
// the key from related_projects[] but not the upstream's id.
func resolveProjectKeyToIDOnInstance(c *Client, key string) (int64, error) {
	body, err := c.do("GET", "/api/projects", nil)
	if err != nil {
		return 0, err
	}
	var list []projectSummary
	if err := json.Unmarshal(body, &list); err != nil {
		return 0, fmt.Errorf("decode projects: %w", err)
	}
	for _, p := range list {
		if strings.EqualFold(strings.TrimSpace(p.Key), strings.TrimSpace(key)) {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("project %q not found on upstream", key)
}

// inheritanceFetcher is the resolver's seam for cross-instance pulls.
// Tests swap in a stub so the bundle merge logic can be exercised
// without a real upstream server. Production wiring routes through
// fetchInheritedFromUpstream.
type inheritanceFetcher func(c *Client, upstream relatedProjectRef) inheritedSources

// inheritanceFetcherImpl is the package-level seam. Indirected so
// tests can swap it without touching the resolver signature. The
// default uses a per-call cache (one entry per (project, upstream)
// per process) so a single bundle resolution doesn't pull the same
// upstream twice when both runbooks and guidelines hit it.
var inheritanceFetcherImpl inheritanceFetcher = fetchInheritedFromUpstream

// mergeInherited folds inherited entries into the project's own
// entries with PAI-348's precedence rules: project-own beats inherited
// on slug collision, inherited entries appear in declaration order,
// and the slugs that came from upstream carry their `Source` annotation
// for the bundle / UI / agent to render.
//
// `own` is the post-filter project memory / runbooks / guidelines
// (the existing PAI-340 filter chain). `inherited` is a list of
// already-annotated upstream entries in declaration order.
func mergeInherited(own, inherited []knowledgeEntry) []knowledgeEntry {
	taken := map[string]bool{}
	for _, e := range own {
		taken[e.Slug] = true
	}
	out := make([]knowledgeEntry, 0, len(own)+len(inherited))
	out = append(out, own...)
	for _, e := range inherited {
		if taken[e.Slug] {
			continue
		}
		taken[e.Slug] = true
		out = append(out, e)
	}
	return out
}

// resolveBundle is the orchestration entry point: it fetches the agent
// artifact and all five knowledge categories, applies the per-category
// filters, and returns the assembled payload. The resolver does not
// touch the cache — that's the caller's responsibility (env / files
// formats persist; json prints to stdout).
//
// `includeLow` (PAI-347) flips the confidence gate: by default `low`
// memories are excluded; pass true to opt them back in.
//
// `includeProposed` (PAI-349) flips the propose gate: by default
// `proposed` memories (bot-authored drafts) are excluded; pass true
// when the agent that drafted them wants to verify its own pending
// work via the bundle.
//
// PAI-348 — when the project declares related_projects[] entries with
// an inheritance-eligible role (upstream-tool / philosophy / infra),
// the resolver pulls memory / runbooks / guidelines from each upstream
// (in declaration order) and merges them into the bundle with project
// precedence on slug collision. Cross-instance failures degrade
// gracefully: the bundle keeps the project's own entries plus a
// `source: warning` marker so the agent never silently misses content.
func resolveBundle(c *Client, project projectSummary, agentName string, includeLow, includeProposed bool) (*bundlePayload, error) {
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
	rbRaw, err := fetchKnowledge(c, project.ID, "runbook")
	if err != nil {
		return nil, err
	}
	exRaw, err := fetchKnowledge(c, project.ID, "external-system")
	if err != nil {
		return nil, err
	}
	rpRaw, err := fetchKnowledge(c, project.ID, "related-project")
	if err != nil {
		return nil, err
	}
	glRaw, err := fetchKnowledge(c, project.ID, "guideline")
	if err != nil {
		return nil, err
	}

	currentUserID := fetchCurrentUserID(c)

	// PAI-345 — cross-scope memory layers (own/user/instance). Best-
	// effort: older servers without the new endpoints return nil. Merge
	// keeps project > user > instance precedence on slug collision.
	userMemRaw := fetchScopedMemory(c, "/api/users/me/memory")
	instanceMemRaw := fetchScopedMemory(c, "/api/instance/memory")

	projectMem := filterMemory(memRaw, currentUserID, agentEnvs, includeLow, includeProposed)
	userMem := filterMemory(userMemRaw, currentUserID, agentEnvs, includeLow, includeProposed)
	instanceMem := filterMemory(instanceMemRaw, currentUserID, agentEnvs, includeLow, includeProposed)
	ownMemory := mergeMemoryByScope(projectMem, userMem, instanceMem)
	ownRunbooks := filterRunbooks(rbRaw, agentName)
	ownGuidelines := filterGuidelines(glRaw, agentName)
	ownRelated := filterAlwaysLive(rpRaw)

	// PAI-348 — pull inheritance from each related_projects[] entry
	// whose role triggers it. Iteration follows declaration order so
	// the merged precedence matches the ticket spec.
	upstreams := inheritableRefs(ownRelated)
	inheritedMemory := []knowledgeEntry{}
	inheritedRunbooks := []knowledgeEntry{}
	inheritedGuidelines := []knowledgeEntry{}
	for _, ref := range upstreams {
		src := inheritanceFetcherImpl(c, ref)
		// Append inherited entries in declaration order. Per-upstream
		// dedup is handled by mergeInherited below.
		inheritedMemory = append(inheritedMemory, src.memory...)
		inheritedRunbooks = append(inheritedRunbooks, src.runbooks...)
		inheritedGuidelines = append(inheritedGuidelines, src.guidelines...)
		if src.warning != nil {
			// Warning rides in the memory slice so agents that read
			// only memory still see it. The synthetic slug embeds the
			// upstream key so multiple warnings stay distinguishable.
			inheritedMemory = append(inheritedMemory, *src.warning)
		}
	}

	bundle := &bundlePayload{
		Project:         project,
		Agent:           json.RawMessage(agentRaw),
		Memory:          mergeInherited(ownMemory, inheritedMemory),
		Runbooks:        mergeInherited(ownRunbooks, inheritedRunbooks),
		ExternalSystems: filterAlwaysLive(exRaw),
		RelatedProjects: ownRelated,
		Guidelines:      mergeInherited(ownGuidelines, inheritedGuidelines),
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
		fmt.Sprintf("/api/projects/%d/knowledge/memory/references", projectID), body)
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
// directory is created on demand (0o750) and the manifest is written
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
	dir, err := bundleCacheDir(cacheRoot, b.Project.Key)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", fmt.Errorf("mkdir cache: %w", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	bs, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal manifest: %w", err)
	}
	tmp := manifestPath + ".tmp"
	if err := os.WriteFile(tmp, append(bs, '\n'), 0o600); err != nil {
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
	// #nosec G304 -- cacheRoot comes from the user's own --cache-dir flag (default ./.paimos/cache) and the fixed manifest.json name is appended.
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
	dir, err := bundleCacheDir(cacheRoot, b.Project.Key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), append(agentBs, '\n'), 0o600); err != nil {
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
	if err := os.MkdirAll(catDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", catDir, err)
	}
	for _, e := range entries {
		// Slugs come from the server payload; refuse anything that
		// would escape the category directory.
		name := e.Slug + ".md"
		if !filepath.IsLocal(name) || strings.ContainsAny(e.Slug, `/\`) {
			return fmt.Errorf("entry slug %q is not a safe file name", e.Slug)
		}
		path := filepath.Join(catDir, name)
		if err := os.WriteFile(path, []byte(renderEntryMarkdown(e)), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// bundleCacheDir joins the server-reported project key under cacheRoot,
// rejecting keys containing separators or ".." so a hostile bundle
// payload cannot steer cache writes outside the cache root.
func bundleCacheDir(cacheRoot, projectKey string) (string, error) {
	if projectKey == "" || !filepath.IsLocal(projectKey) || strings.ContainsAny(projectKey, `/\`) {
		return "", fmt.Errorf("project key %q is not a safe cache directory name", projectKey)
	}
	return filepath.Join(cacheRoot, projectKey), nil
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
