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

package main

// PAI-506 — `paimos agent` command family. First-class CRUD over the
// project-scoped /api/projects/{id}/agents surface introduced in
// PAI-326 / PAI-329, mirroring the `knowledge` verb tree:
//
//	paimos agent list   --project P
//	paimos agent get    <name> --project P
//	paimos agent create --project P --name N [--body-file F] [...]
//	paimos agent update <name> --project P [--body-file F] [...]
//	paimos agent delete <name> --project P [--yes]
//
// Agents are project-scoped, so every verb requires --project (key or
// numeric id). The single-agent read uses the `.json` artifact endpoint
// (there is no plain GET /agents/{name}); the CLI unwraps the `.agent`
// sub-object so `get` / the update-prefetch see the bare record.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/markus-barta/paimos/backend/models"
)

// agentShape mirrors models.ProjectAgent on the wire — same fields,
// same JSON tags. The nested step/rule types are imported from models
// so the bootstrap_steps / non_negotiable_rules file inputs decode into
// the exact shape the server validates.
type agentShape struct {
	ID                 int64                       `json:"id"`
	ProjectID          int64                       `json:"project_id"`
	Name               string                      `json:"name"`
	Description        string                      `json:"description"`
	SlashCommandName   string                      `json:"slash_command_name"`
	LaneTags           []string                    `json:"lane_tags"`
	Metadata           map[string]any              `json:"metadata"`
	Body               string                      `json:"body"`
	BootstrapSteps     []models.AgentBootstrapStep `json:"bootstrap_steps"`
	NonNegotiableRules []models.AgentRule          `json:"non_negotiable_rules"`
	CreatedAt          string                      `json:"created_at"`
	UpdatedAt          string                      `json:"updated_at"`
}

// agentArtifactShape mirrors handlers.AgentArtifact's `.agent` field.
// We only care about the `agent` block here — the single-agent read
// rides on the `.json` artifact endpoint, so we peel off the wrapper
// rather than dump the whole project+repos+environments payload.
type agentArtifactShape struct {
	Agent agentShape `json:"agent"`
}

func agentCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "agent",
		Short: "List and manage project agents",
		Long: `Drive the project-scoped /api/projects/{id}/agents surface
(PAI-326 / PAI-329). One verb tree covers list / get / create / update /
delete. Every verb requires --project (key or numeric id) since agents
are owned by a project.

Multi-line / structured fields accept file inputs: --body-file for the
markdown body, --bootstrap-steps-file and --rules-file for the
structured bootstrap_steps / non_negotiable_rules JSON arrays — so
shell quoting never distorts the content.`,
	}
	c.AddCommand(agentListCmd())
	c.AddCommand(agentGetCmd())
	c.AddCommand(agentCreateCmd())
	c.AddCommand(agentUpdateCmd())
	c.AddCommand(agentDeleteCmd())
	return c
}

func agentListCmd() *cobra.Command {
	var projectRef string
	c := &cobra.Command{
		Use:   "list",
		Short: "List agents for a project",
		Long: `Lists every agent declared on the project, sorted by name.
Returns [] (never null) for projects with no agents yet.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}
			path := fmt.Sprintf("/api/projects/%d/agents", projectID)
			body, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var agents []agentShape
			if err := json.Unmarshal(body, &agents); err != nil {
				return fmt.Errorf("decode agents: %w", err)
			}
			renderAgentListPretty(agents)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	return c
}

func agentGetCmd() *cobra.Command {
	var projectRef string
	c := &cobra.Command{
		Use:   "get <name>",
		Short: "Fetch a single agent",
		Long: `Fetches one agent by name. Reads the canonical artifact
endpoint (/agents/{name}.json) and emits the agent record alone — the
project / repos / environments wrapper is dropped (use the API directly
if you need the full artifact).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return &usageError{msg: "<name> is required"}
			}
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}
			agent, err := fetchAgent(client, projectID, name)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				return emitJSON(agent)
			}
			renderAgentPretty(agent)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	return c
}

