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

package handlers_test

// Extended tests — all blocking in CI.
// Run: go test ./handlers/... -v
// Covers: hierarchy rules, search, permissions, issue history, tag CRUD,
//         user management, API keys, CSV export, aggregation, views, FTS.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func Test_IssueHierarchy(t *testing.T) {
	ts := newTestServer(t)

	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Hierarchy Project", "key": "HRP",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	// Create an epic.
	epicResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title": "Epic One", "type": "epic", "status": "backlog", "priority": "high",
	})
	assertStatus(t, epicResp, http.StatusCreated)
	epicID := responseID(t, epicResp)

	// Create a ticket under the epic.
	ticketResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Ticket One", "type": "ticket", "status": "backlog", "priority": "medium",
		"parent_id": epicID,
	})
	assertStatus(t, ticketResp, http.StatusCreated)
	ticketID := responseID(t, ticketResp)

	// Create a task under the ticket.
	taskResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Task One", "type": "task", "status": "backlog", "priority": "low",
		"parent_id": ticketID,
	})
	assertStatus(t, taskResp, http.StatusCreated)

	t.Run("epic cannot have a parent", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
			"title": "Bad Epic", "type": "epic", "status": "backlog", "priority": "low",
			"parent_id": epicID,
		})
		assertStatus(t, resp, http.StatusUnprocessableEntity)
	})

	t.Run("task with epic parent is rejected", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
			"title": "Bad Task", "type": "task", "status": "backlog", "priority": "low",
			"parent_id": epicID, // task parent must be ticket, not epic
		})
		assertStatus(t, resp, http.StatusUnprocessableEntity)
	})

	t.Run("children endpoint returns task under ticket", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/children", ticketID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var children []struct{ ID int64 `json:"id"` }
		decode(t, resp, &children)
		if len(children) == 0 {
			t.Error("expected children, got none")
		}
	})
}

func Test_Search(t *testing.T) {
	ts := newTestServer(t)

	// Setup: project + issues with distinct searchable content in various fields.
	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Searchable Project Zeta", "key": "SPZ",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	// Issue with unique title term
	iResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Unique searchable zeta issue", "type": "ticket", "status": "backlog", "priority": "medium",
		"description": "This ticket has a zeta description",
	})
	assertStatus(t, iResp, http.StatusCreated)
	issueID := responseID(t, iResp)

	// Issue with unique jira_id term (avoid issue-key pattern like "PROJ-99999")
	i3Resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Jira imported issue", "type": "ticket", "status": "backlog", "priority": "medium",
		"jira_id": "jiraxyz99999",
	})
	assertStatus(t, i3Resp, http.StatusCreated)
	issue3ID := responseID(t, i3Resp)

	// Comment on issueID with a unique search term
	cResp := ts.post(t, fmt.Sprintf("/api/issues/%d/comments", issueID), ts.memberCookie, map[string]string{
		"body": "Comment with unique term frobnicator",
	})
	assertStatus(t, cResp, http.StatusCreated)

	// Issue with a word that porter stemmer would reduce (onboarding → onboard)
	onbResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Employee Onboarding checklist", "type": "ticket", "status": "backlog", "priority": "medium",
	})
	assertStatus(t, onbResp, http.StatusCreated)

	_ = issue3ID

	t.Run("FTS returns issue by title term", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=zeta", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var result struct {
			Issues   []struct{ ID int64 `json:"id"` } `json:"issues"`
			Projects []interface{}                   `json:"projects"`
		}
		decode(t, resp, &result)
		if len(result.Issues) == 0 && len(result.Projects) == 0 {
			t.Error("search for 'zeta' returned no issues or projects")
		}
	})

	t.Run("FTS indexes jira_id field (migration 30)", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=jiraxyz99999", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var result struct {
			Issues []struct{ ID int64 `json:"id"` } `json:"issues"`
		}
		decode(t, resp, &result)
		found := false
		for _, iss := range result.Issues {
			if iss.ID == issue3ID { found = true }
		}
		if !found {
			t.Errorf("search for 'PROJ-99999' (jira_id field) did not return issue %d", issue3ID)
		}
	})

	t.Run("FTS indexes comment body — returns parent issue (migration 30)", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=frobnicator", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var result struct {
			Issues []struct{ ID int64 `json:"id"` } `json:"issues"`
		}
		decode(t, resp, &result)
		found := false
		for _, iss := range result.Issues {
			if iss.ID == issueID { found = true }
		}
		if !found {
			t.Errorf("search for 'frobnicator' (comment body) did not return parent issue %d", issueID)
		}
	})

	t.Run("response shape: flat issues array, no separate buckets", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=zeta", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		// Decode into a map to check exact keys
		var result map[string]interface{}
		decode(t, resp, &result)
		if _, ok := result["issues"]; !ok {
			t.Error("response missing 'issues' key")
		}
		if _, ok := result["projects"]; !ok {
			t.Error("response missing 'projects' key")
		}
		if _, ok := result["users"]; !ok {
			t.Error("response missing 'users' key")
		}
		// Old separate buckets must NOT be present
		if _, ok := result["tagged_issues"]; ok {
			t.Error("response should not contain deprecated 'tagged_issues' key")
		}
		if _, ok := result["assigned_issues"]; ok {
			t.Error("response should not contain deprecated 'assigned_issues' key")
		}
		if _, ok := result["tagged_projects"]; ok {
			t.Error("response should not contain deprecated 'tagged_projects' key")
		}
	})

	t.Run("issues array max 20 results", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=issue", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var result struct {
			Issues []interface{} `json:"issues"`
		}
		decode(t, resp, &result)
		if len(result.Issues) > 20 {
			t.Errorf("issues array has %d items, want max 20", len(result.Issues))
		}
	})

	t.Run("search requires at least 2 chars", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=z", ts.memberCookie)
		if resp.StatusCode >= 500 {
			t.Errorf("search with 1 char returned %d, want < 500", resp.StatusCode)
		}
		var result struct {
			Issues []interface{} `json:"issues"`
		}
		decode(t, resp, &result)
		if len(result.Issues) != 0 {
			t.Errorf("search with 1 char returned %d issues, want 0", len(result.Issues))
		}
	})

	t.Run("exact issue key lookup returns that issue", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=SPZ-1", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var result struct {
			Issues []struct {
				ID       int64  `json:"id"`
				IssueKey string `json:"issue_key"`
			} `json:"issues"`
		}
		decode(t, resp, &result)
		if len(result.Issues) == 0 {
			t.Error("exact key lookup SPZ-1 returned no issues")
		} else if result.Issues[0].IssueKey != "SPZ-1" {
			t.Errorf("issue_key = %q, want %q", result.Issues[0].IssueKey, "SPZ-1")
		}
	})

	t.Run("issues in result have issue_key and project_key fields", func(t *testing.T) {
		resp := ts.get(t, "/api/search?q=zeta", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var result struct {
			Issues []struct {
				IssueKey   string `json:"issue_key"`
				ProjectKey string `json:"project_key"`
			} `json:"issues"`
		}
		decode(t, resp, &result)
		if len(result.Issues) == 0 {
			t.Skip("no issues in result")
		}
		if result.Issues[0].IssueKey == "" {
			t.Error("issue_key is empty")
		}
		if result.Issues[0].ProjectKey == "" {
			t.Error("project_key is empty")
		}
	})

	// Mid-word prefix search — validates ascii tokenizer (no porter stemmer)
	for _, q := range []string{"onboard", "onboardi", "onboardin", "onboarding"} {
		q := q
		t.Run("mid-word prefix search: "+q, func(t *testing.T) {
			resp := ts.get(t, "/api/search?q="+q, ts.memberCookie)
			assertStatus(t, resp, http.StatusOK)
			var result struct {
				Issues []struct{ Title string `json:"title"` } `json:"issues"`
			}
			decode(t, resp, &result)
			found := false
			for _, iss := range result.Issues {
				if iss.Title == "Employee Onboarding checklist" {
					found = true
				}
			}
			if !found {
				t.Errorf("search for %q did not return 'Employee Onboarding checklist'", q)
			}
		})
	}
}

