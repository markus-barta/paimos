// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// Command paimos-mcp is the Model Context Protocol facade for PAIMOS
// instances. Agent clients (Claude Desktop, etc.) spawn it as a stdio
// subprocess and issue JSON-RPC 2.0 messages to discover and call a
// minimal set of PAIMOS tools.
//
// Scope (PAI-95, v1): paimos_schema, paimos_issue_get, _list, _create,
// _update, _relation_add. Deliberately NOT exposing batch-update /
// apply — MCP context tends to bloat fast; agents that need bulk
// should shell out to the `paimos` CLI instead.
//
// Configuration is re-used from the CLI: reads ~/.paimos/config.yaml
// and honours PAIMOS_INSTANCE env var for the instance name.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/markus-barta/paimos/backend/cmd/paimos-mcp/mcpclient"
)

// Version is stamped by goreleaser/-ldflags; "dev" for local builds.
// Surfaced in the initialize response so clients can log which build
// they're talking to.
var Version = "dev"

func main() {
	// MCP servers are spawned by the client and talk over stdio —
	// anything on stderr is the server's own diagnostic stream; stdout
	// is strictly JSON-RPC frames. Keep log output on stderr.
	logger := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "paimos-mcp: "+format+"\n", args...)
	}

	client, err := mcpclient.NewFromConfig()
	if err != nil {
		logger("failed to load paimos config: %v — run `paimos auth login`", err)
		os.Exit(1)
	}
	srv := &Server{
		client: client,
		logger: logger,
	}
	if err := srv.Run(os.Stdin, os.Stdout); err != nil {
		logger("server terminated: %v", err)
		os.Exit(1)
	}
}

// Server holds the MCP protocol state + the PAIMOS client used to
// satisfy tool calls.
type Server struct {
	client *mcpclient.Client
	logger func(format string, args ...any)
}

// rpcRequest is the JSON-RPC 2.0 request envelope used by MCP.
// "id" is nullable for notifications (no response expected).
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse mirrors the JSON-RPC response envelope. Exactly one of
// Result / Error must be set for a given ID.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	// JSON-RPC standard error codes we actually use.
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Run reads newline-delimited JSON-RPC messages from `in` and writes
// responses to `out`. MCP over stdio uses Content-Length framing in
// some implementations; the canonical Python/TS SDKs use newline-
// delimited JSON on stdio, which is what we implement here.
func (s *Server) Run(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	writer := json.NewEncoder(out)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}
		if len(line) == 0 || line[0] == '\n' {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Can't route without a parsed request; log and continue.
			s.logger("bad request: %v", err)
			continue
		}
		resp := s.dispatch(&req)
		if req.ID == nil || string(req.ID) == "null" {
			// Notifications (no ID) expect no response.
			continue
		}
		if err := writer.Encode(resp); err != nil {
			return fmt.Errorf("encode: %w", err)
		}
	}
}

// dispatch routes a request to the right handler and builds the
// response envelope. Keeps error-handling in one place.
func (s *Server) dispatch(req *rpcRequest) *rpcResponse {
	resp := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = s.handleInitialize()
	case "tools/list":
		resp.Result = s.handleToolsList()
	case "tools/call":
		result, err := s.handleToolsCall(req.Params)
		if err != nil {
			resp.Error = err
		} else {
			resp.Result = result
		}
	case "notifications/initialized":
		// No-op for us — the client telling us it's ready.
		resp.Result = map[string]any{}
	default:
		resp.Error = &rpcError{
			Code:    codeMethodNotFound,
			Message: "method not found: " + req.Method,
		}
	}
	return resp
}

// handleInitialize responds to the MCP initialize handshake. Declares
// the tools capability (we don't support resources, prompts, or
// sampling in v1). protocolVersion tracks the MCP spec revision; we
// mirror whichever version the client sent.
func (s *Server) handleInitialize() any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]any{
			"name":    "paimos-mcp",
			"version": Version,
		},
	}
}
