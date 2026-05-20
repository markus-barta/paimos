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
		var env struct {
			User struct {
				Role string `json:"role"`
			} `json:"user"`
		}
		decode(t, resp, &env)
		if env.User.Role != "external" {
			t.Errorf("role = %q, want external", env.User.Role)
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

	t.Run("external viewer cannot accept done issue", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/portal/issues/%d/accept", issueID), ts.externalCookie, nil)
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("admin grants editor access to external user", func(t *testing.T) {
		resp := ts.put(t, fmt.Sprintf("/api/users/%d/memberships/%d", extUserID, projectID), ts.adminCookie, map[string]string{
			"access_level": "editor",
		})
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("external editor accepts done issue", func(t *testing.T) {
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

// ── Portal overview (PAI-452) ───────────────────────────────────────────────

func TestQuick_PortalOverview(t *testing.T) {
	ts := newTestServer(t)

	var extUserID int64
	db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extUserID)

	// Two projects; only one is granted to the external user.
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Overview Granted", "key": "OVG",
	})
	assertStatus(t, resp, http.StatusCreated)
	grantedID := responseID(t, resp)

	resp = ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Overview Hidden", "key": "OVH",
	})
	assertStatus(t, resp, http.StatusCreated)
	hiddenID := responseID(t, resp)

	// Three issues on the granted project: one in-progress (open), one
	// delivered (awaiting), one done (awaiting). One issue on the hidden
	// project that we must NOT see in the queue.
	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", grantedID), ts.adminCookie, map[string]any{
		"title": "Open one", "type": "ticket", "status": "in-progress",
	})
	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", grantedID), ts.adminCookie, map[string]any{
		"title": "Delivered one", "type": "ticket", "status": "delivered",
	})
	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", grantedID), ts.adminCookie, map[string]any{
		"title": "Done one", "type": "ticket", "status": "done",
	})
	ts.post(t, fmt.Sprintf("/api/projects/%d/issues", hiddenID), ts.adminCookie, map[string]any{
		"title": "Other-project delivered", "type": "ticket", "status": "delivered",
	})

	// Grant the external user editor on the first project only.
	resp = ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{
		"project_id": grantedID,
	})
	assertStatus(t, resp, http.StatusCreated)
	resp = ts.put(t, fmt.Sprintf("/api/users/%d/memberships/%d", extUserID, grantedID), ts.adminCookie, map[string]string{
		"access_level": "editor",
	})
	assertStatus(t, resp, http.StatusOK)

	type awaiting struct {
		IssueKey    string `json:"issue_key"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		ProjectID   int64  `json:"project_id"`
		ProjectName string `json:"project_name"`
		CanEdit     bool   `json:"can_edit"`
	}
	type overview struct {
		KPIs struct {
			ActiveProjects     int `json:"active_projects"`
			OpenIssues         int `json:"open_issues"`
			AwaitingAcceptance int `json:"awaiting_acceptance"`
			AcceptedThisMonth  int `json:"accepted_this_month"`
		} `json:"kpis"`
		Projects []struct {
			ID       int64          `json:"id"`
			Name     string         `json:"name"`
			ByStatus map[string]int `json:"by_status"`
		} `json:"projects"`
		AwaitingAcceptance []awaiting `json:"awaiting_acceptance"`
	}

	t.Run("external user sees only granted project + its awaiting items", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/overview", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var ov overview
		decode(t, resp, &ov)
		if ov.KPIs.ActiveProjects != 1 {
			t.Errorf("active_projects = %d, want 1", ov.KPIs.ActiveProjects)
		}
		if ov.KPIs.AwaitingAcceptance != 2 {
			t.Errorf("awaiting_acceptance KPI = %d, want 2", ov.KPIs.AwaitingAcceptance)
		}
		if ov.KPIs.OpenIssues != 1 {
			t.Errorf("open_issues = %d, want 1 (only in-progress; delivered/done are awaiting)", ov.KPIs.OpenIssues)
		}
		if len(ov.AwaitingAcceptance) != 2 {
			t.Fatalf("awaiting list = %d, want 2", len(ov.AwaitingAcceptance))
		}
		for _, a := range ov.AwaitingAcceptance {
			if a.ProjectID != grantedID {
				t.Errorf("awaiting item leaked from project %d (hidden)", a.ProjectID)
			}
			if !a.CanEdit {
				t.Errorf("external editor should have can_edit=true, got false for %s", a.IssueKey)
			}
		}
		if len(ov.Projects) != 1 || ov.Projects[0].ID != grantedID {
			t.Errorf("projects = %+v, want only granted project", ov.Projects)
		}
		// Status breakdown should record the in-progress + delivered + done counts.
		bs := ov.Projects[0].ByStatus
		if bs["in-progress"] != 1 || bs["delivered"] != 1 || bs["done"] != 1 {
			t.Errorf("by_status = %v, want in-progress:1 delivered:1 done:1", bs)
		}
	})

	t.Run("viewer-level external sees can_edit=false", func(t *testing.T) {
		// Downgrade to viewer.
		resp := ts.put(t, fmt.Sprintf("/api/users/%d/memberships/%d", extUserID, grantedID), ts.adminCookie, map[string]string{
			"access_level": "viewer",
		})
		assertStatus(t, resp, http.StatusOK)

		resp = ts.get(t, "/api/portal/overview", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var ov overview
		decode(t, resp, &ov)
		if len(ov.AwaitingAcceptance) == 0 {
			t.Fatal("expected awaiting items to still appear for viewer")
		}
		for _, a := range ov.AwaitingAcceptance {
			if a.CanEdit {
				t.Errorf("viewer should have can_edit=false, got true for %s", a.IssueKey)
			}
		}
	})

	t.Run("admin sees every active project", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/overview", ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var ov overview
		decode(t, resp, &ov)
		if ov.KPIs.ActiveProjects < 2 {
			t.Errorf("admin active_projects = %d, want >= 2", ov.KPIs.ActiveProjects)
		}
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