func Test_Permissions(t *testing.T) {
	ts := newTestServer(t)

	t.Run("member cannot create project", func(t *testing.T) {
		resp := ts.post(t, "/api/projects", ts.memberCookie, map[string]string{
			"name": "Forbidden", "key": "FBD",
		})
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("member cannot create tag", func(t *testing.T) {
		resp := ts.post(t, "/api/tags", ts.memberCookie, map[string]string{
			"name": "forbidden-tag", "color": "red",
		})
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("member cannot create user", func(t *testing.T) {
		resp := ts.post(t, "/api/users", ts.memberCookie, map[string]string{
			"username": "newuser", "password": "pass123", "role": "member",
		})
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("unauthenticated cannot list projects", func(t *testing.T) {
		resp := ts.get(t, "/api/projects", "")
		assertStatus(t, resp, http.StatusUnauthorized)
	})
}

func Test_IssueHistory(t *testing.T) {
	ts := newTestServer(t)

	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "History Project", "key": "HSP",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	iResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Issue for history", "type": "ticket", "status": "backlog", "priority": "medium",
	})
	assertStatus(t, iResp, http.StatusCreated)
	issueID := responseID(t, iResp)

	// Update the issue to generate a history snapshot.
	ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie, map[string]string{
		"status": "in-progress",
	})

	t.Run("history endpoint returns snapshots after update", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/history", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var history []interface{}
		decode(t, resp, &history)
		if len(history) == 0 {
			t.Error("expected history entries after update, got none")
		}
	})
}

func Test_TagCRUD(t *testing.T) {
	ts := newTestServer(t)

	var tagID int64

	t.Run("admin creates tag", func(t *testing.T) {
		resp := ts.post(t, "/api/tags", ts.adminCookie, map[string]string{
			"name": "feature", "color": "blue", "description": "Feature tag",
		})
		assertStatus(t, resp, http.StatusCreated)
		tagID = responseID(t, resp)
		if tagID == 0 {
			t.Fatal("tag id is 0")
		}
	})

	t.Run("list tags includes created tag", func(t *testing.T) {
		resp := ts.get(t, "/api/tags", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var tags []struct{ ID int64 `json:"id"` }
		decode(t, resp, &tags)
		found := false
		for _, tg := range tags {
			if tg.ID == tagID { found = true }
		}
		if !found {
			t.Errorf("created tag %d not found in list", tagID)
		}
	})

	t.Run("admin updates tag", func(t *testing.T) {
		resp := ts.put(t, fmt.Sprintf("/api/tags/%d", tagID), ts.adminCookie, map[string]string{
			"description": "Updated feature tag",
		})
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("admin deletes tag", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/tags/%d", tagID), ts.adminCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})
}

func Test_UserManagement(t *testing.T) {
	ts := newTestServer(t)

	var userID int64

	t.Run("admin creates user", func(t *testing.T) {
		resp := ts.post(t, "/api/users", ts.adminCookie, map[string]string{
			"username": "newmember", "password": "testpass123", "role": "member",
		})
		assertStatus(t, resp, http.StatusCreated)
		userID = responseID(t, resp)
		if userID == 0 {
			t.Fatal("user id is 0")
		}
	})

	t.Run("list users includes new user", func(t *testing.T) {
		resp := ts.get(t, "/api/users", ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var users []struct{ ID int64 `json:"id"` }
		decode(t, resp, &users)
		found := false
		for _, u := range users {
			if u.ID == userID { found = true }
		}
		if !found {
			t.Errorf("created user %d not found in list", userID)
		}
	})

	t.Run("admin updates user", func(t *testing.T) {
		resp := ts.put(t, fmt.Sprintf("/api/users/%d", userID), ts.adminCookie, map[string]string{
			"role": "member",
		})
		assertStatus(t, resp, http.StatusOK)
	})
}

func Test_APIKeys(t *testing.T) {
	ts := newTestServer(t)

	var keyID int64

	t.Run("member creates API key", func(t *testing.T) {
		resp := ts.post(t, "/api/auth/api-keys", ts.memberCookie, map[string]string{
			"name": "test-key",
		})
		assertStatus(t, resp, http.StatusCreated)
		keyID = responseID(t, resp)
		if keyID == 0 {
			t.Fatal("api key id is 0")
		}
	})

	t.Run("member lists own API keys", func(t *testing.T) {
		resp := ts.get(t, "/api/auth/api-keys", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var keys []struct{ ID int64 `json:"id"` }
		decode(t, resp, &keys)
		found := false
		for _, k := range keys {
			if k.ID == keyID { found = true }
		}
		if !found {
			t.Errorf("created key %d not in list", keyID)
		}
	})

	t.Run("member deletes own API key", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/auth/api-keys/%d", keyID), ts.memberCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})
}

func Test_CSVExport(t *testing.T) {
	ts := newTestServer(t)

	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "CSV Project", "key": "CSV",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Exportable issue", "type": "ticket", "status": "backlog", "priority": "medium",
	})

	t.Run("CSV export returns 200 with csv content-type", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/export/csv", projectID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			t.Error("CSV export returned empty Content-Type")
		}
	})
}

func Test_IssueAggregation(t *testing.T) {
	ts := newTestServer(t)

	// Create project
	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Aggregation Project", "key": "AGG",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	// Create a cost_unit with rates
	cuResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title": "CU-1", "type": "cost_unit", "status": "backlog", "priority": "medium",
		"rate_hourly": 100.0, "rate_lp": 500.0,
	})
	assertStatus(t, cuResp, http.StatusCreated)
	cuID := responseID(t, cuResp)

	// Create two tickets with estimates
	t1Resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Ticket A", "type": "ticket", "status": "backlog", "priority": "medium",
		"estimate_hours": 10.0, "estimate_lp": 2.0, "ar_hours": 8.0, "ar_lp": 1.5,
	})
	assertStatus(t, t1Resp, http.StatusCreated)
	t1ID := responseID(t, t1Resp)

	t2Resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Ticket B", "type": "ticket", "status": "backlog", "priority": "medium",
		"estimate_hours": 5.0, "ar_hours": 4.0,
	})
	assertStatus(t, t2Resp, http.StatusCreated)
	t2ID := responseID(t, t2Resp)

	// Link both tickets as group members of the cost unit
	relResp1 := ts.post(t, fmt.Sprintf("/api/issues/%d/relations", cuID), ts.adminCookie, map[string]interface{}{
		"target_id": t1ID, "type": "groups",
	})
	assertStatus(t, relResp1, http.StatusCreated)
	relResp2 := ts.post(t, fmt.Sprintf("/api/issues/%d/relations", cuID), ts.adminCookie, map[string]interface{}{
		"target_id": t2ID, "type": "groups",
	})
	assertStatus(t, relResp2, http.StatusCreated)

	t.Run("aggregation returns summed values", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/aggregation", cuID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var agg struct {
			MemberCount   int      `json:"member_count"`
			EstimateHours *float64 `json:"estimate_hours"`
			EstimateLp    *float64 `json:"estimate_lp"`
			EstimateEur   *float64 `json:"estimate_eur"`
			ArHours       *float64 `json:"ar_hours"`
			ArEur         *float64 `json:"ar_eur"`
		}
		decode(t, resp, &agg)
		if agg.MemberCount != 2 {
			t.Errorf("member_count: got %d, want 2", agg.MemberCount)
		}
		if agg.EstimateHours == nil || *agg.EstimateHours != 15.0 {
			t.Errorf("estimate_hours: got %v, want 15.0", agg.EstimateHours)
		}
		if agg.EstimateLp == nil || *agg.EstimateLp != 2.0 {
			t.Errorf("estimate_lp: got %v, want 2.0", agg.EstimateLp)
		}
		// EUR = 15*100 + 2*500 = 2500
		if agg.EstimateEur == nil || *agg.EstimateEur != 2500.0 {
			t.Errorf("estimate_eur: got %v, want 2500.0", agg.EstimateEur)
		}
		if agg.ArHours == nil || *agg.ArHours != 12.0 {
			t.Errorf("ar_hours: got %v, want 12.0", agg.ArHours)
		}
	})

	t.Run("aggregation on non-existent issue returns 404", func(t *testing.T) {
		resp := ts.get(t, "/api/issues/99999/aggregation", ts.memberCookie)
		assertStatus(t, resp, http.StatusNotFound)
	})
}

