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

// PAI-394 — `paimos knowledge` command family. Drives the unified
// /api/projects/{id}/knowledge surface from one verb tree:
//
//	paimos knowledge list   [--type T] [--project P]
//	paimos knowledge get    <type> <slug> [--project P]
//	paimos knowledge create --type T --slug S --title "..." [--body-file F]
//	paimos knowledge update <type> <slug> [--title ...] [--body-file F]
//	paimos knowledge delete <type> <slug> [--yes]
//	paimos knowledge promote <slug> --to <project|user|instance>
//	paimos knowledge memory bump-refs <slug>...
//	paimos knowledge memory stale [--days N]
//	paimos knowledge memory proposed-stale [--days N]
//
// `<type>` is the kebab-singular URL segment: memory, runbook,
// guideline, external-system, related-project. The server accepts
// both the URL form and the SQL discriminator (snake_case) in
// request bodies, so the CLI uses kebab everywhere for consistency.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// knowledgeTypeSegments is the canonical list of URL segments the
// CLI accepts on `--type` flags and positional arguments. Kept here
// (not fetched from /api/schema) so the CLI works without a server
// round-trip for tab-completion and validation. The set is small
// and changes rarely — when it does, the schema test
// (`TestSchemaKnowledgeBlockMatchesRegistry`) catches the drift.
var knowledgeTypeSegments = []string{
	"memory", "runbook", "guideline", "external-system", "related-project",
}

// knowledgeEntryShape mirrors knowledge.Output on the wire — same
// fields, same JSON tags, scoped to the CLI binary so the sub-
// package stays decoupled.
type knowledgeEntryShape struct {
	ID               int64                  `json:"id"`
	ProjectID        int64                  `json:"project_id"`
	Type             string                 `json:"type"`
	Slug             string                 `json:"slug"`
	Title            string                 `json:"title"`
	Body             string                 `json:"body"`
	Status           string                 `json:"status"`
	Metadata         map[string]interface{} `json:"metadata"`
	CreatedAt        string                 `json:"created_at"`
	UpdatedAt        string                 `json:"updated_at"`
	ReferenceCount   int64                  `json:"reference_count"`
	LastReferencedAt string                 `json:"last_referenced_at,omitempty"`
}

func knowledgeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "knowledge",
		Short: "List and manage project knowledge entries",
		Long: `Drive the unified /api/projects/{id}/knowledge surface
(PAI-394). One verb tree covers all five knowledge types: memory,
runbook, guideline, external-system, related-project. Type is a
URL segment (kebab-singular) on every subcommand.

Memory-specific operations live under "knowledge memory" — they
operate on the same data but target the named subroutes on the
server (references / stale / proposed-stale).`,
	}
	c.AddCommand(knowledgeListCmd())
	c.AddCommand(knowledgeGetCmd())
	c.AddCommand(knowledgeCreateCmd())
	c.AddCommand(knowledgeUpdateCmd())
	c.AddCommand(knowledgeDeleteCmd())
	c.AddCommand(knowledgePromoteCmd())
	c.AddCommand(knowledgeMemoryCmd())
	return c
}

// validateKnowledgeType ensures the provided segment is in the
// canonical CLI list. Returns a usage-style error so the failure
// path looks like the rest of the CLI's "bad flag value" errors.
func validateKnowledgeType(seg string) error {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return &usageError{msg: "--type is required (one of: " + strings.Join(knowledgeTypeSegments, ", ") + ")"}
	}
	for _, valid := range knowledgeTypeSegments {
		if seg == valid {
			return nil
		}
	}
	return &usageError{msg: fmt.Sprintf("--type %q (expected one of: %s)", seg, strings.Join(knowledgeTypeSegments, ", "))}
}

