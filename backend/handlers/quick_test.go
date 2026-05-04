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

// Core tests — blocking in CI, gates the GHCR image publish.
// Run: go test ./handlers/... -v
// Covers: auth, project CRUD, issue CRUD, tags on issues (member), comments (member).

import (
	"fmt"
	"net/http"
	"testing"
)

func Test_Auth(t *testing.T) {
	ts := newTestServer(t)

	t.Run("admin login succeeds", func(t *testing.T) {
		resp := ts.post(t, "/api/auth/login", "", map[string]string{"username": "admin", "password": "adminpass"})
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("member login succeeds", func(t *testing.T) {
		resp := ts.post(t, "/api/auth/login", "", map[string]string{"username": "member", "password": "memberpass"})
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("bad credentials rejected", func(t *testing.T) {
		resp := ts.post(t, "/api/auth/login", "", map[string]string{"username": "admin", "password": "wrongpass"})
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("me returns current user", func(t *testing.T) {
		resp := ts.get(t, "/api/auth/me", ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var env struct {
			User struct {
				Username string `json:"username"`
				Role     string `json:"role"`
			} `json:"user"`
			Access struct {
				AllProjects bool `json:"all_projects"`
			} `json:"access"`
		}
		decode(t, resp, &env)
		if env.User.Username != "admin" {
			t.Errorf("me: username = %q, want %q", env.User.Username, "admin")
		}
		if env.User.Role != "admin" {
			t.Errorf("me: role = %q, want %q", env.User.Role, "admin")
		}
		if !env.Access.AllProjects {
			t.Errorf("me: admin should have access.all_projects=true")
		}
	})

	t.Run("unauthenticated request rejected", func(t *testing.T) {
		resp := ts.get(t, "/api/auth/me", "")
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("login response includes all profile fields", func(t *testing.T) {
		resp := ts.post(t, "/api/auth/login", "", map[string]string{"username": "admin", "password": "adminpass"})
		assertStatus(t, resp, http.StatusOK)
		// Login now returns an envelope: { "user": {...}, "access": {...} }.
		var env struct {
			User map[string]interface{} `json:"user"`
		}
		decode(t, resp, &env)
		for _, key := range []string{"id", "username", "role", "status", "markdown_default", "monospace_fields", "recent_projects_limit", "locale", "issue_auto_refresh_enabled", "issue_auto_refresh_interval_seconds"} {
			if _, ok := env.User[key]; !ok {
				t.Errorf("login response missing user.%q", key)
			}
		}
	})

	t.Run("profile update persists issue auto-refresh preferences with ten second floor", func(t *testing.T) {
		resp := ts.patch(t, "/api/auth/me", ts.memberCookie, map[string]interface{}{
			"issue_auto_refresh_enabled":          false,
			"issue_auto_refresh_interval_seconds": 5,
		})
		assertStatus(t, resp, http.StatusOK)
		var user struct {
			IssueAutoRefreshEnabled         bool `json:"issue_auto_refresh_enabled"`
			IssueAutoRefreshIntervalSeconds int  `json:"issue_auto_refresh_interval_seconds"`
		}
		decode(t, resp, &user)
		if user.IssueAutoRefreshEnabled {
			t.Errorf("issue_auto_refresh_enabled = true, want false")
		}
		if user.IssueAutoRefreshIntervalSeconds != 10 {
			t.Errorf("issue_auto_refresh_interval_seconds = %d, want 10", user.IssueAutoRefreshIntervalSeconds)
		}
	})
}

func Test_ProjectCRUD(t *testing.T) {
	ts := newTestServer(t)

	var projectID int64

	t.Run("admin creates project", func(t *testing.T) {
		resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
			"name": "Test Project", "key": "TST", "description": "quick test project",
		})
		assertStatus(t, resp, http.StatusCreated)
		projectID = responseID(t, resp)
		if projectID == 0 {
			t.Fatal("project id is 0")
		}
	})

	t.Run("list projects returns created project", func(t *testing.T) {
		resp := ts.get(t, "/api/projects", ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var projects []struct {
			ID int64 `json:"id"`
		}
		decode(t, resp, &projects)
		found := false
		for _, p := range projects {
			if p.ID == projectID {
				found = true
			}
		}
		if !found {
			t.Errorf("created project %d not found in list", projectID)
		}
	})

	t.Run("get project by id", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d", projectID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("admin updates project", func(t *testing.T) {
		resp := ts.put(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie, map[string]string{
			"name": "Updated Project",
		})
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("member cannot create project", func(t *testing.T) {
		resp := ts.post(t, "/api/projects", ts.memberCookie, map[string]string{
			"name": "Member Project", "key": "MBR",
		})
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("admin deletes project", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/projects/%d", projectID), ts.adminCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})
}

func Test_IssueCRUD(t *testing.T) {
	ts := newTestServer(t)

	// Setup: create a project first.
	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Issue Test Project", "key": "ISP",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	var issueID int64

	t.Run("member creates issue", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
			"title": "Test Issue", "type": "ticket", "status": "backlog", "priority": "medium",
		})
		assertStatus(t, resp, http.StatusCreated)
		issueID = responseID(t, resp)
		if issueID == 0 {
			t.Fatal("issue id is 0")
		}
	})

	t.Run("member gets issue by id", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue struct {
			Title string `json:"title"`
		}
		decode(t, resp, &issue)
		if issue.Title != "Test Issue" {
			t.Errorf("issue title = %q, want %q", issue.Title, "Test Issue")
		}
	})

	t.Run("list project issues returns created issue", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct {
			ID int64 `json:"id"`
		}
		decode(t, resp, &issues)
		found := false
		for _, i := range issues {
			if i.ID == issueID {
				found = true
			}
		}
		if !found {
			t.Errorf("created issue %d not found in list", issueID)
		}
	})

	t.Run("member updates issue", func(t *testing.T) {
		resp := ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie, map[string]string{
			"title": "Updated Issue",
		})
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("admin deletes issue", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})
}

func Test_TagsOnIssues(t *testing.T) {
	// Regression test for migration 23 FK bug — INSERT INTO issue_tags was
	// failing with "no such table: main.issues_old23" for all users.
	ts := newTestServer(t)

	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Tag Test Project", "key": "TTP",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	iResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Issue for tagging", "type": "ticket", "status": "backlog", "priority": "medium",
	})
	assertStatus(t, iResp, http.StatusCreated)
	issueID := responseID(t, iResp)
	tagID := firstTagID(t)

	t.Run("member attaches tag to issue", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/tags", issueID), ts.memberCookie, map[string]int64{
			"tag_id": tagID,
		})
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("issue has tag after attach", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue struct {
			Tags []struct {
				ID int64 `json:"id"`
			} `json:"tags"`
		}
		decode(t, resp, &issue)
		if len(issue.Tags) == 0 {
			t.Error("expected issue to have tags, got none")
		}
	})

	t.Run("member detaches tag from issue", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/issues/%d/tags/%d", issueID, tagID), ts.memberCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("issue has no tags after detach", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue struct {
			Tags []struct {
				ID int64 `json:"id"`
			} `json:"tags"`
		}
		decode(t, resp, &issue)
		if len(issue.Tags) != 0 {
			t.Errorf("expected no tags after detach, got %d", len(issue.Tags))
		}
	})
}

func Test_APIKeyAuth(t *testing.T) {
	// Regression test: migration 29 added markdown_default + monospace_fields to
	// userSelectCols but the Scan in auth/apikey.go was not updated — causing all
	// API key auth attempts to return 401.
	ts := newTestServer(t)

	// Create an API key as member.
	var rawKey string
	t.Run("member creates api key", func(t *testing.T) {
		resp := ts.post(t, "/api/auth/api-keys", ts.memberCookie, map[string]string{
			"name": "test-key",
		})
		assertStatus(t, resp, http.StatusCreated)
		var body struct {
			Key string `json:"key"`
		}
		decode(t, resp, &body)
		if body.Key == "" {
			t.Fatal("expected raw key in response, got empty string")
		}
		rawKey = body.Key
	})

	t.Run("api key authenticates /auth/me", func(t *testing.T) {
		if rawKey == "" {
			t.Skip("no key from previous sub-test")
		}
		resp := ts.getBearer(t, "/api/auth/me", rawKey)
		assertStatus(t, resp, http.StatusOK)
		var me struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		decode(t, resp, &me)
		if me.User.Username != "member" {
			t.Errorf("api key me: username = %q, want %q", me.User.Username, "member")
		}
	})

	t.Run("invalid api key returns 401", func(t *testing.T) {
		resp := ts.getBearer(t, "/api/auth/me", "paimos_notavalidkey")
		assertStatus(t, resp, http.StatusUnauthorized)
	})
}

func Test_Comments(t *testing.T) {
	// Regression test for migration 23 FK bug — INSERT INTO comments was
	// failing with "no such table: main.issues_old23" for all users.
	ts := newTestServer(t)

	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Comment Test Project", "key": "CTP",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	iResp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]interface{}{
		"title": "Issue for comments", "type": "ticket", "status": "backlog", "priority": "medium",
	})
	assertStatus(t, iResp, http.StatusCreated)
	issueID := responseID(t, iResp)

	var commentID int64

	t.Run("member posts comment", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/comments", issueID), ts.memberCookie, map[string]string{
			"body": "This is a test comment.",
		})
		assertStatus(t, resp, http.StatusCreated)
		commentID = responseID(t, resp)
		if commentID == 0 {
			t.Fatal("comment id is 0")
		}
	})

	t.Run("list comments returns posted comment", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/comments", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var comments []struct {
			ID int64 `json:"id"`
		}
		decode(t, resp, &comments)
		found := false
		for _, c := range comments {
			if c.ID == commentID {
				found = true
			}
		}
		if !found {
			t.Errorf("posted comment %d not found in list", commentID)
		}
	})

	t.Run("member deletes own comment", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/comments/%d", commentID), ts.memberCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})
}

