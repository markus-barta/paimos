// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// PAI-327 — `paimos session start` and companion verbs.
//
// Closes the loop end-to-end: an agent / Claude Code skill / human
// calls one verb to start a session. The verb resolves the project's
// declared agents (PAI-326), validates the chosen agent, mints a fresh
// session UUID, and emits eval-friendly env-var assignments that
// `paimos` itself reads via PAI-325.
//
// Forward-compatibility (PAI-340): the argument parser is structured
// around a single `--format env|json` knob (with `--export` / global
// `--json` as compat aliases) so PAI-340 can add `--format files` and
// `--bundle minimal|full` without reshaping the flag surface or
// rewriting existing call sites.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// sessionCmd is the parent for `paimos session ...` verbs. The MVP
// (PAI-327) ships `start`; `show` and `end` are companion verbs the
// ticket marks as nice-to-have. They land here so the CLI surface stays
// consistent regardless of which subset is wired up.
func sessionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "session",
		Short: "Manage paimos agent-attribution sessions",
		Long: `Session verbs read the project's declared agents (PAI-326), mint a
session UUID, and emit env-var assignments that the rest of the paimos
CLI consumes (PAI-325).

Typical usage from a slash-command activation hook:

  eval $(paimos session start --project BON26 --agent ops)

Subsequent ` + "`paimos`" + ` writes from that shell carry
PAIMOS_AGENT_NAME / PAIMOS_SESSION_ID headers automatically.`,
	}
	c.AddCommand(sessionStartCmd())
	c.AddCommand(sessionShowCmd())
	c.AddCommand(sessionEndCmd())
	return c
}

// sessionFormat enumerates the output shapes for `session start`. It's
// a string-typed enum (rather than a bool pair) deliberately: PAI-340
// adds `files` on top of PAI-327's `env|json`, and we want to extend
// by adding a constant rather than juggling more mutually-exclusive
// booleans.
type sessionFormat string

const (
	sessionFormatEnv   sessionFormat = "env"
	sessionFormatJSON  sessionFormat = "json"
	sessionFormatFiles sessionFormat = "files"
)

// resolveSessionFormat collapses the `--format`, `--export`, `--json`
// (and the global `--json`) flags into a single canonical format. The
// precedence is:
//
//  1. explicit --format <env|json|files> wins
//  2. else --json (or the global --json) → json
//  3. else --export (default-on) → env
//
// Errors out on an unknown --format value so a typo surfaces rather
// than silently regressing to the default behaviour.
func resolveSessionFormat(formatFlag string, exportFlag, localJSONFlag, globalJSONFlag bool) (sessionFormat, error) {
	trimmed := strings.TrimSpace(strings.ToLower(formatFlag))
	if trimmed != "" {
		switch sessionFormat(trimmed) {
		case sessionFormatEnv, sessionFormatJSON, sessionFormatFiles:
			return sessionFormat(trimmed), nil
		default:
			return "", &usageError{
				msg: fmt.Sprintf("invalid --format %q (expected env, json, or files)", formatFlag),
			}
		}
	}
	if localJSONFlag || globalJSONFlag {
		return sessionFormatJSON, nil
	}
	// `--export` defaults to true in the flag definition, so falling
	// here means the user didn't pass --json / --format and we emit env.
	_ = exportFlag
	return sessionFormatEnv, nil
}

// projectSummary is the subset of /api/projects we care about for
// resolving --project (key or numeric id) → numeric id. Mirrors what
// resolveProjectKeyToID does but accepts both forms so session-start
// users can pass either.
type projectSummary struct {
	ID  int64  `json:"id"`
	Key string `json:"key"`
}

// resolveProjectRefToID accepts either a project key (e.g. BON26) or a
// numeric DB id and returns the numeric id. The key-only helper from
// cmd_issue.go is kept untouched because its callers already enforce
// "must be a key"; this one is the relaxed variant the session verbs
// need.
func resolveProjectRefToID(c *Client, ref string) (int64, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return 0, &usageError{msg: "--project is required"}
	}
	body, err := c.do("GET", "/api/projects", nil)
	if err != nil {
		return 0, err
	}
	var list []projectSummary
	if err := json.Unmarshal(body, &list); err != nil {
		return 0, fmt.Errorf("decode projects: %w", err)
	}
	// Numeric form: match by id directly so a typo'd id surfaces as
	// "not found" rather than as a misleading "key not found".
	if id, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		for _, p := range list {
			if p.ID == id {
				return p.ID, nil
			}
		}
		return 0, fmt.Errorf("project id %d not found (are you on the right --instance?)", id)
	}
	for _, p := range list {
		if p.Key == trimmed {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("project %q not found (are you on the right --instance?)", trimmed)
}

// agentSummary is the subset of /api/projects/{id}/agents we use to
// validate `--agent`. The endpoint returns far more (PAI-329 fields);
// we only need the names here.
type agentSummary struct {
	Name string `json:"name"`
}

// fetchProjectAgentNames returns the sorted list of declared agent
// names on the project. The server already sorts by name; we trust the
// order and only normalise to a clean []string for error messages.
func fetchProjectAgentNames(c *Client, projectID int64) ([]string, error) {
	body, err := c.do("GET", fmt.Sprintf("/api/projects/%d/agents", projectID), nil)
	if err != nil {
		return nil, err
	}
	var agents []agentSummary
	if err := json.Unmarshal(body, &agents); err != nil {
		return nil, fmt.Errorf("decode agents: %w", err)
	}
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		if n := strings.TrimSpace(a.Name); n != "" {
			names = append(names, n)
		}
	}
	return names, nil
}