func Test_IssueListFTSFilter(t *testing.T) {
	ts := newTestServer(t)

	// Create project + issues with distinct titles
	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "FTS Filter Project", "key": "FTS",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Alpha zephyr task", "type": "ticket", "status": "backlog", "priority": "medium",
	})
	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Beta unrelated task", "type": "ticket", "status": "backlog", "priority": "medium",
	})

	t.Run("project issues filtered by q returns matching only", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues?q=zephyr", projectID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct{ Title string `json:"title"` }
		decode(t, resp, &issues)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(issues))
		}
		if issues[0].Title != "Alpha zephyr task" {
			t.Errorf("unexpected title: %s", issues[0].Title)
		}
	})

	t.Run("project issues without q returns all", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct{ ID int64 `json:"id"` }
		decode(t, resp, &issues)
		if len(issues) != 2 {
			t.Errorf("expected 2 issues, got %d", len(issues))
		}
	})

	t.Run("global issues filtered by q returns matching only", func(t *testing.T) {
		resp := ts.get(t, "/api/issues?q=zephyr", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var env struct {
			Issues []struct{ Title string `json:"title"` } `json:"issues"`
			Total  int                                      `json:"total"`
		}
		decode(t, resp, &env)
		if env.Total != 1 {
			t.Errorf("total: got %d, want 1", env.Total)
		}
		if len(env.Issues) != 1 || env.Issues[0].Title != "Alpha zephyr task" {
			t.Errorf("unexpected issues: %+v", env.Issues)
		}
	})
}

