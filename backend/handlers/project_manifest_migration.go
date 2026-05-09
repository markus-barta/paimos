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

// PAI-357 — migrate legacy `project_manifests.data` content into the
// PAI-338 knowledge plane. Mapping is deterministic so dry-run +
// non-dry-run share the same plan-builder; only the apply step
// differs.
//
//   top-level non-`_` keys  → 1 runbook (slug: legacy_manifest)
//   _guardrails[i]          → 1 guideline per entry
//   _glossary[term]         → 1 memory(type=reference) per term
//   _dev                    → project_agents.body where name='dev'
//   _ops                    → project_agents.body where name='ops'
//
// After successful (non-dry-run) apply, the source row is stamped
// with `data._migrated_at` and `data._migrated_to_knowledge=true`.
// Re-running on a stamped manifest is a no-op unless `force=true`,
// matching the ticket's idempotency contract.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers/knowledge"
)

// migrationItem is one planned write the migration will perform.
// Used in both dry-run preview (so the UI can show the user what
// will land) and the actual apply path.
type migrationItem struct {
	Kind     string `json:"kind"`               // "memory", "runbook", "external_system", "guideline", "agent_body"
	Slug     string `json:"slug,omitempty"`     // knowledge entry slug (n/a for agent_body)
	AgentName string `json:"agent_name,omitempty"` // populated only for kind=="agent_body"
	Title    string `json:"title"`
	Source   string `json:"source"`             // human-readable source path inside the manifest blob
	Reason   string `json:"reason,omitempty"`   // populated for skipped/conflict items
}

type migrateManifestResult struct {
	DryRun      bool             `json:"dry_run"`
	Created     []migrationItem  `json:"created"`
	Skipped     []migrationItem  `json:"skipped"`
	Conflicts   []migrationItem  `json:"conflicts"`
	MigratedAt  string           `json:"migrated_at,omitempty"`
}

