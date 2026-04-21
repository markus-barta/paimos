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
	"strings"

	"github.com/spf13/cobra"
)

func issueCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "issue",
		Short: "Operate on issues",
	}
	c.AddCommand(issueGetCmd())
	c.AddCommand(issueListCmd())
	c.AddCommand(issueChildrenCmd())
	c.AddCommand(issueCreateCmd())
	c.AddCommand(issueUpdateCmd())
	c.AddCommand(issueEnsureStatusCmd())
	c.AddCommand(issueCommentCmd())
	return c
}

// issueGetCmd: `paimos issue get PAI-83` (or numeric id).
func issueGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <ref>",
		Short: "Fetch a single issue by key or numeric id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			body, err := client.do("GET", "/api/issues/"+url.PathEscape(args[0]), nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var iss map[string]any
			if err := json.Unmarshal(body, &iss); err != nil {
				return fmt.Errorf("decode issue: %w", err)
			}
			renderIssuePretty(iss)
			return nil
		},
	}
}

// issueListCmd: `paimos issue list --project PAI --status backlog --limit 20`.
func issueListCmd() *cobra.Command {
	var (
		projectKey string
		status     string
		typ        string
		priority   string
		assignee   string
		limit      int
		offset     int
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "List issues with filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			q := url.Values{}
			if projectKey != "" {
				// The cross-project /issues endpoint takes project_ids,
				// not keys — resolve the key to id first via /projects.
				pid, err := resolveProjectKeyToID(client, projectKey)
				if err != nil {
					return reportError(err)
				}
				q.Set("project_ids", fmt.Sprintf("%d", pid))
			}
			if status != "" {
				q.Set("status", status)
			}
			if typ != "" {
				q.Set("type", typ)
			}
			if priority != "" {
				q.Set("priority", priority)
			}
			if assignee != "" {
				q.Set("assignee_id", assignee)
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", limit))
			}
			if offset > 0 {
				q.Set("offset", fmt.Sprintf("%d", offset))
			}
			path := "/api/issues"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			body, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var wrap struct {
				Issues []map[string]any `json:"issues"`
				Total  int              `json:"total"`
			}
			if err := json.Unmarshal(body, &wrap); err != nil {
				return fmt.Errorf("decode list: %w", err)
			}
			renderIssueListPretty(wrap.Issues, wrap.Total)
			return nil
		},
	}
	c.Flags().StringVarP(&projectKey, "project", "p", "", "filter by project key (e.g. PAI)")
	c.Flags().StringVar(&status, "status", "", "filter by status")
	c.Flags().StringVar(&typ, "type", "", "filter by type (epic, ticket, task, …)")
	c.Flags().StringVar(&priority, "priority", "", "filter by priority")
	c.Flags().StringVar(&assignee, "assignee", "", "filter by assignee id")
	c.Flags().IntVar(&limit, "limit", 50, "page size (default 50, server cap 100)")
	c.Flags().IntVar(&offset, "offset", 0, "pagination offset")
	return c
}

// issueChildrenCmd: `paimos issue children PAI-29`.
func issueChildrenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "children <ref>",
		Short: "List direct children of an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			body, err := client.do("GET", "/api/issues/"+url.PathEscape(args[0])+"/children", nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var issues []map[string]any
			if err := json.Unmarshal(body, &issues); err != nil {
				return fmt.Errorf("decode children: %w", err)
			}
			renderIssueListPretty(issues, len(issues))
			return nil
		},
	}
}

// resolveProjectKeyToID trades a project key for a numeric id by
// listing projects. Cached within a single CLI invocation would be
// nicer but isn't worth the complexity for v1 — one extra GET.
func resolveProjectKeyToID(c *Client, key string) (int64, error) {
	body, err := c.do("GET", "/api/projects", nil)
	if err != nil {
		return 0, err
	}
	var list []struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return 0, fmt.Errorf("decode projects: %w", err)
	}
	for _, p := range list {
		if p.Key == key {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("project key %q not found (are you on the right --instance?)", key)
}

// renderIssuePretty prints a single issue in human-readable form.
// Mirrors what you'd see in the UI header.
func renderIssuePretty(iss map[string]any) {
	key, _ := iss["issue_key"].(string)
	title, _ := iss["title"].(string)
	status, _ := iss["status"].(string)
	priority, _ := iss["priority"].(string)
	typ, _ := iss["type"].(string)
	fmt.Fprintf(stdout, "%s  %s\n", key, title)
	fmt.Fprintf(stdout, "  type:     %s\n", typ)
	fmt.Fprintf(stdout, "  status:   %s\n", status)
	fmt.Fprintf(stdout, "  priority: %s\n", priority)
	if desc, _ := iss["description"].(string); desc != "" {
		// First 160 chars — the user can go fetch the full thing with --json.
		if len(desc) > 160 {
			desc = desc[:160] + "…"
		}
		fmt.Fprintf(stdout, "\n  %s\n", strings.ReplaceAll(desc, "\n", "\n  "))
	}
}

// renderIssueListPretty prints a compact table.
func renderIssueListPretty(issues []map[string]any, total int) {
	if len(issues) == 0 {
		fmt.Fprintln(stdout, "(no issues)")
		return
	}
	fmt.Fprintln(stdout, "KEY           STATUS         PRIO   TITLE")
	for _, i := range issues {
		key, _ := i["issue_key"].(string)
		status, _ := i["status"].(string)
		priority, _ := i["priority"].(string)
		title, _ := i["title"].(string)
		if len(title) > 60 {
			title = title[:60] + "…"
		}
		fmt.Fprintf(stdout, "%-13s %-14s %-6s %s\n", key, status, priority, title)
	}
	if total > len(issues) {
		fmt.Fprintf(stdout, "\n(showing %d of %d — use --limit / --offset for more)\n", len(issues), total)
	}
}
