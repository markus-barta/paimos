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

// tagAllIssuesAsCustomerPortal back-tags every non-deleted issue in the
// given project with the CUSTOMERPORTAL system tag. Useful for tests
// written before PAI-460 made portal visibility opt-in: the older tests
// seed issues via the admin path and want them visible in the portal
// endpoints, which now require the tag. The migration backfill (PAI-462)
// does the same thing in production; this helper is its test-time twin.
func tagAllIssuesAsCustomerPortal(t *testing.T, projectID int64) {
	t.Helper()
	if _, err := db.DB.Exec(`
		INSERT OR IGNORE INTO issue_tags (issue_id, tag_id)
		SELECT i.id, t.id
		FROM issues i, tags t
		WHERE i.project_id = ? AND i.deleted_at IS NULL AND t.name = 'CUSTOMERPORTAL'
	`, projectID); err != nil {
		t.Fatalf("backfill CUSTOMERPORTAL for project %d: %v", projectID, err)
	}
}

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

	// PAI-460: portal endpoints require CUSTOMERPORTAL. These tests
	// pre-date the visibility tag and want the seed issues visible; the
	// helper mirrors the PAI-462 migration backfill in test scope.
	tagAllIssuesAsCustomerPortal(t, projectID)

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
	tagAllIssuesAsCustomerPortal(t, projectID) // PAI-460: keep the new issue visible.

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

// PAI-459: portal submissions must auto-attach CUSTOMERPORTAL, the
// CUSTOMERPORTAL system tag must be undeletable even by admin, and the
// existing tag-attach API must allow CUSTOMERPORTAL through despite its
// system flag (the toggle in IssueDetailView relies on this exemption).
func TestQuick_PortalCustomerPortalTag(t *testing.T) {
	ts := newTestServer(t)

	var portalTagID int64
	if err := db.DB.QueryRow(`SELECT id FROM tags WHERE name='CUSTOMERPORTAL'`).Scan(&portalTagID); err != nil {
		t.Fatalf("CUSTOMERPORTAL tag missing — migration 109 didn't run: %v", err)
	}
	var sys int
	if err := db.DB.QueryRow(`SELECT system FROM tags WHERE id=?`, portalTagID).Scan(&sys); err != nil {
		t.Fatalf("read system flag: %v", err)
	}
	if sys != 1 {
		t.Fatalf("CUSTOMERPORTAL system flag = %d, want 1", sys)
	}

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Portal-Tag Project", "key": "PCT",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	var extUserID int64
	db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extUserID)
	resp = ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{
		"project_id": projectID,
	})
	assertStatus(t, resp, http.StatusCreated)
	resp = ts.put(t, fmt.Sprintf("/api/users/%d/memberships/%d", extUserID, projectID), ts.adminCookie, map[string]string{
		"access_level": "editor",
	})
	assertStatus(t, resp, http.StatusOK)

	t.Run("portal submission auto-tags issue with CUSTOMERPORTAL", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/portal/projects/%d/requests", projectID), ts.externalCookie, map[string]string{
			"title": "Auto-tag check", "description": "Verify CUSTOMERPORTAL gets attached.",
		})
		assertStatus(t, resp, http.StatusCreated)
		var result struct {
			ID int64 `json:"id"`
		}
		decode(t, resp, &result)
		if result.ID == 0 {
			t.Fatal("submission returned zero id")
		}

		var hit int
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, result.ID, portalTagID).Scan(&hit); err != nil {
			t.Fatalf("scan issue_tags: %v", err)
		}
		if hit != 1 {
			t.Fatalf("portal-submitted issue %d not tagged with CUSTOMERPORTAL", result.ID)
		}

		var mtype string
		if err := db.DB.QueryRow(`SELECT mutation_type FROM mutation_log WHERE subject_type='issue_tag' AND subject_id=? ORDER BY id DESC LIMIT 1`, result.ID).Scan(&mtype); err != nil {
			t.Fatalf("mutation_log lookup: %v", err)
		}
		if mtype != "portal.submit.auto_tag" {
			t.Errorf("mutation_log type = %q, want portal.submit.auto_tag", mtype)
		}
	})

	t.Run("admin cannot delete the CUSTOMERPORTAL tag", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/tags/%d", portalTagID), ts.adminCookie)
		assertStatus(t, resp, http.StatusForbidden)

		var still int
		db.DB.QueryRow(`SELECT COUNT(*) FROM tags WHERE id=?`, portalTagID).Scan(&still)
		if still != 1 {
			t.Errorf("CUSTOMERPORTAL tag removed despite 403; count=%d", still)
		}
	})

	t.Run("internal user can attach + detach CUSTOMERPORTAL via the standard tag API", func(t *testing.T) {
		// Make a vanilla internal issue to toggle.
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": "Toggle target", "type": "ticket", "status": "done",
		})
		assertStatus(t, resp, http.StatusCreated)
		issueID := responseID(t, resp)

		resp = ts.post(t, fmt.Sprintf("/api/issues/%d/tags", issueID), ts.adminCookie, map[string]any{
			"tag_id": portalTagID,
		})
		assertStatus(t, resp, http.StatusNoContent)

		var hit int
		db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, issueID, portalTagID).Scan(&hit)
		if hit != 1 {
			t.Fatalf("attach didn't land; count=%d", hit)
		}

		resp = ts.del(t, fmt.Sprintf("/api/issues/%d/tags/%d", issueID, portalTagID), ts.adminCookie)
		assertStatus(t, resp, http.StatusNoContent)

		db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, issueID, portalTagID).Scan(&hit)
		if hit != 0 {
			t.Errorf("detach didn't land; count=%d", hit)
		}
	})

	t.Run("other system tags still blocked from manual attach", func(t *testing.T) {
		// Create a second synthetic system tag to prove only CUSTOMERPORTAL
		// gets the exemption. The migration set covers no other system
		// tags by name, so we insert directly.
		res, err := db.DB.Exec(`INSERT INTO tags(name,color,description,system) VALUES('TEST_SYSTEM','red','synthetic',1)`)
		if err != nil {
			t.Fatalf("seed system tag: %v", err)
		}
		otherID, _ := res.LastInsertId()
		t.Cleanup(func() {
			db.DB.Exec(`DELETE FROM tags WHERE id=?`, otherID)
		})

		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": "Block target", "type": "ticket", "status": "new",
		})
		assertStatus(t, resp, http.StatusCreated)
		issueID := responseID(t, resp)

		resp = ts.post(t, fmt.Sprintf("/api/issues/%d/tags", issueID), ts.adminCookie, map[string]any{
			"tag_id": otherID,
		})
		assertStatus(t, resp, http.StatusForbidden)
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

	// PAI-460: portal endpoints filter by CUSTOMERPORTAL — backfill both
	// projects so the existing assertions (issue counts, awaiting queue,
	// status rollup) reflect every seeded issue. The hidden-project issue
	// must also be tagged so we exercise the access gate, not the
	// visibility filter.
	tagAllIssuesAsCustomerPortal(t, grantedID)
	tagAllIssuesAsCustomerPortal(t, hiddenID)

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

