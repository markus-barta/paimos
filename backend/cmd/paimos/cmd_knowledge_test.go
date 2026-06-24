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

package main

// PAI-394 — smoke coverage for the new `paimos knowledge` family.
// Asserts the CLI builds the right URLs against the unified
// /api/projects/{id}/knowledge surface. End-to-end coverage of the
// handlers lives in backend/handlers/knowledge_test.go.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestValidateKnowledgeType(t *testing.T) {
	for _, ok := range knowledgeTypeSegments {
		if err := validateKnowledgeType(ok); err != nil {
			t.Errorf("validateKnowledgeType(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "runbooks", "Memory", "external_system", "feedback"} {
		if err := validateKnowledgeType(bad); err == nil {
			t.Errorf("validateKnowledgeType(%q) = nil, want error", bad)
		}
	}
}

// knowledgeReq is one captured HTTP request the fake server saved.
type knowledgeReq struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

// startKnowledgeFakeAPI returns a fake server that resolves the
// `KNW` project to id=99 and records every request hitting the
// unified /knowledge surface so individual tests can assert the
// shape the CLI built.
func startKnowledgeFakeAPI(t *testing.T) (*httptest.Server, *sync.Mutex, *[]knowledgeReq) {
	t.Helper()
	mu := &sync.Mutex{}
	var reqs []knowledgeReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		var body map[string]any
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
		}
		reqs = append(reqs, knowledgeReq{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   body,
		})
		mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":99,"key":"KNW"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/99/knowledge":
			_, _ = w.Write([]byte(`[
				{"id":1,"project_id":99,"type":"memory","slug":"feedback_alpha","title":"Alpha","body":"","status":"backlog","metadata":{},"created_at":"","updated_at":"","reference_count":0}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/99/knowledge/memory/feedback_alpha":
			_, _ = w.Write([]byte(`{"id":1,"project_id":99,"type":"memory","slug":"feedback_alpha","title":"Alpha","body":"existing body","status":"backlog","metadata":{},"created_at":"","updated_at":"","reference_count":0}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/99/knowledge":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2,"project_id":99,"type":"memory","slug":"new_entry","title":"New","body":"","status":"backlog","metadata":{},"created_at":"","updated_at":"","reference_count":0}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/projects/99/knowledge/"):
			_, _ = w.Write([]byte(`{"id":1,"project_id":99,"type":"memory","slug":"feedback_alpha","title":"Updated","body":"new body","status":"backlog","metadata":{},"created_at":"","updated_at":"","reference_count":0}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/projects/99/knowledge/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/99/knowledge/memory/references":
			_, _ = w.Write([]byte(`{"updated":2}`))
		default:
			http.Error(w, `{"error":"unmocked: `+r.Method+" "+r.URL.Path+`"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")
	return srv, mu, &reqs
}

// TestKnowledgeList_BuildsFilteredURL asserts `paimos knowledge
// list --type runbook --project KNW` hits the unified base path
// with ?type=runbook in the query string. PAI-394's central
// invariant: type travels as data, not as URL grammar.
func TestKnowledgeList_BuildsFilteredURL(t *testing.T) {
	_, mu, reqs := startKnowledgeFakeAPI(t)

	if _, _, err := executeCLIForTest(t,
		"knowledge", "list",
		"--project", "KNW",
		"--type", "memory",
		"--json",
	); err != nil {
		t.Fatalf("execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var got *knowledgeReq
	for i := range *reqs {
		r := &(*reqs)[i]
		if r.Method == http.MethodGet && r.Path == "/api/projects/99/knowledge" {
			got = r
			break
		}
	}
	if got == nil {
		t.Fatalf("no list call recorded; got: %+v", *reqs)
	}
	if got.Query != "type=memory" {
		t.Errorf("query = %q, want type=memory", got.Query)
	}
}

// TestKnowledgeCreate_BuildsCreateURL asserts the POST lands at
// /api/projects/{id}/knowledge?type=<seg> with the body shape
// (slug, title, body, optional status). The fallback path
// (?type= as query) is the CLI's chosen wire form.
func TestKnowledgeCreate_BuildsCreateURL(t *testing.T) {
	_, mu, reqs := startKnowledgeFakeAPI(t)

	if _, _, err := executeCLIForTest(t,
		"knowledge", "create",
		"--project", "KNW",
		"--type", "runbook",
		"--slug", "deploy",
		"--title", "Deploy runbook",
		"--body", "step 1",
	); err != nil {
		t.Fatalf("execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var got *knowledgeReq
	for i := range *reqs {
		r := &(*reqs)[i]
		if r.Method == http.MethodPost && r.Path == "/api/projects/99/knowledge" {
			got = r
			break
		}
	}
	if got == nil {
		t.Fatalf("no create call recorded; got: %+v", *reqs)
	}
	if got.Query != "type=runbook" {
		t.Errorf("query = %q, want type=runbook", got.Query)
	}
	if got.Body["slug"] != "deploy" {
		t.Errorf("body.slug = %v, want deploy", got.Body["slug"])
	}
	if got.Body["title"] != "Deploy runbook" {
		t.Errorf("body.title = %v, want 'Deploy runbook'", got.Body["title"])
	}
	if got.Body["body"] != "step 1" {
		t.Errorf("body.body = %v, want 'step 1'", got.Body["body"])
	}
}

// TestKnowledgeGetUpdateDelete_BuildsTypedSlugURL asserts the
// single-entry verbs (get/update/delete) all hit the canonical
// path-segment form (/knowledge/{type}/{slug}). This is the
// alternative wire form to ?type=, used when the operation is
// addressing a specific resource rather than enumerating.
func TestKnowledgeGetUpdateDelete_BuildsTypedSlugURL(t *testing.T) {
	_, mu, reqs := startKnowledgeFakeAPI(t)

	if _, _, err := executeCLIForTest(t,
		"knowledge", "get", "memory", "feedback_alpha",
		"--project", "KNW",
		"--json",
	); err != nil {
		t.Fatalf("execute get: %v", err)
	}
	if _, _, err := executeCLIForTest(t,
		"knowledge", "update", "memory", "feedback_alpha",
		"--project", "KNW",
		"--title", "Updated",
	); err != nil {
		t.Fatalf("execute update: %v", err)
	}
	if _, _, err := executeCLIForTest(t,
		"knowledge", "delete", "memory", "feedback_alpha",
		"--project", "KNW",
		"--yes",
	); err != nil {
		t.Fatalf("execute delete: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	wantPath := "/api/projects/99/knowledge/memory/feedback_alpha"
	hits := map[string]bool{}
	for _, r := range *reqs {
		if r.Path == wantPath {
			hits[r.Method] = true
		}
	}
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if !hits[m] {
			t.Errorf("missing %s %s — got requests: %+v", m, wantPath, *reqs)
		}
	}
}

// TestKnowledgeUpdate_PreservesBodyOnTitleOnly guards against a partial-update
// body wipe: `knowledge update memory <slug> --title X` (no --body) must carry
// the existing body over on the full-replace PUT, not send an empty body. A
// wipe would also (post PAI-351) falsely flag every dependent for re-review.
func TestKnowledgeUpdate_PreservesBodyOnTitleOnly(t *testing.T) {
	_, mu, reqs := startKnowledgeFakeAPI(t)

	if _, _, err := executeCLIForTest(t,
		"knowledge", "update", "memory", "feedback_alpha",
		"--project", "KNW",
		"--title", "Renamed",
	); err != nil {
		t.Fatalf("execute update: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var put *knowledgeReq
	for i := range *reqs {
		if (*reqs)[i].Method == http.MethodPut {
			put = &(*reqs)[i]
		}
	}
	if put == nil {
		t.Fatalf("no PUT recorded: %+v", *reqs)
	}
	if got, _ := put.Body["body"].(string); got != "existing body" {
		t.Errorf("title-only update must preserve the body; PUT body=%q, want %q", got, "existing body")
	}
	if got, _ := put.Body["title"].(string); got != "Renamed" {
		t.Errorf("PUT title=%q, want %q", got, "Renamed")
	}
}

// TestKnowledgeMemoryBumpRefs_BuildsReferencesURL asserts the
// memory subroute survives the URL collapse — the POST lands at
// /knowledge/memory/references rather than the pre-PAI-394
// /memory/references shape.
func TestKnowledgeMemoryBumpRefs_BuildsReferencesURL(t *testing.T) {
	_, mu, reqs := startKnowledgeFakeAPI(t)

	if _, _, err := executeCLIForTest(t,
		"knowledge", "memory", "bump-refs",
		"--project", "KNW",
		"--source", "test",
		"7", "11",
	); err != nil {
		t.Fatalf("execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var got *knowledgeReq
	for i := range *reqs {
		r := &(*reqs)[i]
		if r.Method == http.MethodPost && r.Path == "/api/projects/99/knowledge/memory/references" {
			got = r
			break
		}
	}
	if got == nil {
		t.Fatalf("no bump-refs call recorded; got: %+v", *reqs)
	}
	if got.Body["source"] != "test" {
		t.Errorf("body.source = %v, want test", got.Body["source"])
	}
	ids, _ := got.Body["ids"].([]any)
	if len(ids) != 2 {
		t.Errorf("body.ids len = %d, want 2", len(ids))
	}
}
