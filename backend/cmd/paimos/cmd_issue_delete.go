// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-481: `paimos issue delete` (+ restore / trash).
//
// Motivation: an accidental/probe/duplicate `issue create` left no CLI
// path to clean up — the ticket lived forever as a confusing entry,
// repurposed, or cancelled-with-residue. The backend already has the
// full lifecycle (soft-delete → trash → restore | purge), so this is a
// thin, guarded CLI over it:
//
//   - delete (default)  → soft-delete to trash (reversible). The safe
//     default; nothing is lost. Cascades to descendant tasks.
//   - delete --purge    → permanent removal. Guarded: refused when the
//     issue has comments or attachments — steer to soft-delete or
//     `--status cancelled` so real work keeps its audit trail.
//   - restore           → bring an issue back from the trash.
//   - trash             → list what's in the trash.
//
// All mutations are admin-gated server-side; --yes is required so the
// command is non-interactive and safe in agent/automation contexts.
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// issueDeleteCmd: paimos issue delete <ref> --yes [--purge]
func issueDeleteCmd() *cobra.Command {
	var yes, purge bool
	c := &cobra.Command{
		Use:   "delete <ref>",
		Short: "Soft-delete an issue (move to trash); --purge removes it permanently",
		Long: `Move an issue to the trash. Soft-delete is reversible — bring it back
with 'paimos issue restore <id>', or see the trash with 'paimos issue
trash'. The delete cascades to descendant tasks (via the parent edge).

For a ticket that holds real work, prefer 'paimos issue update <ref>
--status cancelled': it keeps the issue visible as a tombstone with its
history. Use delete for accidental / probe / duplicate tickets.

--purge removes the issue PERMANENTLY (it soft-deletes, then purges).
It is refused when the issue has any comments or attachments — soft-delete
or cancel those instead. Admin only.

Requires --yes (there is no interactive prompt, so the command is safe in
agent/automation contexts).

Examples:
  paimos issue delete PAI-481 --yes
  paimos issue delete 1234 --yes --purge`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !yes {
				return &usageError{msg: "refusing to delete without --yes " +
					"(this moves the issue to trash; add --purge to remove it permanently)"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			id, summary, err := deleteIssueByRef(client, args[0], purge)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				out, _ := json.Marshal(map[string]any{"ok": true, "id": id, "purged": purge})
				fmt.Fprintln(stdout, string(out))
				return nil
			}
			fmt.Fprintln(stdout, summary)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "confirm the delete (required; no interactive prompt)")
	c.Flags().BoolVar(&purge, "purge", false, "remove permanently instead of trashing (refused if the issue has comments/attachments)")
	return c
}

// deleteIssueByRef resolves the ref to a numeric id (the delete/purge
// endpoints take ids, not keys, and resolving works only while the issue
// is still live), soft-deletes it, and — when purge is set — permanently
// removes it. Returns the id and a human summary line. Testable with a
// *Client.
func deleteIssueByRef(client *Client, ref string, purge bool) (int64, string, error) {
	id, err := resolveIssueRefToID(client, ref)
	if err != nil {
		return 0, "", err
	}
	if purge {
		if n, err := issueSubCount(client, id, "comments"); err != nil {
			return id, "", err
		} else if n > 0 {
			return id, "", fmt.Errorf("refusing to purge #%d: it has %d comment(s) — "+
				"soft-delete it (omit --purge) or run `issue update %s --status cancelled`", id, n, ref)
		}
		if n, err := issueSubCount(client, id, "attachments"); err != nil {
			return id, "", err
		} else if n > 0 {
			return id, "", fmt.Errorf("refusing to purge #%d: it has %d attachment(s) — "+
				"soft-delete it (omit --purge) or cancel it instead", id, n)
		}
	}
	idPath := fmt.Sprintf("/api/issues/%d", id)
	if _, err := client.do("DELETE", idPath, nil); err != nil {
		return id, "", err
	}
	if !purge {
		return id, fmt.Sprintf("✓ %s (#%d) moved to trash — restore with `paimos issue restore %d`, "+
			"or remove permanently by re-running with --purge", ref, id, id), nil
	}
	if _, err := client.do("DELETE", idPath+"/purge", nil); err != nil {
		return id, "", fmt.Errorf("soft-deleted #%d but the purge step failed (%w) — "+
			"the issue is in the trash; restore it with `paimos issue restore %d` or retry", id, err, id)
	}
	return id, fmt.Sprintf("✓ %s (#%d) permanently deleted", ref, id), nil
}

// issueSubCount GETs an issue sub-collection (comments, attachments),
// which the API returns as a flat JSON array, and returns its length.
func issueSubCount(client *Client, id int64, sub string) (int, error) {
	body, err := client.do("GET", fmt.Sprintf("/api/issues/%d/%s", id, sub), nil)
	if err != nil {
		return 0, err
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		return 0, fmt.Errorf("decode %s: %w", sub, err)
	}
	return len(arr), nil
}

// issueRestoreCmd: paimos issue restore <id>
func issueRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore a soft-deleted issue from the trash",
		Long: `Restore an issue that was soft-deleted with 'paimos issue delete'.

Takes the numeric id printed by delete or shown in 'paimos issue trash'
(trashed issues are not addressable by key). Restore is single-issue: if
deleting an epic cascaded to its tasks, restore those tasks individually.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			body, err := client.do("POST", "/api/issues/"+url.PathEscape(args[0])+"/restore", nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			fmt.Fprintf(stdout, "✓ restored issue %s from trash\n", args[0])
			return nil
		},
	}
}

// issueTrashCmd: paimos issue trash
func issueTrashCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trash",
		Short: "List soft-deleted issues (the trash)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			body, err := client.do("GET", "/api/issues/trash", nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var issues []map[string]any
			if err := json.Unmarshal(body, &issues); err != nil {
				return fmt.Errorf("decode trash: %w", err)
			}
			renderIssueListPretty(issues, len(issues))
			return nil
		},
	}
}