// PAI-460: every issue-returning portal endpoint must constrain its
// output to issues carrying CUSTOMERPORTAL. This covers the 20-issue
// project with 5 tagged shape, the KPI rollup on /overview, the
// project-summary status counts, the 404-not-403 contract on the
// per-issue endpoint, the in-process tag-id cache, and the snapshot
// override (projektbericht remains readable even for untagged issues).
func TestQuick_PortalCustomerPortalFilter(t *testing.T) {
	ts := newTestServer(t)

	var portalTagID int64
	if err := db.DB.QueryRow(`SELECT id FROM tags WHERE name='CUSTOMERPORTAL'`).Scan(&portalTagID); err != nil {
		t.Fatalf("CUSTOMERPORTAL tag missing: %v", err)
	}

	var extUserID int64
	db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extUserID)

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Filter Test", "key": "FLT",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	resp = ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{
		"project_id": projectID,
	})
	assertStatus(t, resp, http.StatusCreated)
	resp = ts.put(t, fmt.Sprintf("/api/users/%d/memberships/%d", extUserID, projectID), ts.adminCookie, map[string]string{
		"access_level": "editor",
	})
	assertStatus(t, resp, http.StatusOK)

	// Create 20 issues: 5 tagged with CUSTOMERPORTAL (visible), 15 not (hidden).
	// Distribute statuses so the rollup is meaningful.
	visibleStatuses := []string{"in-progress", "in-progress", "delivered", "done", "done"}
	hiddenStatuses := []string{"new", "in-progress", "delivered", "done", "accepted",
		"new", "in-progress", "delivered", "done", "accepted",
		"new", "in-progress", "delivered", "done", "accepted"}
	var visibleIDs []int64
	var hiddenIDs []int64
	for i, st := range visibleStatuses {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": fmt.Sprintf("VIS-%d", i+1), "type": "ticket", "status": st,
		})
		assertStatus(t, resp, http.StatusCreated)
		id := responseID(t, resp)
		visibleIDs = append(visibleIDs, id)
		if _, err := db.DB.Exec(`INSERT OR IGNORE INTO issue_tags(issue_id, tag_id) VALUES(?, ?)`, id, portalTagID); err != nil {
			t.Fatalf("attach CUSTOMERPORTAL: %v", err)
		}
	}
	for i, st := range hiddenStatuses {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": fmt.Sprintf("HID-%d", i+1), "type": "ticket", "status": st,
		})
		assertStatus(t, resp, http.StatusCreated)
		hiddenIDs = append(hiddenIDs, responseID(t, resp))
	}

	t.Run("portal issues list returns only the 5 tagged items", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct {
			Title string `json:"title"`
		}
		decode(t, resp, &issues)
		if len(issues) != 5 {
			t.Fatalf("issues list count = %d, want 5", len(issues))
		}
		for _, iss := range issues {
			if iss.Title[:3] != "VIS" {
				t.Errorf("untagged issue leaked into portal list: %q", iss.Title)
			}
		}
	})

	t.Run("per-issue fetch returns 404 (not 403) for untagged", func(t *testing.T) {
		hiddenID := hiddenIDs[0]
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues/%d", projectID, hiddenID), ts.externalCookie)
		// 404 — existence not disclosed. A 403 would tell the customer the
		// id is real, which is the exact leak we're preventing.
		assertStatus(t, resp, http.StatusNotFound)
	})

	t.Run("per-issue fetch succeeds for tagged", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues/%d", projectID, visibleIDs[0]), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("project summary counts are visible-only", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/summary", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var s struct {
			TotalIssues int            `json:"total_issues"`
			ByStatus    map[string]int `json:"by_status"`
		}
		decode(t, resp, &s)
		if s.TotalIssues != 5 {
			t.Errorf("summary total_issues = %d, want 5", s.TotalIssues)
		}
		// visibleStatuses had 2 in-progress, 1 delivered, 2 done.
		if s.ByStatus["in-progress"] != 2 || s.ByStatus["delivered"] != 1 || s.ByStatus["done"] != 2 {
			t.Errorf("summary by_status = %v, want in-progress:2 delivered:1 done:2", s.ByStatus)
		}
	})

	t.Run("overview KPIs reflect visible-only counts", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/overview", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var ov struct {
			KPIs struct {
				OpenIssues         int `json:"open_issues"`
				AwaitingAcceptance int `json:"awaiting_acceptance"`
			} `json:"kpis"`
			Projects []struct {
				ID         int64          `json:"id"`
				IssueCount int            `json:"issue_count"`
				ByStatus   map[string]int `json:"by_status"`
			} `json:"projects"`
			AwaitingAcceptance []struct {
				Title string `json:"title"`
			} `json:"awaiting_acceptance"`
		}
		decode(t, resp, &ov)

		// Open = in-progress = 2. Awaiting = delivered + done = 3.
		if ov.KPIs.OpenIssues != 2 {
			t.Errorf("overview open_issues = %d, want 2", ov.KPIs.OpenIssues)
		}
		if ov.KPIs.AwaitingAcceptance != 3 {
			t.Errorf("overview awaiting_acceptance = %d, want 3", ov.KPIs.AwaitingAcceptance)
		}
		if len(ov.AwaitingAcceptance) != 3 {
			t.Errorf("overview awaiting list = %d, want 3", len(ov.AwaitingAcceptance))
		}
		for _, a := range ov.AwaitingAcceptance {
			if a.Title[:3] != "VIS" {
				t.Errorf("untagged issue %q leaked into overview awaiting list", a.Title)
			}
		}
		if len(ov.Projects) != 1 || ov.Projects[0].IssueCount != 5 {
			t.Errorf("project issue_count = %v, want a single project with 5", ov.Projects)
		}
		bs := ov.Projects[0].ByStatus
		if bs["in-progress"] != 2 || bs["delivered"] != 1 || bs["done"] != 2 {
			t.Errorf("overview project by_status = %v, want in-progress:2 delivered:1 done:2", bs)
		}
	})

	t.Run("projects list shows the project with visible-only counts", func(t *testing.T) {
		resp := ts.get(t, "/api/portal/projects", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var list []struct {
			ID         int64 `json:"id"`
			IssueCount int   `json:"issue_count"`
			DoneCount  int   `json:"done_count"`
		}
		decode(t, resp, &list)
		var found *struct {
			ID         int64 `json:"id"`
			IssueCount int   `json:"issue_count"`
			DoneCount  int   `json:"done_count"`
		}
		for i := range list {
			if list[i].ID == projectID {
				found = &list[i]
				break
			}
		}
		if found == nil {
			t.Fatal("granted project missing from portal list")
		}
		if found.IssueCount != 5 || found.DoneCount != 2 {
			t.Errorf("portal project counts = %+v, want issue_count=5 done_count=2", *found)
		}
	})

	t.Run("project detail endpoint uses visible-only counts", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var pd struct {
			IssueCount int `json:"issue_count"`
			DoneCount  int `json:"done_count"`
		}
		decode(t, resp, &pd)
		if pd.IssueCount != 5 || pd.DoneCount != 2 {
			t.Errorf("project detail counts = %+v, want issue_count=5 done_count=2", pd)
		}
	})

	t.Run("PAI-461 status multi-select returns only requested + visible", func(t *testing.T) {
		// visibleStatuses had 1 delivered + 2 done; hidden had several
		// delivered/done too. status=done,delivered must return 3 (only
		// visible).
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?status=done,delivered", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		decode(t, resp, &issues)
		if len(issues) != 3 {
			t.Fatalf("status=done,delivered count = %d, want 3", len(issues))
		}
		for _, iss := range issues {
			if iss.Status != "done" && iss.Status != "delivered" {
				t.Errorf("issue %q has status %q outside requested set", iss.Title, iss.Status)
			}
			if iss.Title[:3] != "VIS" {
				t.Errorf("untagged issue %q leaked through multi-select", iss.Title)
			}
		}
	})

	t.Run("PAI-461 type=memory is rejected with 400", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?type=memory", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("PAI-461 type=ticket,task is accepted", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?type=ticket,task", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("PAI-461 sort=internal_note is rejected with 400", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?sort=internal_note", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("PAI-461 sort=key&order=asc orders by issue_number ascending", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?sort=key&order=asc", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct {
			IssueKey string `json:"issue_key"`
		}
		decode(t, resp, &issues)
		// Ascending key order on the 5 visible issues (VIS-1 → VIS-5)
		// translates to ascending issue_number — verify the first two are
		// the lowest of the bunch by simple string comparison on key suffix.
		if len(issues) < 2 || issues[0].IssueKey >= issues[1].IssueKey {
			t.Errorf("ascending sort failed: %+v", issues)
		}
	})

	t.Run("PAI-461 order=sideways is rejected with 400", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?order=sideways", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("projektbericht snapshot is an explicit visibility override", func(t *testing.T) {
		// Seed a minimal snapshot that embeds an untagged (hidden) issue.
		// The /projektberichte/accept/{code} endpoint should still return
		// the snapshot for portal users — the snapshot is its own
		// disclosure unit.
		hiddenID := hiddenIDs[0]
		code := "FLT-SNAP-1"
		_, err := db.DB.Exec(`
			INSERT INTO project_report_snapshots
				(project_id, code, report_type, lang, issue_ids_json,
				 total_issues, pdf_sha256, status, created_at)
			VALUES (?, ?, 'projektbericht', 'de', ?, 1, 'deadbeef', 'generated', datetime('now'))
		`, projectID, code, fmt.Sprintf("[%d]", hiddenID))
		if err != nil {
			t.Fatalf("seed snapshot: %v", err)
		}
		resp := ts.get(t, fmt.Sprintf("/api/projektberichte/accept/%s", code), ts.externalCookie)
		// Snapshot path doesn't apply the CUSTOMERPORTAL filter — the
		// untagged issue must still be readable through it.
		assertStatus(t, resp, http.StatusOK)
	})
}

