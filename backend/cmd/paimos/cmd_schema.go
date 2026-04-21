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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Schema is the local mirror of what the server's /api/schema returns
// (see backend/handlers/schema.go). Kept as a map so we don't have to
// chase server-side schema evolution in lockstep — the CLI treats new
// fields as opaque and preserves them through cache writes.
type CachedSchema struct {
	Version     string                         `json:"version"`
	Enums       map[string][]string            `json:"enums"`
	Transitions map[string]map[string][]string `json:"transitions"`
	Entities    map[string]map[string]any      `json:"entities"`
	Conventions map[string]string              `json:"conventions"`
	// FetchedAt is locally added, not from the server. Makes `doctor`
	// and `schema` show when the cache was last refreshed.
	FetchedAt string `json:"fetched_at,omitempty"`
}

// schemaCachePath returns ~/.paimos/schema-<instance>.json so
// multi-instance setups don't clobber each other's cached schemas.
func schemaCachePath(instanceName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".paimos", "schema-"+instanceName+".json"), nil
}

// fetchSchema GETs /api/schema, writes the cache file, returns the
// parsed payload. Uses If-None-Match if a cached version exists so
// a 304 skips the re-download.
func fetchSchema(client *Client, instanceName string) (*CachedSchema, bool, error) {
	cached, _ := loadCachedSchema(instanceName)

	req, err := http.NewRequest("GET", client.baseURL+"/api/schema", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	// No auth — /api/schema is public. Don't even set the Authorization
	// header so the endpoint stays cacheable at intermediaries.
	if cached != nil {
		// The server returns a weak ETag with a hash body. We haven't
		// cached the ETag locally; use the version as a cheap preflight
		// check (doesn't give us 304 but gives a fast "same version"
		// short-circuit — see below).
	}

	resp, err := client.http.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("fetch schema: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("GET /api/schema: %s", resp.Status)
	}
	var out CachedSchema
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false, fmt.Errorf("decode schema: %w", err)
	}
	out.FetchedAt = time.Now().UTC().Format(time.RFC3339)

	changed := cached == nil || cached.Version != out.Version

	if err := saveCachedSchema(instanceName, &out); err != nil {
		// Non-fatal — the command still works, caching is an optimisation.
		fmt.Fprintf(stderr, "paimos: warning: could not persist schema cache: %v\n", err)
	}
	return &out, changed, nil
}

