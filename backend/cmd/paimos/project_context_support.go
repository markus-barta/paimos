// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type projectListItem struct {
	ID   int64  `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type projectRepoRecord struct {
	ID            int64  `json:"id"`
	ProjectID     int64  `json:"project_id"`
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
	Label         string `json:"label"`
}

type projectManifestRecord struct {
	ProjectID int64 `json:"project_id"`
	Data      any   `json:"data"`
}

func resolveProjectID(client *Client, ref string) (int64, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, fmt.Errorf("project ref is required")
	}
	if n, err := strconv.ParseInt(ref, 10, 64); err == nil && n > 0 {
		return n, nil
	}
	body, err := client.do("GET", "/api/projects", nil)
	if err != nil {
		return 0, err
	}
	var projects []projectListItem
	if err := json.Unmarshal(body, &projects); err != nil {
		return 0, fmt.Errorf("decode projects: %w", err)
	}
	for _, p := range projects {
		if p.Key == ref {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("project %q not found", ref)
}

func loadProjectRepos(client *Client, projectID int64) ([]projectRepoRecord, error) {
	body, err := client.do("GET", fmt.Sprintf("/api/projects/%d/repos", projectID), nil)
	if err != nil {
		return nil, err
	}
	var repos []projectRepoRecord
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, fmt.Errorf("decode repos: %w", err)
	}
	return repos, nil
}

func loadProjectManifestRecord(client *Client, projectID int64) (*projectManifestRecord, error) {
	body, err := client.do("GET", fmt.Sprintf("/api/projects/%d/manifest", projectID), nil)
	if err != nil {
		return nil, err
	}
	var manifest projectManifestRecord
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &manifest, nil
}

func repoRootFrom(path string) (string, error) {
	if strings.TrimSpace(path) != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	out, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	return os.Getwd()
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func listRepoFiles(root string) ([]string, error) {
	out, err := gitOutput(root, "ls-files", "--cached", "--others", "--exclude-standard")
	if err == nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		files := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			files = append(files, filepath.ToSlash(line))
		}
		slices.Sort(files)
		return files, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

func normalizeRepoURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")
	if strings.HasPrefix(s, "git@") {
		s = strings.TrimPrefix(s, "git@")
		parts := strings.SplitN(s, ":", 2)
		if len(parts) == 2 {
			s = "https://" + parts[0] + "/" + parts[1]
		}
	}
	return strings.ToLower(s)
}

func detectRepoIdentity(root string) (repoName, repoRevision, remoteURL string) {
	if head, err := gitOutput(root, "rev-parse", "HEAD"); err == nil {
		repoRevision = head
	}
	if remote, err := gitOutput(root, "remote", "get-url", "origin"); err == nil {
		remoteURL = remote
		trimmed := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
		trimmed = strings.TrimRight(trimmed, "/")
		if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx < len(trimmed)-1 {
			repoName = trimmed[idx+1:]
		}
	}
	if repoName == "" {
		repoName = filepath.Base(root)
	}
	return repoName, repoRevision, remoteURL
}

func marshalDeterministicYAML(v any) ([]byte, error) {
	node, err := toYAMLNode(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

func toYAMLNode(v any) (*yaml.Node, error) {
	switch t := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k})
			child, err := toYAMLNode(t[k])
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range t {
			child, err := toYAMLNode(item)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	case []string:
		items := make([]any, 0, len(t))
		for _, item := range t {
			items = append(items, item)
		}
		return toYAMLNode(items)
	case string, bool, int, int64, float64, float32:
		var node yaml.Node
		if err := node.Encode(t); err != nil {
			return nil, err
		}
		return &node, nil
	default:
		// Route unknown structs through JSON first so map ordering stays explicit.
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		return toYAMLNode(decoded)
	}
}
