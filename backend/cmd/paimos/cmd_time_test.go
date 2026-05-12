package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTimeStartResolvesIssueAndCreatesRunningEntry(t *testing.T) {
	var sawCreate bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-4":
			_, _ = w.Write([]byte(`{"id":104}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/104/time-entries":
			sawCreate = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if body["comment"] != "investigation" {
				t.Errorf("comment=%v", body["comment"])
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":11,"issue_id":104,"started_at":"2026-05-13T08:00:00Z","comment":"investigation"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "--json", "time", "start", "PAI-4", "--note", "investigation")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !sawCreate || !strings.Contains(out, `"id":11`) {
		t.Fatalf("sawCreate=%v stdout=%s", sawCreate, out)
	}
}

func TestTimeStopWithoutRunningTimerIsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/api/time-entries/running" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "--json", "time", "stop")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, `"running": false`) {
		t.Fatalf("stdout=%s", out)
	}
}

func TestTimeSetDurationMapsToOverrideHours(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPut || r.URL.Path != "/api/time-entries/12" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode set body: %v", err)
		}
		if body["override"] != 1.5 {
			t.Errorf("override=%v, want 1.5", body["override"])
		}
		if body["comment"] != "edited" {
			t.Errorf("comment=%v", body["comment"])
		}
		if body["started_at"] != "2026-05-13T08:00:00Z" {
			t.Errorf("started_at=%v", body["started_at"])
		}
		_, _ = w.Write([]byte(`{"id":12,"issue_id":104,"started_at":"2026-05-13T08:00:00Z","override":1.5,"comment":"edited","hours":1.5}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "--json", "time", "set", "12", "--duration", "90m", "--note", "edited", "--started-at", "2026-05-13T08:00:00Z")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !strings.Contains(out, `"override":1.5`) {
		t.Fatalf("stdout=%s", out)
	}
}

func TestTimeListIssueSupportsIssueKeys(t *testing.T) {
	requests := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-5":
			_, _ = w.Write([]byte(`{"id":105}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/105/time-entries":
			_, _ = w.Write([]byte(`[{"id":13,"issue_id":105,"started_at":"2026-05-13T08:00:00Z","stopped_at":"2026-05-13T09:00:00Z","hours":1}]`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "time", "list", "--issue", "PAI-5")
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests=%v", requests)
	}
	if !strings.Contains(out, "13") || !strings.Contains(out, "1.00") {
		t.Fatalf("stdout=%s", out)
	}
}
