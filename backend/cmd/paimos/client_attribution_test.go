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
	"sync"
	"testing"
)

// captureSrv is a one-call stub HTTP server that records the headers
// of the first incoming request. Tests assert on the captured pair
// rather than wrestling with handlers per case.
//
// PAI-325 cares specifically about X-Paimos-Agent-Name (writes only)
// and X-Paimos-Session-Id (all methods, with flag/env precedence on
// top of the existing PAI-97 auto-generated UUID fallback).
type captureSrv struct {
	srv     *httptest.Server
	mu      sync.Mutex
	headers http.Header
	method  string
	path    string
	calls   int
}

func newCaptureSrv(t *testing.T, status int, body string) *captureSrv {
	t.Helper()
	cs := &captureSrv{}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		// Clone so callers see a stable snapshot even if Go's server
		// pool reuses the request object.
		cs.headers = r.Header.Clone()
		cs.method = r.Method
		cs.path = r.URL.Path
		cs.calls++
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(cs.srv.Close)
	return cs
}

// resetAttributionFlags sets the global flag/env state for the test
// and restores it on cleanup. The package-level sessionID is captured
// at import time so it can't be reset here — assertions that need to
// distinguish "auto-generated" from "no header" use header presence
// instead of a value comparison.
func resetAttributionFlags(t *testing.T, agent, session, agentEnv, sessionEnv string) {
	t.Helper()
	oldAgent, oldSession := flagAgentName, flagSessionID
	flagAgentName = agent
	flagSessionID = session
	t.Cleanup(func() {
		flagAgentName, flagSessionID = oldAgent, oldSession
	})
	t.Setenv("PAIMOS_AGENT_NAME", agentEnv)
	t.Setenv("PAIMOS_SESSION_ID", sessionEnv)
}

// TestClient_DoForwardsAttribution_EnvOnly: env vars set, no flags →
// values from env get forwarded on a write.
func TestClient_DoForwardsAttribution_EnvOnly(t *testing.T) {
	resetAttributionFlags(t, "", "", "ops", "11111111-2222-3333-4444-555555555555")
	cs := newCaptureSrv(t, 200, `{"ok":true}`)
	c := &Client{baseURL: cs.srv.URL, http: &http.Client{}}

	if _, err := c.do("POST", "/api/x", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := cs.headers.Get(agentAttrHeader); got != "ops" {
		t.Errorf("agent header = %q, want ops", got)
	}
	if got := cs.headers.Get(sessionAttrHeader); got != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("session header = %q, want env value", got)
	}
}

