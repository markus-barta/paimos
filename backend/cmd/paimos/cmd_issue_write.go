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
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// readMultilineInput returns the value for a markdown-ish field that
// accepts either --foo (inline, single-line) or --foo-file <path>
// (multi-line from disk) or "-" to read stdin. Enforces "only one source"
// so agents can't silently overwrite one with the other.
//
// inlineSet/fileSet indicate whether each flag was set (not just
// non-empty default), so "" is a valid explicit value.
func readMultilineInput(inlineFlag, fileFlag, fieldName string) (string, bool, error) {
	hasInline := inlineFlag != ""
	hasFile := fileFlag != ""
	if hasInline && hasFile {
		return "", false, &usageError{
			msg: fmt.Sprintf("--%s and --%s-file are mutually exclusive", fieldName, fieldName),
		}
	}
	if hasFile {
		if fileFlag == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return "", false, fmt.Errorf("read --%s-file from stdin: %w", fieldName, err)
			}
			return string(b), true, nil
		}
		b, err := os.ReadFile(fileFlag)
		if err != nil {
			return "", false, fmt.Errorf("read --%s-file %s: %w", fieldName, fileFlag, err)
		}
		return string(b), true, nil
	}
	if hasInline {
		return inlineFlag, true, nil
	}
	return "", false, nil
}

// terminalStatuses mirrors the server's understanding of "issue is
// closing". Used by --close-note to decide whether to attach the note.
var terminalStatuses = map[string]bool{
	"done":      true,
	"delivered": true,
	"accepted":  true,
	"invoiced":  true,
	"cancelled": true,
}