// validateAgentName checks whether `want` is one of the declared agents
// and returns a human-readable error listing the valid choices when not.
// Lifted out of the command body so the unit test can pin the exact
// message shape — agents (and humans) read this error frequently when
// they fat-finger a slash-command name.
func validateAgentName(want string, declared []string) error {
	want = strings.TrimSpace(want)
	if want == "" {
		return &usageError{msg: "--agent is required"}
	}
	for _, n := range declared {
		if n == want {
			return nil
		}
	}
	if len(declared) == 0 {
		return fmt.Errorf("no agents declared on this project (define one in the project settings before calling session start)")
	}
	return fmt.Errorf("agent %q is not declared on this project. Valid agents: %s", want, strings.Join(declared, ", "))
}

// newSessionUUID returns a fresh v4 UUID string. The CLI's package-level
// `sessionID` (in client.go) prefers v7 for time-ordered correlation,
// but the ticket explicitly calls for v4 here — this UUID becomes the
// stable identifier of the *agent session*, not of a single CLI
// invocation, so time-ordering across processes isn't useful.
func newSessionUUID() string {
	return uuid.NewString()
}

// sessionStartCmd: `paimos session start --project <key|id> --agent <name>`.
//
// The flag surface is intentionally extension-friendly: `--format` is
// the canonical knob, with `--export` and `--json` as compat shims.
// PAI-340 layered `--bundle minimal|full` on top to also ship the
// project's full context (agent artifact + memory + runbooks +
// external systems + related projects + guidelines).
func sessionStartCmd() *cobra.Command {
	var (
		projectRef string
		agentName  string
		format     string
		exportFlag bool
		jsonFlag   bool
		bundleStr  string
		refresh    bool
		cacheDir   string
		// PAI-347 — opt-in flag to include `low` confidence memories
		// in the bundle. Default-off keeps the bundle compact.
		includeLow bool
	)
	c := &cobra.Command{
		Use:   "start",
		Short: "Start an agent session: validate agent + mint UUID + (optionally) bundle full project context",
		Long: `Validates --agent against the project's declared agents (PAI-326),
mints a fresh session UUID, and prints eval-friendly env-var assignments
to stdout. The output is suitable for:

  eval $(paimos session start --project BON26 --agent ops)

After eval, subsequent paimos writes from that shell carry the
PAIMOS_AGENT_NAME / PAIMOS_SESSION_ID headers automatically (PAI-325).

Output formats:
  --format env   (default)  one "export KEY=value" per line, eval-safe
  --format json             single JSON document (full bundle when --bundle full)
  --format files            write per-entry markdown into the cache dir

Bundle modes (PAI-340):
  --bundle minimal (default) — agent + session UUID env vars only (PAI-327)
  --bundle full              — also resolve memory / runbooks / external
                               systems / related projects / guidelines,
                               filtered to the current agent + user

  --refresh                  — force a re-fetch (ignore cache)
  --cache-dir <path>         — override the .paimos/cache root`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectRef) == "" {
				return &usageError{msg: "--project is required"}
			}
			if strings.TrimSpace(agentName) == "" {
				return &usageError{msg: "--agent is required"}
			}

			// Resolve format + bundle mode BEFORE hitting the API so
			// usage errors fail fast (no needless network round-trip).
			resolvedFormat, err := resolveSessionFormat(format, exportFlag, jsonFlag, flagJSON)
			if err != nil {
				return err
			}
			mode, err := resolveBundleMode(bundleStr)
			if err != nil {
				return err
			}

			// `--format files` only makes sense in bundle mode — there's
			// no per-entry content to write when the bundle is just the
			// agent + session UUID. Surface the conflict as a usageError
			// rather than silently writing an empty tree.
			if resolvedFormat == sessionFormatFiles && mode != bundleModeFull {
				return &usageError{
					msg: "--format files requires --bundle full",
				}
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}

			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}

			declared, err := fetchProjectAgentNames(client, projectID)
			if err != nil {
				return reportError(err)
			}
			if err := validateAgentName(agentName, declared); err != nil {
				// usageError → exit 2; non-usage errors → exit 1.
				return err
			}

			sid := newSessionUUID()

			// PAI-327 fast path: no bundle requested → emit just the
			// agent + session env vars and we're done.
			if mode != bundleModeFull {
				return emitSessionStart(resolvedFormat, agentName, sid)
			}

			// PAI-340 full-bundle path. Resolve project key (used for
			// the cache directory name) by re-decoding the project list;
			// resolveProjectRefToID already validated the project exists.
			projectKey, err := resolveProjectKeyFromID(client, projectID)
			if err != nil {
				return reportError(err)
			}
			project := projectSummary{ID: projectID, Key: projectKey}

			cacheRoot, err := resolveCacheRoot(cacheDir)
			if err != nil {
				return err
			}

			// Cache short-circuit: an existing manifest counts as fresh
			// for v1 (PAI-341 will harden this with a server-side rev
			// check). `--refresh` forces a re-fetch regardless.
			if !refresh && resolvedFormat != sessionFormatJSON {
				if cached, _ := readBundleManifest(cacheRoot, projectKey); cached != nil {
					// We still emit the env exports / file confirmation
					// against the cached payload so a `--bundle full`
					// run is fast on the warm path. The JSON format
					// always re-fetches (it returns the full bundle on
					// stdout — clients calling for json want fresh).
					return emitFromCache(resolvedFormat, agentName, sid, cacheRoot, project, cached)
				}
			}

			bundle, err := resolveBundle(client, project, agentName, includeLow)
			if err != nil {
				return reportError(err)
			}

			switch resolvedFormat {
			case sessionFormatJSON:
				return emitBundleJSON(bundle, agentName, sid)
			case sessionFormatFiles:
				return emitBundleFiles(bundle, agentName, cacheRoot)
			case sessionFormatEnv:
				return emitBundleEnv(bundle, agentName, sid, cacheRoot)
			default:
				return fmt.Errorf("unsupported session format %q", resolvedFormat)
			}
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&agentName, "agent", "", "agent name declared on the project (required)")
	// Forward-compatible format knob: PAI-340 adds `files` here.
	c.Flags().StringVar(&format, "format", "", "output format: env (default), json, or files")
	c.Flags().BoolVar(&exportFlag, "export", true, "emit eval-friendly export lines (default; alias for --format env)")
	c.Flags().BoolVar(&jsonFlag, "json", false, "emit a JSON record (alias for --format json)")
	// PAI-340 bundle controls.
	c.Flags().StringVar(&bundleStr, "bundle", "", "bundle mode: minimal (default; PAI-327 behaviour) or full (PAI-340)")
	c.Flags().BoolVar(&refresh, "refresh", false, "force re-fetch even when a manifest exists in the cache")
	c.Flags().StringVar(&cacheDir, "cache-dir", "", "cache directory root (default: ./.paimos/cache)")
	// PAI-347 — confidence gate. Default-off (skip low-confidence
	// memories); opt in to include them.
	c.Flags().BoolVar(&includeLow, "include-low", false, "include low-confidence memories in --bundle full (default: skip)")
	// `--export` and `--json` aren't strictly mutually-exclusive at the
	// flag layer — resolveSessionFormat handles precedence — so the
	// --bundle / --format extension layers over them cleanly.
	return c
}