// PAI-462: the one-time backfill must auto-tag every terminal-status
// issue idempotently and write a mutation_log row per backfilled issue
// for the audit trail. The dry-run env var must turn the filter off and
// surface would_hide_count per project on /overview.
func TestQuick_PortalCustomerPortalBackfill(t *testing.T) {
	ts := newTestServer(t)

	var portalTagID int64
	if err := db.DB.QueryRow(`SELECT id FROM tags WHERE name='CUSTOMERPORTAL'`).Scan(&portalTagID); err != nil {
		t.Fatalf("CUSTOMERPORTAL tag missing: %v", err)
	}

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Backfill Test", "key": "BFL",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	// Seed 4 terminal-status + 2 non-terminal issues, none CUSTOMERPORTAL-tagged.
	terminal := []string{"delivered", "done", "accepted", "invoiced"}
	var terminalIDs []int64
	for i, st := range terminal {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": fmt.Sprintf("TERM-%d", i+1), "type": "ticket", "status": st,
		})
		assertStatus(t, resp, http.StatusCreated)
		terminalIDs = append(terminalIDs, responseID(t, resp))
	}
	nonTerminal := []string{"new", "in-progress"}
	var nonTerminalIDs []int64
	for i, st := range nonTerminal {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": fmt.Sprintf("OPEN-%d", i+1), "type": "ticket", "status": st,
		})
		assertStatus(t, resp, http.StatusCreated)
		nonTerminalIDs = append(nonTerminalIDs, responseID(t, resp))
	}

	// Run the backfill block from migration M110 in the test scope. (The
	// migration itself runs as part of test-server setup before any
	// issues exist; we replay it to exercise idempotency on a populated
	// schema.)
	runBackfill := func() {
		t.Helper()
		_, err := db.DB.Exec(`
			CREATE TEMPORARY TABLE _bf AS
			SELECT i.id AS issue_id, t.id AS tag_id
			FROM issues i, tags t
			WHERE i.deleted_at IS NULL
			  AND i.status IN ('delivered','done','accepted','invoiced')
			  AND t.name = 'CUSTOMERPORTAL'
			  AND NOT EXISTS (
			    SELECT 1 FROM issue_tags it WHERE it.issue_id = i.id AND it.tag_id = t.id
			  )`)
		if err != nil {
			t.Fatalf("backfill temp: %v", err)
		}
		if _, err := db.DB.Exec(`INSERT INTO issue_tags(issue_id, tag_id) SELECT issue_id, tag_id FROM _bf`); err != nil {
			t.Fatalf("backfill insert tags: %v", err)
		}
		if _, err := db.DB.Exec(`
			INSERT INTO mutation_log
			  (request_id, mutation_type, subject_type, subject_id,
			   batch_id, inverse_op, before_state, after_state,
			   before_hash, after_hash, undoable, on_user_stack)
			SELECT 'migration:m110-test', 'issue.tag.migration_backfill',
			       'issue_tag', issue_id,
			       'm110-customerportal-backfill', '{}',
			       json_object('issue_id', issue_id, 'tag_id', tag_id, 'exists', 0),
			       json_object('issue_id', issue_id, 'tag_id', tag_id, 'exists', 1),
			       '', '', 0, 0
			FROM _bf`); err != nil {
			t.Fatalf("backfill insert audit: %v", err)
		}
		if _, err := db.DB.Exec(`DROP TABLE _bf`); err != nil {
			t.Fatalf("backfill drop temp: %v", err)
		}
	}

	t.Run("backfill tags every terminal-status issue, leaves open ones alone", func(t *testing.T) {
		runBackfill()

		for _, id := range terminalIDs {
			var hit int
			db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, id, portalTagID).Scan(&hit)
			if hit != 1 {
				t.Errorf("terminal issue %d not backfilled", id)
			}
		}
		for _, id := range nonTerminalIDs {
			var hit int
			db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, id, portalTagID).Scan(&hit)
			if hit != 0 {
				t.Errorf("non-terminal issue %d was tagged by backfill (should not be)", id)
			}
		}
	})

	t.Run("backfill writes one audit row per tagged issue", func(t *testing.T) {
		var cnt int
		db.DB.QueryRow(`
			SELECT COUNT(*) FROM mutation_log
			WHERE mutation_type='issue.tag.migration_backfill'
			  AND batch_id='m110-customerportal-backfill'
			  AND subject_id IN (?,?,?,?)`,
			terminalIDs[0], terminalIDs[1], terminalIDs[2], terminalIDs[3]).Scan(&cnt)
		if cnt != 4 {
			t.Errorf("expected 4 audit rows, got %d", cnt)
		}
	})

	t.Run("re-running the backfill is a no-op", func(t *testing.T) {
		var auditBefore, tagsBefore int
		db.DB.QueryRow(`SELECT COUNT(*) FROM mutation_log WHERE batch_id='m110-customerportal-backfill'`).Scan(&auditBefore)
		db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE tag_id=?`, portalTagID).Scan(&tagsBefore)

		runBackfill()

		var auditAfter, tagsAfter int
		db.DB.QueryRow(`SELECT COUNT(*) FROM mutation_log WHERE batch_id='m110-customerportal-backfill'`).Scan(&auditAfter)
		db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE tag_id=?`, portalTagID).Scan(&tagsAfter)

		if auditBefore != auditAfter {
			t.Errorf("re-run added audit rows: before=%d after=%d", auditBefore, auditAfter)
		}
		if tagsBefore != tagsAfter {
			t.Errorf("re-run added tag rows: before=%d after=%d", tagsBefore, tagsAfter)
		}
	})

	var extUserID int64
	db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extUserID)
	ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{"project_id": projectID})
	ts.put(t, fmt.Sprintf("/api/users/%d/memberships/%d", extUserID, projectID), ts.adminCookie, map[string]string{"access_level": "editor"})

	t.Run("dry-run env var leaves the portal list unfiltered", func(t *testing.T) {
		t.Setenv("PAIMOS_PORTAL_VISIBILITY_DRY_RUN", "true")

		// With dry-run on, even the 2 non-tagged open issues should appear.
		resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues", projectID), ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var issues []struct {
			Title string `json:"title"`
		}
		decode(t, resp, &issues)
		if len(issues) != 6 {
			t.Errorf("dry-run: expected all 6 issues, got %d (%+v)", len(issues), issues)
		}
	})

	t.Run("dry-run /overview exposes would_hide_count per project", func(t *testing.T) {
		t.Setenv("PAIMOS_PORTAL_VISIBILITY_DRY_RUN", "true")

		resp := ts.get(t, "/api/portal/overview", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var ov struct {
			Projects []struct {
				ID             int64 `json:"id"`
				WouldHideCount *int  `json:"would_hide_count"`
			} `json:"projects"`
		}
		decode(t, resp, &ov)
		var found bool
		for _, p := range ov.Projects {
			if p.ID == projectID {
				found = true
				if p.WouldHideCount == nil {
					t.Fatal("dry-run: would_hide_count missing on project entry")
				}
				// Two open issues are untagged → would_hide_count = 2.
				if *p.WouldHideCount != 2 {
					t.Errorf("would_hide_count = %d, want 2", *p.WouldHideCount)
				}
				break
			}
		}
		if !found {
			t.Fatal("project not found in overview")
		}
	})

	t.Run("without dry-run /overview omits would_hide_count", func(t *testing.T) {
		// (no t.Setenv → env var unset → enforcement on)
		resp := ts.get(t, "/api/portal/overview", ts.externalCookie)
		assertStatus(t, resp, http.StatusOK)
		var ov struct {
			Projects []struct {
				ID             int64 `json:"id"`
				WouldHideCount *int  `json:"would_hide_count"`
			} `json:"projects"`
		}
		decode(t, resp, &ov)
		for _, p := range ov.Projects {
			if p.ID == projectID && p.WouldHideCount != nil {
				t.Errorf("would_hide_count leaked outside dry-run mode: got %d", *p.WouldHideCount)
			}
		}
	})
}

