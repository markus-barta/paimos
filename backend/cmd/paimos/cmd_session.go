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
// will add `files` and we want to extend by adding a constant rather
// than juggling more mutually-exclusive booleans.
type sessionFormat string

const (
	sessionFormatEnv  sessionFormat = "env"
	sessionFormatJSON sessionFormat = "json"
)

// resolveSessionFormat collapses the `--format`, `--export`, `--json`
// (and the global `--json`) flags into a single canonical format. The
// precedence is:
//
//  1. explicit --format <env|json> wins
//  2. else --json (or the global --json) → json
//  3. else --export (default-on) → env
//
// Errors out on an unknown --format value so PAI-340's `files` add
// doesn't silently regress today's behaviour if we typo it.
func resolveSessionFormat(formatFlag string, exportFlag, localJSONFlag, globalJSONFlag bool) (sessionFormat, error) {
	trimmed := strings.TrimSpace(strings.ToLower(formatFlag))
	if trimmed != "" {
		switch sessionFormat(trimmed) {
		case sessionFormatEnv, sessionFormatJSON:
			return sessionFormat(trimmed), nil
		default:
			return "", &usageError{
				msg: fmt.Sprintf("invalid --format %q (expected env or json)", formatFlag),
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
// the canonical knob, with `--export` and `--json` as compat shims so
// existing scripts (and this ticket's UX promise) keep working when
// PAI-340 adds `--format files` + `--bundle minimal|full`.
func sessionStartCmd() *cobra.Command {
	var (
		projectRef string
		agentName  string
		format     string
		exportFlag bool
		jsonFlag   bool
	)
	c := &cobra.Command{
		Use:   "start",
		Short: "Start an agent session: validate agent + mint UUID + emit env exports",
		Long: `Validates --agent against the project's declared agents (PAI-326),
mints a fresh session UUID, and prints eval-friendly env-var assignments
to stdout. The output is suitable for:

  eval $(paimos session start --project BON26 --agent ops)

After eval, subsequent paimos writes from that shell carry the
PAIMOS_AGENT_NAME / PAIMOS_SESSION_ID headers automatically (PAI-325).

Output formats:
  --format env   (default)  one "export KEY=value" per line, eval-safe
  --format json             {"agent_name": "...", "session_id": "..."}

PAI-340 will extend with --format files and --bundle minimal|full
without reshaping these flags.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectRef) == "" {
				return &usageError{msg: "--project is required"}
			}
			if strings.TrimSpace(agentName) == "" {
				return &usageError{msg: "--agent is required"}
			}

			// Resolve format BEFORE hitting the API so usage errors
			// fail fast (no needless network round-trip).
			resolvedFormat, err := resolveSessionFormat(format, exportFlag, jsonFlag, flagJSON)
			if err != nil {
				return err
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

			return emitSessionStart(resolvedFormat, agentName, sid)
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&agentName, "agent", "", "agent name declared on the project (required)")
	// Forward-compatible format knob: PAI-340 adds `files` here.
	c.Flags().StringVar(&format, "format", "", "output format: env (default) or json")
	c.Flags().BoolVar(&exportFlag, "export", true, "emit eval-friendly export lines (default; alias for --format env)")
	c.Flags().BoolVar(&jsonFlag, "json", false, "emit a JSON record (alias for --format json)")
	// `--export` and `--json` aren't strictly mutually-exclusive at the
	// flag layer — resolveSessionFormat handles precedence — so a future
	// --bundle / --format extension can layer over them cleanly.
	return c
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