func knowledgeListCmd() *cobra.Command {
	var (
		projectRef string
		typeSeg    string
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "List knowledge entries for a project",
		Long: `Lists every non-trashed knowledge entry the project owns,
ordered by (type, slug). With --type, narrows to one kind. Without,
returns the cross-type view — useful for an at-a-glance "what does
this project know" enumeration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			typeSeg = strings.TrimSpace(typeSeg)
			if typeSeg != "" {
				if err := validateKnowledgeType(typeSeg); err != nil {
					return err
				}
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}

			path := fmt.Sprintf("/api/projects/%d/knowledge", projectID)
			if typeSeg != "" {
				path = path + "?type=" + typeSeg
			}
			body, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var entries []knowledgeEntryShape
			if err := json.Unmarshal(body, &entries); err != nil {
				return fmt.Errorf("decode knowledge entries: %w", err)
			}
			renderKnowledgeListPretty(entries)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&typeSeg, "type", "", "narrow to one type: "+strings.Join(knowledgeTypeSegments, ", "))
	return c
}

func knowledgeGetCmd() *cobra.Command {
	var projectRef string
	c := &cobra.Command{
		Use:   "get <type> <slug>",
		Short: "Fetch a single knowledge entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeSeg, slug := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
			if err := validateKnowledgeType(typeSeg); err != nil {
				return err
			}
			if slug == "" {
				return &usageError{msg: "<slug> is required"}
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
			path := fmt.Sprintf("/api/projects/%d/knowledge/%s/%s", projectID, typeSeg, slug)
			body, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var entry knowledgeEntryShape
			if err := json.Unmarshal(body, &entry); err != nil {
				return fmt.Errorf("decode knowledge entry: %w", err)
			}
			renderKnowledgeEntryPretty(entry)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	return c
}

func knowledgeCreateCmd() *cobra.Command {
	var (
		projectRef string
		typeSeg    string
		slug       string
		title      string
		body       string
		bodyFile   string
		status     string
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a knowledge entry",
		Long: `Creates a knowledge entry in the project. The discriminator
travels as the ?type=<seg> query parameter; the body holds slug,
title, body, optional status, and per-type metadata. Use
--body-file for non-trivial markdown so shell quoting doesn't
distort the content.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateKnowledgeType(typeSeg); err != nil {
				return err
			}
			slug = strings.TrimSpace(slug)
			if slug == "" {
				return &usageError{msg: "--slug is required"}
			}
			title = strings.TrimSpace(title)
			if title == "" {
				return &usageError{msg: "--title is required"}
			}
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			bodyContent, _, err := readMultilineInput(body, bodyFile, "body")
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

			payload := map[string]any{
				"slug":  slug,
				"title": title,
				"body":  bodyContent,
			}
			if status = strings.TrimSpace(status); status != "" {
				payload["status"] = status
			}
			path := fmt.Sprintf("/api/projects/%d/knowledge?type=%s", projectID, typeSeg)
			raw, err := client.do("POST", path, payload)
			if err != nil {
				return reportError(err)
			}
			var entry knowledgeEntryShape
			if err := json.Unmarshal(raw, &entry); err != nil {
				return fmt.Errorf("decode knowledge entry: %w", err)
			}
			if flagJSON {
				return emitJSON(entry)
			}
			fmt.Fprintf(stdout, "✓ created %s/%s (#%d)\n", entry.Type, entry.Slug, entry.ID)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&typeSeg, "type", "", "knowledge type (required): "+strings.Join(knowledgeTypeSegments, ", "))
	c.Flags().StringVar(&slug, "slug", "", "URL-addressable slug (required)")
	c.Flags().StringVar(&title, "title", "", "human title (required)")
	c.Flags().StringVar(&body, "body", "", "inline markdown body")
	c.Flags().StringVar(&bodyFile, "body-file", "", "path to markdown body (or - for stdin)")
	c.Flags().StringVar(&status, "status", "", "initial status (defaults to the type's DefaultStatus)")
	return c
}

func knowledgeUpdateCmd() *cobra.Command {
	var (
		projectRef string
		title      string
		body       string
		bodyFile   string
		status     string
		newSlug    string
	)
	c := &cobra.Command{
		Use:   "update <type> <slug>",
		Short: "Update a knowledge entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeSeg, slug := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
			if err := validateKnowledgeType(typeSeg); err != nil {
				return err
			}
			if slug == "" {
				return &usageError{msg: "<slug> is required"}
			}
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}

			bodyContent, bodyChanged, err := readMultilineInput(body, bodyFile, "body")
			if err != nil {
				return err
			}
			titleChanged := cmd.Flags().Changed("title")
			statusChanged := cmd.Flags().Changed("status")
			slugChanged := cmd.Flags().Changed("slug")
			if !titleChanged && !bodyChanged && !statusChanged && !slugChanged {
				return &usageError{msg: "at least one of --title, --body, --body-file, --status, --slug is required"}
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}

			// PUT is full-replace: the server overwrites title + body with
			// whatever we send. Carry over any field the user did NOT change
			// (fetch the existing entry once if title or body needs it) so a
			// partial update never silently wipes the body — and, post
			// PAI-351, never trips the content-revised signal on an unchanged
			// body (which would falsely flag every dependent for re-review).
			payload := map[string]any{}
			if !titleChanged || !bodyChanged {
				existing, err := client.do("GET",
					fmt.Sprintf("/api/projects/%d/knowledge/%s/%s", projectID, typeSeg, slug), nil)
				if err != nil {
					return reportError(err)
				}
				var prev knowledgeEntryShape
				if err := json.Unmarshal(existing, &prev); err != nil {
					return fmt.Errorf("decode existing entry: %w", err)
				}
				if !titleChanged {
					title = prev.Title
				}
				if !bodyChanged {
					bodyContent = prev.Body
				}
			}
			payload["title"] = strings.TrimSpace(title)
			payload["body"] = bodyContent
			if statusChanged {
				payload["status"] = strings.TrimSpace(status)
			}
			if slugChanged {
				payload["slug"] = strings.TrimSpace(newSlug)
			}

			path := fmt.Sprintf("/api/projects/%d/knowledge/%s/%s", projectID, typeSeg, slug)
			raw, err := client.do("PUT", path, payload)
			if err != nil {
				return reportError(err)
			}
			var entry knowledgeEntryShape
			if err := json.Unmarshal(raw, &entry); err != nil {
				return fmt.Errorf("decode knowledge entry: %w", err)
			}
			if flagJSON {
				return emitJSON(entry)
			}
			fmt.Fprintf(stdout, "✓ updated %s/%s (#%d)\n", entry.Type, entry.Slug, entry.ID)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&title, "title", "", "new title")
	c.Flags().StringVar(&body, "body", "", "inline markdown body (replaces current)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "path to markdown body (or - for stdin)")
	c.Flags().StringVar(&status, "status", "", "new status")
	c.Flags().StringVar(&newSlug, "slug", "", "rename the entry to this slug")
	return c
}