func agentCreateCmd() *cobra.Command {
	var (
		projectRef    string
		name          string
		description   string
		body          string
		bodyFile      string
		slashCommand  string
		laneTags      string
		metadata      string
		bootstrapFile string
		rulesFile     string
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create an agent",
		Long: `Creates an agent on the project. --name must be a lowercase
slug ([a-z][a-z0-9_-]*, max 32 chars; 'web-ui' is reserved). Use
--body-file for the markdown body and --bootstrap-steps-file /
--rules-file for the structured JSON arrays so shell quoting doesn't
distort the content.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name = strings.TrimSpace(name)
			if name == "" {
				return &usageError{msg: "--name is required"}
			}
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			bodyContent, bodyChanged, err := readMultilineInput(body, bodyFile, "body")
			if err != nil {
				return err
			}

			payload := map[string]any{
				"name": name,
			}
			// Only send body when the operator actually supplied one
			// (--body / --body-file). Mirrors the conditional handling
			// of the other optional fields below and the update verb;
			// the server normalises an omitted body to "" anyway.
			if bodyChanged {
				payload["body"] = bodyContent
			}
			if description = strings.TrimSpace(description); description != "" {
				payload["description"] = description
			}
			if slashCommand = strings.TrimSpace(slashCommand); slashCommand != "" {
				payload["slash_command_name"] = slashCommand
			}
			if tags := parseLaneTags(laneTags); len(tags) > 0 {
				payload["lane_tags"] = tags
			}
			if strings.TrimSpace(metadata) != "" {
				meta, err := parseJSONObjectFlag("metadata", metadata)
				if err != nil {
					return err
				}
				payload["metadata"] = meta
			}
			if strings.TrimSpace(bootstrapFile) != "" {
				steps, err := readBootstrapStepsFile(bootstrapFile)
				if err != nil {
					return err
				}
				payload["bootstrap_steps"] = steps
			}
			if strings.TrimSpace(rulesFile) != "" {
				rules, err := readRulesFile(rulesFile)
				if err != nil {
					return err
				}
				payload["non_negotiable_rules"] = rules
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}
			path := fmt.Sprintf("/api/projects/%d/agents", projectID)
			raw, err := client.do("POST", path, payload)
			if err != nil {
				return reportError(err)
			}
			var agent agentShape
			if err := json.Unmarshal(raw, &agent); err != nil {
				return fmt.Errorf("decode agent: %w", err)
			}
			if flagJSON {
				return emitJSON(agent)
			}
			fmt.Fprintf(stdout, "✓ created agent %s (#%d)\n", agent.Name, agent.ID)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&name, "name", "", "agent slug (required): [a-z][a-z0-9_-]*, max 32")
	c.Flags().StringVar(&description, "description", "", "short human description")
	c.Flags().StringVar(&body, "body", "", "inline markdown body")
	c.Flags().StringVar(&bodyFile, "body-file", "", "path to markdown body (or - for stdin)")
	c.Flags().StringVar(&slashCommand, "slash-command", "", "slash-command name the agent answers to")
	c.Flags().StringVar(&laneTags, "lane-tags", "", "comma-separated lane tags")
	c.Flags().StringVar(&metadata, "metadata", "", "metadata as a JSON object")
	c.Flags().StringVar(&bootstrapFile, "bootstrap-steps-file", "", "path to a JSON array of {title,command,rationale}")
	c.Flags().StringVar(&rulesFile, "rules-file", "", "path to a JSON array of {title,body,memory_ref}")
	return c
}

func agentUpdateCmd() *cobra.Command {
	var (
		projectRef    string
		newName       string
		description   string
		body          string
		bodyFile      string
		slashCommand  string
		laneTags      string
		metadata      string
		bootstrapFile string
		rulesFile     string
	)
	c := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an agent",
		Long: `Updates an agent by name. The server PUT replaces the whole
record, so the CLI fetches the existing agent first and carries forward
every field you don't set — giving partial-update ergonomics. Pass at
least one field. --name renames the agent (409 on collision).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return &usageError{msg: "<name> is required"}
			}
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}

			bodyContent, bodyChanged, err := readMultilineInput(body, bodyFile, "body")
			if err != nil {
				return err
			}
			nameChanged := cmd.Flags().Changed("name")
			descChanged := cmd.Flags().Changed("description")
			slashChanged := cmd.Flags().Changed("slash-command")
			laneChanged := cmd.Flags().Changed("lane-tags")
			metaChanged := cmd.Flags().Changed("metadata")
			bootstrapChanged := cmd.Flags().Changed("bootstrap-steps-file")
			rulesChanged := cmd.Flags().Changed("rules-file")
			if !nameChanged && !descChanged && !bodyChanged && !slashChanged &&
				!laneChanged && !metaChanged && !bootstrapChanged && !rulesChanged {
				return &usageError{msg: "at least one of --name, --description, --body, --body-file, --slash-command, --lane-tags, --metadata, --bootstrap-steps-file, --rules-file is required"}
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}

			// The server PUT replaces the whole record, so prefetch the
			// existing agent (via the .json artifact, peeling .agent) and
			// carry forward every field the caller didn't change.
			prev, err := fetchAgent(client, projectID, name)
			if err != nil {
				return reportError(err)
			}

			payload := map[string]any{}
			if nameChanged {
				payload["name"] = strings.TrimSpace(newName)
			} else {
				payload["name"] = prev.Name
			}
			if descChanged {
				payload["description"] = strings.TrimSpace(description)
			} else {
				payload["description"] = prev.Description
			}
			if bodyChanged {
				payload["body"] = bodyContent
			} else {
				payload["body"] = prev.Body
			}
			if slashChanged {
				payload["slash_command_name"] = strings.TrimSpace(slashCommand)
			} else {
				payload["slash_command_name"] = prev.SlashCommandName
			}
			if laneChanged {
				payload["lane_tags"] = parseLaneTags(laneTags)
			} else {
				payload["lane_tags"] = prev.LaneTags
			}
			if metaChanged {
				meta, err := parseJSONObjectFlag("metadata", metadata)
				if err != nil {
					return err
				}
				payload["metadata"] = meta
			} else {
				payload["metadata"] = prev.Metadata
			}
			if bootstrapChanged {
				steps, err := readBootstrapStepsFile(bootstrapFile)
				if err != nil {
					return err
				}
				payload["bootstrap_steps"] = steps
			} else {
				payload["bootstrap_steps"] = prev.BootstrapSteps
			}
			if rulesChanged {
				rules, err := readRulesFile(rulesFile)
				if err != nil {
					return err
				}
				payload["non_negotiable_rules"] = rules
			} else {
				payload["non_negotiable_rules"] = prev.NonNegotiableRules
			}

			path := fmt.Sprintf("/api/projects/%d/agents/%s", projectID, url.PathEscape(name))
			raw, err := client.do("PUT", path, payload)
			if err != nil {
				return reportError(err)
			}
			var agent agentShape
			if err := json.Unmarshal(raw, &agent); err != nil {
				return fmt.Errorf("decode agent: %w", err)
			}
			if flagJSON {
				return emitJSON(agent)
			}
			fmt.Fprintf(stdout, "✓ updated agent %s (#%d)\n", agent.Name, agent.ID)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&newName, "name", "", "rename the agent to this slug")
	c.Flags().StringVar(&description, "description", "", "new description")
	c.Flags().StringVar(&body, "body", "", "inline markdown body (replaces current)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "path to markdown body (or - for stdin)")
	c.Flags().StringVar(&slashCommand, "slash-command", "", "new slash-command name")
	c.Flags().StringVar(&laneTags, "lane-tags", "", "comma-separated lane tags (replaces current)")
	c.Flags().StringVar(&metadata, "metadata", "", "metadata as a JSON object (replaces current)")
	c.Flags().StringVar(&bootstrapFile, "bootstrap-steps-file", "", "path to a JSON array of {title,command,rationale} (replaces current)")
	c.Flags().StringVar(&rulesFile, "rules-file", "", "path to a JSON array of {title,body,memory_ref} (replaces current)")
	return c
}