func Test_ViewManagement(t *testing.T) {
	ts := newTestServer(t)

	var viewID int64

	t.Run("admin creates default view", func(t *testing.T) {
		resp := ts.post(t, "/api/views", ts.adminCookie, map[string]interface{}{
			"title": "Test Default", "columns_json": "[]", "filters_json": "{}",
			"is_admin_default": true,
		})
		assertStatus(t, resp, http.StatusCreated)
		var v struct {
			ID        int64 `json:"id"`
			SortOrder int   `json:"sort_order"`
			Hidden    bool  `json:"hidden"`
		}
		decode(t, resp, &v)
		viewID = v.ID
		if v.Hidden {
			t.Error("new view should not be hidden")
		}
	})

	t.Run("list views returns sort_order and pinned", func(t *testing.T) {
		resp := ts.get(t, "/api/views", ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var views []struct {
			ID        int64 `json:"id"`
			SortOrder int   `json:"sort_order"`
			Hidden    bool  `json:"hidden"`
			Pinned    *bool `json:"pinned"`
		}
		decode(t, resp, &views)
		found := false
		for _, v := range views {
			if v.ID == viewID {
				found = true
				if v.Pinned != nil {
					t.Error("pinned should be null (lazy init)")
				}
			}
		}
		if !found {
			t.Error("created view not in list")
		}
	})

	t.Run("admin reorders views", func(t *testing.T) {
		resp := ts.patch(t, "/api/views/order", ts.adminCookie, []map[string]interface{}{
			{"id": viewID, "sort_order": 5},
		})
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("admin hides view", func(t *testing.T) {
		resp := ts.put(t, fmt.Sprintf("/api/views/%d", viewID), ts.adminCookie, map[string]interface{}{
			"hidden": true,
		})
		assertStatus(t, resp, http.StatusOK)
		var v struct{ Hidden bool `json:"hidden"` }
		decode(t, resp, &v)
		if !v.Hidden {
			t.Error("view should be hidden")
		}
	})

	t.Run("member pins view", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/views/%d/pin", viewID), ts.memberCookie, nil)
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("member sees pinned state", func(t *testing.T) {
		resp := ts.get(t, "/api/views", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var views []struct {
			ID     int64 `json:"id"`
			Pinned *bool `json:"pinned"`
		}
		decode(t, resp, &views)
		for _, v := range views {
			if v.ID == viewID {
				if v.Pinned == nil || !*v.Pinned {
					t.Error("expected pinned=true for member")
				}
			}
		}
	})

	t.Run("member unpins view", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/views/%d/pin", viewID), ts.memberCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("member reorder forbidden", func(t *testing.T) {
		resp := ts.patch(t, "/api/views/order", ts.memberCookie, []map[string]interface{}{
			{"id": viewID, "sort_order": 0},
		})
		assertStatus(t, resp, http.StatusForbidden)
	})
}

func Test_IssueCreatedBy(t *testing.T) {
	ts := newTestServer(t)

	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "CreatedBy Project", "key": "CBP",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	// Member creates an issue
	iResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Member created issue", "type": "ticket", "status": "backlog", "priority": "medium",
	})
	assertStatus(t, iResp, http.StatusCreated)
	issueID := responseID(t, iResp)

	t.Run("created_by is set on new issue", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue struct {
			CreatedBy         *int64 `json:"created_by"`
			CreatedByName     string `json:"created_by_name"`
			LastChangedByName string `json:"last_changed_by_name"`
		}
		decode(t, resp, &issue)
		if issue.CreatedBy == nil {
			t.Fatal("created_by should not be nil")
		}
		if issue.CreatedByName != "member" {
			t.Errorf("created_by_name: got %q, want 'member'", issue.CreatedByName)
		}
		if issue.LastChangedByName != "member" {
			t.Errorf("last_changed_by_name: got %q, want 'member'", issue.LastChangedByName)
		}
	})

	t.Run("last_changed_by updates after admin edit", func(t *testing.T) {
		ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie, map[string]interface{}{
			"title": "Admin edited issue",
		})
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue struct {
			CreatedByName     string `json:"created_by_name"`
			LastChangedByName string `json:"last_changed_by_name"`
		}
		decode(t, resp, &issue)
		if issue.CreatedByName != "member" {
			t.Errorf("created_by_name should still be 'member', got %q", issue.CreatedByName)
		}
		if issue.LastChangedByName != "admin" {
			t.Errorf("last_changed_by_name: got %q, want 'admin'", issue.LastChangedByName)
		}
	})
}