// TestClient_DoForwardsAttribution_FlagOnly: flags set, env empty →
// values from flags get forwarded.
func TestClient_DoForwardsAttribution_FlagOnly(t *testing.T) {
	resetAttributionFlags(t, "tooling", "aaaa-bbbb-cccc", "", "")
	cs := newCaptureSrv(t, 200, `{"ok":true}`)
	c := &Client{baseURL: cs.srv.URL, http: &http.Client{}}

	if _, err := c.do("PUT", "/api/x", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := cs.headers.Get(agentAttrHeader); got != "tooling" {
		t.Errorf("agent header = %q, want tooling", got)
	}
	if got := cs.headers.Get(sessionAttrHeader); got != "aaaa-bbbb-cccc" {
		t.Errorf("session header = %q, want flag value", got)
	}
}

// TestClient_DoForwardsAttribution_FlagBeatsEnv: both set → flag wins.
// This is the headline ad-hoc-override path the ticket calls out.
func TestClient_DoForwardsAttribution_FlagBeatsEnv(t *testing.T) {
	resetAttributionFlags(t, "flag-agent", "flag-session", "env-agent", "env-session")
	cs := newCaptureSrv(t, 200, `{"ok":true}`)
	c := &Client{baseURL: cs.srv.URL, http: &http.Client{}}

	if _, err := c.do("PATCH", "/api/x", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := cs.headers.Get(agentAttrHeader); got != "flag-agent" {
		t.Errorf("agent header = %q, want flag-agent (flag must beat env)", got)
	}
	if got := cs.headers.Get(sessionAttrHeader); got != "flag-session" {
		t.Errorf("session header = %q, want flag-session (flag must beat env)", got)
	}
}

// TestClient_DoForwardsAttribution_NeitherSet: no env, no flags → no
// agent header at all. Session header still goes out (PAI-97 UUID
// fallback) because turning that off would regress existing behaviour.
func TestClient_DoForwardsAttribution_NeitherSet(t *testing.T) {
	resetAttributionFlags(t, "", "", "", "")
	cs := newCaptureSrv(t, 200, `{"ok":true}`)
	c := &Client{baseURL: cs.srv.URL, http: &http.Client{}}

	if _, err := c.do("POST", "/api/x", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if _, has := cs.headers[http.CanonicalHeaderKey(agentAttrHeader)]; has {
		t.Errorf("agent header should be absent when neither flag nor env is set, got %q",
			cs.headers.Get(agentAttrHeader))
	}
	// Session header is the auto-generated package-level UUID.
	if got := cs.headers.Get(sessionAttrHeader); got == "" {
		t.Error("session header should still be sent (PAI-97 auto-UUID fallback)")
	}
}

// TestClient_DoForwardsAttribution_EmptyEnv: env vars set to empty/
// whitespace → treated as unset, no agent header.
func TestClient_DoForwardsAttribution_EmptyEnv(t *testing.T) {
	resetAttributionFlags(t, "", "", "   ", "  ")
	cs := newCaptureSrv(t, 200, `{"ok":true}`)
	c := &Client{baseURL: cs.srv.URL, http: &http.Client{}}

	if _, err := c.do("DELETE", "/api/x", nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	if _, has := cs.headers[http.CanonicalHeaderKey(agentAttrHeader)]; has {
		t.Errorf("agent header should be absent when env is whitespace-only, got %q",
			cs.headers.Get(agentAttrHeader))
	}
}

// TestClient_DoOmitsAgentHeaderOnGet: agent header is writes-only.
// Even with env+flag set, GET requests must not carry it.
func TestClient_DoOmitsAgentHeaderOnGet(t *testing.T) {
	resetAttributionFlags(t, "tooling", "abc-def", "envagent", "envsession")
	cs := newCaptureSrv(t, 200, `{"ok":true}`)
	c := &Client{baseURL: cs.srv.URL, http: &http.Client{}}

	if _, err := c.do("GET", "/api/x", nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	if _, has := cs.headers[http.CanonicalHeaderKey(agentAttrHeader)]; has {
		t.Errorf("agent header must not be sent on GET, got %q", cs.headers.Get(agentAttrHeader))
	}
	// Session header still flows on reads — see comment in client.go.
	if got := cs.headers.Get(sessionAttrHeader); got != "abc-def" {
		t.Errorf("session header on GET = %q, want abc-def", got)
	}
}

// TestClient_DoTrimsAndCapsAttribution: long / whitespace-padded values
// are trimmed and capped at 64 chars, matching the server-side
// agentAttrCap from PAI-324.
func TestClient_DoTrimsAndCapsAttribution(t *testing.T) {
	long := strings.Repeat("a", 100)
	resetAttributionFlags(t, "  "+long+"  ", "  short-session  ", "", "")
	cs := newCaptureSrv(t, 200, `{"ok":true}`)
	c := &Client{baseURL: cs.srv.URL, http: &http.Client{}}

	if _, err := c.do("POST", "/api/x", nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	got := cs.headers.Get(agentAttrHeader)
	if len(got) != agentAttrCap {
		t.Errorf("agent header length = %d, want %d (capped)", len(got), agentAttrCap)
	}
	if got != strings.Repeat("a", agentAttrCap) {
		t.Errorf("agent header = %q, want %d 'a's", got, agentAttrCap)
	}
	if s := cs.headers.Get(sessionAttrHeader); s != "short-session" {
		t.Errorf("session header = %q, want trimmed 'short-session'", s)
	}
}

// TestResolveAgentAttribution_Precedence covers the resolver in
// isolation. Same precedence rules as the do() integration tests, but
// faster to run and easier to extend.
func TestResolveAgentAttribution_Precedence(t *testing.T) {
	cases := []struct {
		name        string
		flagAgent   string
		flagSess    string
		envAgent    string
		envSess     string
		wantAgent   string
		wantSessHas bool // session is non-empty (auto fallback if neither set)
		wantSessVal string
	}{
		{name: "all empty", wantAgent: "", wantSessHas: true /* auto UUID */},
		{name: "env only", envAgent: "ops", envSess: "env-sid",
			wantAgent: "ops", wantSessHas: true, wantSessVal: "env-sid"},
		{name: "flag only", flagAgent: "tool", flagSess: "flag-sid",
			wantAgent: "tool", wantSessHas: true, wantSessVal: "flag-sid"},
		{name: "flag wins", flagAgent: "f", envAgent: "e", flagSess: "fs", envSess: "es",
			wantAgent: "f", wantSessHas: true, wantSessVal: "fs"},
		{name: "whitespace ignored", flagAgent: "  ", envAgent: "  ",
			wantAgent: "", wantSessHas: true /* auto */},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetAttributionFlags(t, c.flagAgent, c.flagSess, c.envAgent, c.envSess)
			gotAgent, gotSess := resolveAgentAttribution()
			if gotAgent != c.wantAgent {
				t.Errorf("agent = %q, want %q", gotAgent, c.wantAgent)
			}
			if c.wantSessVal != "" && gotSess != c.wantSessVal {
				t.Errorf("session = %q, want %q", gotSess, c.wantSessVal)
			}
			if c.wantSessHas && gotSess == "" {
				t.Errorf("session must be non-empty (got %q)", gotSess)
			}
		})
	}
}

// TestIsWriteMethod sanity-checks the method classifier so the doc and
// the implementation can't drift.
func TestIsWriteMethod(t *testing.T) {
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "post", "put"} {
		if !isWriteMethod(m) {
			t.Errorf("isWriteMethod(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"GET", "HEAD", "OPTIONS", "get", ""} {
		if isWriteMethod(m) {
			t.Errorf("isWriteMethod(%q) = true, want false", m)
		}
	}
}

// TestAttributionDoctorDetail covers the doctor output formatting.
// Source labels must reflect the precedence rules so users can tell
// flag-driven from env-driven values at a glance.
func TestAttributionDoctorDetail(t *testing.T) {
	cases := []struct {
		name      string
		flagAgent string
		flagSess  string
		envAgent  string
		envSess   string
		wantSubs  []string
	}{
		{
			name:     "all unset",
			wantSubs: []string{"agent=(unset) [unset]", "session=", "[auto]"},
		},
		{
			name:     "env only",
			envAgent: "ops",
			envSess:  "11111111-2222-3333-4444-555555555555",
			wantSubs: []string{"agent=ops [env:PAIMOS_AGENT_NAME]", "session=11111111…", "[env:PAIMOS_SESSION_ID]"},
		},
		{
			name:      "flag wins",
			flagAgent: "tool",
			flagSess:  "deadbeefcafe",
			envAgent:  "ops",
			envSess:   "envvalue",
			wantSubs:  []string{"agent=tool [flag]", "session=deadbeef…", "[flag]"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetAttributionFlags(t, c.flagAgent, c.flagSess, c.envAgent, c.envSess)
			got := attributionDoctorDetail()
			for _, sub := range c.wantSubs {
				if !strings.Contains(got, sub) {
					t.Errorf("detail = %q, want substring %q", got, sub)
				}
			}
		})
	}
}

// TestTruncateForDoctor covers the UUID-display helper. 8 chars +
// ellipsis matches the convention used elsewhere in the CLI for noisy
// long values.
func TestTruncateForDoctor(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"short", "short"},
		{"12345678", "12345678"},
		{"123456789", "12345678…"},
		{"11111111-2222-3333-4444-555555555555", "11111111…"},
	}
	for _, c := range cases {
		got := truncateForDoctor(c.in)
		if got != c.want {
			t.Errorf("truncateForDoctor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
