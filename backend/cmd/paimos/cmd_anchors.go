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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type anchorIndex struct {
	Repo          string                    `json:"repo"`
	SchemaVersion string                    `json:"schema_version"`
	RepoRevision  string                    `json:"repo_revision,omitempty"`
	GeneratedAt   string                    `json:"generated_at"`
	Anchors       map[string][]anchorRecord `json:"anchors"`
}

type anchorRecord struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Label      string `json:"label,omitempty"`
	Confidence string `json:"confidence"`
	Symbol     any    `json:"symbol"`
}

var anchorPattern = regexp.MustCompile(`^\s*(?://|#|--|<!--)\s*@(?:pmo|paimos)\s+([A-Z][A-Z0-9]{0,15}-\d+)(?:\s+"([^"]+)")?\s*(?:-->)?\s*$`)

func anchorsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "anchors",
		Short: "Scan, verify, and upload issue anchors from a repo checkout",
	}
	c.AddCommand(anchorsScanCmd())
	c.AddCommand(anchorsVerifyCmd())
	c.AddCommand(anchorsUploadCmd())
	return c
}

// @paimos PAI-65 "anchor scanner command"
func anchorsScanCmd() *cobra.Command {
	var repoRoot, outputPath, repoName, schemaVersion string
	c := &cobra.Command{
		Use:   "scan",
		Short: "Scan a repository for @paimos / @pmo anchors",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := repoRootFrom(repoRoot)
			if err != nil {
				return err
			}
			index, err := buildAnchorIndex(root, repoName, schemaVersion)
			if err != nil {
				return err
			}
			if outputPath == "" {
				if flagJSON {
					return emitJSON(index)
				}
				b, err := json.MarshalIndent(index, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(stdout, string(b))
				return nil
			}
			if err := writeAnchorIndex(outputPath, index); err != nil {
				return err
			}
			if !flagJSON {
				fmt.Fprintf(stdout, "wrote %s\n", outputPath)
			}
			return nil
		},
	}
	c.Flags().StringVar(&repoRoot, "repo-root", "", "repository root (defaults to git top-level or cwd)")
	c.Flags().StringVar(&outputPath, "output", ".pmo/anchors.json", "output path for the anchor index")
	c.Flags().StringVar(&repoName, "repo", "", "override repo identifier in the index")
	c.Flags().StringVar(&schemaVersion, "schema-version", "1", "anchor index schema version")
	return c
}

// @paimos PAI-66 "anchor staleness verifier"
func anchorsVerifyCmd() *cobra.Command {
	var repoRoot, indexPath, repoName, schemaVersion string
	c := &cobra.Command{
		Use:   "verify",
		Short: "Verify that a committed anchor index still matches the repo's live anchors",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := repoRootFrom(repoRoot)
			if err != nil {
				return err
			}
			current, err := buildAnchorIndex(root, repoName, schemaVersion)
			if err != nil {
				return err
			}
			expected, err := readAnchorIndex(indexPath)
			if err != nil {
				return err
			}
			report := compareAnchorIndexes(expected, current)
			if flagJSON {
				return emitJSON(report)
			}
			if len(report.Errors) == 0 && len(report.Warnings) == 0 {
				fmt.Fprintln(stdout, "anchor index is current")
				return nil
			}
			for _, w := range report.Warnings {
				fmt.Fprintf(stdout, "warn: %s\n", w)
			}
			for _, e := range report.Errors {
				fmt.Fprintf(stderr, "error: %s\n", e)
			}
			if len(report.Errors) > 0 {
				return &apiError{inner: fmt.Errorf("anchor verification failed; regenerate with `paimos anchors scan --repo-root %s --output %s`", root, indexPath)}
			}
			return nil
		},
	}
	c.Flags().StringVar(&repoRoot, "repo-root", "", "repository root (defaults to git top-level or cwd)")
	c.Flags().StringVar(&indexPath, "index", ".pmo/anchors.json", "path to the committed anchor index")
	c.Flags().StringVar(&repoName, "repo", "", "override repo identifier in the generated scan")
	c.Flags().StringVar(&schemaVersion, "schema-version", "1", "anchor index schema version")
	return c
}

