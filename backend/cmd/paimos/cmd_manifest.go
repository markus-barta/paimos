// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed manifest_snippets.json
var manifestSnippetsJSON []byte

type manifestSnippet struct {
	Title   string `json:"title"`
	Command string `json:"command"`
}

func manifestCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "manifest",
		Short: "Mirror PMO project context into a repo checkout",
	}
	c.AddCommand(manifestPullCmd())
	return c
}

// @paimos PAI-72 "manifest mirror command"
func manifestPullCmd() *cobra.Command {
	var repoRoot, projectRef string
	c := &cobra.Command{
		Use:   "pull",
		Short: "Fetch a project manifest and write the deterministic .pmo mirror",
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
			manifest, err := loadProjectManifestRecord(client, projectID)
			if err != nil {
				return err
			}
			if err := writeManifestMirror(root, manifest.Data); err != nil {
				return err
			}
			if !flagJSON {
				fmt.Fprintf(stdout, "wrote %s\n", filepath.Join(root, ".pmo"))
			}
			return nil
		},
	}
	c.Flags().StringVar(&repoRoot, "repo-root", "", "repository root (defaults to git top-level or cwd)")
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id")
	return c
}

func writeManifestMirror(root string, data any) error {
	obj, _ := data.(map[string]any)
	if obj == nil {
		obj = map[string]any{}
	}
	pmoDir := filepath.Join(root, ".pmo")
	adrDir := filepath.Join(pmoDir, "adrs")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		return err
	}
	yml, err := marshalDeterministicYAML(obj)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pmoDir, "manifest.yaml"), yml, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pmoDir, "nfrs.md"), []byte(renderNFRs(obj)), 0o644); err != nil {
		return err
	}
	if err := writeADRs(adrDir, obj); err != nil {
		return err
	}
	return writeManagedAgentsFile(filepath.Join(root, "AGENTS.md"), renderAgentsManagedBlock(obj))
}

func renderNFRs(obj map[string]any) string {
	nfrs, _ := obj["nfrs"].([]any)
	if len(nfrs) == 0 {
		return "# Non-Functional Requirements\n\n_No NFRs mirrored from PMO._\n"
	}
	var b strings.Builder
	b.WriteString("# Non-Functional Requirements\n\n")
	for _, item := range nfrs {
		switch t := item.(type) {
		case map[string]any:
			title := stringValue(t["title"], "Untitled NFR")
			b.WriteString("## " + title + "\n\n")
			if body := strings.TrimSpace(stringValue(t["body"], "")); body != "" {
				b.WriteString(body + "\n\n")
			}
		default:
			b.WriteString("- " + strings.TrimSpace(stringValue(item, "")) + "\n")
		}
	}
	return b.String()
}

func writeADRs(dir string, obj map[string]any) error {
	entries, _ := obj["adrs"].([]any)
	if len(entries) == 0 {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, match := range matches {
			_ = os.Remove(match)
		}
		return nil
	}
	keep := map[string]bool{}
	for i, item := range entries {
		entry, _ := item.(map[string]any)
		title := stringValue(entry["title"], fmt.Sprintf("ADR %d", i+1))
		body := strings.TrimSpace(stringValue(entry["body"], ""))
		slug := slugify(title)
		if slug == "" {
			slug = fmt.Sprintf("adr-%d", i+1)
		}
		path := filepath.Join(dir, slug+".md")
		keep[path] = true
		var b strings.Builder
		b.WriteString("# " + title + "\n\n")
		if body != "" {
			b.WriteString(body + "\n")
		} else {
			b.WriteString("_No ADR body mirrored from PMO._\n")
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	for _, match := range matches {
		if !keep[match] {
			_ = os.Remove(match)
		}
	}
	return nil
}

func renderAgentsManagedBlock(obj map[string]any) string {
	var snippets []manifestSnippet
	_ = json.Unmarshal(manifestSnippetsJSON, &snippets)
	var b strings.Builder
	b.WriteString("<!-- pmo-manifest: managed:start -->\n")
	b.WriteString("# Agent Context\n\n")
	b.WriteString("This section is mirrored from PMO. Re-run `paimos manifest pull` to refresh it.\n\n")
	if stack := renderSimpleSection("Stack", obj["stack"]); stack != "" {
		b.WriteString(stack)
	}
	if commands := renderCommandsSection(obj["commands"]); commands != "" {
		b.WriteString(commands)
	}
	if owners := renderSimpleSection("Owners", obj["owners"]); owners != "" {
		b.WriteString(owners)
	}
	if services := renderSimpleSection("Services", obj["services"]); services != "" {
		b.WriteString(services)
	}
	b.WriteString("## Querying PMO Context\n\n")
	for _, sn := range snippets {
		b.WriteString("### " + sn.Title + "\n\n")
		b.WriteString("```bash\n" + sn.Command + "\n```\n\n")
	}
	b.WriteString("<!-- pmo-manifest: managed:end -->\n")
	return b.String()
}

func renderCommandsSection(v any) string {
	obj, _ := v.(map[string]any)
	if len(obj) == 0 {
		return ""
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("## Commands\n\n")
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("- `%s`: `%s`\n", k, strings.TrimSpace(stringValue(obj[k], ""))))
	}
	b.WriteString("\n")
	return b.String()
}

func renderSimpleSection(title string, v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case map[string]any:
		if len(t) == 0 {
			return ""
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		var b strings.Builder
		b.WriteString("## " + title + "\n\n")
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", k, inlineValue(t[k])))
		}
		b.WriteString("\n")
		return b.String()
	case []any:
		if len(t) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString("## " + title + "\n\n")
		for _, item := range t {
			b.WriteString("- " + inlineValue(item) + "\n")
		}
		b.WriteString("\n")
		return b.String()
	default:
		s := strings.TrimSpace(stringValue(t, ""))
		if s == "" {
			return ""
		}
		return fmt.Sprintf("## %s\n\n%s\n\n", title, s)
	}
}

func inlineValue(v any) string {
	switch t := v.(type) {
	case map[string]any, []any:
		raw, _ := json.Marshal(t)
		return string(raw)
	default:
		return strings.TrimSpace(stringValue(v, ""))
	}
}

func stringValue(v any, fallback string) string {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return fallback
		}
		return strings.TrimSpace(t)
	default:
		if v == nil {
			return fallback
		}
		return fmt.Sprint(v)
	}
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeManagedAgentsFile(path, managed string) error {
	const startMarker = "<!-- pmo-manifest: managed:start -->"
	const endMarker = "<!-- pmo-manifest: managed:end -->"
	existing, _ := os.ReadFile(path)
	content := string(existing)
	if strings.Contains(content, startMarker) && strings.Contains(content, endMarker) {
		start := strings.Index(content, startMarker)
		end := strings.Index(content, endMarker)
		if start >= 0 && end >= start {
			end += len(endMarker)
			content = content[:start] + strings.TrimRight(managed, "\n") + content[end:]
		}
	} else if strings.TrimSpace(content) == "" {
		content = managed
	} else {
		content = strings.TrimRight(content, "\n") + "\n\n" + managed
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
