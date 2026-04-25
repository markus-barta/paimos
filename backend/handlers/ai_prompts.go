// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-175. CRUD endpoints for the AI prompt store (M78 ai_prompts).
//
// Built-in vs custom rows
// -----------------------
//   Built-in rows (is_builtin = 1)
//     - One per registered action key. Seeded on first list call by
//       reading the action registry — keeps the seed in lockstep
//       with what the dispatcher actually serves.
//     - Editable: prompt_template, enabled. Other fields locked.
//     - Reset endpoint clears prompt_template back to empty so the
//       handler falls back to the code-defined default.
//
//   Custom rows (is_builtin = 0)
//     - Admin-created. All fields editable; deletable.
//     - Custom keys must match the validation regex (lowercase
//       a-z, digits, underscore; 3-32 chars) so they're stable
//       enough to grep for in audit lines.
//
// Why prompts are seeded lazily, not by migration
// -----------------------------------------------
// Action keys are registered at init() in their own files. A
// migration that hard-codes the seed list goes stale every time we
// add an action; the lazy seed introspects the action registry on
// every list-call (cheap; happens at most once per admin click on
// the prompts tab) and is therefore self-updating.

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/markus-barta/paimos/backend/db"
)

// promptRow mirrors the wire shape served to the SPA.
type promptRow struct {
	ID                  int64  `json:"id"`
	Key                 string `json:"key"`
	Label               string `json:"label"`
	Surface             string `json:"surface"`
	ParentAction        string `json:"parent_action,omitempty"`
	SubAction           string `json:"sub_action,omitempty"`
	PromptTemplate      string `json:"prompt_template"`
	Enabled             bool   `json:"enabled"`
	IsBuiltin           bool   `json:"is_builtin"`
	DefaultTemplateHash string `json:"default_template_hash,omitempty"`
	UpdatedAt           string `json:"updated_at"`
}

// validCustomKey matches stable, grep-friendly action keys. Refusing
// uppercase + spaces avoids audit-line ambiguity ("action=foo Bar"
// would split mid-line).
var validCustomKey = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