// @paimos PAI-68 "anchor upload command"
func anchorsUploadCmd() *cobra.Command {
	var repoRoot, indexPath, projectRef string
	var repoID int64
	c := &cobra.Command{
		Use:   "upload",
		Short: "Upload a scanned anchor index to a PAIMOS project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectRef) == "" {
				return &usageError{msg: "--project is required"}
			}
			root, err := repoRootFrom(repoRoot)
			if err != nil {
				return err
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectID(client, projectRef)
			if err != nil {
				return err
			}
			index, err := readAnchorIndex(indexPath)
			if err != nil {
				return err
			}
			if repoID == 0 {
				repos, err := loadProjectRepos(client, projectID)
				if err != nil {
					return err
				}
				_, _, remoteURL := detectRepoIdentity(root)
				normalizedRemote := normalizeRepoURL(remoteURL)
				for _, repo := range repos {
					if normalizeRepoURL(repo.URL) == normalizedRemote {
						repoID = repo.ID
						break
					}
				}
				if repoID == 0 {
					return &usageError{msg: "--repo-id is required when the current git remote does not match a linked project repo"}
				}
			}
			payload := map[string]any{
				"repo_id":        repoID,
				"repo":           index.Repo,
				"schema_version": index.SchemaVersion,
				"repo_revision":  index.RepoRevision,
				"generated_at":   index.GeneratedAt,
				"anchors":        index.Anchors,
			}
			body, err := client.do("POST", fmt.Sprintf("/api/projects/%d/anchors", projectID), payload)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
			return nil
		},
	}
	c.Flags().StringVar(&repoRoot, "repo-root", "", "repository root (defaults to git top-level or cwd)")
	c.Flags().StringVar(&indexPath, "index", ".pmo/anchors.json", "path to the anchor index")
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id")
	c.Flags().Int64Var(&repoID, "repo-id", 0, "linked project repo id (auto-detected from git remote when omitted)")
	return c
}

func buildAnchorIndex(root, repoOverride, schemaVersion string) (*anchorIndex, error) {
	files, err := listRepoFiles(root)
	if err != nil {
		return nil, err
	}
	repoName, revision, _ := detectRepoIdentity(root)
	if strings.TrimSpace(repoOverride) != "" {
		repoName = strings.TrimSpace(repoOverride)
	}
	index := &anchorIndex{
		Repo:          repoName,
		SchemaVersion: strings.TrimSpace(schemaVersion),
		RepoRevision:  revision,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Anchors:       map[string][]anchorRecord{},
	}
	for _, rel := range files {
		path := filepath.Join(root, rel)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			match := anchorPattern.FindStringSubmatch(line)
			if len(match) == 0 {
				continue
			}
			issueKey := strings.ToUpper(strings.TrimSpace(match[1]))
			record := anchorRecord{
				File:       filepath.ToSlash(rel),
				Line:       i + 1,
				Label:      strings.TrimSpace(match[2]),
				Confidence: "declared",
				Symbol:     detectAnchorSymbol(rel, content, i+1),
			}
			index.Anchors[issueKey] = append(index.Anchors[issueKey], record)
		}
	}
	keys := make([]string, 0, len(index.Anchors))
	for k := range index.Anchors {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	sorted := make(map[string][]anchorRecord, len(index.Anchors))
	for _, k := range keys {
		records := slices.Clone(index.Anchors[k])
		slices.SortFunc(records, func(a, b anchorRecord) int {
			if a.File != b.File {
				return strings.Compare(a.File, b.File)
			}
			if a.Line != b.Line {
				return a.Line - b.Line
			}
			return strings.Compare(a.Label, b.Label)
		})
		sorted[k] = records
	}
	index.Anchors = sorted
	return index, nil
}

func writeAnchorIndex(path string, index *anchorIndex) error {
	if index == nil {
		return fmt.Errorf("anchor index is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func readAnchorIndex(path string) (*anchorIndex, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx anchorIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	if idx.Anchors == nil {
		idx.Anchors = map[string][]anchorRecord{}
	}
	return &idx, nil
}

type anchorVerifyReport struct {
	Warnings []string `json:"warnings"`
	Errors   []string `json:"errors"`
}

func compareAnchorIndexes(expected, current *anchorIndex) anchorVerifyReport {
	report := anchorVerifyReport{}
	type key struct {
		issue string
		file  string
		label string
	}
	expectedMap := map[key]anchorRecord{}
	currentMap := map[key]anchorRecord{}
	for issue, list := range expected.Anchors {
		for _, rec := range list {
			expectedMap[key{issue: issue, file: rec.File, label: rec.Label}] = rec
		}
	}
	for issue, list := range current.Anchors {
		for _, rec := range list {
			currentMap[key{issue: issue, file: rec.File, label: rec.Label}] = rec
		}
	}
	keys := make([]key, 0, len(expectedMap))
	for k := range expectedMap {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b key) int {
		if a.issue != b.issue {
			return strings.Compare(a.issue, b.issue)
		}
		if a.file != b.file {
			return strings.Compare(a.file, b.file)
		}
		return strings.Compare(a.label, b.label)
	})
	for _, k := range keys {
		oldRec := expectedMap[k]
		newRec, ok := currentMap[k]
		if !ok {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: missing anchor %s:%d %q", k.issue, oldRec.File, oldRec.Line, oldRec.Label))
			continue
		}
		if oldRec.Line != newRec.Line {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: line drift %s %d -> %d (regenerate .pmo/anchors.json)", k.issue, oldRec.File, oldRec.Line, newRec.Line))
		}
	}
	for k, rec := range currentMap {
		if _, ok := expectedMap[k]; !ok {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: new anchor discovered %s:%d %q", k.issue, rec.File, rec.Line, rec.Label))
		}
	}
	slices.Sort(report.Warnings)
	slices.Sort(report.Errors)
	return report
}
