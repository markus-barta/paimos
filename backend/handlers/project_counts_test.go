// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

// PAI-356 — pin the GET /api/projects/:id `counts` aggregate.
// `open_issues` excludes knowledge entries (they live in the same
// table since PAI-346); `knowledge_entries` excludes cancelled rows
// and counts all five knowledge types together.

import (
	"fmt"
	"net/http"
	"testing"
)

func Test_ProjectCounts_OpenIssuesExcludesKnowledge(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Counts Project", "CNT")

	// 2 open tickets, 1 done ticket, 1 cancelled ticket.
	for i, status := range []string{"backlog", "in-progress", "done", "cancelled"} {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title":    fmt.Sprintf("Ticket %d", i),
			"type":     "ticket",
			"status":   status,
			"priority": "medium",
		})
		assertStatus(t, resp, http.StatusCreated)
	}

	// 1 memory + 1 runbook + 1 cancelled guideline.
	memResp := ts.post(t, knowledgeURL(projectID, "memory"), ts.adminCookie, map[string]any{
		"slug": "first_memory", "title": "M", "body": "b",
	})
	assertStatus(t, memResp, http.StatusCreated)
	rbResp := ts.post(t, knowledgeURL(projectID, "runbooks"), ts.adminCookie, map[string]any{
		"slug": "first_runbook", "title": "R", "body": "b",
	})
	assertStatus(t, rbResp, http.StatusCreated)
	glResp := ts.post(t, knowledgeURL(projectID, "guidelines"), ts.adminCookie, map[string]any{
		"slug": "first_guideline", "title": "G", "body": "b", "status": "cancelled",
	})
	assertStatus(t, glResp, http.StatusCreated)

	resp := ts.get(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	var got struct {
		Counts struct {
			OpenIssues       int `json:"open_issues"`
			KnowledgeEntries int `json:"knowledge_entries"`
		} `json:"counts"`
	}
	decode(t, resp, &got)

	if got.Counts.OpenIssues != 2 {
		t.Errorf("open_issues: got %d, want 2 (backlog + in_progress, excluding done/cancelled and all knowledge types)", got.Counts.OpenIssues)
	}
	if got.Counts.KnowledgeEntries != 2 {
		t.Errorf("knowledge_entries: got %d, want 2 (memory + runbook, excluding cancelled guideline)", got.Counts.KnowledgeEntries)
	}
}

func Test_ProjectCounts_EmptyProjectIsZero(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Empty Counts", "ECT")

	resp := ts.get(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	var got struct {
		Counts struct {
			OpenIssues       int `json:"open_issues"`
			KnowledgeEntries int `json:"knowledge_entries"`
		} `json:"counts"`
	}
	decode(t, resp, &got)

	if got.Counts.OpenIssues != 0 || got.Counts.KnowledgeEntries != 0 {
		t.Errorf("empty project counts: got %+v, want both zero", got.Counts)
	}
}