// PAI-463: the IssueDetailView visibility-toggle endpoint must reflect
// the current CUSTOMERPORTAL attachment state and surface the most
// recent mutation_log event for the audit-line.
func TestQuick_IssuePortalVisibility(t *testing.T) {
	ts := newTestServer(t)

	var portalTagID int64
	if err := db.DB.QueryRow(`SELECT id FROM tags WHERE name='CUSTOMERPORTAL'`).Scan(&portalTagID); err != nil {
		t.Fatalf("CUSTOMERPORTAL tag missing: %v", err)
	}

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Visibility Endpoint", "key": "VIS",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": "Untagged issue", "type": "ticket", "status": "new",
	})
	assertStatus(t, resp, http.StatusCreated)
	issueID := responseID(t, resp)

	type visResp struct {
		Visible   bool `json:"visible"`
		LastEvent *struct {
			Actor string `json:"actor"`
			At    string `json:"at"`
			Type  string `json:"type"`
		} `json:"last_event"`
	}

	t.Run("untouched issue: visible=false, no last_event", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/issues/%d/portal-visibility", issueID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var out visResp
		decode(t, resp, &out)
		if out.Visible {
			t.Errorf("visible = true on untouched issue")
		}
		if out.LastEvent != nil {
			t.Errorf("last_event = %+v, want nil", out.LastEvent)
		}
	})

	t.Run("after admin toggles on: visible=true, last_event=toggle_add", func(t *testing.T) {
		resp := ts.post(t, fmt.Sprintf("/api/issues/%d/tags", issueID), ts.adminCookie, map[string]any{
			"tag_id": portalTagID,
		})
		assertStatus(t, resp, http.StatusNoContent)

		resp = ts.get(t, fmt.Sprintf("/api/issues/%d/portal-visibility", issueID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var out visResp
		decode(t, resp, &out)
		if !out.Visible {
			t.Error("visible = false after attach")
		}
		if out.LastEvent == nil {
			t.Fatal("last_event missing after attach")
		}
		if out.LastEvent.Actor != "admin" {
			t.Errorf("last_event.actor = %q, want admin", out.LastEvent.Actor)
		}
		if out.LastEvent.Type != "toggle_add" {
			t.Errorf("last_event.type = %q, want toggle_add", out.LastEvent.Type)
		}
		if out.LastEvent.At == "" {
			t.Error("last_event.at empty")
		}
	})

	t.Run("after detach: visible=false, last_event=toggle_remove", func(t *testing.T) {
		resp := ts.del(t, fmt.Sprintf("/api/issues/%d/tags/%d", issueID, portalTagID), ts.adminCookie)
		assertStatus(t, resp, http.StatusNoContent)

		resp = ts.get(t, fmt.Sprintf("/api/issues/%d/portal-visibility", issueID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var out visResp
		decode(t, resp, &out)
		if out.Visible {
			t.Error("visible = true after detach")
		}
		if out.LastEvent == nil || out.LastEvent.Type != "toggle_remove" {
			t.Errorf("last_event = %+v, want type=toggle_remove", out.LastEvent)
		}
	})

	t.Run("portal-submitted issue: last_event=auto_tag", func(t *testing.T) {
		var extUserID int64
		db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extUserID)
		ts.post(t, fmt.Sprintf("/api/users/%d/projects", extUserID), ts.adminCookie, map[string]any{"project_id": projectID})

		resp := ts.post(t, fmt.Sprintf("/api/portal/projects/%d/requests", projectID), ts.externalCookie, map[string]string{
			"title": "Auto-tag visibility check",
		})
		assertStatus(t, resp, http.StatusCreated)
		var submitted struct {
			ID int64 `json:"id"`
		}
		decode(t, resp, &submitted)

		resp = ts.get(t, fmt.Sprintf("/api/issues/%d/portal-visibility", submitted.ID), ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var out visResp
		decode(t, resp, &out)
		if !out.Visible || out.LastEvent == nil || out.LastEvent.Type != "auto_tag" {
			t.Errorf("portal submission visibility = %+v, want visible=true type=auto_tag", out)
		}
		if out.LastEvent.Actor != "external" {
			t.Errorf("last_event.actor = %q, want external", out.LastEvent.Actor)
		}
	})
}

// PAI-465: bulk attach/detach of a single tag across N issues, with the
// shared mutation_log batch_id, per-issue permission gate, system-tag
// exemption parity, and idempotent semantics matching the singular API.
func TestQuick_BatchTagIssues(t *testing.T) {
	ts := newTestServer(t)

	var portalTagID int64
	if err := db.DB.QueryRow(`SELECT id FROM tags WHERE name='CUSTOMERPORTAL'`).Scan(&portalTagID); err != nil {
		t.Fatalf("CUSTOMERPORTAL tag missing: %v", err)
	}

	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Batch Tag", "key": "BTG",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	var ids []int64
	for i := 0; i < 3; i++ {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": fmt.Sprintf("BTG-%d", i+1), "type": "ticket", "status": "new",
		})
		assertStatus(t, resp, http.StatusCreated)
		ids = append(ids, responseID(t, resp))
	}

	t.Run("bulk add attaches CUSTOMERPORTAL across all listed issues", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
			"issue_ids": ids, "tag_id": portalTagID, "op": "add",
		})
		assertStatus(t, resp, http.StatusOK)
		var out struct {
			BatchID  string `json:"batch_id"`
			Affected int    `json:"affected"`
			Op       string `json:"op"`
		}
		decode(t, resp, &out)
		if out.Op != "add" {
			t.Errorf("op = %q, want add", out.Op)
		}
		if out.Affected != 3 {
			t.Errorf("affected = %d, want 3", out.Affected)
		}
		if out.BatchID == "" {
			t.Error("batch_id empty")
		}

		// Every issue carries the tag.
		for _, id := range ids {
			var hit int
			db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, id, portalTagID).Scan(&hit)
			if hit != 1 {
				t.Errorf("issue %d not tagged after bulk add", id)
			}
		}

		// Audit rows share batch_id and use the bulk_add mutation_type.
		var cnt int
		db.DB.QueryRow(`SELECT COUNT(*) FROM mutation_log WHERE batch_id=? AND mutation_type='issue.tag.bulk_add'`, out.BatchID).Scan(&cnt)
		if cnt != 3 {
			t.Errorf("expected 3 audit rows with batch_id=%s, got %d", out.BatchID, cnt)
		}
	})

	t.Run("re-running bulk add is a no-op (affected=0, no duplicate rows)", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
			"issue_ids": ids, "tag_id": portalTagID, "op": "add",
		})
		assertStatus(t, resp, http.StatusOK)
		var out struct {
			Affected int `json:"affected"`
		}
		decode(t, resp, &out)
		if out.Affected != 0 {
			t.Errorf("affected on re-run = %d, want 0", out.Affected)
		}
	})

	t.Run("bulk remove detaches across all", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
			"issue_ids": ids, "tag_id": portalTagID, "op": "remove",
		})
		assertStatus(t, resp, http.StatusOK)
		var out struct {
			Affected int    `json:"affected"`
			Op       string `json:"op"`
		}
		decode(t, resp, &out)
		if out.Op != "remove" || out.Affected != 3 {
			t.Errorf("remove result = %+v, want op=remove affected=3", out)
		}
		for _, id := range ids {
			var hit int
			db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, id, portalTagID).Scan(&hit)
			if hit != 0 {
				t.Errorf("issue %d still tagged after bulk remove", id)
			}
		}
	})

	t.Run("non-admin without editor on one project fails the whole batch with 403", func(t *testing.T) {
		// External users default to no access on un-granted projects, so
		// they're the natural fit for testing the mixed-permission gate.
		// (Members default to implicit editor and would slip through.)
		resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
			"name": "Locked Project", "key": "LCK",
		})
		assertStatus(t, resp, http.StatusCreated)
		lockedPid := responseID(t, resp)
		resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", lockedPid), ts.adminCookie, map[string]any{
			"title": "Locked Issue", "type": "ticket", "status": "new",
		})
		assertStatus(t, resp, http.StatusCreated)
		lockedID := responseID(t, resp)

		var extID int64
		db.DB.QueryRow("SELECT id FROM users WHERE username='external'").Scan(&extID)
		// External gets editor on BTG only; never granted on LCK.
		resp = ts.post(t, fmt.Sprintf("/api/users/%d/projects", extID), ts.adminCookie, map[string]any{"project_id": projectID})
		assertStatus(t, resp, http.StatusCreated)
		resp = ts.put(t, fmt.Sprintf("/api/users/%d/memberships/%d", extID, projectID), ts.adminCookie, map[string]string{"access_level": "editor"})
		assertStatus(t, resp, http.StatusOK)

		// Sanity — make sure the BTG issues aren't already tagged from a
		// prior subtest (the previous bulk_remove cleared them).
		// Mixed-permission selection: BTG issues (allowed) + LCK issue (denied).
		mixed := append([]int64{}, ids...)
		mixed = append(mixed, lockedID)
		resp = ts.post(t, "/api/issues/batch/tags", ts.externalCookie, map[string]any{
			"issue_ids": mixed, "tag_id": portalTagID, "op": "add",
		})
		assertStatus(t, resp, http.StatusForbidden)

		// And verify the BTG issues stayed untouched — the whole batch
		// must roll back, not just the denied one.
		for _, id := range ids {
			var hit int
			db.DB.QueryRow(`SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?`, id, portalTagID).Scan(&hit)
			if hit != 0 {
				t.Errorf("partial commit: issue %d tagged despite 403", id)
			}
		}
	})

	t.Run("other system tags still rejected", func(t *testing.T) {
		res, err := db.DB.Exec(`INSERT INTO tags(name,color,description,system) VALUES('TEST_BULK_SYSTEM','red','synthetic',1)`)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		sysID, _ := res.LastInsertId()
		t.Cleanup(func() {
			db.DB.Exec(`DELETE FROM tags WHERE id=?`, sysID)
		})

		resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
			"issue_ids": ids, "tag_id": sysID, "op": "add",
		})
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("rejects empty issue_ids", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
			"issue_ids": []int64{}, "tag_id": portalTagID, "op": "add",
		})
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("rejects unknown op", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
			"issue_ids": ids, "tag_id": portalTagID, "op": "toggle",
		})
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("rejects missing issue ids", func(t *testing.T) {
		resp := ts.post(t, "/api/issues/batch/tags", ts.adminCookie, map[string]any{
			"issue_ids": []int64{99999, 99998}, "tag_id": portalTagID, "op": "add",
		})
		assertStatus(t, resp, http.StatusNotFound)
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