func Test_Attachments(t *testing.T) {
	ts := newTestServer(t)

	// Create a project + issue to attach files to
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Attach Project", "key": "ATT",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.memberCookie, map[string]string{
		"title": "Attachment test issue", "type": "ticket",
	})
	assertStatus(t, resp, http.StatusCreated)
	issueID := responseID(t, resp)

	t.Run("list attachments returns empty array", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/attachments", issueID), ts.memberCookie)
		assertStatus(t, resp, http.StatusOK)
		var list []struct {
			ID int64 `json:"id"`
		}
		decode(t, resp, &list)
		if len(list) != 0 {
			t.Errorf("expected empty list, got %d", len(list))
		}
	})

	t.Run("upload returns 503 when storage not configured", func(t *testing.T) {
		fakeFile := []byte("fake image content")
		resp := ts.postMultipart(t,
			fmt.Sprintf("/api/issues/%d/attachments", issueID),
			ts.memberCookie, "file", "test.png", fakeFile,
		)
		assertStatus(t, resp, http.StatusServiceUnavailable)
	})
}

// ── New status tests ─────────────────────────────────────────────────────────

func TestQuick_NewStatus(t *testing.T) {
	ts := newTestServer(t)

	// Create a project
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Status Test", "key": "NST",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	t.Run("create issue defaults to new status", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": "Default Status Issue", "type": "ticket",
		})
		assertStatus(t, resp, http.StatusCreated)
		var issue struct {
			Status string `json:"status"`
		}
		decode(t, resp, &issue)
		if issue.Status != "new" {
			t.Errorf("default status = %q, want new", issue.Status)
		}
	})

	t.Run("create issue with explicit backlog keeps backlog", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": "Backlog Issue", "type": "ticket", "status": "backlog",
		})
		assertStatus(t, resp, http.StatusCreated)
		var issue struct {
			Status string `json:"status"`
		}
		decode(t, resp, &issue)
		if issue.Status != "backlog" {
			t.Errorf("status = %q, want backlog", issue.Status)
		}
	})

	t.Run("update issue to new status succeeds", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": "Update Target", "type": "ticket", "status": "backlog",
		})
		assertStatus(t, resp, http.StatusCreated)
		issueID := responseID(t, resp)

		resp = ts.put(t, fmt.Sprintf("/api/issues/%d", issueID), ts.adminCookie, map[string]any{
			"status": "new",
		})
		assertStatus(t, resp, http.StatusOK)
		var issue struct {
			Status string `json:"status"`
		}
		decode(t, resp, &issue)
		if issue.Status != "new" {
			t.Errorf("status = %q, want new", issue.Status)
		}
	})

	t.Run("auto-promote parent from new to in-progress", func(t *testing.T) {
		// Create epic with status=new
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": "New Epic", "type": "epic", "status": "new",
		})
		assertStatus(t, resp, http.StatusCreated)
		epicID := responseID(t, resp)

		// Create child ticket
		resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": "Child Ticket", "type": "ticket", "parent_id": epicID, "status": "backlog",
		})
		assertStatus(t, resp, http.StatusCreated)
		ticketID := responseID(t, resp)

		// Move child to in-progress — should auto-promote parent
		resp = ts.put(t, fmt.Sprintf("/api/issues/%d", ticketID), ts.adminCookie, map[string]any{
			"status": "in-progress",
		})
		assertStatus(t, resp, http.StatusOK)

		// Check parent status
		resp = ts.get(t, fmt.Sprintf("/api/issues/%d", epicID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var epic struct {
			Status string `json:"status"`
		}
		decode(t, resp, &epic)
		if epic.Status != "in-progress" {
			t.Errorf("epic status = %q, want in-progress (auto-promote from new)", epic.Status)
		}
	})
}
