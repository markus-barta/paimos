// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// issueBatchUpdateCmd: `paimos issue batch-update --from-file ops.jsonl`.
//
// Reads JSONL (one object per line, each a BatchUpdateItem-shaped
// {ref, fields}). Chunks at MaxBatchSize items per API call so files
// larger than 100 are sent in multiple transactions. Reports a
// per-item summary table; exits non-zero if any chunk failed.
func issueBatchUpdateCmd() *cobra.Command {
	var (
		path   string
		dryRun bool
	)
	const maxBatch = 100
	c := &cobra.Command{
		Use:   "batch-update",
		Short: "Apply per-line partial updates from a JSONL file",
		Long: `Each line of the input must be a JSON object of shape
{"ref": "PAI-83", "fields": {"status": "done"}}.

The command splits the input into chunks of 100 (the server's
batch cap) and sends each chunk as one PATCH /api/issues call.
Per-chunk transactions: one bad row inside a chunk rolls that
chunk back, but earlier chunks that already committed stay.
That's the price of handling files of arbitrary length — if you
need strict all-or-nothing, keep the file under 100 items.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return &usageError{msg: "--from-file is required"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}

			// Stream the JSONL rather than slurp the whole file — a
			// 10k-line plan shouldn't need the whole thing in memory.
			var src io.Reader
			if path == "-" {
				src = os.Stdin
			} else {
				f, err := os.Open(path)
				if err != nil {
					return fmt.Errorf("open %s: %w", path, err)
				}
				defer f.Close()
				src = f
			}

			scanner := bufio.NewScanner(src)
			// Grow the buffer — JSONL rows can be long (stack traces in notes).
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)

			var chunk []map[string]any
			chunkNum := 0
			var totalOK, totalFail int
			flush := func() error {
				if len(chunk) == 0 {
					return nil
				}
				chunkNum++
				if dryRun {
					return emitJSON(map[string]any{
						"dry_run":  true,
						"chunk":    chunkNum,
						"method":   "PATCH",
						"path":     "/api/issues",
						"body":     chunk,
					})
				}
				raw, err := client.do("PATCH", "/api/issues", chunk)
				if err != nil {
					// Server returns per-item errors on 400.
					totalFail += len(chunk)
					if flagJSON {
						fmt.Fprintln(stderr, strings.TrimSpace(string(raw)))
					} else {
						fmt.Fprintf(stderr, "chunk %d failed: %v\n", chunkNum, err)
					}
					return err
				}
				totalOK += len(chunk)
				if !flagJSON {
					fmt.Fprintf(stdout, "chunk %d: %d items updated\n", chunkNum, len(chunk))
				}
				chunk = chunk[:0]
				return nil
			}

			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue // blank and comment lines allowed
				}
				var item map[string]any
				if err := json.Unmarshal([]byte(line), &item); err != nil {
					return fmt.Errorf("line %d: invalid JSON: %w", lineNum, err)
				}
				if _, ok := item["ref"]; !ok {
					return fmt.Errorf("line %d: missing \"ref\" field", lineNum)
				}
				if _, ok := item["fields"]; !ok {
					return fmt.Errorf("line %d: missing \"fields\" field", lineNum)
				}
				chunk = append(chunk, item)
				if len(chunk) >= maxBatch {
					if err := flush(); err != nil {
						return err
					}
				}
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			if err := flush(); err != nil {
				return err
			}

			if flagJSON {
				return emitJSON(map[string]any{
					"chunks_sent":  chunkNum,
					"items_ok":     totalOK,
					"items_failed": totalFail,
				})
			}
			fmt.Fprintf(stdout, "\n%d items updated across %d chunk(s); %d failed\n",
				totalOK, chunkNum, totalFail)
			return nil
		},
	}
	c.Flags().StringVarP(&path, "from-file", "f", "", "JSONL input path (or - for stdin)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print chunks without sending")
	return c
}

// ApplyPlan is the YAML schema for `paimos apply --from-file plan.yaml`.
// Named refs (via `name:` on create items) let later create/update/
// relation rows reference an earlier item by name — translated to the
// server's positional parent_ref "#N" before POST /projects/…/batch.
//
// Example:
//
//	project: PAI
//	create:
//	  - name: epic
//	    type: epic
//	    title: My Epic
//	  - name: child1
//	    type: ticket
//	    title: First child
//	    parent: epic        # refs the `name: epic` above
//	update:
//	  - ref: PAI-42
//	    fields: {status: done}
//	relations:
//	  - source: epic
//	    type: follows_from
//	    target: PAI-40
type ApplyPlan struct {
	Project   string                   `yaml:"project"`
	Create    []ApplyCreate            `yaml:"create"`
	Update    []ApplyUpdate            `yaml:"update"`
	Relations []ApplyRelation          `yaml:"relations"`
}

type ApplyCreate struct {
	Name               string            `yaml:"name"` // local ref for cross-ops
	Title              string            `yaml:"title"`
	Type               string            `yaml:"type"`
	Status             string            `yaml:"status"`
	Priority           string            `yaml:"priority"`
	Description        string            `yaml:"description"`
	AcceptanceCriteria string            `yaml:"acceptance_criteria"`
	Notes              string            `yaml:"notes"`
	CostUnit           string            `yaml:"cost_unit"`
	Release            string            `yaml:"release"`
	Parent             string            `yaml:"parent"`  // name of another create item, OR existing ref
	ExtraFields        map[string]any    `yaml:"fields"`  // escape hatch
}

type ApplyUpdate struct {
	Ref    string         `yaml:"ref"`
	Fields map[string]any `yaml:"fields"`
}

type ApplyRelation struct {
	Source string `yaml:"source"` // name (from create) or existing ref
	Type   string `yaml:"type"`
	Target string `yaml:"target"`
}

// applyCmd: top-level `paimos apply`.
func applyCmd() *cobra.Command {
	var (
		path   string
		dryRun bool
	)
	c := &cobra.Command{
		Use:   "apply",
		Short: "Apply a declarative YAML plan (create/update/relate)",
		Long: `Reads a YAML plan and dispatches creates, updates, and relations
in order. Create rows can name themselves so later rows can
reference them (including children referencing a same-plan epic
via its name — translated to the server's parent_ref "#N" index).

NOT idempotent in v1 — running twice creates duplicates. Scaffold
epic+children once, then use `+"`issue ensure-status`"+` or
`+"`batch-update`"+` for subsequent changes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return &usageError{msg: "--from-file is required"}
			}
			var reader io.Reader
			if path == "-" {
				reader = os.Stdin
			} else {
				f, err := os.Open(path)
				if err != nil {
					return fmt.Errorf("open %s: %w", path, err)
				}
				defer f.Close()
				reader = f
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			var plan ApplyPlan
			if err := yaml.Unmarshal(data, &plan); err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}

			if dryRun {
				return emitJSON(map[string]any{
					"dry_run": true,
					"plan":    plan,
				})
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}

			// 1) Creates — translate `parent: <name>` to parent_ref "#N"
			//    when the parent is another create item; else treat as
			//    existing key/id.
			nameToIndex := map[string]int{}
			for i, c := range plan.Create {
				if c.Name != "" {
					nameToIndex[c.Name] = i
				}
			}
			nameToID := map[string]int64{}
			if len(plan.Create) > 0 {
				if plan.Project == "" {
					return &usageError{msg: "plan.create requires a top-level `project: <key>`"}
				}
				items := make([]map[string]any, 0, len(plan.Create))
				for _, c := range plan.Create {
					m := map[string]any{}
					if c.Title != "" {
						m["title"] = c.Title
					}
					if c.Type != "" {
						m["type"] = c.Type
					}
					if c.Status != "" {
						m["status"] = c.Status
					}
					if c.Priority != "" {
						m["priority"] = c.Priority
					}
					if c.Description != "" {
						m["description"] = c.Description
					}
					if c.AcceptanceCriteria != "" {
						m["acceptance_criteria"] = c.AcceptanceCriteria
					}
					if c.Notes != "" {
						m["notes"] = c.Notes
					}
					if c.CostUnit != "" {
						m["cost_unit"] = c.CostUnit
					}
					if c.Release != "" {
						m["release"] = c.Release
					}
					if c.Parent != "" {
						if idx, ok := nameToIndex[c.Parent]; ok {
							m["parent_ref"] = fmt.Sprintf("#%d", idx)
						} else {
							// Treat as an external ref; resolve to numeric id.
							pid, err := resolveIssueRefToID(client, c.Parent)
							if err != nil {
								return reportError(fmt.Errorf("resolve parent %q: %w", c.Parent, err))
							}
							m["parent_id"] = pid
						}
					}
					for k, v := range c.ExtraFields {
						m[k] = v
					}
					items = append(items, m)
				}
				raw, err := client.do("POST",
					"/api/projects/"+url.PathEscape(plan.Project)+"/issues/batch", items)
				if err != nil {
					return reportError(err)
				}
				var resp struct {
					Issues []map[string]any `json:"issues"`
				}
				if err := json.Unmarshal(raw, &resp); err != nil {
					return fmt.Errorf("decode batch response: %w", err)
				}
				for i, iss := range resp.Issues {
					if plan.Create[i].Name != "" {
						id, _ := iss["id"].(float64)
						nameToID[plan.Create[i].Name] = int64(id)
					}
				}
				if !flagJSON {
					fmt.Fprintf(stdout, "✓ created %d issues\n", len(resp.Issues))
				}
			}

			// 2) Updates — use PATCH /api/issues bulk.
			if len(plan.Update) > 0 {
				items := make([]map[string]any, 0, len(plan.Update))
				for _, u := range plan.Update {
					items = append(items, map[string]any{
						"ref":    u.Ref,
						"fields": u.Fields,
					})
				}
				if _, err := client.do("PATCH", "/api/issues", items); err != nil {
					return reportError(err)
				}
				if !flagJSON {
					fmt.Fprintf(stdout, "✓ updated %d issues\n", len(plan.Update))
				}
			}

			// 3) Relations.
			for _, rel := range plan.Relations {
				srcRef := rel.Source
				if id, ok := nameToID[rel.Source]; ok {
					srcRef = fmt.Sprintf("%d", id)
				}
				targetRef := rel.Target
				if id, ok := nameToID[rel.Target]; ok {
					targetRef = fmt.Sprintf("%d", id)
				}
				targetID, err := resolveIssueRefToID(client, targetRef)
				if err != nil {
					return reportError(fmt.Errorf("resolve target %q: %w", rel.Target, err))
				}
				if _, err := client.do("POST",
					"/api/issues/"+url.PathEscape(srcRef)+"/relations",
					map[string]any{"target_id": targetID, "type": rel.Type}); err != nil {
					return reportError(err)
				}
			}
			if !flagJSON && len(plan.Relations) > 0 {
				fmt.Fprintf(stdout, "✓ added %d relations\n", len(plan.Relations))
			}

			if flagJSON {
				return emitJSON(map[string]any{
					"ok":        true,
					"created":   len(nameToID),
					"updated":   len(plan.Update),
					"relations": len(plan.Relations),
				})
			}
			return nil
		},
	}
	c.Flags().StringVarP(&path, "from-file", "f", "", "YAML plan path (or - for stdin)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "parse the plan and print without sending")
	return c
}
