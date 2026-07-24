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

	"github.com/inspr-at/paimos/backend/db"
)

func Test_ProjectCounts_OpenIssuesExcludesKnowledge(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "Counts Project", "CNT")

	// 2 open tickets, 4 terminal completed tickets, 1 cancelled ticket.
	for i, status := range []string{"backlog", "in-progress", "done", "delivered", "accepted", "invoiced", "cancelled"} {
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
		t.Errorf("open_issues: got %d, want 2 (backlog + in-progress, excluding terminal statuses and all knowledge types)", got.Counts.OpenIssues)
	}
	if got.Counts.KnowledgeEntries != 2 {
		t.Errorf("knowledge_entries: got %d, want 2 (memory + runbook, excluding cancelled guideline)", got.Counts.KnowledgeEntries)
	}
}

func Test_ListProjects_CountsExcludeKnowledge(t *testing.T) {
	ts := newTestServer(t)
	projectID := createTestProject(t, ts, "List Counts Project", "LCP")

	// Work items: 1 open, 4 terminal completed, 1 cancelled, plus one
	// soft-deleted open ticket that must not inflate list counters.
	for i, status := range []string{"backlog", "done", "delivered", "accepted", "invoiced", "cancelled"} {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title":    fmt.Sprintf("Ticket %d", i),
			"type":     "ticket",
			"status":   status,
			"priority": "medium",
		})
		assertStatus(t, resp, http.StatusCreated)
	}
	deletedResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title":    "Deleted backlog ticket",
		"type":     "ticket",
		"status":   "backlog",
		"priority": "medium",
	})
	assertStatus(t, deletedResp, http.StatusCreated)
	if _, err := db.DB.Exec(
		`UPDATE issues SET deleted_at=datetime('now') WHERE project_id=? AND title='Deleted backlog ticket'`,
		projectID,
	); err != nil {
		t.Fatalf("soft-delete fixture issue: %v", err)
	}

	// Knowledge entries are stored in issues but must not inflate project-list
	// "open" or total issue counters.
	for _, tc := range []struct {
		seg    string
		slug   string
		title  string
		status string
	}{
		{seg: "memory", slug: "list_memory", title: "Memory", status: ""},
		{seg: "runbooks", slug: "list_runbook", title: "Runbook", status: ""},
		{seg: "guidelines", slug: "list_guideline", title: "Guideline", status: "cancelled"},
	} {
		body := map[string]any{"slug": tc.slug, "title": tc.title, "body": "b"}
		if tc.status != "" {
			body["status"] = tc.status
		}
		assertStatus(t, ts.post(t, knowledgeURL(projectID, tc.seg), ts.adminCookie, body), http.StatusCreated)
	}

	resp := ts.get(t, "/api/projects", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	var projects []struct {
		Key              string `json:"key"`
		IssueCount       int    `json:"issue_count"`
		OpenIssueCount   int    `json:"open_issue_count"`
		DoneIssueCount   int    `json:"done_issue_count"`
		ActiveIssueCount int    `json:"active_issue_count"`
	}
	decode(t, resp, &projects)

	var got *struct {
		Key              string `json:"key"`
		IssueCount       int    `json:"issue_count"`
		OpenIssueCount   int    `json:"open_issue_count"`
		DoneIssueCount   int    `json:"done_issue_count"`
		ActiveIssueCount int    `json:"active_issue_count"`
	}
	for i := range projects {
		if projects[i].Key == "LCP" {
			got = &projects[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("project LCP missing from /api/projects response: %+v", projects)
	}
	if got.IssueCount != 6 {
		t.Errorf("issue_count: got %d, want 6 work items (knowledge entries and deleted rows excluded)", got.IssueCount)
	}
	if got.OpenIssueCount != 1 {
		t.Errorf("open_issue_count: got %d, want 1 open work item (knowledge entries, terminal statuses, and deleted rows excluded)", got.OpenIssueCount)
	}
	if got.DoneIssueCount != 4 {
		t.Errorf("done_issue_count: got %d, want 4 completed work items", got.DoneIssueCount)
	}
	if got.ActiveIssueCount != 5 {
		t.Errorf("active_issue_count: got %d, want 5 non-cancelled work items", got.ActiveIssueCount)
	}

	detailResp := ts.get(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie)
	assertStatus(t, detailResp, http.StatusOK)
	var detail struct {
		IssueCount       int `json:"issue_count"`
		OpenIssueCount   int `json:"open_issue_count"`
		DoneIssueCount   int `json:"done_issue_count"`
		ActiveIssueCount int `json:"active_issue_count"`
		Counts           struct {
			OpenIssues       int `json:"open_issues"`
			KnowledgeEntries int `json:"knowledge_entries"`
		} `json:"counts"`
	}
	decode(t, detailResp, &detail)
	if detail.IssueCount != got.IssueCount ||
		detail.OpenIssueCount != got.OpenIssueCount ||
		detail.DoneIssueCount != got.DoneIssueCount ||
		detail.ActiveIssueCount != got.ActiveIssueCount {
		t.Errorf("project detail counters = issue:%d open:%d done:%d active:%d, want list counters issue:%d open:%d done:%d active:%d",
			detail.IssueCount, detail.OpenIssueCount, detail.DoneIssueCount, detail.ActiveIssueCount,
			got.IssueCount, got.OpenIssueCount, got.DoneIssueCount, got.ActiveIssueCount)
	}
	if detail.Counts.OpenIssues != 1 || detail.Counts.KnowledgeEntries != 2 {
		t.Errorf("project detail counts block = %+v, want open_issues=1 knowledge_entries=2", detail.Counts)
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
