// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// PAI-343 — CLI side of the lesson-capture flow.
//
// Two helpers:
//
//   1. maybePrintLessonCaptureHint — non-blocking hint printed after a
//      terminal-state transition when --draft-memory was NOT passed.
//      Calls the read-only GET /lesson-capture-prompt endpoint and
//      prints a one-liner pointing the user at the flag. Silently
//      no-ops on any error (network, 404, etc.) so a flaky network
//      never blocks ticket close.
//
//   2. runDraftMemoryFlow — agent-friendly headless capture. Reads
//      the --memory-* flags, fetches the ticket once for project +
//      key, POSTs the memory creation, then POSTs the
//      applies_to_memory relation (mirrors what the UI's modal does).

// maybePrintLessonCaptureHint asks the server whether this ticket
// qualifies for the lesson-capture prompt and, if so, prints a hint
// nudging the operator at the --draft-memory flag. Always non-blocking.
func maybePrintLessonCaptureHint(client *Client, ref string) {
	body, err := client.do("GET", "/api/issues/"+url.PathEscape(ref)+"/lesson-capture-prompt", nil)
	if err != nil {
		return // silent — purely advisory
	}
	var resp struct {
		ShouldPrompt  bool   `json:"should_prompt"`
		Reason        string `json:"reason"`
		SuggestedName string `json:"suggested_name"`
		TicketKey     string `json:"ticket_key"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}
	if !resp.ShouldPrompt {
		return
	}
	fmt.Fprintf(stdout, "💡 hint: this ticket may teach a lesson (%s).\n", resp.Reason)
	fmt.Fprintf(stdout,
		"     Re-run with --draft-memory --memory-rule \"...\" --memory-why-file ... --memory-how-file ... to capture it as %q.\n",
		resp.SuggestedName)
}

// runDraftMemoryFlow performs the headless lesson-capture submission:
//  1. Resolve the ticket's project_id + key by GETting it.
//  2. POST /api/projects/:id/memory with the form data.
//  3. POST /api/issues/:ticket-id/relations with type=applies_to_memory.
//
// Returns the first error encountered. Failure during the relation
// link is non-fatal — we print a warning and return nil so the caller
// considers the flow successful enough (the memory exists).
func runDraftMemoryFlow(client *Client, ref, rule, why, whyFile, how, howFile, memType, tagsCSV, slug string) error {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return &usageError{msg: "--draft-memory requires --memory-rule (one-sentence rule)"}
	}
	whyVal, _, err := readMultilineInput(why, whyFile, "memory-why")
	if err != nil {
		return err
	}
	if strings.TrimSpace(whyVal) == "" {
		return &usageError{msg: "--draft-memory requires --memory-why or --memory-why-file"}
	}
	howVal, _, err := readMultilineInput(how, howFile, "memory-how")
	if err != nil {
		return err
	}
	if strings.TrimSpace(howVal) == "" {
		return &usageError{msg: "--draft-memory requires --memory-how or --memory-how-file"}
	}
	memType = strings.ToLower(strings.TrimSpace(memType))
	if memType == "" {
		memType = "feedback"
	}
	switch memType {
	case "feedback", "project", "reference":
	default:
		return &usageError{msg: "--memory-type must be one of: feedback, project, reference"}
	}
	if strings.TrimSpace(slug) == "" {
		slug = suggestMemorySlug(memType, rule)
	}

	// Resolve ticket → project_id + issue_key. The server's
	// /api/issues/:ref endpoint accepts both numeric ids and PAI-NNN
	// keys, so the caller's ref doesn't need to be parsed here.
	tBody, err := client.do("GET", "/api/issues/"+url.PathEscape(ref), nil)
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	var ticket struct {
		ID        int64  `json:"id"`
		ProjectID *int64 `json:"project_id"`
		IssueKey  string `json:"issue_key"`
	}
	if err := json.Unmarshal(tBody, &ticket); err != nil {
		return fmt.Errorf("decode ticket: %w", err)
	}
	if ticket.ProjectID == nil {
		return fmt.Errorf("ticket %s has no project_id (cannot create memory)", ref)
	}

	// Build the memory body — same shape the UI modal uses so the
	// "## Why / ## How to apply" sections render identically in the
	// Knowledge tab regardless of which surface authored them.
	memBody := fmt.Sprintf("## Why\n\n%s\n\n## How to apply\n\n%s\n",
		strings.TrimSpace(whyVal), strings.TrimSpace(howVal))

	tags := splitAndTrim(tagsCSV, ",")
	if tags == nil {
		tags = []string{}
	}
	originating := []map[string]any{}
	if ticket.IssueKey != "" {
		originating = append(originating, map[string]any{
			"key":          ticket.IssueKey,
			"instance_url": client.baseURL,
		})
	}

	createBody := map[string]any{
		"slug":  slug,
		"title": rule,
		"body":  memBody,
		"metadata": map[string]any{
			"type":                 memType,
			"tags":                 tags,
			"originating_tickets":  originating,
		},
	}
	memPath := fmt.Sprintf("/api/projects/%d/memory", *ticket.ProjectID)
	memRaw, err := client.do("POST", memPath, createBody)
	if err != nil {
		return fmt.Errorf("create memory: %w", err)
	}
	var mem struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	_ = json.Unmarshal(memRaw, &mem)

	// Bidirectional link via the existing relation endpoint. Soft
	// failure — the memory exists even if linking didn't take.
	if _, err := client.do("POST",
		"/api/issues/"+url.PathEscape(ref)+"/relations",
		map[string]any{"target_id": mem.ID, "type": "applies_to_memory"},
	); err != nil {
		fmt.Fprintf(stdout, "⚠ memory %q created but relation link failed: %v\n", mem.Slug, err)
		return nil
	}

	if flagJSON {
		return emitJSON(map[string]any{
			"ok":             true,
			"memory_id":      mem.ID,
			"memory_slug":    mem.Slug,
			"linked_to":      ref,
			"link_relation":  "applies_to_memory",
		})
	}
	fmt.Fprintf(stdout, "✓ captured lesson as memory %q (linked to %s)\n", mem.Slug, ref)
	return nil
}

// suggestMemorySlug builds the canonical "<type>_<first-six-words>"
// slug the modal + the backend's SuggestMemorySlug also use. Mirrors
// the JS / Go versions on purpose so the suggestion is identical no
// matter which surface generates it. Pure function — no I/O.
func suggestMemorySlug(memType, rule string) string {
	mt := strings.ToLower(strings.TrimSpace(memType))
	if mt == "" {
		mt = "feedback"
	}
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return mt + "_lesson"
	}
	fields := strings.FieldsFunc(rule, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	if len(fields) > 6 {
		fields = fields[:6]
	}
	for i, f := range fields {
		fields[i] = strings.ToLower(f)
	}
	tail := strings.Join(fields, "_")
	if tail == "" {
		tail = "lesson"
	}
	return mt + "_" + tail
}

// splitAndTrim is a tiny helper that splits raw on sep, trims each
// element, and drops empties. Returns nil for an empty / whitespace-
// only input so the caller can omit the field cleanly.
func splitAndTrim(raw, sep string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