// issueCreateCmd: paimos issue create --project PAI --type ticket --title "..." [flags]
func issueCreateCmd() *cobra.Command {
	var (
		projectKey    string
		title         string
		typ           string
		status        string
		priority      string
		parent        string
		assignee      string
		costUnit      string
		release       string
		desc          string
		descFile      string
		ac            string
		acFile        string
		notes         string
		notesFile     string
		dryRun        bool
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create an issue",
		Long: `Creates an issue on the active instance.

Multi-line fields accept either --foo "inline" or --foo-file path (or -
for stdin). Inline+file together is an error — agents frequently hit
this when concatenating arguments.

Use --dry-run to print the request payload without hitting the API.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectKey == "" {
				return &usageError{msg: "--project is required"}
			}
			if title == "" {
				return &usageError{msg: "--title is required"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			description, _, err := readMultilineInput(desc, descFile, "description")
			if err != nil {
				return err
			}
			acceptance, _, err := readMultilineInput(ac, acFile, "ac")
			if err != nil {
				return err
			}
			notesVal, _, err := readMultilineInput(notes, notesFile, "notes")
			if err != nil {
				return err
			}

			body := map[string]any{"title": title}
			if typ != "" {
				body["type"] = typ
			}
			if status != "" {
				body["status"] = status
			}
			if priority != "" {
				body["priority"] = priority
			}
			if description != "" {
				body["description"] = description
			}
			if acceptance != "" {
				body["acceptance_criteria"] = acceptance
			}
			if notesVal != "" {
				body["notes"] = notesVal
			}
			if costUnit != "" {
				body["cost_unit"] = costUnit
			}
			if release != "" {
				body["release"] = release
			}
			if parent != "" {
				pid, err := resolveIssueRefToID(client, parent)
				if err != nil {
					return reportError(err)
				}
				body["parent_id"] = pid
			}
			if assignee != "" {
				body["assignee_id"] = assignee
			}

			if dryRun {
				return emitJSON(map[string]any{
					"dry_run": true,
					"method":  "POST",
					"path":    "/api/projects/" + projectKey + "/issues",
					"body":    body,
				})
			}

			// Resolve project key → id (CreateIssue takes :id).
			pid, err := resolveProjectKeyToID(client, projectKey)
			if err != nil {
				return reportError(err)
			}
			path := fmt.Sprintf("/api/projects/%d/issues", pid)
			raw, err := client.do("POST", path, body)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			var iss map[string]any
			_ = json.Unmarshal(raw, &iss)
			fmt.Fprintf(stdout, "✓ created %v — %v\n", iss["issue_key"], iss["title"])
			return nil
		},
	}
	c.Flags().StringVarP(&projectKey, "project", "p", "", "project key (required)")
	c.Flags().StringVar(&title, "title", "", "single-line title (required)")
	c.Flags().StringVar(&typ, "type", "", "epic|cost_unit|release|sprint|ticket|task (default ticket)")
	c.Flags().StringVar(&status, "status", "", "initial status (default new)")
	c.Flags().StringVar(&priority, "priority", "", "low|medium|high")
	c.Flags().StringVar(&parent, "parent", "", "parent issue ref (key or id)")
	c.Flags().StringVar(&assignee, "assignee", "", "assignee user id")
	c.Flags().StringVar(&costUnit, "cost-unit", "", "cost unit name")
	c.Flags().StringVar(&release, "release", "", "release name")
	c.Flags().StringVar(&desc, "description", "", "inline description (single-line only; use --description-file for markdown)")
	c.Flags().StringVar(&descFile, "description-file", "", "path to markdown description (or - for stdin)")
	c.Flags().StringVar(&ac, "ac", "", "inline acceptance criteria")
	c.Flags().StringVar(&acFile, "ac-file", "", "path to markdown acceptance-criteria file")
	c.Flags().StringVar(&notes, "notes", "", "inline notes")
	c.Flags().StringVar(&notesFile, "notes-file", "", "path to markdown notes file")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the payload without sending")
	registerEnumCompletions(c, "status", "type", "priority")
	return c
}

// issueUpdateCmd: paimos issue update <ref> --status done --close-note ...
func issueUpdateCmd() *cobra.Command {
	var (
		title         string
		typ           string
		status        string
		priority      string
		parent        string
		assignee      string
		costUnit      string
		release       string
		desc          string
		descFile      string
		ac            string
		acFile        string
		notes         string
		notesFile     string
		closeNote     string
		closeNoteFile string
		dryRun        bool
	)
	c := &cobra.Command{
		Use:   "update <ref>",
		Short: "Partial-update an issue",
		Long: `Updates an issue by key or numeric id. Only the flags you pass
are written; everything else is left alone.

When --status moves to a terminal state (done / delivered / accepted /
invoiced / cancelled), passing --close-note or --close-note-file also
appends a formatted comment so the "why" is captured in the history
alongside the status change. The two actions aren't atomic at the API
level, but the failure window is tiny and errors are reported clearly.

Use --dry-run to print the payload without sending.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			description, descSet, err := readMultilineInput(desc, descFile, "description")
			if err != nil {
				return err
			}
			acceptance, acSet, err := readMultilineInput(ac, acFile, "ac")
			if err != nil {
				return err
			}
			notesVal, notesSet, err := readMultilineInput(notes, notesFile, "notes")
			if err != nil {
				return err
			}
			closeNoteVal, _, err := readMultilineInput(closeNote, closeNoteFile, "close-note")
			if err != nil {
				return err
			}
			if closeNoteVal != "" && status == "" {
				return &usageError{msg: "--close-note requires --status (can't leave a close note without closing)"}
			}
			if closeNoteVal != "" && !terminalStatuses[status] {
				return &usageError{
					msg: fmt.Sprintf("--close-note only applies when moving to a terminal status (%s); got --status=%s",
						"done/delivered/accepted/invoiced/cancelled", status),
				}
			}

			body := map[string]any{}
			if title != "" {
				body["title"] = title
			}
			if typ != "" {
				body["type"] = typ
			}
			if status != "" {
				body["status"] = status
			}
			if priority != "" {
				body["priority"] = priority
			}
			if descSet {
				body["description"] = description
			}
			if acSet {
				body["acceptance_criteria"] = acceptance
			}
			if notesSet {
				body["notes"] = notesVal
			}
			if costUnit != "" {
				body["cost_unit"] = costUnit
			}
			if release != "" {
				body["release"] = release
			}
			if parent != "" {
				pid, err := resolveIssueRefToID(client, parent)
				if err != nil {
					return reportError(err)
				}
				body["parent_id"] = pid
			}
			if assignee != "" {
				body["assignee_id"] = assignee
			}
			if len(body) == 0 && closeNoteVal == "" {
				return &usageError{msg: "nothing to update — pass at least one field"}
			}

			ref := args[0]
			if dryRun {
				out := map[string]any{
					"dry_run": true,
					"method":  "PUT",
					"path":    "/api/issues/" + ref,
					"body":    body,
				}
				if closeNoteVal != "" {
					out["close_note_will_comment"] = closeNoteVal
				}
				return emitJSON(out)
			}

			// Execute update.
			if len(body) > 0 {
				raw, err := client.do("PUT", "/api/issues/"+url.PathEscape(ref), body)
				if err != nil {
					return reportError(err)
				}
				if !flagJSON && closeNoteVal == "" {
					var iss map[string]any
					_ = json.Unmarshal(raw, &iss)
					fmt.Fprintf(stdout, "✓ updated %v\n", iss["issue_key"])
				}
			}

			// Attach close-note as a comment. Use a prefix so the UI can
			// style/filter it without a dedicated schema change for v1.
			if closeNoteVal != "" {
				commentBody := fmt.Sprintf("**Close note** (status → %s):\n\n%s", status, closeNoteVal)
				if _, err := client.do("POST", "/api/issues/"+url.PathEscape(ref)+"/comments",
					map[string]any{"body": commentBody}); err != nil {
					return reportError(fmt.Errorf("status updated, but close-note comment failed: %w", err))
				}
				if !flagJSON {
					fmt.Fprintf(stdout, "✓ updated %s with close note\n", ref)
				}
			}

			if flagJSON {
				return emitJSON(map[string]any{"ok": true, "ref": ref})
			}
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "new title")
	c.Flags().StringVar(&typ, "type", "", "new type")
	c.Flags().StringVar(&status, "status", "", "new status")
	c.Flags().StringVar(&priority, "priority", "", "new priority")
	c.Flags().StringVar(&parent, "parent", "", "new parent (ref or id, or 'null' to detach)")
	c.Flags().StringVar(&assignee, "assignee", "", "new assignee user id")
	c.Flags().StringVar(&costUnit, "cost-unit", "", "new cost unit")
	c.Flags().StringVar(&release, "release", "", "new release")
	c.Flags().StringVar(&desc, "description", "", "inline description")
	c.Flags().StringVar(&descFile, "description-file", "", "path to new description (or -)")
	c.Flags().StringVar(&ac, "ac", "", "inline acceptance criteria")
	c.Flags().StringVar(&acFile, "ac-file", "", "path to new acceptance criteria (or -)")
	c.Flags().StringVar(&notes, "notes", "", "inline notes")
	c.Flags().StringVar(&notesFile, "notes-file", "", "path to new notes (or -)")
	c.Flags().StringVar(&closeNote, "close-note", "", "single-line close-note (requires --status terminal)")
	c.Flags().StringVar(&closeNoteFile, "close-note-file", "", "path to close-note file (requires --status terminal)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the payload without sending")
	registerEnumCompletions(c, "status", "type", "priority")
	return c
}