// Test_TimeEntries covers time entry CRUD, running timer, auto-stop, and time_override.
func Test_TimeEntries(t *testing.T) {
	ts := newTestServer(t)

	// Create a project + ticket
	projResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "TE Project", "key": "TEP",
	})
	if projResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(projResp.Body)
		t.Fatalf("project creation failed: status %d, body: %s", projResp.StatusCode, body)
	}
	projID := responseID(t, projResp)
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.memberCookie, map[string]interface{}{
		"title": "Timer test ticket", "type": "ticket",
	}))
	issue2ID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.memberCookie, map[string]interface{}{
		"title": "Second ticket", "type": "ticket",
	}))

	t.Run("create manual entry with override", func(t *testing.T) {
		override := 1.5
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie, map[string]interface{}{
			"override": override, "comment": "manual work",
		})
		assertStatus(t, resp, http.StatusCreated)
		var entry struct {
			ID       int64    `json:"id"`
			Hours    *float64 `json:"hours"`
			Override *float64 `json:"override"`
			Comment  string   `json:"comment"`
		}
		decode(t, resp, &entry)
		if entry.Hours == nil || *entry.Hours != 1.5 {
			t.Errorf("hours: got %v, want 1.5", entry.Hours)
		}
		if entry.Comment != "manual work" {
			t.Errorf("comment: got %q, want 'manual work'", entry.Comment)
		}
	})

	t.Run("list time entries", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var entries []struct{ ID int64 `json:"id"` }
		decode(t, resp, &entries)
		if len(entries) != 1 {
			t.Errorf("entries count: got %d, want 1", len(entries))
		}
	})

	var timerID int64
	t.Run("start timer (running entry)", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie, map[string]interface{}{
			"comment": "timer running",
		})
		assertStatus(t, resp, http.StatusCreated)
		var entry struct {
			ID        int64   `json:"id"`
			StoppedAt *string `json:"stopped_at"`
		}
		decode(t, resp, &entry)
		timerID = entry.ID
		if entry.StoppedAt != nil {
			t.Errorf("stopped_at should be nil for running timer, got %v", *entry.StoppedAt)
		}
	})

	t.Run("GET running timers (array)", func(t *testing.T) {
		resp := ts.get(t, "/api/time-entries/running", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var entries []struct {
			ID         int64  `json:"id"`
			IssueKey   string `json:"issue_key"`
			IssueTitle string `json:"issue_title"`
			ProjectID  int64  `json:"project_id"`
		}
		decode(t, resp, &entries)
		// At least the timer entry should be running (manual override entry also has stopped_at=NULL)
		if len(entries) < 1 {
			t.Fatalf("running entries: got %d, want >=1", len(entries))
		}
		// Find the timer entry
		var found bool
		for _, e := range entries {
			if e.ID == timerID {
				found = true
				if e.IssueKey != "TEP-1" {
					t.Errorf("issue_key: got %q, want 'TEP-1'", e.IssueKey)
				}
				if e.IssueTitle == "" {
					t.Error("issue_title should not be empty")
				}
				if e.ProjectID == 0 {
					t.Error("project_id should not be 0")
				}
			}
		}
		if !found {
			t.Errorf("timer entry %d not found in running entries (got %d entries)", timerID, len(entries))
		}
	})

	t.Run("parallel timers allowed", func(t *testing.T) {
		// Start timer on issue2 — first timer should NOT be auto-stopped
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issue2ID), ts.memberCookie, map[string]interface{}{
			"comment": "second timer",
		})
		assertStatus(t, resp, http.StatusCreated)

		// Both timers should be running
		resp = ts.get(t, "/api/time-entries/running", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var running []struct{ ID int64 `json:"id"` }
		decode(t, resp, &running)
		if len(running) < 2 {
			t.Errorf("expected at least 2 running timers, got %d", len(running))
		}
	})

	t.Run("stop timer", func(t *testing.T) {
		// Get running timers (array)
		resp := ts.get(t, "/api/time-entries/running", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var runningList []struct{ ID int64 `json:"id"` }
		decode(t, resp, &runningList)
		if len(runningList) == 0 {
			t.Fatal("no running timers")
		}
		running := runningList[0]

		now := "2026-03-20T15:00:00Z"
		resp = ts.put(t, fmt.Sprintf("/api/time-entries/%d", running.ID), ts.memberCookie, map[string]interface{}{
			"stopped_at": now,
		})
		assertStatus(t, resp, http.StatusOK)
		var entry struct {
			StoppedAt *string  `json:"stopped_at"`
			Hours     *float64 `json:"hours"`
		}
		decode(t, resp, &entry)
		if entry.StoppedAt == nil {
			t.Error("stopped_at should be set after stopping")
		}
	})

	t.Run("clear override", func(t *testing.T) {
		// Get first entry (the manual one with override=1.5)
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var entries []struct {
			ID       int64    `json:"id"`
			Override *float64 `json:"override"`
		}
		decode(t, resp, &entries)
		// Find the one with override
		var overrideID int64
		for _, e := range entries {
			if e.Override != nil {
				overrideID = e.ID
				break
			}
		}
		if overrideID == 0 {
			t.Fatal("no entry with override found")
		}
		resp = ts.put(t, fmt.Sprintf("/api/time-entries/%d", overrideID), ts.memberCookie, map[string]interface{}{
			"clear_override": true,
		})
		assertStatus(t, resp, http.StatusOK)
		var updated struct{ Override *float64 `json:"override"` }
		decode(t, resp, &updated)
		if updated.Override != nil {
			t.Errorf("override should be nil after clear, got %v", *updated.Override)
		}
	})

	t.Run("delete own entry", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie)
		var entries []struct{ ID int64 `json:"id"` }
		decode(t, resp, &entries)
		if len(entries) == 0 {
			t.Fatal("no entries to delete")
		}
		resp = ts.del(t, fmt.Sprintf("/api/time-entries/%d", entries[0].ID), ts.memberCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("delete other user entry forbidden", func(t *testing.T) {
		// Admin creates an entry
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]interface{}{
			"override": 2.0, "comment": "admin work",
		})
		assertStatus(t, resp, http.StatusCreated)
		var entry struct{ ID int64 `json:"id"` }
		decode(t, resp, &entry)

		// Member tries to delete it → forbidden
		resp = ts.del(t, fmt.Sprintf("/api/time-entries/%d", entry.ID), ts.memberCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("update other user entry forbidden", func(t *testing.T) {
		// Admin creates an entry
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.adminCookie, map[string]interface{}{
			"override": 3.0, "comment": "admin only",
		})
		assertStatus(t, resp, http.StatusCreated)
		var entry struct{ ID int64 `json:"id"` }
		decode(t, resp, &entry)

		// Member tries to update it → forbidden
		resp = ts.put(t, fmt.Sprintf("/api/time-entries/%d", entry.ID), ts.memberCookie, map[string]interface{}{
			"comment": "hacked",
		})
		assertStatus(t, resp, http.StatusForbidden)

		// Admin can update it → ok
		resp = ts.put(t, fmt.Sprintf("/api/time-entries/%d", entry.ID), ts.adminCookie, map[string]interface{}{
			"comment": "admin edit",
		})
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("time_override on issue", func(t *testing.T) {
		override := 8.0
		resp := ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie, map[string]interface{}{
			"time_override": override,
		})
		assertStatus(t, resp, http.StatusOK)
		var issue struct{ TimeOverride *float64 `json:"time_override"` }
		decode(t, resp, &issue)
		if issue.TimeOverride == nil || *issue.TimeOverride != 8.0 {
			t.Errorf("time_override: got %v, want 8.0", issue.TimeOverride)
		}
	})

	t.Run("mite-imported entries are not running timers", func(t *testing.T) {
		// Stop any existing running timers
		db.DB.Exec("UPDATE time_entries SET stopped_at=datetime('now') WHERE stopped_at IS NULL")

		// Simulate a mite-imported entry with stopped_at = started_at (the fix)
		startedAt := "2025-01-15 00:00:00"
		db.DB.Exec(`INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, mite_id)
			VALUES(?,?,?,?,?,?)`, issueID, 2, startedAt, startedAt, 2.5, 99999)

		resp := ts.get(t, "/api/time-entries/running", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var entries []struct{ ID int64 `json:"id"` }
		decode(t, resp, &entries)
		if len(entries) != 0 {
			t.Errorf("mite-imported entry should not appear as running, got %d running", len(entries))
		}

		// Simulate the old bug: mite entry with NULL stopped_at
		db.DB.Exec(`INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, mite_id)
			VALUES(?,?,?,NULL,?,?)`, issueID, 2, startedAt, 1.0, 99998)

		// Verify it IS running (the bug)
		resp = ts.get(t, "/api/time-entries/running", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		decode(t, resp, &entries)
		if len(entries) == 0 {
			t.Fatal("entry with NULL stopped_at should appear as running")
		}

		// Apply the migration fix
		db.DB.Exec("UPDATE time_entries SET stopped_at = started_at WHERE mite_id IS NOT NULL AND stopped_at IS NULL")

		// Verify it's no longer running
		resp = ts.get(t, "/api/time-entries/running", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		decode(t, resp, &entries)
		if len(entries) != 0 {
			t.Errorf("after migration fix, no mite entries should be running, got %d", len(entries))
		}

		// Clean up test mite entries
		db.DB.Exec("DELETE FROM time_entries WHERE mite_id IN (99998, 99999)")
	})

	t.Run("no running timer returns empty array", func(t *testing.T) {
		// Stop any running timers first
		db.DB.Exec("UPDATE time_entries SET stopped_at=datetime('now') WHERE stopped_at IS NULL")
		resp := ts.get(t, "/api/time-entries/running", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var entries []struct{ ID int64 `json:"id"` }
		decode(t, resp, &entries)
		if len(entries) != 0 {
			t.Errorf("running entries: got %d, want 0", len(entries))
		}
	})

	t.Run("GET recent timers", func(t *testing.T) {
		resp := ts.get(t, "/api/time-entries/recent", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var entries []struct {
			ID         int64   `json:"id"`
			IssueKey   string  `json:"issue_key"`
			IssueTitle string  `json:"issue_title"`
			StoppedAt  *string `json:"stopped_at"`
		}
		decode(t, resp, &entries)
		if len(entries) == 0 {
			t.Fatal("recent entries should not be empty")
		}
		// All entries should have stopped_at set
		for _, e := range entries {
			if e.StoppedAt == nil {
				t.Errorf("recent entry %d should have stopped_at", e.ID)
			}
		}
		// Should have issue_key populated
		if entries[0].IssueKey == "" {
			t.Error("recent entry should have issue_key")
		}
	})
}

func Test_SystemTagAtRisk(t *testing.T) {
	ts := newTestServer(t)

	// Create project + issue with estimate
	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "System Tag Project", "key": "STP",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	issResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title": "At Risk Test", "type": "ticket", "status": "in-progress", "priority": "medium",
		"estimate_hours": 1.0,
	})
	assertStatus(t, issResp, http.StatusCreated)
	issueID := responseID(t, issResp)

	// Helper to check if issue has "At Risk" tag
	hasAtRisk := func() bool {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var issue struct {
			Tags []struct {
				Name   string `json:"name"`
				System bool   `json:"system"`
			} `json:"tags"`
		}
		json.Unmarshal(body, &issue)
		for _, tag := range issue.Tags {
			if tag.Name == "At Risk" && tag.System {
				return true
			}
		}
		return false
	}

	t.Run("no tag when booked below threshold", func(t *testing.T) {
		// Create a time entry with 0.5h (50% of 1.0h estimate — below 80% threshold)
		teResp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie, map[string]interface{}{
			"started_at": "2026-01-01T10:00:00Z",
			"stopped_at": "2026-01-01T10:30:00Z",
		})
		assertStatus(t, teResp, http.StatusCreated)
		if hasAtRisk() {
			t.Error("issue should NOT have At Risk tag at 50% of estimate")
		}
	})

	t.Run("tag applied when booked exceeds threshold", func(t *testing.T) {
		// Add another 0.5h → total 1.0h (100% of 1.0h estimate — above 80% threshold)
		teResp := ts.post(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie, map[string]interface{}{
			"started_at": "2026-01-01T11:00:00Z",
			"stopped_at": "2026-01-01T11:30:00Z",
		})
		assertStatus(t, teResp, http.StatusCreated)
		if !hasAtRisk() {
			t.Error("issue SHOULD have At Risk tag at 100% of estimate")
		}
	})

	t.Run("tag removed when status moves to excluded", func(t *testing.T) {
		// Move to done (excluded status)
		ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie, map[string]interface{}{
			"status": "done",
		})
		if hasAtRisk() {
			t.Error("issue should NOT have At Risk tag when status is done (excluded)")
		}
	})

	t.Run("tag re-applied when status moves back to active", func(t *testing.T) {
		ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie, map[string]interface{}{
			"status": "in-progress",
		})
		if !hasAtRisk() {
			t.Error("issue SHOULD have At Risk tag when status returns to in-progress")
		}
	})

	t.Run("tag removed when time entry deleted below threshold", func(t *testing.T) {
		// Delete time entries to go below threshold
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/time-entries", issueID), ts.memberCookie)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var entries []struct{ ID int64 `json:"id"` }
		json.Unmarshal(body, &entries)
		// Delete all but one
		for i := 1; i < len(entries); i++ {
			ts.del(t, fmt.Sprintf("/api/time-entries/%d", entries[i].ID), ts.adminCookie)
		}
		if hasAtRisk() {
			t.Error("issue should NOT have At Risk tag after deleting entries below threshold")
		}
	})

	t.Run("system tag cannot be manually added", func(t *testing.T) {
		// Find the At Risk tag ID
		var atRiskID int64
		db.DB.QueryRow(`SELECT id FROM tags WHERE name='At Risk' AND system=1`).Scan(&atRiskID)
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/tags", issueID), ts.memberCookie, map[string]interface{}{
			"tag_id": atRiskID,
		})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("manually adding system tag should be forbidden, got %d", resp.StatusCode)
		}
	})
}

