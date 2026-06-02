package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
)

func getWithINM(t *testing.T, ts *testServer, path, cookie, etag string) *http.Response {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.srv.URL+path, nil)
	req.Header.Set("Cookie", cookie)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s with If-None-Match: %v", path, err)
	}
	return resp
}

func TestIssueListEndpointsEmitAndHonorETags(t *testing.T) {
	ts := newTestServer(t)

	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "ETag Project",
		"key":  "ETAG",
	}))
	projectPath := strconv.FormatInt(projectID, 10)
	issueID := responseID(t, ts.post(t, "/api/projects/"+projectPath+"/issues", ts.adminCookie, map[string]any{
		"title":    "First issue",
		"status":   "backlog",
		"priority": "medium",
		"type":     "ticket",
	}))
	ts.post(t, "/api/projects/"+projectPath+"/issues", ts.adminCookie, map[string]any{
		"title":    "Second issue",
		"status":   "done",
		"priority": "medium",
		"type":     "ticket",
	})

	paths := []string{
		"/api/projects/" + projectPath + "/issues?fields=list",
		"/api/projects/" + projectPath + "/issues/tree",
		"/api/issues?fields=list&limit=100&offset=0",
		"/api/issues/recent",
	}

	for _, path := range paths {
		resp := ts.get(t, path, ts.adminCookie)
		assertStatus(t, resp, http.StatusOK)
		etag := resp.Header.Get("ETag")
		if etag == "" {
			t.Fatalf("%s missing ETag", path)
		}
		resp.Body.Close()

		notModified := getWithINM(t, ts, path, ts.adminCookie, etag)
		assertStatus(t, notModified, http.StatusNotModified)
		notModified.Body.Close()
	}

	before := ts.get(t, "/api/projects/"+projectPath+"/issues?fields=list&status=backlog", ts.adminCookie)
	assertStatus(t, before, http.StatusOK)
	backlogEtag := before.Header.Get("ETag")
	before.Body.Close()

	after := ts.get(t, "/api/projects/"+projectPath+"/issues?fields=list&status=done", ts.adminCookie)
	assertStatus(t, after, http.StatusOK)
	doneEtag := after.Header.Get("ETag")
	after.Body.Close()

	if backlogEtag == doneEtag {
		t.Fatal("expected filter-specific ETags to differ")
	}

	update := ts.put(t, "/api/issues/"+strconv.FormatInt(issueID, 10), ts.adminCookie, map[string]any{
		"title":    "First issue updated",
		"status":   "backlog",
		"priority": "medium",
		"type":     "ticket",
	})
	assertStatus(t, update, http.StatusOK)
	update.Body.Close()

	updated := ts.get(t, "/api/projects/"+projectPath+"/issues?fields=list", ts.adminCookie)
	assertStatus(t, updated, http.StatusOK)
	if updated.Header.Get("ETag") == "" || updated.Header.Get("ETag") == backlogEtag {
		t.Fatal("expected ETag to change after issue mutation")
	}
	updated.Body.Close()
}

// bookedForIssue decodes a list envelope and returns booked_hours for issueID.
func bookedForIssue(t *testing.T, resp *http.Response, issueID int64) float64 {
	t.Helper()
	var env struct {
		Issues []struct {
			ID          int64   `json:"id"`
			BookedHours float64 `json:"booked_hours"`
		} `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode list envelope: %v", err)
	}
	for _, i := range env.Issues {
		if i.ID == issueID {
			return i.BookedHours
		}
	}
	t.Fatalf("issue %d not present in list", issueID)
	return 0
}

// TestIssueListETagInvalidatesOnTimeEntry reproduces PAI-577: booking time must
// invalidate the issue-list conditional-GET ETag so the BOOKED column refreshes,
// instead of the server replying 304 and the client keeping a stale value (the
// reported bug — it even survived a hard reload because programmatic fetches
// still send If-None-Match).
func TestIssueListETagInvalidatesOnTimeEntry(t *testing.T) {
	ts := newTestServer(t)
	projectID := responseID(t, ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Booked Project",
		"key":  "BKD",
	}))
	pp := strconv.FormatInt(projectID, 10)
	issueID := responseID(t, ts.post(t, "/api/projects/"+pp+"/issues", ts.adminCookie, map[string]any{
		"title":    "Bookable",
		"status":   "backlog",
		"priority": "medium",
		"type":     "ticket",
	}))

	listPath := "/api/projects/" + pp + "/issues?envelope=1&fields=list&type=ticket"

	// 1. Baseline fetch: capture the ETag, confirm booked is 0.
	resp := ts.get(t, listPath, ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header on list response")
	}
	if booked := bookedForIssue(t, resp, issueID); booked != 0 {
		t.Fatalf("baseline booked=%v, want 0", booked)
	}
	resp.Body.Close()

	// 2. Conditional GET with that ETag → 304: caching is genuinely active,
	//    so this test would actually catch a stale-cache regression.
	pre := getWithINM(t, ts, listPath, ts.adminCookie, etag)
	preCode := pre.StatusCode
	pre.Body.Close()
	if preCode != http.StatusNotModified {
		t.Fatalf("conditional GET before booking: status=%d, want 304", preCode)
	}

	// 3. Book 2h on the issue (override = 2.0).
	post := ts.post(t, "/api/issues/"+strconv.FormatInt(issueID, 10)+"/time-entries", ts.adminCookie, map[string]any{"override": 2.0})
	if post.StatusCode != http.StatusCreated && post.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(post.Body)
		post.Body.Close()
		t.Fatalf("create time entry: status=%d body=%s", post.StatusCode, b)
	}
	post.Body.Close()

	// 4. Conditional GET with the SAME (now-stale) ETag → must be 200, not 304,
	//    the ETag must have changed, and booked must reflect the 2h.
	after := getWithINM(t, ts, listPath, ts.adminCookie, etag)
	defer after.Body.Close()
	if after.StatusCode != http.StatusOK {
		t.Fatalf("conditional GET after booking: status=%d, want 200 (a 304 here is the PAI-577 bug)", after.StatusCode)
	}
	if newETag := after.Header.Get("ETag"); newETag == etag {
		t.Fatalf("ETag did not change after booking: %q", newETag)
	}
	if booked := bookedForIssue(t, after, issueID); booked != 2 {
		t.Fatalf("after booking booked=%v, want 2", booked)
	}
}