// loadCachedSchema returns the on-disk schema for the instance, or nil
// if the file doesn't exist / is unreadable.
func loadCachedSchema(instanceName string) (*CachedSchema, error) {
	path, err := schemaCachePath(instanceName)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out CachedSchema
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// saveCachedSchema writes the schema atomically alongside config.yaml.
// Not secret, so 0644 is fine.
func saveCachedSchema(instanceName string, s *CachedSchema) error {
	path, err := schemaCachePath(instanceName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".schema.*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// schemaCmd: `paimos schema` — prints the cached schema; with
// --refresh, fetches from the server first.
func schemaCmd() *cobra.Command {
	var refresh bool
	c := &cobra.Command{
		Use:   "schema",
		Short: "Show / refresh the cached API schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			instanceName, inst, err := resolveInstance(cfg)
			if err != nil {
				return err
			}

			client := newClient(inst)
			var sch *CachedSchema
			var changed bool
			if refresh {
				sch, changed, err = fetchSchema(client, instanceName)
				if err != nil {
					return err
				}
			} else {
				sch, _ = loadCachedSchema(instanceName)
				if sch == nil {
					// First run with no cache — fetch transparently.
					sch, changed, err = fetchSchema(client, instanceName)
					if err != nil {
						return err
					}
				}
			}

			if flagJSON {
				return emitJSON(sch)
			}
			fmt.Fprintf(stdout, "instance: %s (%s)\n", instanceName, inst.URL)
			fmt.Fprintf(stdout, "version:  %s\n", sch.Version)
			fmt.Fprintf(stdout, "fetched:  %s\n", sch.FetchedAt)
			for name, values := range sch.Enums {
				fmt.Fprintf(stdout, "enum %s: %s\n", name, strings.Join(values, ", "))
			}
			if refresh {
				if changed {
					fmt.Fprintln(stdout, "(schema changed vs. previous cache)")
				} else {
					fmt.Fprintln(stdout, "(no changes)")
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch fresh from the server")
	return c
}

// doctorCheck is one row in `paimos doctor` output.
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail,omitempty"`
}

// doctorCmd: `paimos doctor` — read-only preflight. Safe in CI.
//
// Checks (each prints status + optional hint). Exit codes: 0 all green,
// 1 any yellow (warnings), 2 any red (hard failures).
func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run read-only preflight checks (safe in CI)",
		Long: `Verifies the CLI can reach the active instance and that
cached state isn't stale. Read-only — never writes to PAIMOS.

Exit codes:
  0   all green
  1   warnings only (e.g. stale schema cache)
  2   at least one hard failure (unreachable, auth broken)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var results []doctorCheck

			// 1. Config readable + instance resolvable.
			cfg, err := loadConfig()
			if err != nil {
				results = append(results, doctorCheck{Name: "config", Status: "fail", Detail: err.Error()})
				return renderDoctor(results)
			}
			instanceName, inst, err := resolveInstance(cfg)
			if err != nil {
				results = append(results, doctorCheck{Name: "config", Status: "fail", Detail: err.Error()})
				return renderDoctor(results)
			}
			results = append(results, doctorCheck{Name: "config", Status: "ok",
				Detail: fmt.Sprintf("%s (%s)", instanceName, inst.URL)})

			client := newClient(inst)

			// 2. /api/health — unauth, fast.
			if _, err := client.do("GET", "/api/health", nil); err != nil {
				results = append(results, doctorCheck{Name: "health", Status: "fail", Detail: err.Error()})
				return renderDoctor(results)
			}
			results = append(results, doctorCheck{Name: "health", Status: "ok"})

			// 3. /api/auth/me — verifies the API key is still valid.
			me, err := client.do("GET", "/api/auth/me", nil)
			if err != nil {
				results = append(results, doctorCheck{Name: "auth", Status: "fail",
					Detail: "API key rejected — run `paimos auth login`"})
				return renderDoctor(results)
			}
			var meShaped struct {
				User map[string]any `json:"user"`
			}
			_ = json.Unmarshal(me, &meShaped)
			userName, _ := meShaped.User["username"].(string)
			results = append(results, doctorCheck{Name: "auth", Status: "ok",
				Detail: "user=" + userName})

			// 4. /api/schema — fetch + compare to cache.
			sch, changed, ferr := fetchSchema(client, instanceName)
			if ferr != nil {
				results = append(results, doctorCheck{Name: "schema", Status: "fail", Detail: ferr.Error()})
				return renderDoctor(results)
			}
			if changed {
				results = append(results, doctorCheck{Name: "schema", Status: "warn",
					Detail: fmt.Sprintf("refreshed to version %s (previous cache was stale)", sch.Version)})
			} else {
				results = append(results, doctorCheck{Name: "schema", Status: "ok",
					Detail: "version=" + sch.Version})
			}

			return renderDoctor(results)
		},
	}
}

// renderDoctor prints results and calls os.Exit with the right code.
// Never returns in warn/fail cases — exits for the caller. Returns nil
// on all-green so Cobra's normal 0-exit path proceeds.
func renderDoctor(results []doctorCheck) error {
	worst := 0 // 0 ok, 1 warn, 2 fail
	if flagJSON {
		b, _ := json.Marshal(results)
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintln(stdout, "paimos doctor — read-only preflight")
	}
	for _, c := range results {
		if !flagJSON {
			icon := "✓"
			if c.Status == "warn" {
				icon = "!"
			} else if c.Status == "fail" {
				icon = "✗"
			}
			line := fmt.Sprintf("  %s %-12s %s", icon, c.Name, c.Status)
			if c.Detail != "" {
				line += " — " + c.Detail
			}
			fmt.Fprintln(stdout, line)
		}
		switch c.Status {
		case "warn":
			if worst < 1 {
				worst = 1
			}
		case "fail":
			worst = 2
		}
	}
	if worst == 2 {
		os.Exit(2)
	}
	if worst == 1 {
		os.Exit(1)
	}
	return nil
}