// AIListPrompts handles GET /api/ai/prompts. Admin-only. Lazily
// seeds built-in rows for any registered action that doesn't yet
// have a row, so a fresh install / a recently-added action surfaces
// without a manual migration.
func AIListPrompts(w http.ResponseWriter, r *http.Request) {
	if err := seedBuiltinPrompts(); err != nil {
		log.Printf("ai_prompts: seed: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows, err := readAllPromptRows()
	if err != nil {
		log.Printf("ai_prompts: list: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"prompts": rows})
}

// seedBuiltinPrompts walks the action registry and ensures one
// is_builtin row exists per key. We DO NOT delete rows for actions
// that disappear — a feature-flagged-off action should keep its
// admin-edited prompt for when it returns.
func seedBuiltinPrompts() error {
	for _, d := range actionRegistry {
		if !d.Implemented {
			// Stubs aren't yet useful as configurable prompts; only
			// seed once the action's real handler ships.
			continue
		}
		_, err := db.DB.Exec(
			`INSERT OR IGNORE INTO ai_prompts(key, label, surface, parent_action, sub_action, is_builtin)
			 VALUES (?, ?, ?, ?, ?, 1)`,
			d.Key, d.Label, d.Surface, "", "",
		)
		if err != nil {
			return fmt.Errorf("seed %q: %w", d.Key, err)
		}
	}
	return nil
}

func readAllPromptRows() ([]promptRow, error) {
	const q = `
SELECT id, key, label, surface, COALESCE(parent_action,''), COALESCE(sub_action,''),
       prompt_template, enabled, is_builtin, default_template_hash, updated_at
FROM ai_prompts
ORDER BY surface ASC, label ASC, key ASC
`
	rows, err := db.DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []promptRow
	for rows.Next() {
		var r promptRow
		var enabled, builtin int
		if err := rows.Scan(
			&r.ID, &r.Key, &r.Label, &r.Surface, &r.ParentAction, &r.SubAction,
			&r.PromptTemplate, &enabled, &builtin, &r.DefaultTemplateHash, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		r.IsBuiltin = builtin == 1
		out = append(out, r)
	}
	return out, nil
}

type promptUpdatePayload struct {
	Label          *string `json:"label,omitempty"`
	Surface        *string `json:"surface,omitempty"`
	ParentAction   *string `json:"parent_action,omitempty"`
	SubAction      *string `json:"sub_action,omitempty"`
	PromptTemplate *string `json:"prompt_template,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

// AIUpdatePrompt handles PUT /api/ai/prompts/{id}.
// Built-in rows: only prompt_template + enabled mutate.
// Custom rows: everything except is_builtin can change.
func AIUpdatePrompt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var existing promptRow
	if err := readPromptRowByID(id, &existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "prompt not found", http.StatusNotFound)
			return
		}
		log.Printf("ai_prompts: read: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var p promptUpdatePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if existing.IsBuiltin {
		// Built-in: lock most fields. Returning a clear 400 beats
		// silently dropping the field on the floor — admins should
		// know they need a custom row to change label/surface.
		if p.Label != nil || p.Surface != nil || p.ParentAction != nil || p.SubAction != nil {
			jsonError(w, "built-in rows: only prompt_template and enabled are mutable", http.StatusBadRequest)
			return
		}
	} else {
		if p.Surface != nil && *p.Surface != "issue" && *p.Surface != "customer" {
			jsonError(w, "surface must be \"issue\" or \"customer\"", http.StatusBadRequest)
			return
		}
	}

	updates, args := buildUpdate(existing, p)
	if len(updates) == 0 {
		jsonOK(w, existing)
		return
	}
	updates = append(updates, "updated_at = datetime('now')")
	args = append(args, id)
	q := "UPDATE ai_prompts SET " + strings.Join(updates, ", ") + " WHERE id = ?"
	if _, err := db.DB.Exec(q, args...); err != nil {
		log.Printf("ai_prompts: update: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	var updated promptRow
	_ = readPromptRowByID(id, &updated)
	jsonOK(w, updated)
}

func buildUpdate(existing promptRow, p promptUpdatePayload) (sets []string, args []any) {
	if p.Label != nil && !existing.IsBuiltin {
		sets = append(sets, "label = ?")
		args = append(args, *p.Label)
	}
	if p.Surface != nil && !existing.IsBuiltin {
		sets = append(sets, "surface = ?")
		args = append(args, *p.Surface)
	}
	if p.ParentAction != nil && !existing.IsBuiltin {
		sets = append(sets, "parent_action = ?")
		args = append(args, *p.ParentAction)
	}
	if p.SubAction != nil && !existing.IsBuiltin {
		sets = append(sets, "sub_action = ?")
		args = append(args, *p.SubAction)
	}
	if p.PromptTemplate != nil {
		sets = append(sets, "prompt_template = ?")
		args = append(args, *p.PromptTemplate)
	}
	if p.Enabled != nil {
		v := 0
		if *p.Enabled {
			v = 1
		}
		sets = append(sets, "enabled = ?")
		args = append(args, v)
	}
	return sets, args
}

// AICreatePrompt handles POST /api/ai/prompts (custom rows only).
type promptCreatePayload struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Surface        string `json:"surface"`
	ParentAction   string `json:"parent_action"`
	SubAction      string `json:"sub_action"`
	PromptTemplate string `json:"prompt_template"`
	Enabled        bool   `json:"enabled"`
}

func AICreatePrompt(w http.ResponseWriter, r *http.Request) {
	var p promptCreatePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	p.Key = strings.TrimSpace(p.Key)
	p.Label = strings.TrimSpace(p.Label)
	p.Surface = strings.TrimSpace(p.Surface)
	if !validCustomKey.MatchString(p.Key) {
		jsonError(w, "key must match ^[a-z][a-z0-9_]{2,31}$", http.StatusBadRequest)
		return
	}
	if p.Label == "" {
		jsonError(w, "label required", http.StatusBadRequest)
		return
	}
	if p.Surface != "issue" && p.Surface != "customer" {
		jsonError(w, "surface must be \"issue\" or \"customer\"", http.StatusBadRequest)
		return
	}

	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	res, err := db.DB.Exec(
		`INSERT INTO ai_prompts(key, label, surface, parent_action, sub_action, prompt_template, enabled, is_builtin)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		p.Key, p.Label, p.Surface, p.ParentAction, p.SubAction, p.PromptTemplate, enabled,
	)
	if err != nil {
		// SQLite UNIQUE violation surfaces as 409 — clearer than 500.
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "key already exists", http.StatusConflict)
			return
		}
		log.Printf("ai_prompts: create: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	var created promptRow
	_ = readPromptRowByID(id, &created)
	jsonOK(w, created)
}

func AIDeletePrompt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var existing promptRow
	if err := readPromptRowByID(id, &existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "prompt not found", http.StatusNotFound)
			return
		}
		log.Printf("ai_prompts: read: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existing.IsBuiltin {
		jsonError(w, "built-in rows cannot be deleted; use reset to clear the override", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec(`DELETE FROM ai_prompts WHERE id = ?`, id); err != nil {
		log.Printf("ai_prompts: delete: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func AIResetPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var existing promptRow
	if err := readPromptRowByID(id, &existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "prompt not found", http.StatusNotFound)
			return
		}
		log.Printf("ai_prompts: read: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !existing.IsBuiltin {
		jsonError(w, "reset only applies to built-in rows", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec(
		`UPDATE ai_prompts SET prompt_template = '', updated_at = datetime('now') WHERE id = ?`, id,
	); err != nil {
		log.Printf("ai_prompts: reset: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	var updated promptRow
	_ = readPromptRowByID(id, &updated)
	jsonOK(w, updated)
}

func readPromptRowByID(id int64, into *promptRow) error {
	const q = `
SELECT id, key, label, surface, COALESCE(parent_action,''), COALESCE(sub_action,''),
       prompt_template, enabled, is_builtin, default_template_hash, updated_at
FROM ai_prompts WHERE id = ?
`
	var enabled, builtin int
	err := db.DB.QueryRow(q, id).Scan(
		&into.ID, &into.Key, &into.Label, &into.Surface, &into.ParentAction, &into.SubAction,
		&into.PromptTemplate, &enabled, &builtin, &into.DefaultTemplateHash, &into.UpdatedAt,
	)
	if err != nil {
		return err
	}
	into.Enabled = enabled == 1
	into.IsBuiltin = builtin == 1
	return nil
}

// PAI-177. AIDryRunPrompt handles POST /api/ai/prompts/{id}/dry-run.
// Renders the template against a real issue (specified by issue_id
// in the request body) and calls the LLM. Returns BOTH the
// rendered prompt and the LLM response side-by-side. NO state
// changes — strictly preview.
type dryRunRequest struct {
	IssueID int64 `json:"issue_id"`
}
type dryRunResponse struct {
	RenderedSystem    string `json:"rendered_system"`
	RenderedUser      string `json:"rendered_user"`
	Response          string `json:"response"`
	Model             string `json:"model"`
	LatencyMs         int64  `json:"latency_ms"`
	PromptTokens      int    `json:"prompt_tokens"`
	CompletionTokens  int    `json:"completion_tokens"`
	UsedDefault       bool   `json:"used_default"`
}

func AIDryRunPrompt(w http.ResponseWriter, r *http.Request) {
	// PAI-177 lands in a follow-on commit that wires the rendered
	// prompt through provider.Optimize against an issue selected by
	// the admin. Stubbed for now so the endpoint exists and the
	// settings UI can render its form against a known shape.
	jsonError(w, "dry-run preview lands with PAI-177 — stay tuned", http.StatusNotImplemented)
}

// helpers used by shape pinning + tests
var _ = errors.New
var _ = strconv.Atoi
var _ = fmt.Errorf
