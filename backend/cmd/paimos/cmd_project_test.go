// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectShowResolvesKeyAndNumericID(t *testing.T) {
	for _, projectRef := range []string{"PAI", "6"} {
		projectRef := projectRef
		t.Run(projectRef, func(t *testing.T) {
			var sawProjectDetail bool
			var handlerErr string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
					_, _ = w.Write([]byte(`[{"id":6,"key":"PAI","name":"PAIMOS"}]`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/projects/6":
					sawProjectDetail = true
					_, _ = w.Write([]byte(`{"id":6,"key":"PAI","name":"PAIMOS","status":"active","counts":{"open_issues":3,"knowledge_entries":2},"repos":[{"id":1}]}`))
				default:
					handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
				}
			}))
			t.Cleanup(srv.Close)
			t.Setenv(envURL, srv.URL)
			t.Setenv(envAPIKey, "test_key")

			out, _, err := executeCLIForTest(t, "--json", "project", "show", projectRef)
			if err != nil {
				t.Fatalf("executeCLIForTest: %v", err)
			}
			if handlerErr != "" {
				t.Fatal(handlerErr)
			}
			if !sawProjectDetail {
				t.Fatal("project detail endpoint was not called")
			}
			if !strings.Contains(out, `"key":"PAI"`) || !strings.Contains(out, `"open_issues":3`) {
				t.Fatalf("stdout missing project JSON: %s", out)
			}
		})
	}
}

func TestProjectMetadataCommandsResolveAndFetchResources(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantPath   string
		response   string
		wantOutput string
	}{
		{
			name:       "repos",
			args:       []string{"project", "repos", "PAI"},
			wantPath:   "/api/projects/6/repos",
			response:   `[{"id":1,"project_id":6,"label":"app","url":"https://github.com/example/app","default_branch":"main","sort_order":0}]`,
			wantOutput: `"default_branch":"main"`,
		},
		{
			name:       "releases",
			args:       []string{"project", "releases", "PAI"},
			wantPath:   "/api/projects/6/releases",
			response:   `["v3.2.5"]`,
			wantOutput: `"v3.2.5"`,
		},
		{
			name:       "anchors",
			args:       []string{"project", "anchors", "PAI"},
			wantPath:   "/api/projects/6/anchors",
			response:   `[{"id":9,"project_id":6,"issue_id":101,"issue_key":"PAI-101","repo_id":1,"repo_label":"app","file_path":"backend/main.go","line":42,"label":"router"}]`,
			wantOutput: `"issue_key":"PAI-101"`,
		},
		{
			name:       "tags",
			args:       []string{"project", "tags", "PAI"},
			wantPath:   "/api/projects/6/tags",
			response:   `[{"id":44,"name":"blocked","color":"red","description":"Blocks release"}]`,
			wantOutput: `"blocked"`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var sawResource bool
			var handlerErr string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
					_, _ = w.Write([]byte(`[{"id":6,"key":"PAI","name":"PAIMOS"}]`))
				case r.Method == http.MethodGet && r.URL.Path == tc.wantPath:
					sawResource = true
					_, _ = w.Write([]byte(tc.response))
				default:
					handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
				}
			}))
			t.Cleanup(srv.Close)
			t.Setenv(envURL, srv.URL)
			t.Setenv(envAPIKey, "test_key")

			args := append([]string{"--json"}, tc.args...)
			out, _, err := executeCLIForTest(t, args...)
			if err != nil {
				t.Fatalf("executeCLIForTest: %v", err)
			}
			if handlerErr != "" {
				t.Fatal(handlerErr)
			}
			if !sawResource {
				t.Fatalf("resource endpoint %s was not called", tc.wantPath)
			}
			if !strings.Contains(out, tc.wantOutput) {
				t.Fatalf("stdout missing %q: %s", tc.wantOutput, out)
			}
		})
	}
}

func TestProjectMetadataPrettyOutput(t *testing.T) {
	var handlerErr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":6,"key":"PAI","name":"PAIMOS"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/6/repos":
			_, _ = w.Write([]byte(`[{"id":1,"project_id":6,"label":"app","url":"https://github.com/example/app","default_branch":"main","sort_order":0}]`))
		default:
			handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "project", "repos", "PAI")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if handlerErr != "" {
		t.Fatal(handlerErr)
	}
	if !strings.Contains(out, "LABEL") || !strings.Contains(out, "app") || !strings.Contains(out, "main") {
		t.Fatalf("pretty output missing repo table: %s", out)
	}
}
