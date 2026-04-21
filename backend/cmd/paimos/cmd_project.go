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
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	_, inst, err := resolveInstance(cfg)
	if err != nil {
		return nil, err
	}
	return newClient(inst), nil
}
