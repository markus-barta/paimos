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
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestBroker(t *testing.T) (*contextBroker, string) {
	t.Helper()
	root := t.TempDir()
	root, err := canonicalRepoRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	b := newContextBroker(nil, 6, "PAI", root, true)
	b.logger = log.New(io.Discard, "", 0)
	return b, root
}

func writeServeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServeLoopbackListenAddrValidation(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0", "localhost:8080"} {
		if !isLoopbackListenAddr(addr) {
			t.Fatalf("%s should be accepted as loopback", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "192.168.1.9:8080"} {
		if isLoopbackListenAddr(addr) {
			t.Fatalf("%s should be rejected without --unsafe-allow-remote", addr)
		}
	}
}

func TestServeResolveRepoPathRejectsTraversalAndSymlinkEscape(t *testing.T) {
	b, root := newTestBroker(t)
	outside := t.TempDir()
	writeServeFixture(t, root, "safe.go", "package main\n")
	writeServeFixture(t, outside, "secret.txt", "token=supersecretvalue\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, err := b.resolveRepoPath("safe.go"); err != nil {
		t.Fatalf("safe file rejected: %v", err)
	}
	if _, err := b.resolveRepoPath("../secret.txt"); err == nil {
		t.Fatal("path traversal should be rejected")
	}
	if _, err := b.resolveRepoPath("link.txt"); err == nil {
		t.Fatal("symlink escape should be rejected")
	}
}

func TestServeReadRedactsSecretsAndBlocksSecretFiles(t *testing.T) {
	b, root := newTestBroker(t)
	writeServeFixture(t, root, "config/app.txt", "api_key = \"abc123456789\"\nAuthorization: Bearer verylongtokenvalue\n")
	writeServeFixture(t, root, ".env", "PASSWORD=abc123456789\n")

	resp, err := b.readFile(contextReadRequest{Path: "config/app.txt"})
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !resp.Redacted {
		t.Fatal("expected redacted=true")
	}
	if strings.Contains(resp.Content, "abc123456789") || strings.Contains(resp.Content, "verylongtokenvalue") {
		t.Fatalf("secret leaked in content: %q", resp.Content)
	}
	if _, err := b.readFile(contextReadRequest{Path: ".env"}); err == nil {
		t.Fatal(".env should be blocked")
	}
}

func TestServeHTTPRejectsRemoteClientsWhenLoopbackOnly(t *testing.T) {
	b, _ := newTestBroker(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "203.0.113.8:4444"
	rec := httptest.NewRecorder()
	b.router().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusForbidden)
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:4444"
	rec = httptest.NewRecorder()
	b.router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback status=%d want %d", rec.Code, http.StatusOK)
	}
}

func TestServeMCPStdioRepoState(t *testing.T) {
	b, root := newTestBroker(t)
	writeServeFixture(t, root, "AGENTS.md", "# agent notes\n")
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"paimos_repo_state","arguments":{}}}` + "\n")
	var out bytes.Buffer
	if err := b.serveMCP(input, &out); err != nil {
		t.Fatalf("serve MCP: %v", err)
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode MCP response: %v\n%s", err, out.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected MCP error: %#v", resp.Error)
	}
	if len(resp.Result.Content) != 1 || !strings.Contains(resp.Result.Content[0].Text, "AGENTS.md") {
		t.Fatalf("repo state content missing AGENTS.md: %s", out.String())
	}
}
