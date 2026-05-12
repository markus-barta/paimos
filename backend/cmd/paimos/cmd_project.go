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
	"strings"

	"github.com/spf13/cobra"
)

func projectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "project",
		Short: "Operate on projects",
	}
	c.AddCommand(projectListCmd())
	c.AddCommand(projectCreateCmd())
	return c
}

// projectCreateCmd: paimos project create --name "Foo" --key FOO [--description-file ...]
//
// PAI-379: agent-accessible project bootstrap. The server requires admin
// role on POST /api/projects and, for api-key auth, the projects:write
// scope. Members get 403; admins can either use a default `*` key or
// issue a narrowed projects:write-only key for service-account use.
func projectCreateCmd() *cobra.Command {
	var (
		name        string
		key         string
		description string
		descFile    string
		dryRun      bool
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a project",
		Long: `Creates a project on the active instance.

--name is required. --key is required server-side; if omitted here, the
server derives one from --name (sanitized + length-clamped). Pass
--description or --description-file to attach an optional description;
markdown is preferred via --description-file (or - for stdin).

Authorization: the server requires the admin role AND, for api-key
auth, the "projects:write" scope (see /api/schema → scopes block).
Members get 403 even with a narrowed key, by design (PAI-379).

Use --dry-run to print the payload without hitting the API.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return &usageError{msg: "--name is required"}
			}
			description, _, err := readMultilineInput(description, descFile, "description")
			if err != nil {
				return err
			}
			body := map[string]any{"name": name}
			if key != "" {
				body["key"] = key
			}
			if description != "" {
				body["description"] = description
			}

			if dryRun {
				return emitJSON(map[string]any{
					"dry_run": true,
					"method":  "POST",
					"path":    "/api/projects",
					"body":    body,
				})
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			raw, err := client.do("POST", "/api/projects", body)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			var p map[string]any
			_ = json.Unmarshal(raw, &p)
			fmt.Fprintf(stdout, "✓ created %v — %v\n", p["key"], p["name"])
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "human-friendly project name (required)")
	c.Flags().StringVar(&key, "key", "", "short project key (e.g. PAI); server derives one from --name if omitted")
	c.Flags().StringVar(&description, "description", "", "inline single-line description (use --description-file for markdown)")
	c.Flags().StringVar(&descFile, "description-file", "", "path to markdown description (or - for stdin)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the payload without sending")
	return c
}

func projectListCmd() *cobra.Command {
	var includeArchived bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List projects on the current instance",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			path := "/api/projects"
			if includeArchived {
				path += "?status=archived"
			}
			body, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				// Pass through as-is — the server shape is already
				// agent-friendly.
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var projects []map[string]any
			if err := json.Unmarshal(body, &projects); err != nil {
				return fmt.Errorf("decode projects: %w", err)
			}
			if len(projects) == 0 {
				fmt.Fprintln(stdout, "(no projects)")
				return nil
			}
			fmt.Fprintln(stdout, "KEY           NAME")
			for _, p := range projects {
				key, _ := p["key"].(string)
				name, _ := p["name"].(string)
				fmt.Fprintf(stdout, "%-13s %s\n", key, name)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&includeArchived, "archived", false, "include archived projects")
	return c
}

// instanceClient resolves the active instance and builds a client.
// All read commands share this preamble — keep it in one place so the
// error message stays consistent.
func instanceClient() (*Client, error) {
	_, inst, err := resolveActiveInstance()
	if err != nil {
		return nil, err
	}
	return newClient(inst), nil
}
