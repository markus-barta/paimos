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
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func projectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "project",
		Short: "Operate on projects",
	}
	c.AddCommand(projectListCmd())
	c.AddCommand(projectShowCmd())
	c.AddCommand(projectReposCmd())
	c.AddCommand(projectReleasesCmd())
	c.AddCommand(projectAnchorsCmd())
	c.AddCommand(projectTagsCmd())
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

func projectShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <key|id>",
		Short: "Fetch a single project by key or numeric id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, args[0])
			if err != nil {
				return reportError(err)
			}
			body, err := client.do("GET", fmt.Sprintf("/api/projects/%d", projectID), nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var project map[string]any
			if err := json.Unmarshal(body, &project); err != nil {
				return fmt.Errorf("decode project: %w", err)
			}
			renderProjectPretty(project)
			return nil
		},
	}
}

func projectReposCmd() *cobra.Command {
	return projectResourceCmd(
		"repos <key|id>",
		"List project repositories",
		"repos",
		renderProjectReposPretty,
	)
}

func projectReleasesCmd() *cobra.Command {
	return projectResourceCmd(
		"releases <key|id>",
		"List project releases",
		"releases",
		func(body []byte) error { return renderStringArrayPretty(body, "releases") },
	)
}

func projectAnchorsCmd() *cobra.Command {
	return projectResourceCmd(
		"anchors <key|id>",
		"List project anchors",
		"anchors",
		renderProjectAnchorsPretty,
	)
}

func projectTagsCmd() *cobra.Command {
	return projectResourceCmd(
		"tags <key|id>",
		"List project tags",
		"tags",
		func(body []byte) error {
			var tags []cliTag
			if err := json.Unmarshal(body, &tags); err != nil {
				return fmt.Errorf("decode tags: %w", err)
			}
			renderTagsPretty(tags)
			return nil
		},
	)
}

func projectResourceCmd(use, short, suffix string, render func([]byte) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, args[0])
			if err != nil {
				return reportError(err)
			}
			body, err := client.do("GET", fmt.Sprintf("/api/projects/%d/%s", projectID, url.PathEscape(suffix)), nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			return render(body)
		},
	}
}

func renderProjectPretty(project map[string]any) {
	key, _ := project["key"].(string)
	name, _ := project["name"].(string)
	status, _ := project["status"].(string)
	fmt.Fprintf(stdout, "%s  %s\n", key, name)
	if id, ok := project["id"].(float64); ok {
		fmt.Fprintf(stdout, "  id:     %.0f\n", id)
	}
	fmt.Fprintf(stdout, "  status: %s\n", status)
	if customer, _ := project["customer_name"].(string); customer != "" {
		fmt.Fprintf(stdout, "  customer: %s\n", customer)
	}
	if owner, ok := project["product_owner"].(float64); ok && owner > 0 {
		fmt.Fprintf(stdout, "  product_owner: %.0f\n", owner)
	}
	if counts, ok := project["counts"].(map[string]any); ok {
		openIssues, _ := counts["open_issues"].(float64)
		knowledgeEntries, _ := counts["knowledge_entries"].(float64)
		fmt.Fprintf(stdout, "  counts: %.0f open issues, %.0f knowledge entries\n", openIssues, knowledgeEntries)
	}
	if repos, ok := project["repos"].([]any); ok {
		fmt.Fprintf(stdout, "  repos:  %d\n", len(repos))
	}
}

func renderProjectReposPretty(body []byte) error {
	var repos []map[string]any
	if err := json.Unmarshal(body, &repos); err != nil {
		return fmt.Errorf("decode repos: %w", err)
	}
	if len(repos) == 0 {
		fmt.Fprintln(stdout, "(no repos)")
		return nil
	}
	fmt.Fprintln(stdout, "LABEL         BRANCH        URL")
	for _, repo := range repos {
		label, _ := repo["label"].(string)
		branch, _ := repo["default_branch"].(string)
		repoURL, _ := repo["url"].(string)
		fmt.Fprintf(stdout, "%-13s %-13s %s\n", defaultCLIString(label, "-"), defaultCLIString(branch, "-"), repoURL)
	}
	return nil
}

func renderProjectAnchorsPretty(body []byte) error {
	var anchors []map[string]any
	if err := json.Unmarshal(body, &anchors); err != nil {
		return fmt.Errorf("decode anchors: %w", err)
	}
	if len(anchors) == 0 {
		fmt.Fprintln(stdout, "(no anchors)")
		return nil
	}
	fmt.Fprintln(stdout, "ISSUE         REPO          LOCATION                       LABEL")
	for _, anchor := range anchors {
		issueKey, _ := anchor["issue_key"].(string)
		if issueKey == "" {
			if issueID, ok := anchor["issue_id"].(float64); ok {
				issueKey = "#" + strconv.FormatInt(int64(issueID), 10)
			}
		}
		repoLabel, _ := anchor["repo_label"].(string)
		filePath, _ := anchor["file_path"].(string)
		line, _ := anchor["line"].(float64)
		label, _ := anchor["label"].(string)
		location := filePath
		if line > 0 {
			location = fmt.Sprintf("%s:%.0f", filePath, line)
		}
		if len(location) > 30 {
			location = location[:29] + "…"
		}
		if len(label) > 50 {
			label = label[:49] + "…"
		}
		fmt.Fprintf(stdout, "%-13s %-13s %-30s %s\n", issueKey, defaultCLIString(repoLabel, "-"), location, label)
	}
	return nil
}

func renderStringArrayPretty(body []byte, label string) error {
	var values []string
	if err := json.Unmarshal(body, &values); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	if len(values) == 0 {
		fmt.Fprintf(stdout, "(no %s)\n", label)
		return nil
	}
	for _, value := range values {
		fmt.Fprintln(stdout, value)
	}
	return nil
}

func defaultCLIString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
