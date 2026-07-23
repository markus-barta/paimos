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

// moveOne is one issue's move outcome, shaped to match the server's moveResult.
type moveOne struct {
	IssueID   int64    `json:"issue_id"`
	OldKey    string   `json:"old_key"`
	NewKey    string   `json:"new_key"`
	ProjectID int64    `json:"project_id"`
	Detached  []string `json:"detached"`
	Notes     []string `json:"notes"`
}

// runIssueMove re-homes one or more issues to targetRef's project. A single ref
// uses POST /api/issues/{id}/move; several refs use the bulk endpoint so the
// whole reorg reports in one call and a per-issue failure never blocks the
// rest. Shared by `issue move` and `issue update --project`.
func runIssueMove(client *Client, refs []string, targetRef string, dryRun bool) error {
	if len(refs) == 0 {
		return &usageError{msg: "no issue refs given"}
	}
	targetID, err := resolveProjectRefToID(client, targetRef)
	if err != nil {
		return reportError(err)
	}

	// Resolve every issue ref up front so a typo fails before any write.
	ids := make([]int64, 0, len(refs))
	for _, ref := range refs {
		id, err := resolveIssueRefToID(client, ref)
		if err != nil {
			return reportError(fmt.Errorf("resolve %s: %w", ref, err))
		}
		ids = append(ids, id)
	}

	if len(ids) == 1 {
		path := fmt.Sprintf("/api/issues/%d/move", ids[0])
		body := map[string]any{"project_id": targetID}
		if dryRun {
			return emitJSON(map[string]any{"dry_run": true, "method": "POST", "path": path, "body": body})
		}
		raw, err := client.do("POST", path, body)
		if err != nil {
			return reportError(err)
		}
		if flagJSON {
			fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
			return nil
		}
		var res moveOne
		_ = json.Unmarshal(raw, &res)
		printMoveOne(res)
		return nil
	}

	body := map[string]any{"issue_ids": ids, "project_id": targetID}
	if dryRun {
		return emitJSON(map[string]any{"dry_run": true, "method": "POST", "path": "/api/issues/move", "body": body})
	}
	raw, err := client.do("POST", "/api/issues/move", body)
	if err != nil {
		return reportError(err)
	}
	if flagJSON {
		fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
		return nil
	}
	var bulk struct {
		Moved   int `json:"moved"`
		Failed  int `json:"failed"`
		Results []struct {
			IssueID int64    `json:"issue_id"`
			OK      bool     `json:"ok"`
			Error   string   `json:"error"`
			Result  *moveOne `json:"result"`
		} `json:"results"`
	}
	_ = json.Unmarshal(raw, &bulk)
	for _, r := range bulk.Results {
		if r.OK && r.Result != nil {
			printMoveOne(*r.Result)
			continue
		}
		fmt.Fprintf(stdout, "✗ issue %d — %s\n", r.IssueID, r.Error)
	}
	fmt.Fprintf(stdout, "moved %d, failed %d\n", bulk.Moved, bulk.Failed)
	return nil
}

// printMoveOne renders one successful move in human-readable form.
func printMoveOne(res moveOne) {
	fmt.Fprintf(stdout, "✓ moved %s → %s\n", res.OldKey, res.NewKey)
	for _, d := range res.Detached {
		fmt.Fprintf(stdout, "  detached: %s\n", d)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(stdout, "  note: %s\n", n)
	}
}

// issueMoveCmd: `paimos issue move <ref>... --to <key|id>`.
func issueMoveCmd() *cobra.Command {
	var to string
	var dryRun bool
	c := &cobra.Command{
		Use:   "move <ref>... --to <key|id>",
		Short: "Reassign one or more issues to another project",
		Long: `Moves each issue to the --to project, preserving comments, time
entries, history, tags, and cross-project dependencies. The issue is re-keyed
to the target project's prefix + next number (e.g. PAI-690 → OPS-12); its
former key is aliased so existing references still resolve.

Project-scoped structural links that would become cross-project — parent,
sprint, cost unit, release, group — are detached and reported. Re-link them in
the target project afterward.

Pass several refs to move them in one campaign; each is reported independently
and a per-issue failure never blocks the rest. Use --dry-run to preview.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(to) == "" {
				return &usageError{msg: "--to <project key|id> is required"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			return runIssueMove(client, args, to, dryRun)
		},
	}
	c.Flags().StringVar(&to, "to", "", "target project key or id (required)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the payload without sending")
	return c
}