func knowledgeDeleteCmd() *cobra.Command {
	var (
		projectRef string
		yes        bool
	)
	c := &cobra.Command{
		Use:   "delete <type> <slug>",
		Short: "Soft-delete a knowledge entry",
		Long: `Moves a knowledge entry to the Trash (same flow as
/api/issues/{id} DELETE). --yes skips the interactive confirm; the
default refuses to proceed without a TTY-attached prompt so a
stray script can't lose data.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			typeSeg, slug := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
			if err := validateKnowledgeType(typeSeg); err != nil {
				return err
			}
			if slug == "" {
				return &usageError{msg: "<slug> is required"}
			}
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			if err := confirmKnowledgeDelete(typeSeg, slug, yes); err != nil {
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
			path := fmt.Sprintf("/api/projects/%d/knowledge/%s/%s", projectID, typeSeg, slug)
			if _, err := client.do("DELETE", path, nil); err != nil {
				return reportError(err)
			}
			if flagJSON {
				return emitJSON(map[string]any{
					"ok":     true,
					"type":   typeSeg,
					"slug":   slug,
					"action": "delete",
				})
			}
			fmt.Fprintf(stdout, "✓ deleted %s/%s\n", typeSeg, slug)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().BoolVar(&yes, "yes", false, "skip the interactive confirm")
	return c
}

func knowledgePromoteCmd() *cobra.Command {
	var (
		toScope    string
		fromScope  string
		projectRef string
	)
	c := &cobra.Command{
		Use:   "promote <slug>",
		Short: "Promote a memory entry across scopes",
		Long: `Promotes a memory entry between user / project / instance
scopes. Wraps the existing POST /api/memory/{slug}/promote endpoint
(PAI-345) — currently only memory entries are scope-promotable.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := strings.TrimSpace(args[0])
			if slug == "" {
				return &usageError{msg: "<slug> is required"}
			}
			toScope = strings.TrimSpace(toScope)
			if toScope == "" {
				return &usageError{msg: "--to is required (project|user|instance)"}
			}
			payload := map[string]any{"to": toScope}
			if from := strings.TrimSpace(fromScope); from != "" {
				payload["from"] = from
			}
			if pr := strings.TrimSpace(projectRef); pr != "" {
				client, err := instanceClient()
				if err != nil {
					return err
				}
				projectID, err := resolveProjectRefToID(client, pr)
				if err != nil {
					return reportError(err)
				}
				payload["project_id"] = projectID
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			raw, err := client.do("POST", fmt.Sprintf("/api/memory/%s/promote", slug), payload)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			fmt.Fprintf(stdout, "✓ promoted memory %q to %s scope\n", slug, toScope)
			return nil
		},
	}
	c.Flags().StringVar(&toScope, "to", "", "destination scope: project|user|instance (required)")
	c.Flags().StringVar(&fromScope, "from", "", "source scope (optional; the server infers when omitted)")
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id when scope == project")
	return c
}

// ── knowledge memory subcommands ────────────────────────────────

func knowledgeMemoryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "memory",
		Short: "Memory-specific operations (references, stale review)",
		Long: `Memory carries decay tracking (PAI-347) and a draft-review
surface (PAI-349) that other knowledge types don't. These verbs hit
the named subroutes under /knowledge/memory/...`,
	}
	c.AddCommand(knowledgeMemoryBumpRefsCmd())
	c.AddCommand(knowledgeMemoryStaleCmd())
	c.AddCommand(knowledgeMemoryProposedStaleCmd())
	return c
}

func knowledgeMemoryBumpRefsCmd() *cobra.Command {
	var (
		projectRef string
		source     string
	)
	c := &cobra.Command{
		Use:   "bump-refs <memory-id> [<memory-id>...]",
		Short: "Increment the reference counter on memory entries",
		Long: `Tells the server that the named memory entries were
included in an agent context bundle. Increments reference_count
and stamps last_referenced_at so decay-based stale detection sees
them as still useful.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRef = strings.TrimSpace(projectRef)
			if projectRef == "" {
				return &usageError{msg: "--project is required"}
			}
			ids := make([]int64, 0, len(args))
			for _, raw := range args {
				id, err := parsePositiveInt64Flag("memory-id", strings.TrimSpace(raw))
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}
			payload := map[string]any{"ids": ids}
			if source = strings.TrimSpace(source); source != "" {
				payload["source"] = source
			}
			path := fmt.Sprintf("/api/projects/%d/knowledge/memory/references", projectID)
			raw, err := client.do("POST", path, payload)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			fmt.Fprintf(stdout, "✓ bumped references for %d memory entries\n", len(ids))
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&source, "source", "", "optional event source (e.g. 'bundle', 'agent')")
	return c
}

func knowledgeMemoryStaleCmd() *cobra.Command {
	var (
		projectRef string
		days       int
	)
	c := &cobra.Command{
		Use:   "stale",
		Short: "List memory entries that look stale (decay candidates)",
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
			path := fmt.Sprintf("/api/projects/%d/knowledge/memory/stale", projectID)
			if days > 0 {
				path = path + fmt.Sprintf("?days=%d", days)
			}
			raw, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().IntVar(&days, "days", 0, "stale threshold in days (server default when 0)")
	return c
}

func knowledgeMemoryProposedStaleCmd() *cobra.Command {
	var (
		projectRef string
		days       int
	)
	c := &cobra.Command{
		Use:   "proposed-stale",
		Short: "List bot-proposed memory drafts that have aged out (admin review)",
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
			path := fmt.Sprintf("/api/projects/%d/knowledge/memory/proposed/stale", projectID)
			if days > 0 {
				path = path + fmt.Sprintf("?days=%d", days)
			}
			raw, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().IntVar(&days, "days", 0, "stale threshold in days (server default when 0)")
	return c
}

// ── pretty rendering ────────────────────────────────────────────

func renderKnowledgeListPretty(entries []knowledgeEntryShape) {
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "(no entries)")
		return
	}
	fmt.Fprintln(stdout, "TYPE             SLUG                            STATUS       TITLE")
	for _, e := range entries {
		title := e.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		fmt.Fprintf(stdout, "%-16s %-31s %-12s %s\n", e.Type, e.Slug, e.Status, title)
	}
}

func renderKnowledgeEntryPretty(e knowledgeEntryShape) {
	fmt.Fprintf(stdout, "%s/%s (#%d)\n", e.Type, e.Slug, e.ID)
	fmt.Fprintf(stdout, "  title:  %s\n", e.Title)
	fmt.Fprintf(stdout, "  status: %s\n", e.Status)
	if e.ReferenceCount > 0 {
		fmt.Fprintf(stdout, "  refs:   %d (last %s)\n", e.ReferenceCount, e.LastReferencedAt)
	}
	if len(e.Metadata) > 0 {
		if b, err := json.MarshalIndent(e.Metadata, "  ", "  "); err == nil {
			fmt.Fprintf(stdout, "  metadata: %s\n", string(b))
		}
	}
	if strings.TrimSpace(e.Body) != "" {
		fmt.Fprintln(stdout, "  body:")
		for _, line := range strings.Split(e.Body, "\n") {
			fmt.Fprintln(stdout, "    "+line)
		}
	}
}

func confirmKnowledgeDelete(typeSeg, slug string, yes bool) error {
	if yes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return &usageError{msg: "refusing to delete knowledge entry without --yes in non-interactive mode"}
	}
	fmt.Fprintf(stderr, "Delete %s/%s? Type delete to confirm: ", typeSeg, slug)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return err
	}
	if strings.TrimSpace(line) != "delete" {
		return &usageError{msg: "delete cancelled"}
	}
	return nil
}
