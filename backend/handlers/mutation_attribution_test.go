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

// PAI-354 — agent + session attribution on mutation_log rows for the
// three write surfaces that don't mint issue_history snapshots:
//
//   • POST /api/issues/:id/comments          — comment-add
//   • POST /api/issues/:id/tags              — tag-add
//   • DELETE /api/issues/:id/tags/:tag_id    — tag-remove
//   • POST /api/issues/:id/relations         — relation-add (already
//                                              wrote mutation_log via
//                                              recordMutation; this
//                                              test guards the new
//                                              agent_name column)
//   • DELETE /api/issues/:id/relations       — relation-remove (ditto)
//
// Coverage:
//   • headers present  → mutation_log row carries them.
//   • headers absent   → row has SQL NULL.
//   • header > 64 char → row carries the 64-char prefix.

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

// latestMutationLogAttribution returns the most recent mutation_log
// row for a given subject_type / subject_id pair. Tests anchor on
// subject_id (the issue id, the comment id, etc.) which is unique
// per-write — no risk of grabbing a sibling row.
func latestMutationLogAttribution(t *testing.T, subjectType string, subjectID int64) (agent *string, session *string, mutationType string) {
	t.Helper()
	var a, s sql.NullString
	var mt string
	err := db.DB.QueryRow(`
		SELECT agent_name, session_id, mutation_type
		FROM mutation_log
		WHERE subject_type = ? AND subject_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, subjectType, subjectID).Scan(&a, &s, &mt)
	if err != nil {
		t.Fatalf("latestMutationLogAttribution(%s,%d): %v", subjectType, subjectID, err)
	}
	if a.Valid {
		v := a.String
		agent = &v
	}
	if s.Valid {
		v := s.String
		session = &v
	}
	return agent, session, mt
}

// latestCommentID returns the highest-id comment for an issue — the one
// the most recent POST /comments just inserted.
func latestCommentID(t *testing.T, issueID int64) int64 {
	t.Helper()
	var id int64
	if err := db.DB.QueryRow(`SELECT id FROM comments WHERE issue_id = ? ORDER BY id DESC LIMIT 1`, issueID).Scan(&id); err != nil {
		t.Fatalf("latestCommentID(%d): %v", issueID, err)
	}
	return id
}

func seedMutAttrProject(t *testing.T, ts *testServer, name, key string) int64 {
	t.Helper()
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": name, "key": key,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project %s/%s: status=%d body=%s", name, key, resp.StatusCode, body)
	}
	return responseID(t, resp)
}

func seedMutAttrIssue(t *testing.T, ts *testServer, projectID int64, title string) int64 {
	t.Helper()
	resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
		"title": title, "type": "ticket", "status": "backlog", "priority": "medium",
	})
	assertStatus(t, resp, http.StatusCreated)
	return responseID(t, resp)
}

// ── Comment-add ────────────────────────────────────────────────────────

// TestMutationAttribution_CommentAdd_WithHeaders covers the load-bearing
// path: POST /api/issues/{id}/comments with both X-Paimos-Agent-Name
// and X-Paimos-Session-Id persists them onto the mutation_log row.
func TestMutationAttribution_CommentAdd_WithHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Comment", "MAC")
	issueID := seedMutAttrIssue(t, ts, projectID, "C1")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/comments", issueID),
		ts.adminCookie,
		map[string]any{"body": "from agent ops"},
		map[string]string{
			"X-Paimos-Agent-Name": "ops",
			"X-Paimos-Session-Id": "1f6046a7-cccc-dddd-eeee-1234567890ab",
		})
	assertStatus(t, resp, http.StatusCreated)

	commentID := latestCommentID(t, issueID)
	agent, session, mt := latestMutationLogAttribution(t, "comment", commentID)
	if agent == nil || *agent != "ops" {
		t.Errorf("agent_name = %v, want \"ops\"", agent)
	}
	if session == nil || *session != "1f6046a7-cccc-dddd-eeee-1234567890ab" {
		t.Errorf("session_id = %v, want canonical UUID", session)
	}
	if mt != "issue.comment.create" {
		t.Errorf("mutation_type = %q, want \"issue.comment.create\"", mt)
	}
}

// TestMutationAttribution_CommentAdd_WithoutHeaders covers the
// backwards-compat path: POST without headers succeeds and stores NULL
// for both attribution columns.
func TestMutationAttribution_CommentAdd_WithoutHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Comment 2", "MA2")
	issueID := seedMutAttrIssue(t, ts, projectID, "C2")

	resp := ts.post(t,
		fmt.Sprintf("/api/issues/%d/comments", issueID),
		ts.adminCookie,
		map[string]any{"body": "from web ui"})
	assertStatus(t, resp, http.StatusCreated)

	commentID := latestCommentID(t, issueID)
	agent, session, _ := latestMutationLogAttribution(t, "comment", commentID)
	if agent != nil {
		t.Errorf("agent_name = %v, want NULL", *agent)
	}
	if session != nil {
		t.Errorf("session_id = %v, want NULL", *session)
	}
}

// TestMutationAttribution_CommentAdd_LongHeaderTruncation guards the
// 64-char cap (handlers.agentAttrCap) — defensive against accidental
// log-spam payloads. SQLite ALTER TABLE can't add CHECK retroactively
// so the truncation lives in the handler.
func TestMutationAttribution_CommentAdd_LongHeaderTruncation(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Comment Long", "MCL")
	issueID := seedMutAttrIssue(t, ts, projectID, "C3")

	longAgent := strings.Repeat("a", 200)
	longSession := strings.Repeat("s", 200)

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/comments", issueID),
		ts.adminCookie,
		map[string]any{"body": "long-headers"},
		map[string]string{
			"X-Paimos-Agent-Name": longAgent,
			"X-Paimos-Session-Id": longSession,
		})
	assertStatus(t, resp, http.StatusCreated)

	commentID := latestCommentID(t, issueID)
	agent, session, _ := latestMutationLogAttribution(t, "comment", commentID)
	if agent == nil || len(*agent) != 64 {
		t.Errorf("agent_name not capped at 64: got %v", agent)
	}
	if session == nil || len(*session) != 64 {
		t.Errorf("session_id not capped at 64: got %v", session)
	}
}

// ── Tag-add / Tag-remove ───────────────────────────────────────────────

// TestMutationAttribution_TagAdd_WithHeaders covers POST /tags persisting
// attribution onto the mutation_log row. Subject is the issue id (the
// tag-add row uses subject_type='issue_tag', subject_id=issue_id).
func TestMutationAttribution_TagAdd_WithHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Tag", "MAT")
	issueID := seedMutAttrIssue(t, ts, projectID, "T1")
	tagID := firstTagID(t)

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/tags", issueID),
		ts.adminCookie,
		map[string]any{"tag_id": tagID},
		map[string]string{
			"X-Paimos-Agent-Name": "tooling",
			"X-Paimos-Session-Id": "6c4b3d2f-aaaa-bbbb-cccc-deadbeef0010",
		})
	assertStatus(t, resp, http.StatusNoContent)

	agent, session, mt := latestMutationLogAttribution(t, "issue_tag", issueID)
	if agent == nil || *agent != "tooling" {
		t.Errorf("agent_name = %v, want \"tooling\"", agent)
	}
	if session == nil || *session != "6c4b3d2f-aaaa-bbbb-cccc-deadbeef0010" {
		t.Errorf("session_id = %v, want canonical UUID", session)
	}
	if mt != "issue.tag.add" {
		t.Errorf("mutation_type = %q, want \"issue.tag.add\"", mt)
	}
}

// TestMutationAttribution_TagAdd_WithoutHeaders — backwards-compat: row
// has NULL when the headers are absent.
func TestMutationAttribution_TagAdd_WithoutHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Tag 2", "MT2")
	issueID := seedMutAttrIssue(t, ts, projectID, "T2")
	tagID := firstTagID(t)

	resp := ts.post(t,
		fmt.Sprintf("/api/issues/%d/tags", issueID),
		ts.adminCookie,
		map[string]any{"tag_id": tagID})
	assertStatus(t, resp, http.StatusNoContent)

	agent, session, _ := latestMutationLogAttribution(t, "issue_tag", issueID)
	if agent != nil {
		t.Errorf("agent_name = %v, want NULL", *agent)
	}
	if session != nil {
		t.Errorf("session_id = %v, want NULL", *session)
	}
}

// TestMutationAttribution_TagRemove_WithHeaders covers DELETE /tags/{tag_id}
// — the mutation_type=issue.tag.remove row carries attribution.
func TestMutationAttribution_TagRemove_WithHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Tag Rm", "MTR")
	issueID := seedMutAttrIssue(t, ts, projectID, "T3")
	tagID := firstTagID(t)

	// Attach first (without headers — keeps the test focused on the
	// remove path's attribution).
	addResp := ts.post(t,
		fmt.Sprintf("/api/issues/%d/tags", issueID),
		ts.adminCookie,
		map[string]any{"tag_id": tagID})
	assertStatus(t, addResp, http.StatusNoContent)

	rmResp := ts.requestWithHeaders(t, http.MethodDelete,
		fmt.Sprintf("/api/issues/%d/tags/%d", issueID, tagID),
		ts.adminCookie,
		nil,
		map[string]string{
			"X-Paimos-Agent-Name": "dev",
			"X-Paimos-Session-Id": "7d5c4e3a-bbbb-cccc-dddd-deadbeef0011",
		})
	assertStatus(t, rmResp, http.StatusNoContent)

	agent, session, mt := latestMutationLogAttribution(t, "issue_tag", issueID)
	if agent == nil || *agent != "dev" {
		t.Errorf("agent_name = %v, want \"dev\"", agent)
	}
	if session == nil || *session != "7d5c4e3a-bbbb-cccc-dddd-deadbeef0011" {
		t.Errorf("session_id = %v, want canonical UUID", session)
	}
	if mt != "issue.tag.remove" {
		t.Errorf("mutation_type = %q, want \"issue.tag.remove\"", mt)
	}
}

// ── Relation-add / Relation-remove ─────────────────────────────────────

// TestMutationAttribution_RelationAdd_WithHeaders covers POST /relations.
// The relations handler already used recordMutation for SessionID — this
// test guards the new agent_name column on the same row.
func TestMutationAttribution_RelationAdd_WithHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Rel", "MAR")
	src := seedMutAttrIssue(t, ts, projectID, "R-source")
	tgt := seedMutAttrIssue(t, ts, projectID, "R-target")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/relations", src),
		ts.adminCookie,
		map[string]any{"target_id": tgt, "type": "related"},
		map[string]string{
			"X-Paimos-Agent-Name": "refinement",
			"X-Paimos-Session-Id": "8e6d5f4b-cccc-dddd-eeee-deadbeef0012",
		})
	assertStatus(t, resp, http.StatusCreated)

	agent, session, mt := latestMutationLogAttribution(t, "issue_relation", src)
	if agent == nil || *agent != "refinement" {
		t.Errorf("agent_name = %v, want \"refinement\"", agent)
	}
	if session == nil || *session != "8e6d5f4b-cccc-dddd-eeee-deadbeef0012" {
		t.Errorf("session_id = %v, want canonical UUID", session)
	}
	if mt != "issue.relation.create" {
		t.Errorf("mutation_type = %q, want \"issue.relation.create\"", mt)
	}
}

// TestMutationAttribution_RelationAdd_WithoutHeaders — NULL on absence.
func TestMutationAttribution_RelationAdd_WithoutHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Rel 2", "MR2")
	src := seedMutAttrIssue(t, ts, projectID, "R2-source")
	tgt := seedMutAttrIssue(t, ts, projectID, "R2-target")

	resp := ts.post(t,
		fmt.Sprintf("/api/issues/%d/relations", src),
		ts.adminCookie,
		map[string]any{"target_id": tgt, "type": "related"})
	assertStatus(t, resp, http.StatusCreated)

	agent, session, _ := latestMutationLogAttribution(t, "issue_relation", src)
	if agent != nil {
		t.Errorf("agent_name = %v, want NULL", *agent)
	}
	if session != nil {
		t.Errorf("session_id = %v, want NULL", *session)
	}
}

// TestMutationAttribution_RelationRemove_WithHeaders covers
// DELETE /relations and the issue.relation.delete mutation_type.
func TestMutationAttribution_RelationRemove_WithHeaders(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Rel Rm", "MRR")
	src := seedMutAttrIssue(t, ts, projectID, "R3-source")
	tgt := seedMutAttrIssue(t, ts, projectID, "R3-target")

	addResp := ts.post(t,
		fmt.Sprintf("/api/issues/%d/relations", src),
		ts.adminCookie,
		map[string]any{"target_id": tgt, "type": "related"})
	assertStatus(t, addResp, http.StatusCreated)

	rmResp := ts.requestWithHeaders(t, http.MethodDelete,
		fmt.Sprintf("/api/issues/%d/relations", src),
		ts.adminCookie,
		map[string]any{"target_id": tgt, "type": "related"},
		map[string]string{
			"X-Paimos-Agent-Name": "ops",
			"X-Paimos-Session-Id": "9f7e6a5c-dddd-eeee-ffff-deadbeef0013",
		})
	assertStatus(t, rmResp, http.StatusNoContent)

	agent, session, mt := latestMutationLogAttribution(t, "issue_relation", src)
	if agent == nil || *agent != "ops" {
		t.Errorf("agent_name = %v, want \"ops\"", agent)
	}
	if session == nil || *session != "9f7e6a5c-dddd-eeee-ffff-deadbeef0013" {
		t.Errorf("session_id = %v, want canonical UUID", session)
	}
	if mt != "issue.relation.delete" {
		t.Errorf("mutation_type = %q, want \"issue.relation.delete\"", mt)
	}
}

// TestMutationAttribution_LongHeader_Truncation_AcrossPaths makes sure
// the 64-char cap fires on tag-add and relation-add as well — these
// share the agentNameFromRequest / sessionIDFromRequest helpers but the
// path coverage here is what catches a future caller forgetting to use
// the helpers.
func TestMutationAttribution_LongHeader_Truncation_AcrossPaths(t *testing.T) {
	ts := newTestServer(t)
	longAgent := strings.Repeat("L", 200)
	longSession := strings.Repeat("S", 200)

	t.Run("tag.add", func(t *testing.T) {
		projectID := seedMutAttrProject(t, ts, "MutAttr Tag Long", "MTL")
		issueID := seedMutAttrIssue(t, ts, projectID, "TL")
		tagID := firstTagID(t)
		resp := ts.requestWithHeaders(t, http.MethodPost,
			fmt.Sprintf("/api/issues/%d/tags", issueID),
			ts.adminCookie,
			map[string]any{"tag_id": tagID},
			map[string]string{
				"X-Paimos-Agent-Name": longAgent,
				"X-Paimos-Session-Id": longSession,
			})
		assertStatus(t, resp, http.StatusNoContent)
		agent, session, _ := latestMutationLogAttribution(t, "issue_tag", issueID)
		if agent == nil || len(*agent) != 64 {
			t.Errorf("tag.add agent_name not capped at 64: got %v", agent)
		}
		if session == nil || len(*session) != 64 {
			t.Errorf("tag.add session_id not capped at 64: got %v", session)
		}
	})

	t.Run("relation.create", func(t *testing.T) {
		projectID := seedMutAttrProject(t, ts, "MutAttr Rel Long", "MRL")
		src := seedMutAttrIssue(t, ts, projectID, "RL-source")
		tgt := seedMutAttrIssue(t, ts, projectID, "RL-target")
		resp := ts.requestWithHeaders(t, http.MethodPost,
			fmt.Sprintf("/api/issues/%d/relations", src),
			ts.adminCookie,
			map[string]any{"target_id": tgt, "type": "related"},
			map[string]string{
				"X-Paimos-Agent-Name": longAgent,
				"X-Paimos-Session-Id": longSession,
			})
		assertStatus(t, resp, http.StatusCreated)
		agent, session, _ := latestMutationLogAttribution(t, "issue_relation", src)
		if agent == nil || len(*agent) != 64 {
			t.Errorf("relation.create agent_name not capped at 64: got %v", agent)
		}
		if session == nil || len(*session) != 64 {
			t.Errorf("relation.create session_id not capped at 64: got %v", session)
		}
	})
}

// TestMutationAttribution_EmptyHeaders_StoreNull guards the
// empty-string-vs-NULL contract on the new write paths: whitespace-only
// header values behave the same as the header being absent.
func TestMutationAttribution_EmptyHeaders_StoreNull(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedMutAttrProject(t, ts, "MutAttr Empty", "MAE")
	issueID := seedMutAttrIssue(t, ts, projectID, "E1")

	resp := ts.requestWithHeaders(t, http.MethodPost,
		fmt.Sprintf("/api/issues/%d/comments", issueID),
		ts.adminCookie,
		map[string]any{"body": "empty-headers"},
		map[string]string{
			"X-Paimos-Agent-Name": "   ",
			"X-Paimos-Session-Id": "",
		})
	assertStatus(t, resp, http.StatusCreated)

	commentID := latestCommentID(t, issueID)
	agent, session, _ := latestMutationLogAttribution(t, "comment", commentID)
	if agent != nil {
		t.Errorf("agent_name = %v, want NULL for whitespace-only header", *agent)
	}
	if session != nil {
		t.Errorf("session_id = %v, want NULL for empty header", *session)
	}
}
