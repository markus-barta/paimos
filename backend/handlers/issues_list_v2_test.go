package handlers_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

func seedListV2Issue(t *testing.T, projectID int64, num int, title string, status string) int64 {
	t.Helper()
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status, priority) VALUES(?,?,?,?,?,?)`,
		projectID, num, "ticket", title, status, "medium",
	)
	if err != nil {
		t.Fatalf("seed issue %s: %v", title, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedListV2AgentRun(t *testing.T, projectID int64, issueID int64, status string) int64 {
	t.Helper()
	res, err := db.DB.Exec(
		`INSERT INTO agent_runs(issue_id, project_id, status, agent_name, device_id) VALUES(?,?,?,?,?)`,
		issueID, projectID, status, "test-agent", "dev-test",
	)
	if err != nil {
		t.Fatalf("seed agent run %s: %v", status, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestIssueListV2ProjectEnvelopeSortAndWindow(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedBatchProject(t, "PAI", "PAI")
	seedListV2Issue(t, projectID, 1, "Gamma", "backlog")
	seedListV2Issue(t, projectID, 2, "Alpha", "backlog")
	seedListV2Issue(t, projectID, 3, "Beta", "done")

	resp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues?envelope=1&fields=list&limit=2&offset=0&sort=title&order=asc", projectID), ts.adminCookie)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, body=%s", resp.StatusCode, b)
	}
	var first struct {
		Issues []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"issues"`
		Total                int    `json:"total"`
		Offset               int    `json:"offset"`
		Limit                int    `json:"limit"`
		Returned             int    `json:"returned"`
		HasMore              bool   `json:"has_more"`
		Sort                 string `json:"sort"`
		Order                string `json:"order"`
		Revision             string `json:"revision"`
		Fingerprint          string `json:"fingerprint"`
		SelectionFingerprint string `json:"selection_fingerprint"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&first)
	if first.Total != 3 || first.Offset != 0 || first.Limit != 2 || first.Returned != 2 || !first.HasMore {
		t.Fatalf("bad envelope metadata: %+v", first)
	}
	if first.Sort != "title" || first.Order != "asc" {
		t.Fatalf("bad sort metadata: sort=%q order=%q", first.Sort, first.Order)
	}
	if first.Revision == "" || strings.ContainsAny(first.Revision, `/"`) {
		t.Fatalf("revision should be the bare list revision hash, got %q", first.Revision)
	}
	if first.Fingerprint == "" || first.SelectionFingerprint == "" || first.Fingerprint == first.SelectionFingerprint {
		t.Fatalf("bad fingerprints: fingerprint=%q selection=%q", first.Fingerprint, first.SelectionFingerprint)
	}
	if got := []string{first.Issues[0].Title, first.Issues[1].Title}; got[0] != "Alpha" || got[1] != "Beta" {
		t.Fatalf("page 1 order=%v, want Alpha,Beta", got)
	}
	if first.Issues[0].Description != "" {
		t.Fatal("fields=list should strip large body fields")
	}

	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/issues?envelope=1&fields=list&limit=2&offset=2&sort=title&order=asc", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var second struct {
		Issues []struct {
			Title string `json:"title"`
		} `json:"issues"`
		Total                int    `json:"total"`
		Offset               int    `json:"offset"`
		Returned             int    `json:"returned"`
		HasMore              bool   `json:"has_more"`
		Fingerprint          string `json:"fingerprint"`
		SelectionFingerprint string `json:"selection_fingerprint"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&second)
	if second.Total != 3 || second.Offset != 2 || second.Returned != 1 || second.HasMore || len(second.Issues) != 1 || second.Issues[0].Title != "Gamma" {
		t.Fatalf("page 2 envelope=%+v, want only Gamma", second)
	}
	if second.Fingerprint != first.Fingerprint || second.SelectionFingerprint != first.SelectionFingerprint {
		t.Fatalf("fingerprints should be stable across windows: first=%q/%q second=%q/%q",
			first.Fingerprint, first.SelectionFingerprint, second.Fingerprint, second.SelectionFingerprint)
	}
}

func TestIssueListV2IdsOnlyStaysProjectScoped(t *testing.T) {
	ts := newTestServer(t)
	p1 := seedBatchProject(t, "PAI", "PAI")
	p2 := seedBatchProject(t, "OTHER", "OTH")
	want1 := seedListV2Issue(t, p1, 1, "Project backlog", "backlog")
	_ = seedListV2Issue(t, p1, 2, "Project done", "done")
	_ = seedListV2Issue(t, p2, 1, "Other backlog", "backlog")

	resp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues?ids_only=1&status=backlog", p1), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var body struct {
		IDs         []int64 `json:"ids"`
		Total       int     `json:"total"`
		Truncated   bool    `json:"truncated"`
		Fingerprint string  `json:"fingerprint"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 1 || len(body.IDs) != 1 || body.IDs[0] != want1 || body.Truncated || body.Fingerprint == "" {
		t.Fatalf("ids_only leaked or miscounted: %+v, want [%d]", body, want1)
	}
}