func agentDeleteCmd() *cobra.Command {
	var (
		projectRef string
		yes        bool
	)
	c := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an agent",
		Long: `Deletes an agent from the project. --yes skips the interactive
confirm; the default refuses to proceed without a TTY-attached prompt so
a stray script can't lose data.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return &usageError{msg: "<name> is required"}
			}
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			if err := confirmAgentDelete(name, yes); err != nil {
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
			path := fmt.Sprintf("/api/projects/%d/agents/%s", projectID, url.PathEscape(name))
			if _, err := client.do("DELETE", path, nil); err != nil {
				return reportError(err)
			}
			if flagJSON {
				return emitJSON(map[string]any{
					"ok":     true,
					"name":   name,
					"action": "delete",
				})
			}
			fmt.Fprintf(stdout, "✓ deleted agent %s\n", name)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().BoolVar(&yes, "yes", false, "skip the interactive confirm")
	return c
}

// ── helpers ─────────────────────────────────────────────────────────

// fetchAgent reads one agent via the canonical artifact endpoint
// (/agents/{name}.json) and unwraps the `.agent` sub-object. There is
// no plain GET /agents/{name}, so this is the single-record read path
// for both `get` and the update-prefetch.
func fetchAgent(client *Client, projectID int64, name string) (agentShape, error) {
	path := fmt.Sprintf("/api/projects/%d/agents/%s.json", projectID, url.PathEscape(name))
	body, err := client.do("GET", path, nil)
	if err != nil {
		return agentShape{}, err
	}
	var artifact agentArtifactShape
	if err := json.Unmarshal(body, &artifact); err != nil {
		return agentShape{}, fmt.Errorf("decode agent artifact: %w", err)
	}
	return artifact.Agent, nil
}

// parseLaneTags splits a comma-separated --lane-tags value into a
// trimmed, non-empty slice. Returns nil for an empty input so callers
// can decide whether to include the key.
func parseLaneTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseJSONObjectFlag decodes a --metadata-style JSON object flag into
// a map, surfacing a usage-style error on bad JSON.
func parseJSONObjectFlag(flagName, raw string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, &usageError{msg: fmt.Sprintf("--%s must be a JSON object: %v", flagName, err)}
	}
	return out, nil
}

// readBootstrapStepsFile reads a JSON array file of bootstrap steps.
func readBootstrapStepsFile(path string) ([]models.AgentBootstrapStep, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from the CLI user's own --bootstrap-steps-file flag.
	if err != nil {
		return nil, fmt.Errorf("read --bootstrap-steps-file %s: %w", path, err)
	}
	var steps []models.AgentBootstrapStep
	if err := json.Unmarshal(b, &steps); err != nil {
		return nil, &usageError{msg: fmt.Sprintf("--bootstrap-steps-file must be a JSON array of {title,command,rationale}: %v", err)}
	}
	return steps, nil
}

// readRulesFile reads a JSON array file of non-negotiable rules.
func readRulesFile(path string) ([]models.AgentRule, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from the CLI user's own --rules-file flag.
	if err != nil {
		return nil, fmt.Errorf("read --rules-file %s: %w", path, err)
	}
	var rules []models.AgentRule
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, &usageError{msg: fmt.Sprintf("--rules-file must be a JSON array of {title,body,memory_ref}: %v", err)}
	}
	return rules, nil
}

func confirmAgentDelete(name string, yes bool) error {
	if yes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return &usageError{msg: "refusing to delete agent without --yes in non-interactive mode"}
	}
	fmt.Fprintf(stderr, "Delete agent %s? Type delete to confirm: ", name)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return err
	}
	if strings.TrimSpace(line) != "delete" {
		return &usageError{msg: "delete cancelled"}
	}
	return nil
}

// ── pretty rendering ────────────────────────────────────────────────

func renderAgentListPretty(agents []agentShape) {
	if len(agents) == 0 {
		fmt.Fprintln(stdout, "(no agents)")
		return
	}
	fmt.Fprintln(stdout, "NAME                             SLASH-COMMAND               LANE-TAGS")
	for _, a := range agents {
		fmt.Fprintf(stdout, "%-32s %-27s %s\n", a.Name, a.SlashCommandName, strings.Join(a.LaneTags, ","))
	}
}

func renderAgentPretty(a agentShape) {
	fmt.Fprintf(stdout, "%s (#%d)\n", a.Name, a.ID)
	if a.Description != "" {
		fmt.Fprintf(stdout, "  description:    %s\n", a.Description)
	}
	if a.SlashCommandName != "" {
		fmt.Fprintf(stdout, "  slash-command:  %s\n", a.SlashCommandName)
	}
	if len(a.LaneTags) > 0 {
		fmt.Fprintf(stdout, "  lane-tags:      %s\n", strings.Join(a.LaneTags, ", "))
	}
	if len(a.Metadata) > 0 {
		if b, err := json.MarshalIndent(a.Metadata, "  ", "  "); err == nil {
			fmt.Fprintf(stdout, "  metadata: %s\n", string(b))
		}
	}
	if len(a.BootstrapSteps) > 0 {
		fmt.Fprintln(stdout, "  bootstrap-steps:")
		for _, s := range a.BootstrapSteps {
			fmt.Fprintf(stdout, "    - %s: %s\n", s.Title, s.Command)
		}
	}
	if len(a.NonNegotiableRules) > 0 {
		fmt.Fprintln(stdout, "  non-negotiable-rules:")
		for _, r := range a.NonNegotiableRules {
			fmt.Fprintf(stdout, "    - %s\n", r.Title)
		}
	}
	if strings.TrimSpace(a.Body) != "" {
		fmt.Fprintln(stdout, "  body:")
		for _, line := range strings.Split(a.Body, "\n") {
			fmt.Fprintln(stdout, "    "+line)
		}
	}
}
