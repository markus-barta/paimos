// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchCommandPassesFiltersInOneRequest(t *testing.T) {
	requests := 0
	var handlerErr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/api/search" {
			handlerErr = "unexpected request " + r.Method + " " + r.URL.Path
			http.Error(w, `{"error":"unexpected request"}`, http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		if got := q.Get("q"); got != "two words" {
			handlerErr = "q = " + got
			http.Error(w, `{"error":"bad query"}`, http.StatusBadRequest)
			return
		}
		if got := q.Get("project"); got != "PAI" {
			handlerErr = "project = " + got
			http.Error(w, `{"error":"bad project"}`, http.StatusBadRequest)
			return
		}
		if got := q.Get("type"); got != "ticket" {
			handlerErr = "type = " + got
			http.Error(w, `{"error":"bad type"}`, http.StatusBadRequest)
			return
		}
		if got := q.Get("limit"); got != "20" {
			handlerErr = "limit = " + got
			http.Error(w, `{"error":"bad limit"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{
			"projects": [],
			"issues": [{"issue_key":"PAI-1","title":"Two words match","type":"ticket","status":"backlog"}],
			"users": [],
			"tags": [],
			"has_more": false
		}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "search", "two", "words", "--project", "PAI", "--type", "ticket", "--limit", "20")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if handlerErr != "" {
		t.Fatal(handlerErr)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if !strings.Contains(out, "PAI-1") || !strings.Contains(out, "Two words match") {
		t.Fatalf("stdout missing search row: %s", out)
	}
}

func TestSearchCommandEmptyResult(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[],"issues":[],"users":[],"tags":[],"has_more":false}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "search", "nothing", "--project", "6")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if strings.TrimSpace(out) != "(no issues)" {
		t.Fatalf("stdout = %q, want no-issues marker", out)
	}
}

func TestSearchCommandJSONPassThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[],"issues":[{"issue_key":"PAI-2","title":"JSON row","type":"task","status":"in-progress"}],"users":[],"tags":[],"has_more":true}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "--json", "search", "json row")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, `"has_more":true`) || !strings.Contains(out, `"issue_key":"PAI-2"`) {
		t.Fatalf("stdout should be raw search JSON: %s", out)
	}
}

func TestSearchCommandRejectsNegativeLimitBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":"should not be called"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "search", "term", "--limit", "-1")
	if err == nil {
		t.Fatal("expected negative limit error")
	}
	if _, ok := err.(*usageError); !ok {
		t.Fatalf("err type = %T, want *usageError (%v)", err, err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}
