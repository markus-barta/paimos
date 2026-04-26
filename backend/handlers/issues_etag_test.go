package handlers_test

import (
	"context"
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
