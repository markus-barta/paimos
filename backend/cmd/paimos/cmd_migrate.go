// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

// PAI-357 — `paimos migrate manifest-to-knowledge` is a thin wrapper
// over POST /api/projects/{id}/migrate-manifest-to-knowledge. Mirrors
// the dry-run / force semantics of the HTTP endpoint so operators can
// preview a migration from a shell pipeline before committing.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func migrateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Schema-and-content migrations against an instance (admin-only)",
	}
	c.AddCommand(migrateManifestToKnowledgeCmd())
	return c
}

func migrateManifestToKnowledgeCmd() *cobra.Command {
	var projectRef string
	var dryRun, force bool
	c := &cobra.Command{
		Use:   "manifest-to-knowledge",
		Short: "Migrate legacy project_manifests.data into the PAI-338 knowledge plane",
		Long: `Walk a project's legacy manifest blob and create equivalent knowledge entries.

Mapping (deterministic):
  - top-level non-_ keys  → 1 runbook (slug: legacy_manifest)
  - _guardrails[i]        → 1 guideline per entry (slug: legacy_guardrail_<title>)
  - _glossary[term]       → 1 memory(type=reference) per term (slug: glossary_<term>)
  - _dev / _ops           → project_agents.body for those agents

Idempotent. A migrated manifest carries a data._migrated_at marker;
re-runs no-op unless --force is passed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectRef) == "" {
				return &usageError{msg: "--project is required"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectID(client, projectRef)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/api/projects/%d/migrate-manifest-to-knowledge", projectID)
			body := map[string]any{
				"dry_run": dryRun,
				"force":   force,
			}
			raw, err := client.do("POST", path, body)
			if err != nil {
				return err
			}
			if flagJSON {
				fmt.Fprintln(stdout, string(raw))
				return nil
			}
			var resp struct {
				DryRun     bool             `json:"dry_run"`
				Created    []map[string]any `json:"created"`
				Skipped    []map[string]any `json:"skipped"`
				Conflicts  []map[string]any `json:"conflicts"`
				MigratedAt string           `json:"migrated_at"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}
			tag := "applied"
			if resp.DryRun {
				tag = "dry-run"
			}
			fmt.Fprintf(stdout, "%s: %d created, %d skipped, %d conflicts\n", tag, len(resp.Created), len(resp.Skipped), len(resp.Conflicts))
			for _, item := range resp.Created {
				fmt.Fprintf(stdout, "  + %s/%s — %s\n", item["kind"], item["slug"], item["title"])
			}
			for _, item := range resp.Conflicts {
				fmt.Fprintf(stdout, "  ! %s/%s — %s (%s)\n", item["kind"], item["slug"], item["title"], item["reason"])
			}
			for _, item := range resp.Skipped {
				fmt.Fprintf(stdout, "  - skipped %s — %s\n", item["source"], item["reason"])
			}
			if !resp.DryRun && resp.MigratedAt != "" {
				fmt.Fprintf(stdout, "marker: data._migrated_at=%s\n", resp.MigratedAt)
			}
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing")
	c.Flags().BoolVar(&force, "force", false, "re-run even if data._migrated_at is set; overwrite existing slugs")
	return c
}