// resolveCacheRoot picks the cache directory root: the explicit
// `--cache-dir` value (when non-empty) or `<cwd>/.paimos/cache`. The
// path is filepath.Cleaned so a relative `--cache-dir` resolves
// against cwd consistently.
func resolveCacheRoot(flag string) (string, error) {
	if s := strings.TrimSpace(flag); s != "" {
		if filepath.IsAbs(s) {
			return filepath.Clean(s), nil
		}
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.Join(wd, s)), nil
	}
	return defaultCacheRoot()
}

// resolveProjectKeyFromID looks up a project's key given its numeric
// id. The session-start path already validates the project exists via
// resolveProjectRefToID; this helper exists so the bundle code can
// take the canonical key for the cache directory name without re-
// implementing the lookup.
func resolveProjectKeyFromID(c *Client, projectID int64) (string, error) {
	body, err := c.do("GET", "/api/projects", nil)
	if err != nil {
		return "", err
	}
	var list []projectSummary
	if err := json.Unmarshal(body, &list); err != nil {
		return "", fmt.Errorf("decode projects: %w", err)
	}
	for _, p := range list {
		if p.ID == projectID {
			return p.Key, nil
		}
	}
	return "", fmt.Errorf("project id %d not found", projectID)
}

// emitFromCache reuses an existing on-disk manifest. The cache path
// fires only when `--refresh` is unset and the format is not `json`
// (json always returns a freshly-fetched payload — see the caller).
func emitFromCache(format sessionFormat, agentName, sessionID, cacheRoot string, project projectSummary, m *cacheManifest) error {
	dir := filepath.Clean(filepath.Join(cacheRoot, project.Key))
	switch format {
	case sessionFormatEnv:
		fmt.Fprintf(stdout, "export PAIMOS_AGENT_NAME=%s\n", agentName)
		fmt.Fprintf(stdout, "export PAIMOS_SESSION_ID=%s\n", sessionID)
		fmt.Fprintf(stdout, "export PAIMOS_KNOWLEDGE_DIR=%s\n", dir)
		return nil
	case sessionFormatFiles:
		fmt.Fprintf(stdout, "wrote bundle to %s (rev=%s, cached)\n", dir, m.Rev)
		return nil
	default:
		return fmt.Errorf("unsupported cache format %q", format)
	}
}

