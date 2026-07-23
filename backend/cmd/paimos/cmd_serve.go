// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	serveDefaultAddr       = "127.0.0.1:0"
	serveMaxRequestBytes   = 1 << 20
	serveMaxReadBytes      = 64 * 1024
	serveMaxReadLines      = 400
	serveMaxSearchResults  = 50
	serveDefaultSearchK    = 12
	serveMaxPackTokens     = 50000
	serveDefaultPackTokens = 6000
	serveCommandTimeout    = 4 * time.Second
)

var (
	serveRedactionPatterns = []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[A-Za-z0-9._~+/=-]{12,}`), `${1}[REDACTED]`},
		{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{12,}`), `${1}[REDACTED]`},
		{regexp.MustCompile(`(?i)((api[_-]?key|token|secret|password|passwd|credential)\s*[:=]\s*['"]?)[A-Za-z0-9._~+/=-]{8,}(['"]?)`), `${1}[REDACTED]${3}`},
		{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), `[REDACTED_AWS_KEY]`},
		{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), `[REDACTED_PRIVATE_KEY]`},
	}
	symbolLineRE = regexp.MustCompile(`(?i)^\s*(export\s+)?(async\s+)?(func|type|class|interface|struct|enum|function|const|let|var)\b`)
)

func serveCmd() *cobra.Command {
	var (
		projectRef        string
		repoRootFlag      string
		addr              string
		unsafeAllowRemote bool
		mcpStdio          bool
	)
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run a local read-only context broker for coding agents",
		Long: `serve exposes a bounded local context broker for coding agents.

By default it listens only on loopback and serves read-only repo context plus
authenticated PAIMOS retrieval. Use --mcp-stdio for MCP clients that launch the
CLI as a stdio server.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectRef) == "" {
				return &usageError{msg: "--project is required"}
			}
			root, err := repoRootFrom(repoRootFlag)
			if err != nil {
				return fmt.Errorf("resolve repo root: %w", err)
			}
			root, err = canonicalRepoRoot(root)
			if err != nil {
				return err
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectID(client, projectRef)
			if err != nil {
				return reportError(err)
			}
			broker := newContextBroker(client, projectID, projectRef, root, !unsafeAllowRemote)
			if mcpStdio {
				return broker.serveMCP(os.Stdin, stdout)
			}
			addr = strings.TrimSpace(addr)
			if addr == "" {
				addr = serveDefaultAddr
			}
			if !unsafeAllowRemote && !isLoopbackListenAddr(addr) {
				return &usageError{msg: "--addr must be loopback by default (use --unsafe-allow-remote only on a trusted network)"}
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", addr, err)
			}
			defer ln.Close()
			actual := ln.Addr().String()
			if flagJSON {
				b, _ := json.Marshal(map[string]any{
					"url":        "http://" + actual,
					"project_id": projectID,
					"repo_root":  root,
					"mcp_stdio":  false,
					"read_only":  true,
				})
				fmt.Fprintln(stdout, string(b))
			} else {
				fmt.Fprintf(stdout, "paimos serve listening on http://%s (project=%d, repo=%s)\n", actual, projectID, root)
			}
			srv := &http.Server{
				Handler:           broker.router(),
				ReadHeaderTimeout: 5 * time.Second,
			}
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	c.Flags().StringVarP(&projectRef, "project", "p", "", "project key or numeric id (required)")
	c.Flags().StringVar(&repoRootFlag, "repo-root", "", "repository root (default: current git root)")
	c.Flags().StringVar(&addr, "addr", serveDefaultAddr, "HTTP listen address")
	c.Flags().BoolVar(&unsafeAllowRemote, "unsafe-allow-remote", false, "allow non-loopback HTTP bind/clients")
	c.Flags().BoolVar(&mcpStdio, "mcp-stdio", false, "serve MCP JSON-RPC over stdin/stdout instead of HTTP")
	return c
}

type contextBroker struct {
	client       *Client
	projectID    int64
	projectRef   string
	repoRoot     string
	loopbackOnly bool
	startedAt    time.Time
	logger       *log.Logger
}

func newContextBroker(client *Client, projectID int64, projectRef, repoRoot string, loopbackOnly bool) *contextBroker {
	return &contextBroker{
		client:       client,
		projectID:    projectID,
		projectRef:   strings.TrimSpace(projectRef),
		repoRoot:     repoRoot,
		loopbackOnly: loopbackOnly,
		startedAt:    time.Now().UTC(),
		logger:       log.New(stderr, "paimos serve: ", 0),
	}
}

func (b *contextBroker) router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.handleHealth)
	mux.HandleFunc("/context/repo", b.handleRepoState)
	mux.HandleFunc("/context/search", b.handleSearch)
	mux.HandleFunc("/context/read", b.handleRead)
	mux.HandleFunc("/context/symbols", b.handleSymbols)
	mux.HandleFunc("/context/retrieve", b.handleRetrieve)
	mux.HandleFunc("/context/pack", b.handlePack)
	mux.HandleFunc("/mcp/config", b.handleMCPConfig)
	return b.localOnly(mux)
}

func (b *contextBroker) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.loopbackOnly && !isLoopbackRemoteAddr(r.RemoteAddr) {
			writeBrokerError(w, http.StatusForbidden, "remote clients are disabled")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (b *contextBroker) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeBrokerJSON(w, map[string]any{
		"status":       "ok",
		"version":      Version,
		"project_id":   b.projectID,
		"project_ref":  b.projectRef,
		"repo_root":    b.repoRoot,
		"read_only":    true,
		"loopbackOnly": b.loopbackOnly,
		"started_at":   b.startedAt.Format(time.RFC3339),
	})
}

func (b *contextBroker) handleRepoState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	state := b.repoState()
	b.audit("repo_state", nil, 1, nil)
	writeBrokerJSON(w, state)
}

func (b *contextBroker) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req localSearchRequest
	if !decodeBrokerRequest(w, r, &req) {
		return
	}
	resp, err := b.localSearch(r.Context(), req)
	b.audit("local_search", map[string]any{"q": req.Q, "k": req.K}, len(resp.Hits), err)
	if err != nil {
		writeBrokerError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeBrokerJSON(w, resp)
}

func (b *contextBroker) handleRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req contextReadRequest
	if !decodeBrokerRequest(w, r, &req) {
		return
	}
	resp, err := b.readFile(req)
	b.audit("local_read", map[string]any{"path": req.Path, "start_line": req.StartLine, "end_line": req.EndLine}, 1, err)
	if err != nil {
		writeBrokerError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeBrokerJSON(w, resp)
}

func (b *contextBroker) handleSymbols(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req localSearchRequest
	if !decodeBrokerRequest(w, r, &req) {
		return
	}
	resp, err := b.symbolSearch(r.Context(), req)
	b.audit("symbol_search", map[string]any{"q": req.Q, "k": req.K}, len(resp.Hits), err)
	if err != nil {
		writeBrokerError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeBrokerJSON(w, resp)
}

func (b *contextBroker) handleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req retrieveBrokerRequest
	if !decodeBrokerRequest(w, r, &req) {
		return
	}
	resp, err := b.retrieve(r.Context(), req)
	if err != nil {
		b.audit("retrieve", map[string]any{"q": req.Q, "k": req.K}, 0, err)
		writeBrokerError(w, http.StatusBadGateway, err.Error())
		return
	}
	count := len(resp.LocalHits) + len(resp.SymbolHits)
	b.audit("retrieve", map[string]any{"q": req.Q, "k": req.K}, count, nil)
	writeBrokerJSON(w, resp)
}

func (b *contextBroker) handlePack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req packBrokerRequest
	if !decodeBrokerRequest(w, r, &req) {
		return
	}
	resp, err := b.packContext(r.Context(), req)
	count := len(resp.LocalHits) + len(resp.SymbolHits)
	b.audit("pack_context", map[string]any{"issue": req.Issue, "q": req.Q, "k": req.K, "token_budget": req.TokenBudget}, count, err)
	if err != nil {
		writeBrokerError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeBrokerJSON(w, resp)
}

func (b *contextBroker) handleMCPConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBrokerError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeBrokerJSON(w, map[string]any{
		"name":      "paimos-local",
		"transport": "stdio",
		"command":   "paimos",
		"args":      []string{"serve", "--project", b.projectRef, "--repo-root", b.repoRoot, "--mcp-stdio"},
		"auth":      "none on local MCP stdio; remote PAIMOS API uses the active paimos CLI config",
		"tools":     brokerToolNames(),
	})
}

type localSearchRequest struct {
	Q string `json:"q"`
	K int    `json:"k"`
}

type retrieveBrokerRequest struct {
	Q string `json:"q"`
	K int    `json:"k"`
}

type contextReadRequest struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type packBrokerRequest struct {
	Issue       string `json:"issue"`
	Q           string `json:"q"`
	K           int    `json:"k"`
	TokenBudget int    `json:"token_budget"`
}

type localHit struct {
	Source        string         `json:"source"`
	Kind          string         `json:"kind"`
	Path          string         `json:"path"`
	Line          int            `json:"line,omitempty"`
	Column        int            `json:"column,omitempty"`
	Text          string         `json:"text"`
	Score         float64        `json:"score"`
	Provenance    map[string]any `json:"provenance"`
	UntrustedData bool           `json:"untrusted_data"`
}

type localSearchResponse struct {
	Query         string     `json:"query"`
	Method        string     `json:"method"`
	Hits          []localHit `json:"hits"`
	Truncated     bool       `json:"truncated"`
	UntrustedData bool       `json:"untrusted_data"`
}

type contextReadResponse struct {
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	Content       string `json:"content"`
	Bytes         int    `json:"bytes"`
	Truncated     bool   `json:"truncated"`
	Redacted      bool   `json:"redacted"`
	UntrustedData bool   `json:"untrusted_data"`
}

type retrieveBrokerResponse struct {
	Query         string            `json:"query"`
	ProjectID     int64             `json:"project_id"`
	Remote        any               `json:"remote,omitempty"`
	LocalHits     []localHit        `json:"local_hits"`
	SymbolHits    []localHit        `json:"symbol_hits"`
	Repo          repoStateResponse `json:"repo"`
	Metadata      map[string]any    `json:"metadata"`
	UntrustedData bool              `json:"untrusted_data"`
}

type packBrokerResponse struct {
	ProjectID       int64          `json:"project_id"`
	RepoRoot        string         `json:"repo_root"`
	Issue           any            `json:"issue,omitempty"`
	Remote          any            `json:"remote,omitempty"`
	LocalHits       []localHit     `json:"local_hits"`
	SymbolHits      []localHit     `json:"symbol_hits"`
	Content         string         `json:"content"`
	TokenBudget     int            `json:"token_budget"`
	EstimatedTokens int            `json:"estimated_tokens"`
	Omissions       []string       `json:"omissions,omitempty"`
	Metadata        map[string]any `json:"metadata"`
	UntrustedData   bool           `json:"untrusted_data"`
}

type repoStateResponse struct {
	RepoRoot       string   `json:"repo_root"`
	Branch         string   `json:"branch,omitempty"`
	Head           string   `json:"head,omitempty"`
	Dirty          bool     `json:"dirty"`
	Staged         int      `json:"staged"`
	Unstaged       int      `json:"unstaged"`
	Untracked      int      `json:"untracked"`
	AgentFiles     []string `json:"agent_files"`
	AnchorsIndex   string   `json:"anchors_index,omitempty"`
	AnchorsPresent bool     `json:"anchors_present"`
	ReadOnly       bool     `json:"read_only"`
	UntrustedData  bool     `json:"untrusted_data"`
}

func (b *contextBroker) localSearch(ctx context.Context, req localSearchRequest) (localSearchResponse, error) {
	query := strings.TrimSpace(req.Q)
	if query == "" {
		return localSearchResponse{}, fmt.Errorf("q is required")
	}
	k := boundedK(req.K)
	hits, method, err := b.ripgrepSearch(ctx, query, k)
	if err != nil {
		hits, method, err = b.walkSearch(query, k)
	}
	if err != nil {
		return localSearchResponse{}, err
	}
	return localSearchResponse{
		Query:         query,
		Method:        method,
		Hits:          hits,
		Truncated:     len(hits) >= k,
		UntrustedData: true,
	}, nil
}

func (b *contextBroker) symbolSearch(ctx context.Context, req localSearchRequest) (localSearchResponse, error) {
	k := boundedK(req.K)
	hits, method, err := b.ripgrepSymbols(ctx, strings.TrimSpace(req.Q), k)
	if err != nil {
		hits, method, err = b.walkSymbols(strings.TrimSpace(req.Q), k)
	}
	if err != nil {
		return localSearchResponse{}, err
	}
	return localSearchResponse{
		Query:         strings.TrimSpace(req.Q),
		Method:        method,
		Hits:          hits,
		Truncated:     len(hits) >= k,
		UntrustedData: true,
	}, nil
}

func (b *contextBroker) retrieve(ctx context.Context, req retrieveBrokerRequest) (retrieveBrokerResponse, error) {
	query := strings.TrimSpace(req.Q)
	if query == "" {
		return retrieveBrokerResponse{}, fmt.Errorf("q is required")
	}
	k := boundedK(req.K)
	remote, remoteErr := b.remoteRetrieve(query, k)
	localResp, localErr := b.localSearch(ctx, localSearchRequest{Q: query, K: minInt(k, 12)})
	symbolResp, symbolErr := b.symbolSearch(ctx, localSearchRequest{Q: query, K: minInt(k, 12)})
	if remoteErr != nil && localErr != nil && symbolErr != nil {
		return retrieveBrokerResponse{}, fmt.Errorf("retrieve failed: remote=%v local=%v symbols=%v", remoteErr, localErr, symbolErr)
	}
	meta := map[string]any{
		"remote_available": remoteErr == nil,
		"local_available":  localErr == nil,
		"symbols_method":   symbolResp.Method,
		"read_only":        true,
		"lsp_available":    false,
	}
	if remoteErr != nil {
		meta["remote_error"] = remoteErr.Error()
	}
	if localErr != nil {
		meta["local_error"] = localErr.Error()
	}
	if symbolErr != nil {
		meta["symbols_error"] = symbolErr.Error()
	}
	return retrieveBrokerResponse{
		Query:         query,
		ProjectID:     b.projectID,
		Remote:        remote,
		LocalHits:     localResp.Hits,
		SymbolHits:    symbolResp.Hits,
		Repo:          b.repoState(),
		Metadata:      meta,
		UntrustedData: true,
	}, nil
}

func (b *contextBroker) packContext(ctx context.Context, req packBrokerRequest) (packBrokerResponse, error) {
	query := strings.TrimSpace(req.Q)
	if query == "" {
		query = strings.TrimSpace(req.Issue)
	}
	if query == "" {
		return packBrokerResponse{}, fmt.Errorf("q or issue is required")
	}
	k := boundedK(req.K)
	tokenBudget := req.TokenBudget
	if tokenBudget <= 0 {
		tokenBudget = serveDefaultPackTokens
	}
	if tokenBudget > serveMaxPackTokens {
		tokenBudget = serveMaxPackTokens
	}
	var issue any
	var issueErr error
	if strings.TrimSpace(req.Issue) != "" && b.client != nil {
		issue, issueErr = b.remoteIssue(strings.TrimSpace(req.Issue))
	}
	retrieved, retrieveErr := b.retrieve(ctx, retrieveBrokerRequest{Q: query, K: k})
	if retrieveErr != nil && issueErr != nil {
		return packBrokerResponse{}, fmt.Errorf("pack failed: issue=%v retrieve=%v", issueErr, retrieveErr)
	}
	var omissions []string
	if issueErr != nil {
		omissions = append(omissions, "issue fetch failed: "+issueErr.Error())
	}
	if retrieveErr != nil {
		omissions = append(omissions, "retrieve failed: "+retrieveErr.Error())
	}
	var buf strings.Builder
	remaining := tokenBudget
	addPackSection(&buf, &remaining, &omissions, "repo", b.repoState())
	if issue != nil {
		addPackSection(&buf, &remaining, &omissions, "issue", issue)
	}
	if retrieved.Remote != nil {
		addPackSection(&buf, &remaining, &omissions, "paimos_retrieve", retrieved.Remote)
	}
	addPackSection(&buf, &remaining, &omissions, "local_hits", retrieved.LocalHits)
	addPackSection(&buf, &remaining, &omissions, "symbol_hits", retrieved.SymbolHits)
	content := redactSensitiveText(buf.String())
	estimated := estimateTokens(content)
	return packBrokerResponse{
		ProjectID:       b.projectID,
		RepoRoot:        b.repoRoot,
		Issue:           issue,
		Remote:          retrieved.Remote,
		LocalHits:       retrieved.LocalHits,
		SymbolHits:      retrieved.SymbolHits,
		Content:         content,
		TokenBudget:     tokenBudget,
		EstimatedTokens: estimated,
		Omissions:       omissions,
		Metadata: map[string]any{
			"read_only":     true,
			"lsp_available": false,
			"packer":        "bounded-json-sections-v1",
		},
		UntrustedData: true,
	}, nil
}

func (b *contextBroker) remoteRetrieve(query string, k int) (any, error) {
	if b.client == nil {
		return nil, fmt.Errorf("remote PAIMOS client is not configured")
	}
	body, err := b.client.do(http.MethodPost, fmt.Sprintf("/api/projects/%d/retrieve", b.projectID), map[string]any{
		"q": query,
		"k": k,
	})
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode remote retrieve: %w", err)
	}
	return decoded, nil
}

func (b *contextBroker) remoteIssue(ref string) (any, error) {
	body, err := b.client.do(http.MethodGet, "/api/issues/"+url.PathEscape(ref), nil)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode issue: %w", err)
	}
	return decoded, nil
}

func (b *contextBroker) ripgrepSearch(ctx context.Context, query string, k int) ([]localHit, string, error) {
	args := append(ripgrepBaseArgs(k), "--fixed-strings", "--", query, ".")
	hits, err := b.runRipgrep(ctx, args, k, "text_match", "ripgrep-fixed")
	return hits, "ripgrep-fixed", err
}

func (b *contextBroker) ripgrepSymbols(ctx context.Context, query string, k int) ([]localHit, string, error) {
	pattern := `(?i)^\s*(export\s+)?(async\s+)?(func|type|class|interface|struct|enum|function|const|let|var)\b.*`
	if query != "" {
		pattern += regexp.QuoteMeta(query)
	}
	args := append(ripgrepBaseArgs(k), "--regexp", pattern, ".")
	hits, err := b.runRipgrep(ctx, args, k, "symbol", "regex-fallback")
	return hits, "regex-fallback", err
}

func ripgrepBaseArgs(k int) []string {
	return []string{
		"--json",
		"--line-number",
		"--column",
		"--color=never",
		"--max-columns=240",
		"--max-columns-preview",
		"--max-count=4",
		"--glob=!.git/**",
		"--glob=!node_modules/**",
		"--glob=!dist/**",
		"--glob=!build/**",
		"--glob=!vendor/**",
		"--glob=!coverage/**",
		"--glob=!tmp/**",
		"--glob=!*.lock",
		"--max-filesize=1M",
		"--threads=2",
		"--sort=path",
	}
}

func (b *contextBroker) runRipgrep(parent context.Context, args []string, k int, kind, method string) ([]localHit, error) {
	ctx, cancel := context.WithTimeout(parent, serveCommandTimeout)
	defer cancel()
	// #nosec G204 -- fixed "rg" binary; args are a fixed flag set plus the query passed as structured argv (after "--" or regexp-quoted), no shell involved.
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = b.repoRoot
	raw, err := cmd.CombinedOutput()
	if err != nil && !isExitCode(err, 1) {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("rg failed: %s", truncateForLog(string(raw), 240))
	}
	hits := parseRipgrepJSON(raw, k, kind, method)
	return hits, nil
}

func parseRipgrepJSON(raw []byte, k int, kind, method string) []localHit {
	type rgEvent struct {
		Type string `json:"type"`
		Data struct {
			Path struct {
				Text string `json:"text"`
			} `json:"path"`
			Lines struct {
				Text string `json:"text"`
			} `json:"lines"`
			LineNumber int `json:"line_number"`
			Submatches []struct {
				Start int `json:"start"`
			} `json:"submatches"`
		} `json:"data"`
	}
	var hits []localHit
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var ev rgEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Type != "match" {
			continue
		}
		col := 0
		if len(ev.Data.Submatches) > 0 {
			col = ev.Data.Submatches[0].Start + 1
		}
		path := filepath.ToSlash(strings.TrimPrefix(ev.Data.Path.Text, "./"))
		text, redacted := redactSensitiveTextWithFlag(strings.TrimRight(ev.Data.Lines.Text, "\r\n"))
		hits = append(hits, localHit{
			Source: "repo",
			Kind:   kind,
			Path:   path,
			Line:   ev.Data.LineNumber,
			Column: col,
			Text:   text,
			Score:  1 / float64(len(hits)+1),
			Provenance: map[string]any{
				"method":   method,
				"redacted": redacted,
			},
			UntrustedData: true,
		})
		if len(hits) >= k {
			break
		}
	}
	return hits
}

func (b *contextBroker) walkSearch(query string, k int) ([]localHit, string, error) {
	queryLower := strings.ToLower(query)
	return b.walkMatching(k, "text_match", "walk-fixed", func(line string) bool {
		return strings.Contains(strings.ToLower(line), queryLower)
	})
}

func (b *contextBroker) walkSymbols(query string, k int) ([]localHit, string, error) {
	queryLower := strings.ToLower(query)
	return b.walkMatching(k, "symbol", "regex-fallback", func(line string) bool {
		if !symbolLineRE.MatchString(line) {
			return false
		}
		return queryLower == "" || strings.Contains(strings.ToLower(line), queryLower)
	})
}

func (b *contextBroker) walkMatching(k int, kind, method string, match func(string) bool) ([]localHit, string, error) {
	files, err := listRepoFiles(b.repoRoot)
	if err != nil {
		return nil, method, err
	}
	var hits []localHit
	for _, rel := range files {
		if len(hits) >= k {
			break
		}
		if denyUnsafeRepoRel(rel) != nil {
			continue
		}
		abs, err := b.resolveRepoPath(rel)
		if err != nil {
			continue
		}
		f, err := os.Open(abs) // #nosec G304 -- abs is validated by resolveRepoPath (symlinks resolved, contained in repoRoot, denyUnsafeRepoRel).
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := sc.Text()
			if match(line) {
				text, redacted := redactSensitiveTextWithFlag(line)
				hits = append(hits, localHit{
					Source: "repo",
					Kind:   kind,
					Path:   filepath.ToSlash(rel),
					Line:   lineNo,
					Text:   text,
					Score:  1 / float64(len(hits)+1),
					Provenance: map[string]any{
						"method":   method,
						"redacted": redacted,
					},
					UntrustedData: true,
				})
				if len(hits) >= k {
					break
				}
			}
		}
		_ = f.Close()
	}
	return hits, method, nil
}

func (b *contextBroker) readFile(req contextReadRequest) (contextReadResponse, error) {
	abs, err := b.resolveRepoPath(req.Path)
	if err != nil {
		return contextReadResponse{}, err
	}
	rel, _ := filepath.Rel(b.repoRoot, abs)
	rel = filepath.ToSlash(rel)
	start := req.StartLine
	if start <= 0 {
		start = 1
	}
	end := req.EndLine
	if end <= 0 || end-start+1 > serveMaxReadLines {
		end = start + serveMaxReadLines - 1
	}
	if end < start {
		return contextReadResponse{}, fmt.Errorf("end_line must be greater than or equal to start_line")
	}
	f, err := os.Open(abs) // #nosec G304 -- abs is validated by resolveRepoPath (symlinks resolved, contained in repoRoot, denyUnsafeRepoRel).
	if err != nil {
		return contextReadResponse{}, err
	}
	defer f.Close()
	var buf strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	lastLine := start - 1
	truncated := false
	for sc.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if lineNo > end {
			truncated = true
			break
		}
		line := sc.Text()
		if buf.Len()+len(line)+1 > serveMaxReadBytes {
			truncated = true
			break
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		lastLine = lineNo
	}
	if err := sc.Err(); err != nil {
		return contextReadResponse{}, err
	}
	content, redacted := redactSensitiveTextWithFlag(buf.String())
	return contextReadResponse{
		Path:          rel,
		StartLine:     start,
		EndLine:       lastLine,
		Content:       content,
		Bytes:         len(content),
		Truncated:     truncated,
		Redacted:      redacted,
		UntrustedData: true,
	}, nil
}

func (b *contextBroker) repoState() repoStateResponse {
	state := repoStateResponse{
		RepoRoot:      b.repoRoot,
		ReadOnly:      true,
		UntrustedData: true,
	}
	if branch, err := gitOutput(b.repoRoot, "branch", "--show-current"); err == nil {
		state.Branch = strings.TrimSpace(branch)
	}
	if head, err := gitOutput(b.repoRoot, "rev-parse", "HEAD"); err == nil {
		state.Head = strings.TrimSpace(head)
	}
	if status, err := gitOutput(b.repoRoot, "status", "--porcelain=v1"); err == nil {
		for _, line := range strings.Split(status, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			state.Dirty = true
			if strings.HasPrefix(line, "??") {
				state.Untracked++
				continue
			}
			if len(line) > 0 && line[0] != ' ' {
				state.Staged++
			}
			if len(line) > 1 && line[1] != ' ' {
				state.Unstaged++
			}
		}
	}
	state.AgentFiles = findAgentInstructionFiles(b.repoRoot)
	anchors := filepath.Join(b.repoRoot, ".paimos", "anchors.json")
	if _, err := os.Stat(anchors); err == nil {
		state.AnchorsIndex = ".paimos/anchors.json"
		state.AnchorsPresent = true
	}
	return state
}

func findAgentInstructionFiles(root string) []string {
	files, err := listRepoFiles(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, rel := range files {
		if strings.EqualFold(filepath.Base(rel), "AGENTS.md") {
			out = append(out, filepath.ToSlash(rel))
		}
	}
	slices.Sort(out)
	return out
}

func (b *contextBroker) resolveRepoPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	path = filepath.Clean(path)
	var joined string
	if filepath.IsAbs(path) {
		joined = path
	} else {
		joined = filepath.Join(b.repoRoot, path)
	}
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	rel, err := filepath.Rel(b.repoRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes repo root")
	}
	if err := denyUnsafeRepoRel(rel); err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	return resolved, nil
}

func canonicalRepoRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("repo root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repo root is not a directory")
	}
	return resolved, nil
}

func denyUnsafeRepoRel(rel string) error {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" {
		return nil
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		switch part {
		case ".git", "node_modules", "dist", "build", "vendor", "coverage", "tmp":
			return fmt.Errorf("path %q is blocked by the local broker policy", rel)
		}
	}
	base := strings.ToLower(filepath.Base(rel))
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == ".npmrc" || base == ".pypirc" ||
		base == "id_rsa" || base == "id_dsa" || base == "id_ed25519" || base == "credentials" {
		return fmt.Errorf("path %q is blocked by the local broker policy", rel)
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(base, suffix) {
			return fmt.Errorf("path %q is blocked by the local broker policy", rel)
		}
	}
	return nil
}

func redactSensitiveText(raw string) string {
	out, _ := redactSensitiveTextWithFlag(raw)
	return out
}

func redactSensitiveTextWithFlag(raw string) (string, bool) {
	out := raw
	redacted := false
	for _, pattern := range serveRedactionPatterns {
		next := pattern.re.ReplaceAllString(out, pattern.repl)
		if next != out {
			redacted = true
			out = next
		}
	}
	return out, redacted
}

func boundedK(k int) int {
	if k <= 0 {
		return serveDefaultSearchK
	}
	if k > serveMaxSearchResults {
		return serveMaxSearchResults
	}
	return k
}

func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isLoopbackRemoteAddr(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isExitCode(err error, code int) bool {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode() == code
	}
	return false
}

func decodeBrokerRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	body := http.MaxBytesReader(w, r.Body, serveMaxRequestBytes)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "decode request: "+err.Error())
		return false
	}
	return true
}

func writeBrokerJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeBrokerError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":  msg,
		"status": status,
	})
}

func (b *contextBroker) audit(op string, params map[string]any, resultCount int, err error) {
	if b.logger == nil {
		return
	}
	safeParams := map[string]any{}
	for k, v := range params {
		switch t := v.(type) {
		case string:
			safeParams[k] = truncateForLog(redactSensitiveText(t), 160)
		default:
			safeParams[k] = v
		}
	}
	row := map[string]any{
		"ts":           time.Now().UTC().Format(time.RFC3339),
		"op":           op,
		"project_id":   b.projectID,
		"params":       safeParams,
		"result_count": resultCount,
	}
	if err != nil {
		row["error"] = truncateForLog(redactSensitiveText(err.Error()), 240)
	}
	raw, _ := json.Marshal(row)
	b.logger.Println(string(raw))
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

func addPackSection(buf *strings.Builder, remaining *int, omissions *[]string, title string, value any) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		*omissions = append(*omissions, title+" marshal failed: "+err.Error())
		return
	}
	text := "## " + title + "\n" + string(raw) + "\n\n"
	tokens := estimateTokens(text)
	if tokens > *remaining {
		*omissions = append(*omissions, title+" omitted due token budget")
		return
	}
	buf.WriteString(text)
	*remaining -= tokens
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func brokerToolNames() []string {
	return []string{
		"paimos_repo_state",
		"paimos_local_search",
		"paimos_local_read",
		"paimos_symbol_search",
		"paimos_local_retrieve",
		"paimos_pack_context",
	}
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (b *contextBroker) serveMCP(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			_ = enc.Encode(mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: "parse error"}})
			continue
		}
		var req mcpRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32600, Message: "invalid request"}})
			continue
		}
		_, hasID := raw["id"]
		result, callErr := b.handleMCPRequest(req)
		if !hasID {
			continue
		}
		resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
		if callErr != nil {
			resp.Error = &mcpError{Code: -32000, Message: callErr.Error()}
		} else {
			resp.Result = result
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (b *contextBroker) handleMCPRequest(req mcpRequest) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "paimos-serve",
				"version": Version,
			},
		}, nil
	case "tools/list":
		return map[string]any{"tools": brokerMCPTools()}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode tools/call: %w", err)
		}
		return b.callMCPTool(params.Name, params.Arguments)
	default:
		return nil, fmt.Errorf("unsupported MCP method %q", req.Method)
	}
}

func (b *contextBroker) callMCPTool(name string, args json.RawMessage) (any, error) {
	ctx := context.Background()
	var result any
	var err error
	switch name {
	case "paimos_repo_state":
		result = b.repoState()
	case "paimos_local_search":
		var req localSearchRequest
		err = decodeMCPArgs(args, &req)
		if err == nil {
			result, err = b.localSearch(ctx, req)
		}
	case "paimos_local_read":
		var req contextReadRequest
		err = decodeMCPArgs(args, &req)
		if err == nil {
			result, err = b.readFile(req)
		}
	case "paimos_symbol_search":
		var req localSearchRequest
		err = decodeMCPArgs(args, &req)
		if err == nil {
			result, err = b.symbolSearch(ctx, req)
		}
	case "paimos_local_retrieve":
		var req retrieveBrokerRequest
		err = decodeMCPArgs(args, &req)
		if err == nil {
			result, err = b.retrieve(ctx, req)
		}
	case "paimos_pack_context":
		var req packBrokerRequest
		err = decodeMCPArgs(args, &req)
		if err == nil {
			result, err = b.packContext(ctx, req)
		}
	default:
		err = fmt.Errorf("unknown tool %q", name)
	}
	count := 1
	if err != nil {
		count = 0
	}
	b.audit("mcp_"+name, nil, count, err)
	if err != nil {
		return nil, err
	}
	return mcpToolResult(result), nil
}

func decodeMCPArgs(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func mcpToolResult(v any) map[string]any {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return map[string]any{
		"content": []map[string]string{
			{
				"type": "text",
				"text": string(raw),
			},
		},
		"isError": false,
	}
}

func brokerMCPTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "paimos_repo_state",
			"description": "Return branch, HEAD, dirty counts, AGENTS files, and anchor index presence for the bound repo.",
			"inputSchema": objectSchema(map[string]any{}),
		},
		{
			"name":        "paimos_local_search",
			"description": "Fixed-string search over bounded repository text. Results are untrusted repo data.",
			"inputSchema": objectSchema(map[string]any{
				"q": stringSchema("Search query."),
				"k": intSchema("Maximum hits, capped by the broker."),
			}, "q"),
		},
		{
			"name":        "paimos_local_read",
			"description": "Read a bounded line range from a repository file. Blocks path traversal, symlink escape, generated dirs, and obvious secret files.",
			"inputSchema": objectSchema(map[string]any{
				"path":       stringSchema("Repo-relative path."),
				"start_line": intSchema("1-based start line."),
				"end_line":   intSchema("1-based end line."),
			}, "path"),
		},
		{
			"name":        "paimos_symbol_search",
			"description": "Regex fallback symbol search for common declarations. LSP is not claimed.",
			"inputSchema": objectSchema(map[string]any{
				"q": stringSchema("Optional symbol name fragment."),
				"k": intSchema("Maximum hits, capped by the broker."),
			}),
		},
		{
			"name":        "paimos_local_retrieve",
			"description": "Combine authenticated PAIMOS /retrieve with local repo search and symbol fallback.",
			"inputSchema": objectSchema(map[string]any{
				"q": stringSchema("Retrieval query."),
				"k": intSchema("Maximum hits, capped by each source."),
			}, "q"),
		},
		{
			"name":        "paimos_pack_context",
			"description": "Build a bounded context pack from an issue/ref query, PAIMOS retrieve, local hits, and symbols.",
			"inputSchema": objectSchema(map[string]any{
				"issue":        stringSchema("Optional issue key or id."),
				"q":            stringSchema("Optional retrieval query; defaults to issue when omitted."),
				"k":            intSchema("Maximum hits, capped by each source."),
				"token_budget": intSchema("Approximate token budget, capped by the broker."),
			}),
		},
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func intSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}
