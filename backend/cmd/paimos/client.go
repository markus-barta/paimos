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

// sessionID is generated once per CLI invocation and sent on every
// request as X-PAIMOS-Session-Id. Lets PAI-97's server-side audit
// correlate the mutations from one `paimos …` run. Honors the env
// override PAIMOS_SESSION_ID so multi-step shell scripts can share
// one session across invocations.
var sessionID = func() string {
	if s := os.Getenv("PAIMOS_SESSION_ID"); s != "" {
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
	req.Header.Set("X-PAIMOS-Session-Id", sessionID)
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
