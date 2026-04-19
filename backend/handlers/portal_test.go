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

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

// ── External user blocking ──────────────────────────────────────────────────

func TestQuick_ExternalUserBlocked(t *testing.T) {
	ts := newTestServer(t)

	t.Run("external user can call /auth/me", func(t *testing.T) {
		resp := ts.get(t, "/api/auth/me", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var u struct {
			Role string `json:"role"`
		}
		decode(t, resp, &u)
		if u.Role != "external" {
			t.Errorf("role = %q, want external", u.Role)
		}
	})

	t.Run("external user blocked from internal projects", func(t *testing.T) {
		resp := ts.get(t, "/api/projects", ts.externalCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("external user blocked from internal issues", func(t *testing.T) {
		resp := ts.get(t, "/api/issues/recent", ts.externalCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("external user blocked from internal tags", func(t *testing.T) {
		resp := ts.get(t, "/api/tags", ts.externalCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("external user blocked from internal users", func(t *testing.T) {
		resp := ts.get(t, "/api/users", ts.externalCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("member user blocked from portal", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/projects", ts.memberCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})
}

// ── Portal endpoints ────────────────────────────────────────────────────────

func TestQuick_Portal(t *testing.T) {
	ts := newTestServer(t)

	// Get external user ID
	var extUserID int64
	db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extUserID)

	// Admin creates a project
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Portal Project", "key": "PPO",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	// Create an issue in the project
	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "Portal Issue", "type": "ticket", "status": "done",
	})
	assertStatus(t, resp, http.StatusCreated)
	issueID := responseID(t, resp)

	t.Run("external user sees no projects without access", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/projects", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var projects []struct{ ID int64 `json:"id"` }
		decode(t, resp, &projects)
		if len(projects) != 0 {
			t.Errorf("expected 0 projects, got %d", len(projects))
		}
	})

	t.Run("admin assigns project to external user", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{
			"project_id": projectID,
		})
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("external user sees assigned project", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/projects", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var projects []struct{ ID int64 `json:"id"` }
		decode(t, resp, &projects)
		if len(projects) != 1 {
			t.Fatalf("expected 1 project, got %d", len(projects))
		}
		if projects[0].ID != projectID {
			t.Errorf("project id = %d, want %d", projects[0].ID, projectID)
		}
	})

	t.Run("external user gets project detail", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var p struct {
			Name string `json:"name"`
		}
		decode(t, resp, &p)
		if p.Name != "Portal Project" {
			t.Errorf("name = %q, want Portal Project", p.Name)
		}
	})

	t.Run("external user cannot access unassigned project", func(t *testing.T) {
		// Create another project
		resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
			"name": "Other Project", "key": "OTH",
		})
		assertStatus(t, resp, http.StatusCreated)
		otherID := responseID(t, resp)
		resp = ts.get(t, fmt.Sprintf("/api/portal/projects/%d", otherID), ts.externalCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("external user lists issues", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			IssueKey string `json:"issue_key"`
		}
		decode(t, resp, &issues)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(issues))
		}
		if issues[0].Title != "Portal Issue" {
			t.Errorf("title = %q, want Portal Issue", issues[0].Title)
		}
	})

	// Create another issue for search testing
	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "Onboarding Checklist", "type": "ticket", "status": "backlog",
	})

	t.Run("portal issues filtered by q", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?q=onboarding", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct{ Title string `json:"title"` }
		decode(t, resp, &issues)
		if len(issues) != 1 || issues[0].Title != "Onboarding Checklist" {
			t.Errorf("expected 1 issue 'Onboarding Checklist', got %+v", issues)
		}
	})

	t.Run("portal issue has no internal fields", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues/%d", projectID, issueID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var raw map[string]interface{}
		decode(t, resp, &raw)
		// Should not have notes, assignee_id, tags, sprint_ids
		for _, forbidden := range []string{"notes", "assignee_id", "assignee", "tags", "sprint_ids"} {
			if _, ok := raw[forbidden]; ok {
				t.Errorf("portal issue should not have %q field", forbidden)
			}
		}
	})

	t.Run("external user accepts done issue", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/portal/issues/%d/accept", issueID), ts.externalCookie, nil)
		assertStatus(t, resp, http.StatusOK)
		var result struct {
			Accepted bool `json:"accepted"`
		}
		decode(t, resp, &result)
		if !result.Accepted {
			t.Error("expected accepted=true")
		}
	})

	t.Run("accept is idempotent", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/portal/issues/%d/accept", issueID), ts.externalCookie, nil)
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("external user submits request", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/portal/projects/%d/requests", projectID), ts.externalCookie, map[string]string{
			"title": "Customer Wish", "description": "Please add feature X",
		})
		assertStatus(t, resp, http.StatusCreated)
		var result struct {
			ID       int64  `json:"id"`
			IssueKey string `json:"issue_key"`
		}
		decode(t, resp, &result)
		if result.ID == 0 {
			t.Error("expected non-zero issue id")
		}
	})

	t.Run("portal request creates issue with new status", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/portal/projects/%d/requests", projectID), ts.externalCookie, map[string]string{
			"title": "Status Check", "description": "Verify default status",
		})
		assertStatus(t, resp, http.StatusCreated)
		var result struct{ ID int64 `json:"id"` }
		decode(t, resp, &result)
		// Fetch via admin to check the status
		resp = ts.get(t, fmt.Sprintf("/api/issues/%d", result.ID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var issue struct{ Status string `json:"status"` }
		decode(t, resp, &issue)
		if issue.Status != "new" {
			t.Errorf("portal request status = %q, want new", issue.Status)
		}
	})

	t.Run("project summary", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/summary", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var s struct {
			TotalIssues int `json:"total_issues"`
		}
		decode(t, resp, &s)
		if s.TotalIssues < 1 {
			t.Errorf("expected at least 1 issue in summary, got %d", s.TotalIssues)
		}
	})

	t.Run("admin can also access portal", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/projects", ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
	})
}

// ── User project access admin endpoints ─────────────────────────────────────

func TestQuick_UserProjectAccess(t *testing.T) {
	ts := newTestServer(t)

	var extUserID int64
	db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extUserID)

	// Create a project
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "UPA Test", "key": "UPA",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	t.Run("list empty", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var items []struct{ ProjectID int64 `json:"project_id"` }
		decode(t, resp, &items)
		if len(items) != 0 {
			t.Errorf("expected 0, got %d", len(items))
		}
	})

	t.Run("add project", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{
			"project_id": projectID,
		})
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("list shows project", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var items []struct{ ProjectID int64 `json:"project_id"` }
		decode(t, resp, &items)
		if len(items) != 1 || items[0].ProjectID != projectID {
			t.Errorf("expected 1 project with id %d, got %v", projectID, items)
		}
	})

	t.Run("duplicate add is idempotent", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{
			"project_id": projectID,
		})
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("remove project", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/users/%d/projects/%d", extUserID, projectID), ts.adminCookie)
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("list empty after remove", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var items []struct{ ProjectID int64 `json:"project_id"` }
		decode(t, resp, &items)
		if len(items) != 0 {
			t.Errorf("expected 0 after remove, got %d", len(items))
		}
	})

	t.Run("member cannot manage user projects", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.memberCookie)
		assertStatus(t, resp, http.StatusForbidden)
	})
}
