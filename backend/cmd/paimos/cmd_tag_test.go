// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateTagColor(t *testing.T) {
	if err := validateTagColor("red"); err != nil {
		t.Fatalf("red should be valid: %v", err)
	}
	if err := validateTagColor(""); err != nil {
		t.Fatalf("empty color should defer to server default: %v", err)
	}
	err := validateTagColor("beige")
	if err == nil {
		t.Fatal("expected invalid color error")
	}
	if !strings.Contains(err.Error(), "gray") || !strings.Contains(err.Error(), "cyan") {
		t.Fatalf("error should list palette, got %q", err.Error())
	}
}

func TestTagListProjectResolvesKeyAndNumericID(t *testing.T) {
	for _, projectRef := range []string{"PAI", "6"} {
		projectRef := projectRef
		t.Run(projectRef, func(t *testing.T) {
			var sawProjectTags bool
			var handlerErr string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
					_, _ = w.Write([]byte(`[{"id":6,"key":"PAI","name":"PAIMOS"}]`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/projects/6/tags":
					sawProjectTags = true
					_, _ = w.Write([]byte(`[{"id":44,"name":"blocked","color":"red","description":"Blocks release"}]`))
				default:
					handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
				}
			}))
			t.Cleanup(srv.Close)
			t.Setenv(envURL, srv.URL)
			t.Setenv(envAPIKey, "test_key")

			out, _, err := executeCLIForTest(t, "--json", "tag", "list", "--project", projectRef)
			if err != nil {
				t.Fatalf("executeCLIForTest: %v", err)
			}
			if handlerErr != "" {
				t.Fatal(handlerErr)
			}
			if !sawProjectTags {
				t.Fatal("project tags endpoint was not called")
			}
			if !strings.Contains(out, `"blocked"`) {
				t.Fatalf("stdout missing tag JSON: %s", out)
			}
		})
	}
}

func TestTagCreateAttachesToProject(t *testing.T) {
	var createBody map[string]any
	var attachBody map[string]any
	var handlerErr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":6,"key":"PAI","name":"PAIMOS"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/tags":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				handlerErr = fmt.Sprintf("decode create body: %v", err)
				http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":44,"name":"blocked","color":"red","description":"Blocks release"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/6/tags":
			if err := json.NewDecoder(r.Body).Decode(&attachBody); err != nil {
				handlerErr = fmt.Sprintf("decode attach body: %v", err)
				http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t,
		"--json",
		"tag", "create",
		"--project", "PAI",
		"--name", "blocked",
		"--color", "red",
		"--description", "Blocks release",
	)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if handlerErr != "" {
		t.Fatal(handlerErr)
	}
	if createBody["name"] != "blocked" || createBody["color"] != "red" || createBody["description"] != "Blocks release" {
		t.Fatalf("create body = %#v", createBody)
	}
	if attachBody["tag_id"].(float64) != 44 {
		t.Fatalf("attach body = %#v, want tag_id 44", attachBody)
	}
	if !strings.Contains(out, `"attached": true`) || !strings.Contains(out, `"project_id": 6`) {
		t.Fatalf("stdout missing attachment JSON: %s", out)
	}
}

func TestTagUpdateSendsSparseBody(t *testing.T) {
	var received map[string]any
	var handlerErr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPut || r.URL.Path != "/api/tags/44" {
			handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			handlerErr = fmt.Sprintf("decode body: %v", err)
			http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"id":44,"name":"blocked-now","color":"orange","description":""}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	if _, _, err := executeCLIForTest(t, "tag", "update", "44", "--name", "blocked-now", "--color", "orange"); err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if handlerErr != "" {
		t.Fatal(handlerErr)
	}
	if received["name"] != "blocked-now" || received["color"] != "orange" {
		t.Fatalf("received body = %#v", received)
	}
	if _, ok := received["description"]; ok {
		t.Fatalf("description should be absent from sparse update body: %#v", received)
	}
}

func TestTagDeleteRequiresYesBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":"should not be called"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "tag", "delete", "44")
	if err == nil {
		t.Fatal("expected delete confirmation error")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("err type=%T, want *usageError (%v)", err, err)
	}
	if requests != 0 {
		t.Fatalf("requests=%d, want 0", requests)
	}
}

func TestTagDeleteYesFetchesThenDeletes(t *testing.T) {
	var deleted bool
	var handlerErr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			_, _ = w.Write([]byte(`[{"id":44,"name":"blocked","color":"red"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/tags/44":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			handlerErr = fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "--json", "tag", "delete", "44", "--yes")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if handlerErr != "" {
		t.Fatal(handlerErr)
	}
	if !deleted {
		t.Fatal("DELETE /api/tags/44 was not called")
	}
	if !strings.Contains(out, `"action": "delete"`) || !strings.Contains(out, `"tag": "blocked"`) {
		t.Fatalf("stdout missing delete JSON: %s", out)
	}
}

func TestTagCreateInvalidColorFailsBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":"should not be called"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "tag", "create", "--name", "blocked", "--color", "beige")
	if err == nil {
		t.Fatal("expected invalid color error")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("err type=%T, want *usageError (%v)", err, err)
	}
	if requests != 0 {
		t.Fatalf("requests=%d, want 0", requests)
	}
}