func TestIssueListV2GlobalSortValidationAndNegatedProjectFilter(t *testing.T) {
	ts := newTestServer(t)
	p1 := seedBatchProject(t, "PAI", "PAI")
	p2 := seedBatchProject(t, "OTHER", "OTH")
	seedListV2Issue(t, p1, 1, "Alpha", "backlog")
	seedListV2Issue(t, p2, 1, "Beta", "backlog")

	resp := ts.get(t, "/api/issues?fields=list&limit=10&offset=0&sort=internal_note", ts.adminCookie)
	assertStatus(t, resp, http.StatusBadRequest)

	resp = ts.get(t, fmt.Sprintf("/api/issues?fields=list&limit=10&offset=0&project_ids=!%d&sort=key&order=asc", p1), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var env struct {
		Issues []struct {
			ProjectID int64  `json:"project_id"`
			IssueKey  string `json:"issue_key"`
		} `json:"issues"`
		Total       int    `json:"total"`
		Returned    int    `json:"returned"`
		HasMore     bool   `json:"has_more"`
		Fingerprint string `json:"fingerprint"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&env)
	if env.Total != 1 || env.Returned != 1 || env.HasMore || env.Fingerprint == "" || len(env.Issues) != 1 || env.Issues[0].ProjectID != p2 || env.Issues[0].IssueKey != "OTH-1" {
		t.Fatalf("negated project_ids response=%+v, want OTH-1 only", env)
	}
}

func TestIssueListV2AIWorkStatusFilterAndSort(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedBatchProject(t, "PAI", "PAI")
	seedListV2Issue(t, projectID, 1, "No run", "backlog")
	queued := seedListV2Issue(t, projectID, 2, "Queued run", "backlog")
	deployed := seedListV2Issue(t, projectID, 3, "Deployed latest", "backlog")
	failed := seedListV2Issue(t, projectID, 4, "Failed run", "backlog")

	seedListV2AgentRun(t, projectID, queued, "queued")
	seedListV2AgentRun(t, projectID, deployed, "failed")
	seedListV2AgentRun(t, projectID, deployed, "deployed")
	seedListV2AgentRun(t, projectID, failed, "failed")

	titlesFor := func(path string) []string {
		t.Helper()
		resp := ts.get(t, path, ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		var env struct {
			Issues []struct {
				Title        string `json:"title"`
				AIWorkStatus *struct {
					Status string `json:"status"`
				} `json:"ai_work_status"`
			} `json:"issues"`
			Total int `json:"total"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&env)
		titles := make([]string, 0, len(env.Issues))
		for _, iss := range env.Issues {
			titles = append(titles, iss.Title)
		}
		return titles
	}

	base := fmt.Sprintf("/api/projects/%d/issues?envelope=1&fields=list&limit=0", projectID)
	if got := titlesFor(base + "&ai_status=deployed"); len(got) != 1 || got[0] != "Deployed latest" {
		t.Fatalf("ai_status=deployed titles=%v, want Deployed latest", got)
	}
	if got := titlesFor(base + "&ai_work_status=queued"); len(got) != 1 || got[0] != "Queued run" {
		t.Fatalf("ai_work_status alias titles=%v, want Queued run", got)
	}
	if got := titlesFor(base + "&ai_status=none"); len(got) != 1 || got[0] != "No run" {
		t.Fatalf("ai_status=none titles=%v, want No run", got)
	}
	if got := titlesFor(base + "&ai_status=!failed&sort=ai_status&order=asc"); strings.Join(got, ",") != "No run,Queued run,Deployed latest" {
		t.Fatalf("ai_status=!failed sorted titles=%v", got)
	}
	if got := titlesFor(base + "&sort=ai_status&order=asc"); strings.Join(got, ",") != "No run,Queued run,Deployed latest,Failed run" {
		t.Fatalf("sort=ai_status titles=%v", got)
	}
}

