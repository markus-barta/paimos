// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/inspr-at/paimos/backend/db"
)

const (
	aiPromptPresetMetaKey      = "ai_prompt_preset"
	aiPromptPresetDefaultRef   = "default"
	aiPromptPresetMaxBodyBytes = 12_000
)

var aiPromptPresetAllowedTypes = map[string]struct{}{
	"memory":    {},
	"runbook":   {},
	"guideline": {},
}

type aiPromptPresetChoice struct {
	Ref      string   `json:"ref"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Slug     string   `json:"slug"`
	Status   string   `json:"status"`
	Revision string   `json:"revision"`
	Actions  []string `json:"actions"`
}

type aiPromptPresetRecord struct {
	aiPromptPresetChoice
	Body string `json:"-"`
}

type aiPromptPresetMeta struct {
	Enabled bool
	Label   string
	Status  string
	Actions []string
}

func listProjectAIPromptPresets(projectID int64) ([]aiPromptPresetChoice, error) {
	if projectID <= 0 {
		return nil, nil
	}
	rows, err := db.DB.Query(`
		SELECT id, type, COALESCE(slug,''), title, description, status,
		       COALESCE(category_metadata,''), COALESCE(updated_at,''), COALESCE(content_revised_at,'')
		  FROM issues
		 WHERE project_id = ?
		   AND type IN ('memory','runbook','guideline')
		   AND deleted_at IS NULL
		   AND slug IS NOT NULL
		   AND status != 'cancelled'
	  ORDER BY type ASC, slug ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []aiPromptPresetChoice{}
	for rows.Next() {
		rec, ok, err := scanAIPromptPreset(rows)
		if err != nil {
			// Invalid metadata makes the entry unavailable as a prompt
			// preset, but should not break the whole options endpoint.
			continue
		}
		if !ok || !rec.usableForList() {
			continue
		}
		out = append(out, rec.aiPromptPresetChoice)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Label == out[j].Label {
			return out[i].Ref < out[j].Ref
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

func resolveProjectAIPromptPreset(projectID *int64, actionKey, ref string) (aiPromptPresetRecord, *userError) {
	typ, slug, ok := splitAIPromptPresetRef(ref)
	if !ok {
		return aiPromptPresetRecord{}, &userError{status: 400, msg: "prompt preset is not available for AI actions"}
	}
	if projectID == nil || *projectID <= 0 {
		return aiPromptPresetRecord{}, &userError{status: 400, msg: "prompt preset requires a project-scoped AI action"}
	}
	row := db.DB.QueryRow(`
		SELECT id, type, COALESCE(slug,''), title, description, status,
		       COALESCE(category_metadata,''), COALESCE(updated_at,''), COALESCE(content_revised_at,'')
		  FROM issues
		 WHERE project_id = ?
		   AND type = ?
		   AND slug = ?
		   AND deleted_at IS NULL
	`, *projectID, typ, slug)
	rec, exists, err := scanAIPromptPreset(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return aiPromptPresetRecord{}, &userError{status: 400, msg: "prompt preset is not available for AI actions"}
		}
		return aiPromptPresetRecord{}, &userError{status: 500, msg: "internal error"}
	}
	if !exists || !rec.usableForAction(actionKey) {
		return aiPromptPresetRecord{}, &userError{status: 400, msg: "prompt preset is not available for this action"}
	}
	return rec, nil
}

func applyAIPromptPreset(base string, ax *aiActionContext) string {
	body := strings.TrimSpace(ax.Options.promptPresetBody)
	if body == "" || ax.Options.PromptPresetRef == aiPromptPresetDefaultRef {
		return base
	}
	label := strings.TrimSpace(ax.Options.promptPresetLabel)
	if label == "" {
		label = ax.Options.PromptPresetRef
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\nProject prompt preset: ")
	b.WriteString(label)
	b.WriteString(" (")
	b.WriteString(ax.Options.PromptPresetRef)
	b.WriteString(")\n")
	b.WriteString(body)
	return b.String()
}

func resolveActionPromptWithPreset(ax *aiActionContext, key string) string {
	return applyAIPromptPreset(resolveActionPrompt(key), ax)
}

type aiPromptPresetScanner interface {
	Scan(dest ...any) error
}

func scanAIPromptPreset(s aiPromptPresetScanner) (aiPromptPresetRecord, bool, error) {
	var (
		id               int64
		typ, slug, title string
		body             string
		entryStatus      string
		metaRaw          string
		updatedAt        string
		contentRevisedAt string
	)
	if err := s.Scan(&id, &typ, &slug, &title, &body, &entryStatus, &metaRaw, &updatedAt, &contentRevisedAt); err != nil {
		return aiPromptPresetRecord{}, false, err
	}
	if _, ok := aiPromptPresetAllowedTypes[typ]; !ok {
		return aiPromptPresetRecord{}, false, nil
	}
	meta, exists, err := parseAIPromptPresetMeta(metaRaw, title)
	if err != nil || !exists {
		return aiPromptPresetRecord{}, exists, err
	}
	revision := aiPromptPresetRevision(id, updatedAt, contentRevisedAt, body)
	rec := aiPromptPresetRecord{
		aiPromptPresetChoice: aiPromptPresetChoice{
			Ref:      fmt.Sprintf("kb:%s:%s", typ, slug),
			Label:    meta.Label,
			Type:     typ,
			Slug:     slug,
			Status:   meta.Status,
			Revision: revision,
			Actions:  append([]string(nil), meta.Actions...),
		},
		Body: body,
	}
	if entryStatus == "cancelled" {
		rec.Status = "archived"
	}
	return rec, true, nil
}

func (r aiPromptPresetRecord) usableForList() bool {
	return r.Status == "active" &&
		strings.TrimSpace(r.Body) != "" &&
		len(r.Body) <= aiPromptPresetMaxBodyBytes
}

func (r aiPromptPresetRecord) usableForAction(actionKey string) bool {
	if !r.usableForList() {
		return false
	}
	return aiPromptPresetAppliesTo(r.Actions, actionKey)
}

func parseAIPromptPresetMeta(raw string, fallbackLabel string) (aiPromptPresetMeta, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return aiPromptPresetMeta{}, false, nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return aiPromptPresetMeta{}, false, err
	}
	rawPreset, exists := meta[aiPromptPresetMetaKey]
	if !exists {
		return aiPromptPresetMeta{}, false, nil
	}
	preset := aiPromptPresetMeta{
		Enabled: true,
		Label:   strings.TrimSpace(fallbackLabel),
		Status:  "active",
		Actions: []string{"*"},
	}
	switch v := rawPreset.(type) {
	case bool:
		preset.Enabled = v
	case map[string]any:
		if rawEnabled, ok := v["enabled"]; ok {
			enabled, ok := rawEnabled.(bool)
			if !ok {
				return aiPromptPresetMeta{}, true, fmt.Errorf("%s.enabled must be boolean", aiPromptPresetMetaKey)
			}
			preset.Enabled = enabled
		}
		if rawLabel, ok := v["label"]; ok {
			label, ok := rawLabel.(string)
			if !ok {
				return aiPromptPresetMeta{}, true, fmt.Errorf("%s.label must be string", aiPromptPresetMetaKey)
			}
			if trimmed := strings.TrimSpace(label); trimmed != "" {
				preset.Label = trimmed
			}
		}
		if rawStatus, ok := v["status"]; ok {
			status, ok := rawStatus.(string)
			if !ok {
				return aiPromptPresetMeta{}, true, fmt.Errorf("%s.status must be string", aiPromptPresetMetaKey)
			}
			status = strings.ToLower(strings.TrimSpace(status))
			if status != "active" && status != "draft" && status != "archived" {
				return aiPromptPresetMeta{}, true, fmt.Errorf("%s.status must be active, draft, or archived", aiPromptPresetMetaKey)
			}
			preset.Status = status
		}
		if rawActions, ok := v["actions"]; ok {
			actions, err := normalizeAIPromptPresetActions(rawActions)
			if err != nil {
				return aiPromptPresetMeta{}, true, err
			}
			preset.Actions = actions
		}
	default:
		return aiPromptPresetMeta{}, true, fmt.Errorf("%s must be an object or boolean", aiPromptPresetMetaKey)
	}
	if !preset.Enabled {
		preset.Status = "archived"
	}
	if preset.Label == "" {
		preset.Label = "Prompt preset"
	}
	return preset, true, nil
}

func normalizeAIPromptPresetActions(raw any) ([]string, error) {
	add := func(out []string, value string) []string {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "all" {
				part = "*"
			}
			if part == "" {
				continue
			}
			out = append(out, part)
		}
		return out
	}
	out := []string{}
	switch v := raw.(type) {
	case string:
		out = add(out, v)
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s.actions must contain strings", aiPromptPresetMetaKey)
			}
			out = add(out, s)
		}
	default:
		return nil, fmt.Errorf("%s.actions must be an array or comma-separated string", aiPromptPresetMetaKey)
	}
	if len(out) == 0 {
		out = []string{"*"}
	}
	return uniqueSortedStrings(out), nil
}

func aiPromptPresetAppliesTo(actions []string, actionKey string) bool {
	actionKey = strings.ToLower(strings.TrimSpace(actionKey))
	for _, action := range actions {
		if action == "*" || action == actionKey {
			return true
		}
	}
	return false
}

func splitAIPromptPresetRef(ref string) (typ, slug string, ok bool) {
	ref = strings.TrimSpace(ref)
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	parts := strings.Split(ref, ":")
	if len(parts) != 3 || parts[0] != "kb" {
		return "", "", false
	}
	typ = strings.TrimSpace(parts[1])
	slug = strings.TrimSpace(parts[2])
	if _, allowed := aiPromptPresetAllowedTypes[typ]; !allowed || slug == "" {
		return "", "", false
	}
	return typ, slug, true
}

func aiPromptPresetRevision(id int64, updatedAt, contentRevisedAt, body string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", id, updatedAt, contentRevisedAt, body)))
	return hex.EncodeToString(sum[:])[:12]
}

func uniqueSortedStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