// issueEnsureStatusCmd: idempotent status transition. Great for CI scripts.
func issueEnsureStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ensure-status <ref> <status>",
		Short: "Set an issue's status only if it's not already there (idempotent)",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// First arg (ref) — can't complete cheaply without a round
			// trip; leave to the shell. Second arg (status) comes from
			// the schema cache.
			if len(args) == 1 {
				return enumFromCachedSchema("status"), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, want := args[0], args[1]
			client, err := instanceClient()
			if err != nil {
				return err
			}
			// Get current state.
			raw, err := client.do("GET", "/api/issues/"+url.PathEscape(ref), nil)
			if err != nil {
				return reportError(err)
			}
			var iss map[string]any
			_ = json.Unmarshal(raw, &iss)
			cur, _ := iss["status"].(string)
			if cur == want {
				if flagJSON {
					return emitJSON(map[string]any{"ok": true, "ref": ref, "status": cur, "changed": false})
				}
				fmt.Fprintf(stdout, "✓ %s already %s\n", ref, want)
				return nil
			}
			if _, err := client.do("PUT", "/api/issues/"+url.PathEscape(ref),
				map[string]any{"status": want}); err != nil {
				return reportError(err)
			}
			if flagJSON {
				return emitJSON(map[string]any{"ok": true, "ref": ref, "status": want, "changed": true, "previous": cur})
			}
			fmt.Fprintf(stdout, "✓ %s: %s → %s\n", ref, cur, want)
			return nil
		},
	}
}