func TestIssueListV2GlobalLimitZeroMeansUnbounded(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedBatchProject(t, "PAI", "PAI")
	seedListV2Issue(t, projectID, 1, "One", "backlog")
	seedListV2Issue(t, projectID, 2, "Two", "backlog")
	seedListV2Issue(t, projectID, 3, "Three", "backlog")

	resp := ts.get(t, "/api/issues?fields=list&limit=0&offset=0&sort=key&order=asc", ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	var env struct {
		Issues []struct {
			IssueKey string `json:"issue_key"`
		} `json:"issues"`
		Total    int  `json:"total"`
		Limit    int  `json:"limit"`
		Returned int  `json:"returned"`
		HasMore  bool `json:"has_more"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&env)
	if env.Total != 3 || env.Limit != 0 || env.Returned != 3 || env.HasMore || len(env.Issues) != 3 {
		t.Fatalf("limit=0 envelope=%+v, want all three issues with no more pages", env)
	}
}

func TestPortalIssueListV2EnvelopeSearchAndWindow(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedBatchProject(t, "Portal", "PRT")
	seedListV2Issue(t, projectID, 1, "Alpha request", "backlog")
	seedListV2Issue(t, projectID, 2, "Alphabet report", "done")
	seedListV2Issue(t, projectID, 3, "Beta request", "backlog")
	tagAllIssuesAsCustomerPortal(t, projectID)

	var externalID int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='external'`).Scan(&externalID); err != nil {
		t.Fatalf("external user missing: %v", err)
	}
	if _, err := db.DB.Exec(
		`INSERT OR REPLACE INTO project_members(project_id, user_id, access_level) VALUES(?,?,?)`,
		projectID, externalID, "viewer",
	); err != nil {
		t.Fatalf("grant portal access: %v", err)
	}

	resp := ts.get(t, fmt.Sprintf("/api/portal/projects/%d/issues?envelope=1&q=alp&limit=1&offset=0&sort=title&order=asc", projectID), ts.externalCookie)
	assertStatus(t, resp, http.StatusOK)
	var env struct {
		Issues []struct {
			Title string `json:"title"`
		} `json:"issues"`
		Total                int    `json:"total"`
		Limit                int    `json:"limit"`
		Returned             int    `json:"returned"`
		HasMore              bool   `json:"has_more"`
		Query                string `json:"query"`
		Sort                 string `json:"sort"`
		Order                string `json:"order"`
		Fingerprint          string `json:"fingerprint"`
		SelectionFingerprint string `json:"selection_fingerprint"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&env)
	if env.Total != 2 || env.Limit != 1 || env.Returned != 1 || !env.HasMore || env.Query != "alp" || env.Sort != "title" || env.Order != "asc" || env.Fingerprint == "" || env.SelectionFingerprint == "" {
		t.Fatalf("bad portal envelope metadata: %+v", env)
	}
	if len(env.Issues) != 1 || env.Issues[0].Title != "Alpha request" {
		t.Fatalf("portal page = %+v, want first Alpha request", env.Issues)
	}
}