// MigrateManifestToKnowledge handles
// POST /api/projects/{id}/migrate-manifest-to-knowledge.
// Admin-only; the route wiring already enforces that.
func MigrateManifestToKnowledge(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if getProjectByID(projectID) == nil {
		jsonError(w, "project not found", http.StatusNotFound)
		return
	}

	var body struct {
		DryRun bool `json:"dry_run"`
		Force  bool `json:"force"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid body", http.StatusBadRequest)
			return
		}
	}

	manifest, err := loadProjectManifest(projectID)
	if err != nil {
		jsonError(w, "failed to load manifest", http.StatusInternalServerError)
		return
	}
	dataMap, _ := manifest.Data.(map[string]any)
	if dataMap == nil {
		dataMap = map[string]any{}
	}

	// Idempotency: a previously-migrated manifest no-ops unless force=true.
	// Empty manifests still get the marker so re-runs short-circuit.
	if !body.Force {
		if _, ok := dataMap["_migrated_at"]; ok {
			result := migrateManifestResult{
				DryRun:     body.DryRun,
				Created:    []migrationItem{},
				Skipped:    []migrationItem{{Kind: "all", Source: "manifest", Reason: "already migrated; pass force=true to re-run"}},
				Conflicts:  []migrationItem{},
				MigratedAt: stringField(dataMap, "_migrated_at"),
			}
			jsonOK(w, result)
			return
		}
	}

	plan := buildManifestMigrationPlan(dataMap)

	result := migrateManifestResult{
		DryRun:    body.DryRun,
		Created:   []migrationItem{},
		Skipped:   []migrationItem{},
		Conflicts: []migrationItem{},
	}

	if body.DryRun {
		// Pre-flight: surface conflicts (existing slugs / non-empty agent
		// bodies) so the UI can warn before commit.
		for _, item := range plan {
			if conflict := detectMigrationConflict(projectID, item, body.Force); conflict != "" {
				cp := item
				cp.Reason = conflict
				result.Conflicts = append(result.Conflicts, cp)
				continue
			}
			result.Created = append(result.Created, item)
		}
		jsonOK(w, result)
		return
	}

	// Apply path. Knowledge writes go through CreateEntryHook (PAI-353)
	// so the entries get history snapshots + mutation_log + SSE.
	for _, item := range plan {
		conflict := detectMigrationConflict(projectID, item, body.Force)
		if conflict != "" && !body.Force {
			cp := item
			cp.Reason = conflict
			result.Conflicts = append(result.Conflicts, cp)
			continue
		}
		if err := applyMigrationItem(r, projectID, item, body.Force); err != nil {
			cp := item
			cp.Reason = err.Error()
			result.Skipped = append(result.Skipped, cp)
			continue
		}
		result.Created = append(result.Created, item)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	dataMap["_migrated_at"] = now
	dataMap["_migrated_to_knowledge"] = true
	if _, err := saveProjectManifest(projectID, dataMap, nil, time.Now().UTC().Format("2006-01-02 15:04:05")); err != nil {
		jsonError(w, "marker write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	result.MigratedAt = now
	jsonOK(w, result)
}

// buildManifestMigrationPlan walks the legacy manifest blob and
// returns one migrationItem per write. Pure function — same input
// always yields the same plan; conflict detection is layered on top
// at apply time.
func buildManifestMigrationPlan(data map[string]any) []migrationItem {
	plan := []migrationItem{}

	// 1. Top-level non-`_` keys → one runbook combining everything.
	regular := map[string]any{}
	for k, v := range data {
		if strings.HasPrefix(k, "_") {
			continue
		}
		regular[k] = v
	}
	if len(regular) > 0 {
		plan = append(plan, migrationItem{
			Kind:   "runbook",
			Slug:   "legacy_manifest",
			Title:  "Legacy manifest",
			Source: "manifest top-level keys",
		})
	}

	// 2. _guardrails → guideline per entry.
	if raw, ok := data["_guardrails"]; ok {
		switch g := raw.(type) {
		case []any:
			for i, entry := range g {
				slug, title := slugAndTitleForGuardrail(i, entry)
				plan = append(plan, migrationItem{
					Kind:   "guideline",
					Slug:   slug,
					Title:  title,
					Source: fmt.Sprintf("_guardrails[%d]", i),
				})
			}
		default:
			// Malformed: dump the raw value into a single fallback entry
			// so users don't lose data silently.
			plan = append(plan, migrationItem{
				Kind:   "guideline",
				Slug:   "legacy_guardrails_unknown",
				Title:  "Legacy guardrails (unparsed)",
				Source: "_guardrails (non-array)",
			})
		}
	}

	// 3. _glossary → memory per term.
	if raw, ok := data["_glossary"]; ok {
		if gloss, ok := raw.(map[string]any); ok {
			// Sorted keys → deterministic plan ordering, easier dry-run review.
			terms := make([]string, 0, len(gloss))
			for k := range gloss {
				terms = append(terms, k)
			}
			sort.Strings(terms)
			for _, term := range terms {
				plan = append(plan, migrationItem{
					Kind:   "memory",
					Slug:   "glossary_" + slugifyForKnowledge(term),
					Title:  term,
					Source: fmt.Sprintf("_glossary[%q]", term),
				})
			}
		}
	}

	// 4. _dev / _ops → project_agents.body.
	for _, agentName := range []string{"dev", "ops"} {
		if _, ok := data["_"+agentName]; ok {
			plan = append(plan, migrationItem{
				Kind:      "agent_body",
				AgentName: agentName,
				Title:     fmt.Sprintf("%s agent body", agentName),
				Source:    "_" + agentName,
			})
		}
	}

	return plan
}

// detectMigrationConflict returns a non-empty string when the
// destination already holds content that would collide with this
// migration's write. The conflict is informational — apply paths
// either skip (default) or overwrite (when force=true).
func detectMigrationConflict(projectID int64, item migrationItem, force bool) string {
	if force {
		return ""
	}
	switch item.Kind {
	case "memory", "runbook", "guideline":
		if knowledgeSlugExists(projectID, item.Kind, item.Slug) {
			return fmt.Sprintf("knowledge entry %s/%s already exists", item.Kind, item.Slug)
		}
	case "agent_body":
		body := agentBodyByName(projectID, item.AgentName)
		if strings.TrimSpace(body) != "" {
			return fmt.Sprintf("agent %q already has a non-empty body", item.AgentName)
		}
	}
	return ""
}

// applyMigrationItem performs the actual write for one planned item.
// Knowledge entries flow through the CreateEntryHook so they pick up
// history snapshots + SSE; agent body writes hit project_agents
// directly because no equivalent hook exists for that table yet.
func applyMigrationItem(r *http.Request, projectID int64, item migrationItem, force bool) error {
	manifest, err := loadProjectManifest(projectID)
	if err != nil {
		return err
	}
	dataMap, _ := manifest.Data.(map[string]any)
	if dataMap == nil {
		dataMap = map[string]any{}
	}

	switch item.Kind {
	case "runbook":
		body := buildLegacyManifestBody(dataMap)
		return upsertKnowledgeEntry(r, projectID, "runbook", item.Slug, "Legacy manifest", body, nil, force)

	case "guideline":
		i := -1
		if _, parseErr := fmt.Sscanf(item.Source, "_guardrails[%d]", &i); parseErr != nil {
			i = -1
		}
		title, body, meta := guardrailContentForIndex(dataMap, i, item.Source)
		return upsertKnowledgeEntry(r, projectID, "guideline", item.Slug, title, body, meta, force)

	case "memory":
		// item.Title is the original term; reverse-lookup the body.
		term := item.Title
		body := stringField(asMap(dataMap["_glossary"]), term)
		return upsertKnowledgeEntry(r, projectID, "memory", item.Slug, term, body, map[string]any{
			"type": "reference",
		}, force)

	case "agent_body":
		raw, ok := dataMap["_"+item.AgentName]
		if !ok {
			return fmt.Errorf("agent source %q not found", item.AgentName)
		}
		body := agentBodyFromBlob(raw)
		return upsertAgentBody(projectID, item.AgentName, body, force)
	}
	return fmt.Errorf("unknown migration kind: %s", item.Kind)
}

// upsertKnowledgeEntry creates a new knowledge entry, or — if the
// slug already exists and force=true — overwrites the existing one
// via UpdateEntryHook. Side-effects identical to a regular CRUD call.
func upsertKnowledgeEntry(r *http.Request, projectID int64, typ, slug, title, body string, metadata map[string]any, force bool) error {
	mod, err := knowledge.RouteByType(typ)
	if err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	in := knowledge.Input{
		Slug:     slug,
		Title:    title,
		Body:     body,
		Metadata: metadata,
	}
	if knowledgeSlugExists(projectID, typ, slug) {
		if !force {
			return fmt.Errorf("slug %s already exists", slug)
		}
		if knowledge.UpdateEntryHook == nil {
			return fmt.Errorf("update hook not registered")
		}
		_, err := knowledge.UpdateEntryHook(r, projectID, mod, slug, in)
		return err
	}
	if knowledge.CreateEntryHook == nil {
		return fmt.Errorf("create hook not registered")
	}
	_, err = knowledge.CreateEntryHook(r, projectID, mod, in)
	return err
}

// upsertAgentBody fills `body` on project_agents (creating the row
// if no agent with this name exists). Force=true overwrites; default
// behavior leaves a non-empty body alone.
func upsertAgentBody(projectID int64, agentName, body string, force bool) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	existing := agentBodyByName(projectID, agentName)
	if existing == "" {
		_, err := db.DB.Exec(`
			INSERT INTO project_agents(project_id, name, description, slash_command_name,
			                           lane_tags, metadata, body, bootstrap_steps,
			                           non_negotiable_rules, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(project_id, name) DO UPDATE SET
				body       = excluded.body,
				updated_at = excluded.updated_at
		`, projectID, agentName, "", "", "[]", "{}", body, "[]", "[]", now, now)
		return err
	}
	if !force && strings.TrimSpace(existing) != "" {
		return fmt.Errorf("agent %q already has a body; pass force=true to overwrite", agentName)
	}
	_, err := db.DB.Exec(`
		UPDATE project_agents
		   SET body=?, updated_at=?
		 WHERE project_id=? AND name=?
	`, body, now, projectID, agentName)
	return err
}

// ── helpers ───────────────────────────────────────────────────────────

func knowledgeSlugExists(projectID int64, typ, slug string) bool {
	var n int
	_ = db.DB.QueryRow(`
		SELECT COUNT(*) FROM issues
		 WHERE project_id=? AND type=? AND slug=? AND deleted_at IS NULL
	`, projectID, typ, slug).Scan(&n)
	return n > 0
}

func agentBodyByName(projectID int64, name string) string {
	var body string
	err := db.DB.QueryRow(`SELECT body FROM project_agents WHERE project_id=? AND name=?`, projectID, name).Scan(&body)
	if err != nil {
		return ""
	}
	return body
}

// buildLegacyManifestBody renders the non-reserved keys of the
// manifest into a markdown body. Each key becomes an H2; primitives
// inline, nested objects pretty-printed as a JSON code block.
func buildLegacyManifestBody(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		if strings.HasPrefix(k, "_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("# Legacy manifest\n\nMigrated from `project_manifests.data` (PAI-357). Edit individual sections in the Knowledge tab.\n\n")
	for _, k := range keys {
		sb.WriteString("## ")
		sb.WriteString(k)
		sb.WriteString("\n\n")
		switch v := data[k].(type) {
		case string:
			sb.WriteString(v)
			sb.WriteString("\n\n")
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			sb.WriteString("```json\n")
			sb.Write(b)
			sb.WriteString("\n```\n\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// guardrailContentForIndex pulls the [i]th _guardrails entry,
// returning a renderable title + body + metadata. Falls back to the
// raw JSON when the entry isn't shaped like a typical {title, body}.
func guardrailContentForIndex(data map[string]any, idx int, source string) (title, body string, meta map[string]any) {
	meta = map[string]any{}
	raw, ok := data["_guardrails"]
	if !ok {
		return "Legacy guardrail", "", meta
	}
	arr, _ := raw.([]any)
	if idx < 0 || idx >= len(arr) {
		// Fallback: serialize the whole _guardrails value.
		b, _ := json.MarshalIndent(raw, "", "  ")
		return "Legacy guardrails (unparsed)", "```json\n" + string(b) + "\n```", meta
	}
	entry := arr[idx]
	switch v := entry.(type) {
	case map[string]any:
		title = stringField(v, "title")
		if title == "" {
			title = stringField(v, "name")
		}
		if title == "" {
			title = fmt.Sprintf("Legacy guardrail %d", idx+1)
		}
		body = stringField(v, "body")
		if body == "" {
			body = stringField(v, "rule")
		}
		if body == "" {
			b, _ := json.MarshalIndent(v, "", "  ")
			body = "```json\n" + string(b) + "\n```"
		}
	case string:
		title = fmt.Sprintf("Legacy guardrail %d", idx+1)
		body = v
	default:
		title = fmt.Sprintf("Legacy guardrail %d", idx+1)
		b, _ := json.MarshalIndent(entry, "", "  ")
		body = "```json\n" + string(b) + "\n```"
	}
	return title, body, meta
}

// agentBodyFromBlob coerces _dev / _ops payloads into a markdown
// body suitable for project_agents.body. Strings pass through;
// maps with a `body` field use it; everything else gets JSON-dumped.
func agentBodyFromBlob(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case map[string]any:
		if s := stringField(v, "body"); s != "" {
			return s
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return "```json\n" + string(b) + "\n```"
	default:
		b, _ := json.MarshalIndent(raw, "", "  ")
		return "```json\n" + string(b) + "\n```"
	}
}

// slugAndTitleForGuardrail derives a deterministic slug + title
// from a single _guardrails[i] entry. Used at plan-build time so
// dry-run previews show the exact slugs the apply step will write.
func slugAndTitleForGuardrail(idx int, entry any) (slug, title string) {
	if m, ok := entry.(map[string]any); ok {
		if t := stringField(m, "title"); t != "" {
			return "legacy_guardrail_" + slugifyForKnowledge(t), t
		}
		if t := stringField(m, "name"); t != "" {
			return "legacy_guardrail_" + slugifyForKnowledge(t), t
		}
	}
	return fmt.Sprintf("legacy_guardrail_%d", idx+1), fmt.Sprintf("Legacy guardrail %d", idx+1)
}

// slugifyForKnowledge produces a [a-z][a-z0-9_-]* slug at most
// MaxSlugLen-N chars long (the caller prefixes with `glossary_` or
// `legacy_guardrail_`, so we cap shorter to leave room).
var nonSlugChars = regexp.MustCompile(`[^a-z0-9_-]+`)

func slugifyForKnowledge(s string) string {
	out := strings.ToLower(strings.TrimSpace(s))
	out = nonSlugChars.ReplaceAllString(out, "_")
	out = strings.Trim(out, "_-")
	if out == "" {
		return "x"
	}
	// Slug must start with a letter [a-z]. Prepend `x_` when
	// the cleaned string starts with a digit/dash.
	if !regexp.MustCompile(`^[a-z]`).MatchString(out) {
		out = "x_" + out
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