// issueCommentCmd: paimos issue comment <ref> --body-file note.md
func issueCommentCmd() *cobra.Command {
	var body, bodyFile string
	c := &cobra.Command{
		Use:   "comment <ref>",
		Short: "Add a comment to an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, set, err := readMultilineInput(body, bodyFile, "body")
			if err != nil {
				return err
			}
			if !set || strings.TrimSpace(text) == "" {
				return &usageError{msg: "--body or --body-file required (non-empty)"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			raw, err := client.do("POST", "/api/issues/"+url.PathEscape(args[0])+"/comments",
				map[string]any{"body": text})
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			fmt.Fprintf(stdout, "✓ commented on %s\n", args[0])
			return nil
		},
	}
	c.Flags().StringVar(&body, "body", "", "inline single-line comment")
	c.Flags().StringVar(&bodyFile, "body-file", "", "path to markdown comment (or -)")
	return c
}

// relationCmd: paimos relation add <source> <type> <target>
func relationCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "relation",
		Short: "Operate on issue relations",
	}
	c.AddCommand(relationAddCmd())
	return c
}

func relationAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <source-ref> <type> <target-ref>",
		Short: "Add a relation between two issues",
		Args:  cobra.ExactArgs(3),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// source-ref, type, target-ref. Only the middle one (type)
			// is cheaply completable from the schema cache.
			if len(args) == 1 {
				return enumFromCachedSchema("relation"), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			src, typ, tgt := args[0], args[1], args[2]
			client, err := instanceClient()
			if err != nil {
				return err
			}
			targetID, err := resolveIssueRefToID(client, tgt)
			if err != nil {
				return reportError(err)
			}
			raw, err := client.do("POST",
				"/api/issues/"+url.PathEscape(src)+"/relations",
				map[string]any{"target_id": targetID, "type": typ},
			)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			fmt.Fprintf(stdout, "✓ %s %s %s\n", src, typ, tgt)
			return nil
		},
	}
}

// resolveIssueRefToID converts a ref (key or numeric) to a numeric ID
// by GETting the issue once. The server's /issues/{id} endpoint accepts
// both forms since PAI-86; this lets us always pass numeric IDs on
// endpoints that don't (e.g. the relation target_id field is int64 in
// the API body). Used by the CLI's create + relation-add flows.
func resolveIssueRefToID(client *Client, ref string) (int64, error) {
	body, err := client.do("GET", "/api/issues/"+url.PathEscape(ref), nil)
	if err != nil {
		return 0, err
	}
	var iss struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &iss); err != nil {
		return 0, fmt.Errorf("decode issue: %w", err)
	}
	return iss.ID, nil
}
