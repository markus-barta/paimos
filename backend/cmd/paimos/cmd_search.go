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

type searchResponse struct {
	Issues  []searchIssue `json:"issues"`
	HasMore bool          `json:"has_more"`
}

type searchIssue struct {
	IssueKey string `json:"issue_key"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Status   string `json:"status"`
}

func searchCmd() *cobra.Command {
	var (
		projectRef string
		issueType  string
		limit      int
	)
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Search issues by free text",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 0 {
				return &usageError{msg: "--limit must be 0 or greater"}
			}
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return &usageError{msg: "search query must not be empty"}
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}

			q := url.Values{}
			q.Set("q", query)
			if projectRef = strings.TrimSpace(projectRef); projectRef != "" {
				q.Set("project", projectRef)
			}
			if issueType = strings.TrimSpace(issueType); issueType != "" {
				q.Set("type", issueType)
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", limit))
			}

			body, err := client.do("GET", "/api/search?"+q.Encode(), nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}

			var result searchResponse
			if err := json.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("decode search results: %w", err)
			}
			renderSearchIssuesPretty(result)
			return nil
		},
	}
	c.Flags().StringVarP(&projectRef, "project", "p", "", "filter by project key or id (e.g. PAI or 6)")
	c.Flags().StringVar(&issueType, "type", "", "filter by issue type (epic, ticket, task, ...)")
	c.Flags().IntVar(&limit, "limit", 0, "page size (default: server default)")
	registerEnumCompletions(c, "type")
	return c
}

func renderSearchIssuesPretty(result searchResponse) {
	if len(result.Issues) == 0 {
		fmt.Fprintln(stdout, "(no issues)")
		return
	}
	fmt.Fprintln(stdout, "KEY           TYPE     STATUS         TITLE")
	for _, issue := range result.Issues {
		fmt.Fprintf(stdout, "%-13s %-8s %-14s %s\n",
			issue.IssueKey,
			issue.Type,
			issue.Status,
			truncateSearchTitle(issue.Title),
		)
	}
	if result.HasMore {
		fmt.Fprintln(stdout, "\n(more issues available; raise --limit or use --json for has_more)")
	}
}

func truncateSearchTitle(title string) string {
	if len(title) <= 60 {
		return title
	}
	return title[:57] + "..."
}
