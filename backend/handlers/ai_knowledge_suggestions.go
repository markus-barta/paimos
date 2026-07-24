// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/inspr-at/paimos/backend/db"
)

type aiKnowledgeSuggestion struct {
	Ref                string   `json:"ref"`
	Type               string   `json:"type"`
	Slug               string   `json:"slug"`
	Title              string   `json:"title"`
	Status             string   `json:"status"`
	Revision           string   `json:"revision"`
	SuggestedUse       string   `json:"suggested_use"`
	PromptPreset       bool     `json:"prompt_preset"`
	PromptPresetRef    string   `json:"prompt_preset_ref,omitempty"`
	PromptPresetLabel  string   `json:"prompt_preset_label,omitempty"`
	PromptPresetStatus string   `json:"prompt_preset_status,omitempty"`
	Actions            []string `json:"actions,omitempty"`
}

func listAIKnowledgeSuggestions(projectID int64) ([]aiKnowledgeSuggestion, error) {
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
	  ORDER BY updated_at DESC, id DESC
		 LIMIT 24
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []aiKnowledgeSuggestion{}
	for rows.Next() {
		var (
			id               int64
			typ, slug, title string
			body             string
			entryStatus      string
			metaRaw          string
			updatedAt        string
			contentRevisedAt string
		)
		if err := rows.Scan(&id, &typ, &slug, &title, &body, &entryStatus, &metaRaw, &updatedAt, &contentRevisedAt); err != nil {
			return nil, err
		}
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		ref := fmt.Sprintf("kb:%s:%s", typ, slug)
		suggestion := aiKnowledgeSuggestion{
			Ref:          ref,
			Type:         typ,
			Slug:         slug,
			Title:        title,
			Status:       entryStatus,
			Revision:     aiPromptPresetRevision(id, updatedAt, contentRevisedAt, body),
			SuggestedUse: "context",
		}
		if _, allowed := aiPromptPresetAllowedTypes[typ]; allowed {
			meta, exists, err := parseAIPromptPresetMeta(metaRaw, title)
			if err == nil && exists {
				suggestion.PromptPreset = true
				suggestion.PromptPresetRef = ref
				suggestion.PromptPresetLabel = meta.Label
				suggestion.PromptPresetStatus = meta.Status
				suggestion.Actions = append([]string(nil), meta.Actions...)
				if meta.Status == "active" && len(body) <= aiPromptPresetMaxBodyBytes {
					suggestion.SuggestedUse = "prompt"
				}
			}
		}
		out = append(out, suggestion)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SuggestedUse != out[j].SuggestedUse {
			return out[i].SuggestedUse == "prompt"
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}
