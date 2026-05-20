// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// PAI-475: comment visibility internal/external. Covers create defaulting,
// explicit override, validation, the PATCH visibility flip endpoint with
// authorization, and the portal-side filter on /api/portal/issues/{id}/comments.

package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

type commentRow struct {
	ID         int64  `json:"id"`
	Body       string `json:"body"`
	Visibility string `json:"visibility"`
	AuthorID   *int64 `json:"author_id"`
}

func grantPortalAccess(t *testing.T, projectID int64, username string) {
	t.Helper()
	var userID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&userID); err != nil {
		t.Fatalf("lookup %s: %v", username, err)
	}
	if _, err := db.DB.Exec(
		`INSERT OR REPLACE INTO project_members(user_id, project_id, access_level) VALUES(?, ?, ?)`,
		userID, projectID, "viewer",
	); err != nil {
		t.Fatalf("grant access: %v", err)
	}
}

func seedVisibilityIssue(t *testing.T) (projectID, issueID int64) {
	t.Helper()
	projectID = seedBatchProject(t, "Visibility Project", "VIS")
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?, ?, ?, ?, ?)`,
		projectID, 1, "ticket", "visible to nobody yet", "done",
	)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ = res.LastInsertId()
	return projectID, issueID
}

func createComment(t *testing.T, ts *testServer, issueID int64, cookie string, body map[string]any) *http.Response {
	t.Helper()
	return ts.post(t, fmt.Sprintf("/api/issues/%d/comments", issueID), cookie, body)
}

func TestQuick_CommentDefaultsToInternal(t *testing.T) {
	ts := newTestServer(t)
	_, issueID := seedVisibilityIssue(t)

	resp := createComment(t, ts, issueID, ts.adminCookie, map[string]any{"body": "no visibility specified"})
	assertStatus(t, resp, http.StatusCreated)

	var got commentRow
	decode(t, resp, &got)
	if got.Visibility != "internal" {
		t.Fatalf("default visibility: got %q want internal", got.Visibility)
	}
}

func TestQuick_CommentExplicitExternalPersists(t *testing.T) {
	ts := newTestServer(t)
	_, issueID := seedVisibilityIssue(t)

	resp := createComment(t, ts, issueID, ts.adminCookie, map[string]any{
		"body":       "customer-facing announcement",
		"visibility": "external",
	})
	assertStatus(t, resp, http.StatusCreated)

	var got commentRow
	decode(t, resp, &got)
	if got.Visibility != "external" {
		t.Fatalf("visibility: got %q want external", got.Visibility)
	}
}

func TestQuick_CommentInvalidVisibilityRejected(t *testing.T) {
	ts := newTestServer(t)
	_, issueID := seedVisibilityIssue(t)

	resp := createComment(t, ts, issueID, ts.adminCookie, map[string]any{
		"body":       "garbage flag",
		"visibility": "public", // not in enum
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestQuick_CommentVisibilityFlipByAuthor(t *testing.T) {
	ts := newTestServer(t)
	_, issueID := seedVisibilityIssue(t)

	// Member creates an internal comment, then flips it to external.
	resp := createComment(t, ts, issueID, ts.memberCookie, map[string]any{"body": "evolved my opinion"})
	assertStatus(t, resp, http.StatusCreated)
	var created commentRow
	decode(t, resp, &created)

	flip := ts.patch(t, fmt.Sprintf("/api/comments/%d", created.ID), ts.memberCookie,
		map[string]any{"visibility": "external"})
	assertStatus(t, flip, http.StatusOK)

	var after struct {
		Visibility string `json:"visibility"`
	}
	decode(t, flip, &after)
	if after.Visibility != "external" {
		t.Fatalf("flip response: visibility=%q want external", after.Visibility)
	}

	// Confirm DB persists.
	var dbVisibility string
	if err := db.DB.QueryRow(`SELECT visibility FROM comments WHERE id=?`, created.ID).Scan(&dbVisibility); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if dbVisibility != "external" {
		t.Fatalf("db visibility: got %q want external", dbVisibility)
	}

	// And confirm the mutation_log captured the change.
	var count int
	db.DB.QueryRow(`SELECT COUNT(*) FROM mutation_log
		WHERE subject_type='comment' AND subject_id=? AND mutation_type='issue.comment.visibility.change'`,
		created.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("mutation_log rows: got %d want 1", count)
	}
}

func TestQuick_CommentVisibilityFlipForbiddenForNonAuthor(t *testing.T) {
	ts := newTestServer(t)
	_, issueID := seedVisibilityIssue(t)

	// Admin authors a comment.
	resp := createComment(t, ts, issueID, ts.adminCookie, map[string]any{"body": "admin's words"})
	assertStatus(t, resp, http.StatusCreated)
	var created commentRow
	decode(t, resp, &created)

	// Member (not author, not admin) tries to flip it — should be 403.
	flip := ts.patch(t, fmt.Sprintf("/api/comments/%d", created.ID), ts.memberCookie,
		map[string]any{"visibility": "external"})
	assertStatus(t, flip, http.StatusForbidden)
}

func TestQuick_CommentVisibilityFlipAdminCanOverride(t *testing.T) {
	ts := newTestServer(t)
	_, issueID := seedVisibilityIssue(t)

	// Member authors internally.
	resp := createComment(t, ts, issueID, ts.memberCookie, map[string]any{"body": "team draft"})
	assertStatus(t, resp, http.StatusCreated)
	var created commentRow
	decode(t, resp, &created)

	// Admin flips it — admin override should succeed.
	flip := ts.patch(t, fmt.Sprintf("/api/comments/%d", created.ID), ts.adminCookie,
		map[string]any{"visibility": "external"})
	assertStatus(t, flip, http.StatusOK)
}

func TestQuick_PortalCommentsFilterExternalOnly(t *testing.T) {
	ts := newTestServer(t)
	projectID, issueID := seedVisibilityIssue(t)

	// Make the issue customer-visible by attaching the CUSTOMERPORTAL tag
	// (M109 ensures the tag exists). Without this the portal endpoint
	// returns 404 (which is the correct behavior, but not what we are
	// testing here).
	tagAllIssuesAsCustomerPortal(t, projectID)

	// Three comments: two internal, one external.
	for i, vis := range []string{"internal", "internal", "external"} {
		resp := createComment(t, ts, issueID, ts.adminCookie, map[string]any{
			"body":       fmt.Sprintf("comment %d", i+1),
			"visibility": vis,
		})
		assertStatus(t, resp, http.StatusCreated)
	}

	// Grant the external user portal access to this project.
	grantPortalAccess(t, projectID, "external")

	resp := ts.get(t, fmt.Sprintf("/api/portal/issues/%d/comments", issueID), ts.externalCookie)
	assertStatus(t, resp, http.StatusOK)
	body := readBody(t, resp)

	var got []commentRow
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode: %v — body: %s", err, body)
	}
	if len(got) != 1 {
		t.Fatalf("portal comments: got %d rows, want 1 — body: %s", len(got), body)
	}
	if got[0].Visibility != "external" {
		t.Fatalf("portal comment visibility: got %q want external", got[0].Visibility)
	}
}

func TestQuick_PortalCommentsHiddenWhenIssueLacksCustomerPortalTag(t *testing.T) {
	ts := newTestServer(t)
	projectID, issueID := seedVisibilityIssue(t)

	// Note: NO CUSTOMERPORTAL tag attached. The portal endpoint must
	// 404 — never disclose that an internal-only issue exists at this id.
	resp := createComment(t, ts, issueID, ts.adminCookie, map[string]any{
		"body":       "external but on a hidden issue",
		"visibility": "external",
	})
	assertStatus(t, resp, http.StatusCreated)

	grantPortalAccess(t, projectID, "external")

	resp = ts.get(t, fmt.Sprintf("/api/portal/issues/%d/comments", issueID), ts.externalCookie)
	assertStatus(t, resp, http.StatusNotFound)
}
