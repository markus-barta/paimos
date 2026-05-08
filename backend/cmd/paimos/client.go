// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// userAgent identifies this CLI to PAIMOS servers (and, more
// importantly, to Cloudflare WAF — the default urllib/python User-Agent
// gets blocked on pm.bytepoets.com, see paimos_api_gotchas.md).
var userAgent = "paimos-cli/" + Version

// agentAttrCap mirrors the server-side defensive cap from PAI-324
// (handlers/issues_history.go: agentAttrCap). The server already
// truncates, but trimming on the client too keeps the audit log
// clean and avoids sending obviously bogus payloads.
const agentAttrCap = 64

// agentAttrHeader and sessionAttrHeader are the canonical header
// names PAI-324 reads on the server side. Keep the spellings identical
// (Go's http.Header canonicalises anyway, but matching the server's
// constants makes greps easier and prevents drift).
const (
	agentAttrHeader   = "X-Paimos-Agent-Name"
	sessionAttrHeader = "X-Paimos-Session-Id"
)

// sessionID is generated once per CLI invocation and sent on every
// request as X-Paimos-Session-Id. Lets PAI-97's server-side audit
// correlate the mutations from one `paimos …` run. Honors the env
// override PAIMOS_SESSION_ID so multi-step shell scripts can share
// one session across invocations. PAI-325 layers on a flag override
// (--session-id) that wins over the env var; see resolveAgentAttribution.
var sessionID = func() string {
	if s := strings.TrimSpace(os.Getenv("PAIMOS_SESSION_ID")); s != "" {
		return s
	}
	id, err := uuid.NewV7()
	if err != nil {
		// Fall back to v4 — losing time-ordering is fine, the only
		// concern is uniqueness.
		return uuid.NewString()
	}
	return id.String()
}()

// resolveAgentAttribution returns the (agent-name, session-id) pair
// that write commands should forward to the server, applying the
// PAI-325 precedence rules:
//
//	flag (--agent-name / --session-id) > env > nothing
//
// Both values are trimmed and capped at agentAttrCap to mirror the
// server's defensive truncation. An empty return means "send no header"
// — the caller (do) checks before setting it.
//
// Session-id has one extra wrinkle for backwards-compat: the auto-
// generated per-invocation UUID from the package-level sessionID is
// used as a final fallback so PAI-97's server-side correlation still
// works for old call sites. PAI-325's "no header when unset" rule
// applies specifically to the agent-attribution surface — flipping
// that fallback off would regress existing audit behaviour and is
// not what the ticket asks for.
func resolveAgentAttribution() (agent, session string) {
	agent = strings.TrimSpace(flagAgentName)
	if agent == "" {
		agent = strings.TrimSpace(os.Getenv("PAIMOS_AGENT_NAME"))
	}
	if len(agent) > agentAttrCap {
		agent = agent[:agentAttrCap]
	}

	session = strings.TrimSpace(flagSessionID)
	if session == "" {
		session = strings.TrimSpace(os.Getenv("PAIMOS_SESSION_ID"))
	}
	if session == "" {
		// Fall back to the per-invocation UUID. Documented in the
		// package var above; preserves PAI-97 behaviour.
		session = sessionID
	}
	if len(session) > agentAttrCap {
		session = session[:agentAttrCap]
	}
	return agent, session
}

// isWriteMethod reports whether the HTTP method mutates server state.
// PAI-325 only forwards agent-attribution headers on writes; GETs (and
// HEAD/OPTIONS in the unlikely event the CLI ever sends them) carry no
// audit payload.
func isWriteMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// Client is a thin HTTP wrapper with auth + error semantics tailored
// for the CLI's JSON-first flow. One per command invocation.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// newClient builds a client from the resolved InstanceConfig.
func newClient(inst InstanceConfig) *Client {
	return &Client{
		baseURL: strings.TrimRight(inst.URL, "/"),
		apiKey:  inst.APIKey,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// do makes an HTTP request and returns the decoded JSON body (as raw
// bytes, the caller unmarshals into a concrete type). On any 4xx/5xx
// it returns a typed error that includes the server's JSON error
// payload so callers can surface a useful message.
func (c *Client) do(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	// Agent-attribution forwarding (PAI-325). Centralised here so every
	// command picks it up automatically — sprinkling the header logic
	// across each subcommand was the obvious alternative and it was
	// strictly worse: easy to forget, hard to test, drift-prone.
	agent, session := resolveAgentAttribution()
	if session != "" {
		// Session header is sent on all methods (preserves PAI-97
		// per-invocation correlation; reads benefit from it too).
		req.Header.Set(sessionAttrHeader, session)
	}
	if isWriteMethod(method) && agent != "" {
		// Agent header is writes-only — server only persists it on
		// history-snapshotted mutations, sending it on GETs would be
		// noise.
		req.Header.Set(agentAttrHeader, agent)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return rawBody, &httpError{Code: resp.StatusCode, Body: rawBody, Method: method, Path: path}
	}
	return rawBody, nil
}

// httpError carries the full API failure so the caller can render it
// in --json mode as `{error, code}` or in pretty mode as a human line.
type httpError struct {
	Code   int
	Method string
	Path   string
	Body   []byte
}

func (e *httpError) Error() string {
	// Try to extract a `{error: "..."}` message; fall back to the raw
	// body trimmed. Never return HTML — if the server returned HTML
	// (e.g. behind a misconfigured proxy) say so explicitly.
	msg := e.friendlyMessage()
	return fmt.Sprintf("API error %d %s %s: %s", e.Code, e.Method, e.Path, msg)
}

func (e *httpError) friendlyMessage() string {
	trimmed := strings.TrimSpace(string(e.Body))
	if strings.HasPrefix(trimmed, "<") {
		return "non-JSON response (proxy/WAF?)"
	}
	var shaped struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(trimmed), &shaped) == nil && shaped.Error != "" {
		return shaped.Error
	}
	if len(trimmed) > 200 {
		trimmed = trimmed[:200] + "…"
	}
	return trimmed
}

// reportError writes a failure to stderr in the caller-chosen format.
// Returns an apiError so main() can suppress Cobra's own Error: prefix.
func reportError(err error) error {
	if he, ok := err.(*httpError); ok {
		if flagJSON {
			out := map[string]any{
				"error": he.friendlyMessage(),
				"code":  he.Code,
			}
			b, _ := json.Marshal(out)
			fmt.Fprintln(stderr, string(b))
		} else {
			fmt.Fprintln(stderr, "paimos: "+he.Error())
		}
		return &apiError{inner: err}
	}
	// Non-HTTP error (config issue, I/O, marshaling …). Let main()
	// print it uniformly.
	return err
}

// stdout/stderr are package-level so tests can swap them out for
// captured output. In main() they point at the real streams.
var (
	stdout io.Writer
	stderr io.Writer
)

func init() {
	stdout = osStdout()
	stderr = osStderr()
}