// emitSessionStart writes the session-start payload in the chosen
// format. Split out so tests can call it directly without spinning up
// the full Cobra command.
func emitSessionStart(format sessionFormat, agentName, sessionID string) error {
	switch format {
	case sessionFormatJSON:
		return emitJSON(map[string]any{
			"agent_name": agentName,
			"session_id": sessionID,
		})
	case sessionFormatEnv:
		// Two `export` lines, one per env var. Quoting is unnecessary —
		// agent names are validated against [a-z][a-z0-9_-]* server-side
		// and UUIDs are hex+hyphens — but use the standard form so
		// future agents with looser names still eval cleanly.
		fmt.Fprintf(stdout, "export PAIMOS_AGENT_NAME=%s\n", agentName)
		fmt.Fprintf(stdout, "export PAIMOS_SESSION_ID=%s\n", sessionID)
		return nil
	default:
		return fmt.Errorf("unsupported session format %q", format)
	}
}

// sessionShowCmd: `paimos session show` — print the current
// PAIMOS_AGENT_NAME / PAIMOS_SESSION_ID env values (or "(unset)" when
// missing). Companion verb the ticket marks as nice-to-have.
func sessionShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the current PAIMOS_AGENT_NAME / PAIMOS_SESSION_ID env values",
		Long: `Prints the agent-attribution env vars the current shell will forward
on paimos writes. Prefers the persistent env (PAIMOS_AGENT_NAME /
PAIMOS_SESSION_ID) over the per-invocation flags (--agent-name /
--session-id) so the output matches what new shells will see.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			agent := strings.TrimSpace(os.Getenv("PAIMOS_AGENT_NAME"))
			session := strings.TrimSpace(os.Getenv("PAIMOS_SESSION_ID"))
			if flagJSON {
				return emitJSON(map[string]any{
					"agent_name": agent,
					"session_id": session,
				})
			}
			if agent == "" {
				agent = "(unset)"
			}
			if session == "" {
				session = "(unset)"
			}
			fmt.Fprintf(stdout, "PAIMOS_AGENT_NAME=%s\n", agent)
			fmt.Fprintf(stdout, "PAIMOS_SESSION_ID=%s\n", session)
			return nil
		},
	}
}

// sessionEndCmd: `paimos session end` — symmetric closer. The ticket
// marks this optional ("log a session-close audit record"); since the
// server doesn't yet have a no-op write that carries attribution
// headers without mutating state, this verb just prints the end marker
// and clears the local env vars from the eval-friendly output. A
// follow-up can wire in a server-side audit endpoint.
func sessionEndCmd() *cobra.Command {
	var (
		format     string
		exportFlag bool
		jsonFlag   bool
	)
	c := &cobra.Command{
		Use:   "end",
		Short: "Emit env-clearing exports to close the current session",
		Long: `Prints "unset" lines for PAIMOS_AGENT_NAME / PAIMOS_SESSION_ID so the
caller can:

  eval $(paimos session end)

and drop attribution back to per-invocation defaults. Audit-log
write-back is a follow-up (PAI-340 / future); this verb is currently a
local-only env reset.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedFormat, err := resolveSessionFormat(format, exportFlag, jsonFlag, flagJSON)
			if err != nil {
				return err
			}
			switch resolvedFormat {
			case sessionFormatJSON:
				return emitJSON(map[string]any{"ok": true, "ended": true})
			case sessionFormatEnv:
				fmt.Fprintln(stdout, "unset PAIMOS_AGENT_NAME")
				fmt.Fprintln(stdout, "unset PAIMOS_SESSION_ID")
				return nil
			default:
				return fmt.Errorf("unsupported session format %q", resolvedFormat)
			}
		},
	}
	c.Flags().StringVar(&format, "format", "", "output format: env (default) or json")
	c.Flags().BoolVar(&exportFlag, "export", true, "emit eval-friendly unset lines (default)")
	c.Flags().BoolVar(&jsonFlag, "json", false, "emit a JSON record")
	return c
}

