// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// Package mcpclient is a minimal HTTP client for the PAIMOS REST API
// used by the paimos-mcp binary. Intentionally duplicates a subset of
// the paimos CLI's client rather than depending on it — keeps the MCP
// binary import surface tight and side-steps cross-cmd-package
// coupling. If a shared `internal/paimosclient` package is introduced
// later, this can collapse into it.
package mcpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Config mirrors the CLI's ~/.paimos/config.yaml layout so we can
// reuse files written by `paimos auth login`.
type Config struct {
	DefaultInstance string                    `yaml:"default_instance"`
	Instances       map[string]InstanceConfig `yaml:"instances"`
}

type InstanceConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
}

// Client wraps http.Client with auth + the PAIMOS session header so
// the backend's PAI-97 audit (when enabled) can correlate MCP-driven
// mutations back to a single agent session.
type Client struct {
	baseURL   string
	apiKey    string
	sessionID string
	http      *http.Client
}

// NewFromConfig reads ~/.paimos/config.yaml, picks the instance named
// by PAIMOS_INSTANCE (or default_instance if unset), and returns a
// ready-to-use client. Returns an error if no instance is configured.
func NewFromConfig() (*Client, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(cfg.Instances) == 0 {
		return nil, fmt.Errorf("no instances configured in %s — run `paimos auth login`", path)
	}
	instName := os.Getenv("PAIMOS_INSTANCE")
	if instName == "" {
		instName = cfg.DefaultInstance
	}
	if instName == "" && len(cfg.Instances) == 1 {
		for k := range cfg.Instances {
			instName = k
		}
	}
	if instName == "" {
		return nil, fmt.Errorf("multiple instances configured; set PAIMOS_INSTANCE")
	}
	inst, ok := cfg.Instances[instName]
	if !ok {
		return nil, fmt.Errorf("instance %q not in config", instName)
	}

	// Session ID per MCP server invocation. Honors PAIMOS_SESSION_ID
	// so a driver script can correlate CLI + MCP traffic.
	sid := os.Getenv("PAIMOS_SESSION_ID")
	if sid == "" {
		if id, err := uuid.NewV7(); err == nil {
			sid = id.String()
		} else {
			sid = uuid.NewString()
		}
	}

	return &Client{
		baseURL:   strings.TrimRight(inst.URL, "/"),
		apiKey:    inst.APIKey,
		sessionID: sid,
		http:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// configPath returns the config file location. PAIMOS_CONFIG env var
// overrides the default ~/.paimos/config.yaml — useful for tests and
// for MCP clients that spawn paimos-mcp with a custom config path.
func configPath() (string, error) {
	if p := os.Getenv("PAIMOS_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".paimos", "config.yaml"), nil
}

// Do issues an HTTP request with auth + session headers and returns
// the raw response body. Non-2xx responses are mapped to a Go error
// that includes the server's `{error}` message.
func (c *Client) Do(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "paimos-mcp")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-PAIMOS-Session-Id", c.sessionID)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var shaped struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &shaped) == nil && shaped.Error != "" {
			msg = shaped.Error
		}
		return raw, fmt.Errorf("API %d %s %s: %s", resp.StatusCode, method, path, msg)
	}
	return raw, nil
}