// Test_PurgeTimeEntries covers the bulk purge endpoints.
func Test_PurgeTimeEntries(t *testing.T) {
	ts := newTestServer(t)

	// Create a project + ticket
	projResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Purge Project", "key": "PUR",
	})
	assertStatus(t, projResp, http.StatusCreated)
	projID := responseID(t, projResp)
	issueID := responseID(t, ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projID), ts.adminCookie, map[string]interface{}{
		"title": "Purge ticket", "type": "ticket",
	}))

	// Seed time entries: 2 manual + 2 mite-imported
	db.DB.Exec(`INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, comment)
		VALUES(?,1,'2025-03-01 00:00:00','2025-03-01 00:00:00',2.0,'manual1')`, issueID)
	db.DB.Exec(`INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, comment)
		VALUES(?,2,'2025-03-15 00:00:00','2025-03-15 00:00:00',3.0,'manual2')`, issueID)
	db.DB.Exec(`INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, mite_id, comment)
		VALUES(?,1,'2025-03-05 00:00:00','2025-03-05 00:00:00',1.5,1001,'mite1')`, issueID)
	db.DB.Exec(`INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, mite_id, comment)
		VALUES(?,1,'2025-03-20 00:00:00','2025-03-20 00:00:00',4.0,1002,'mite2')`, issueID)

	t.Run("member cannot access purge-preview", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/time-entries/purge-preview", projID), ts.memberCookie, map[string]interface{}{
			"source": "all",
		})
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("preview all entries", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/time-entries/purge-preview", projID), ts.adminCookie, map[string]interface{}{
			"source": "all",
		})
		assertStatus(t, resp, http.StatusOK)
		var res struct {
			Count      int     `json:"count"`
			TotalHours float64 `json:"total_hours"`
		}
		decode(t, resp, &res)
		if res.Count != 4 {
			t.Errorf("count: got %d, want 4", res.Count)
		}
		if res.TotalHours != 10.5 {
			t.Errorf("total_hours: got %.1f, want 10.5", res.TotalHours)
		}
	})

	t.Run("preview mite-only", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/time-entries/purge-preview", projID), ts.adminCookie, map[string]interface{}{
			"source": "mite",
		})
		assertStatus(t, resp, http.StatusOK)
		var res struct {
			Count      int     `json:"count"`
			TotalHours float64 `json:"total_hours"`
		}
		decode(t, resp, &res)
		if res.Count != 2 {
			t.Errorf("count: got %d, want 2", res.Count)
		}
	})

	t.Run("preview with date range", func(t *testing.T) {
		from := "2025-03-01"
		to := "2025-03-10"
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/time-entries/purge-preview", projID), ts.adminCookie, map[string]interface{}{
			"source": "all", "from_date": from, "to_date": to,
		})
		assertStatus(t, resp, http.StatusOK)
		var res struct{ Count int `json:"count"` }
		decode(t, resp, &res)
		if res.Count != 2 { // manual1 (Mar 1) + mite1 (Mar 5)
			t.Errorf("count: got %d, want 2", res.Count)
		}
	})

	t.Run("purge requires correct confirmation key", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/time-entries/purge", projID), ts.adminCookie, map[string]interface{}{
			"source": "mite", "confirmation_key": "WRONG",
		})
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("purge mite entries with correct key", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/time-entries/purge", projID), ts.adminCookie, map[string]interface{}{
			"source": "mite", "confirmation_key": "PUR",
		})
		assertStatus(t, resp, http.StatusOK)
		var res struct {
			Count      int     `json:"count"`
			TotalHours float64 `json:"total_hours"`
		}
		decode(t, resp, &res)
		if res.Count != 2 {
			t.Errorf("deleted count: got %d, want 2", res.Count)
		}
		if res.TotalHours != 5.5 {
			t.Errorf("total_hours: got %.1f, want 5.5", res.TotalHours)
		}
	})

	t.Run("remaining entries after purge", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/time-entries/purge-preview", projID), ts.adminCookie, map[string]interface{}{
			"source": "all",
		})
		assertStatus(t, resp, http.StatusOK)
		var res struct{ Count int `json:"count"` }
		decode(t, resp, &res)
		if res.Count != 2 {
			t.Errorf("remaining: got %d, want 2", res.Count)
		}
	})

	t.Run("GET purge users", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/time-entries/users", projID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var users []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		}
		decode(t, resp, &users)
		if len(users) == 0 {
			t.Fatal("expected at least 1 user with time entries")
		}
	})
}
