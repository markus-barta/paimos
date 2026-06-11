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

// PAI-352 — `paimos onboard` produces a human-readable briefing for a
// new contributor (or a fresh agent). The data plane is shared with
// PAI-340's `session start --bundle full` (resolveBundle): we never
// duplicate the fetch, only the rendering.
//
// "Bundle" (PAI-340) = machine-loadable artifact for an agent runtime.
// "Briefing" (this file) = lossy human-readable narrative. Top-N
// selections, summaries, no JSON noise.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// onboardFormat enumerates the rendering targets. Markdown is the
// default (terminal / IDE / GitHub paste); HTML is for "open in
// browser" UX. PDF is explicitly out-of-scope per the ticket.
type onboardFormat string

const (
	onboardFormatMarkdown onboardFormat = "md"
	onboardFormatHTML     onboardFormat = "html"
)

// onboardHeaderPrefix is the literal prefix the briefing carries on
// the first line. Mirrors PAI-330's adapter header convention so a
// future drift detector / management UI can treat both surfaces with
// the same logic. The trailing fields identify the project + (optional)
// agent + canonical bundle rev so `--check` can compare against the
// current state without re-rendering the whole briefing.
const onboardHeaderPrefix = "<!-- paimos: onboarded "

// defaultReadingListSize is the cap on the "Reading list" section.
// Configurable via --reading-list-size; the briefing stays terse by
// default so a fresh contributor doesn't get drowned in links.
const defaultReadingListSize = 10

// onboardCmd is the parent verb. The interactive `--tutorial` flow is
// explicitly out-of-scope for v1; we ship the renderer + drift check.
func onboardCmd() *cobra.Command {
	var (
		projectRef      string
		agentName       string
		formatStr       string
		outPath         string
		checkOnly       bool
		readingListSize int
		// PAI-347 confidence gate parity with `session start --bundle
		// full`. Off by default so the reading list stays high-signal.
		includeLow bool
	)
	c := &cobra.Command{
		Use:   "onboard",
		Short: "Render a human-readable briefing for a project (or one of its agents)",
		Long: `onboard composes a single readable briefing from the project's
canonical bundle (PAI-340 data plane) — what to know, in what order,
with the right pointers — to onboard either a person or an agent in
minutes.

Without --agent: project-level briefing (overview + related projects +
external systems + top guidelines + recent context).

With --agent: also includes the agent definition (PAI-329 fields:
description, body excerpt, bootstrap_steps, non_negotiable_rules) plus
the agent-relevant memory and runbooks.

Output formats:
  --format md    (default)  markdown for terminal / IDE / GitHub paste
  --format html             self-contained HTML with minimal styling

The briefing is prefixed with a paimos drift-detection header. Re-run
with --check to compare an existing on-disk briefing against the
current canonical bundle and exit non-zero on drift.

Exit codes (with --check):
  0  identical (briefing matches current canonical state)
  1  drift (header rev differs from current bundle rev)
  2  --check found the file but it has no paimos-managed header`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectRef) == "" {
				return &usageError{msg: "--project is required"}
			}
			format, err := resolveOnboardFormat(formatStr)
			if err != nil {
				return err
			}
			if readingListSize <= 0 {
				readingListSize = defaultReadingListSize
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}
			projectKey, err := resolveProjectKeyFromID(client, projectID)
			if err != nil {
				return reportError(err)
			}
			project, err := fetchProjectDetail(client, projectID, projectKey)
			if err != nil {
				return reportError(err)
			}

			// Resolve the agent (best-effort): if no --agent passed, pick
			// the first declared agent so resolveBundle has a name to key
			// the artifact fetch off. The briefing renderer skips the
			// agent-role section unless the user explicitly opted in.
			declared, err := fetchProjectAgentNames(client, projectID)
			if err != nil {
				return reportError(err)
			}
			renderAgent := strings.TrimSpace(agentName)
			bundleAgent := renderAgent
			if bundleAgent == "" {
				if len(declared) > 0 {
					bundleAgent = declared[0]
				}
			} else {
				if err := validateAgentName(bundleAgent, declared); err != nil {
					return err
				}
			}

			// Empty edge case: no agents declared. Render a project-only
			// skeleton; the bundle resolver expects an agent name so we
			// short-circuit by passing an empty raw agent through.
			var bundle *bundlePayload
			if bundleAgent != "" {
				// includeProposed=false: onboarding briefings show
				// the curated reading list, not pending bot drafts.
				bundle, err = resolveBundle(client, projectSummary{ID: projectID, Key: projectKey}, bundleAgent, includeLow, false)
				if err != nil {
					return reportError(err)
				}
			} else {
				bundle = &bundlePayload{
					Project:   projectSummary{ID: projectID, Key: projectKey},
					Agent:     json.RawMessage(`{}`),
					FetchedAt: time.Now().UTC().Format(time.RFC3339),
				}
			}

			recent, err := fetchRecentContextIssues(client, projectID)
			if err != nil {
				// Recent context is best-effort: a fetch failure surfaces
				// as an empty list rather than blocking the briefing.
				recent = nil
			}

			input := briefingInput{
				project:         project,
				bundle:          bundle,
				agentName:       renderAgent,
				recent:          recent,
				readingListSize: readingListSize,
			}
			rev := computeOnboardRev(bundle)
			body, err := renderBriefing(input, format, rev)
			if err != nil {
				return err
			}

			// --check semantics: read the existing file, compare its
			// embedded header rev against the current bundle rev. We
			// never re-render to compare bytes (the briefing carries
			// timestamps that drift independent of canonical state).
			if checkOnly {
				return runOnboardCheck(resolveOnboardOutPath(outPath, format), rev, format)
			}

			// Default: write to --out, or stdout when no --out passed.
			if strings.TrimSpace(outPath) != "" {
				path := resolveOnboardOutPath(outPath, format)
				if err := writeRendered(path, body); err != nil {
					return err
				}
				if flagJSON {
					payload := map[string]any{
						"path":  path,
						"rev":   rev,
						"bytes": len(body),
					}
					b, _ := json.Marshal(payload)
					fmt.Fprintln(stdout, string(b))
				} else {
					fmt.Fprintf(stdout, "wrote %s (%d bytes, rev=%s)\n", path, len(body), rev)
				}
				return nil
			}
			fmt.Fprint(stdout, body)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&agentName, "agent", "", "agent name (declared on the project) — adds the agent role section")
	c.Flags().StringVar(&formatStr, "format", "md", "output format: md (default) or html")
	c.Flags().StringVar(&outPath, "out", "", "output file path (default: stdout)")
	c.Flags().BoolVar(&checkOnly, "check", false, "do not render; compare on-disk briefing rev against the current canonical bundle")
	c.Flags().IntVar(&readingListSize, "reading-list-size", defaultReadingListSize, "max entries in the Reading list section")
	c.Flags().BoolVar(&includeLow, "include-low", false, "include low-confidence memories in Reading list (default: skip)")
	return c
}

// resolveOnboardFormat normalises the --format flag value. Unknown
// values surface as a usageError so a typo fails fast (rather than
// silently writing markdown when html was meant).
func resolveOnboardFormat(raw string) (onboardFormat, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	switch onboardFormat(trimmed) {
	case onboardFormatMarkdown, "":
		return onboardFormatMarkdown, nil
	case onboardFormatHTML:
		return onboardFormatHTML, nil
	default:
		return "", &usageError{
			msg: fmt.Sprintf("invalid --format %q (expected md or html)", raw),
		}
	}
}

// resolveOnboardOutPath returns the cleaned out path. When the caller
// supplied a directory or a path without an extension, append the
// canonical extension for the chosen format so a stray `--out .` writes
// to a sensible filename inside that directory.
func resolveOnboardOutPath(raw string, format onboardFormat) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	clean := filepath.Clean(trimmed)
	// Common ergonomic shortcut: --out points at an existing directory.
	// We never auto-rename a non-existing target; we trust the user's
	// chosen extension when one is present.
	if info, err := os.Stat(clean); err == nil && info.IsDir() {
		ext := "md"
		if format == onboardFormatHTML {
			ext = "html"
		}
		return filepath.Join(clean, "onboarding."+ext)
	}
	return clean
}

// projectDetail is the (small) subset of /api/projects/:id we render
// in the briefing header. Description is the load-bearing field; the
// rest exist so the briefing can show a useful "What this project is"
// section even when description is blank (which is common on fresh
// projects).
type projectDetail struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// fetchProjectDetail resolves /api/projects/:id and returns the
// minimal subset the briefing needs. The endpoint returns a richer
// object (agents/repos/envs/recipes); we ignore those — the bundle
// already carries everything we render.
func fetchProjectDetail(c *Client, projectID int64, fallbackKey string) (projectDetail, error) {
	body, err := c.do("GET", fmt.Sprintf("/api/projects/%d", projectID), nil)
	if err != nil {
		return projectDetail{}, err
	}
	// The endpoint wraps the project under `project`; but legacy /
	// portal variants return the project as the top-level object. Try
	// the wrapper first, fall back to direct unmarshal so we work with
	// either shape.
	var wrap struct {
		Project projectDetail `json:"project"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil && wrap.Project.ID > 0 {
		return wrap.Project, nil
	}
	var direct projectDetail
	if err := json.Unmarshal(body, &direct); err != nil {
		// Worst case: synthesise a minimal record from what we already
		// know so the briefing still renders a header.
		return projectDetail{ID: projectID, Key: fallbackKey, Name: fallbackKey}, nil
	}
	if direct.ID == 0 {
		direct.ID = projectID
	}
	if strings.TrimSpace(direct.Key) == "" {
		direct.Key = fallbackKey
	}
	if strings.TrimSpace(direct.Name) == "" {
		direct.Name = fallbackKey
	}
	return direct, nil
}

// recentIssue is the projection of /api/projects/:id/issues we render
// in the "Recent context" section. Status drives the one-line summary;
// updated_at drives the sort and is shown as a relative timestamp.
type recentIssue struct {
	IssueKey  string `json:"issue_key"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

// recentContextStatuses are the issue states we treat as "what's been
// happening". Live issues (new/backlog/accepted) are noisy on a fresh
// briefing — the reader is more interested in shipped/closed work.
// (Note: cmd_issue_write.go's terminalStatuses includes accepted /
// invoiced — that's the close-note set, a slightly different filter.)
var recentContextStatuses = []string{"done", "delivered", "cancelled"}

// fetchRecentContextIssues hits the project-scoped issues list with a
// terminal-state filter, sorted by updated_at descending. The endpoint
// applies its own ORDER BY (type DESC, issue_number ASC) — we re-sort
// client-side by updated_at so the briefing surfaces the freshest 5.
//
// Best-effort: an older server / permissions issue surfaces as an
// empty slice so the briefing still renders.
func fetchRecentContextIssues(c *Client, projectID int64) ([]recentIssue, error) {
	statusCSV := strings.Join(recentContextStatuses, ",")
	q := url.Values{}
	q.Set("status", statusCSV)
	// Take a generous slice so the client-side updated_at sort has
	// enough material; the server's ordering is by type/issue_number
	// which doesn't match our "freshest first" need.
	q.Set("limit", "50")
	body, err := c.do("GET", fmt.Sprintf("/api/projects/%d/issues?%s", projectID, q.Encode()), nil)
	if err != nil {
		return nil, err
	}
	var raw []recentIssue
	if err := json.Unmarshal(body, &raw); err != nil {
		// Some endpoints wrap; tolerate.
		var wrap struct {
			Items []recentIssue `json:"items"`
		}
		if werr := json.Unmarshal(body, &wrap); werr == nil {
			raw = wrap.Items
		} else {
			return nil, fmt.Errorf("decode issues: %w", err)
		}
	}
	sort.Slice(raw, func(i, j int) bool {
		return raw[i].UpdatedAt > raw[j].UpdatedAt
	})
	if len(raw) > 5 {
		raw = raw[:5]
	}
	return raw, nil
}

// briefingInput bundles everything renderBriefing needs. Splitting it
// out keeps the renderer signature stable as we add fields and lets
// tests assemble inputs without going through a real API.
type briefingInput struct {
	project         projectDetail
	bundle          *bundlePayload
	agentName       string // empty → project-only briefing
	recent          []recentIssue
	readingListSize int
}

// agentArtifactProbe is the subset of the canonical agent artifact the
// briefing renders. We probe with a tolerant struct so the renderer
// survives PAI-329 schema additions (extra fields round-trip via
// the bundle's RawMessage).
type agentArtifactProbe struct {
	Agent struct {
		Name               string `json:"name"`
		Description        string `json:"description"`
		Body               string `json:"body"`
		BootstrapSteps     []struct {
			Title     string `json:"title"`
			Command   string `json:"command"`
			Rationale string `json:"rationale"`
		} `json:"bootstrap_steps"`
		NonNegotiableRules []struct {
			Title     string `json:"title"`
			Body      string `json:"body"`
			MemoryRef string `json:"memory_ref"`
		} `json:"non_negotiable_rules"`
	} `json:"agent"`
}

// renderBriefing is the dispatch entry point. The underlying renderers
// (markdown, HTML) share the same composition — we render once into a
// canonical "section list" then format. Keeping the section model
// shared means an HTML/markdown drift bug must surface in BOTH formats.
func renderBriefing(in briefingInput, format onboardFormat, rev string) (string, error) {
	sections := buildBriefingSections(in)
	switch format {
	case onboardFormatMarkdown:
		return renderBriefingMarkdown(in, sections, rev), nil
	case onboardFormatHTML:
		return renderBriefingHTML(in, sections, rev), nil
	default:
		return "", fmt.Errorf("unsupported onboard format %q", format)
	}
}

// briefingSection is one rendered chunk of the briefing. Each section
// carries a heading + a body string already formatted for the chosen
// format. The renderer assembles by joining with blank lines.
type briefingSection struct {
	heading string
	// body is the section content as rendered for the chosen format.
	// Markdown bodies use markdown syntax; HTML bodies use HTML — we
	// build them inline below to keep the section model format-agnostic
	// at the call site (the markdown / HTML wrappers do their own
	// list-item / paragraph formatting).
	body string
}

// buildBriefingSections is a stub — the actual section composition is
// done inline by the format-specific renderers because each format has
// different list-item conventions and we want to avoid an intermediate
// AST. The sections slice exists so we can grow this into a unified
// pipeline if a third format (PDF / JSON-summary) ever arrives.
func buildBriefingSections(_ briefingInput) []briefingSection {
	return nil
}

// renderBriefingMarkdown produces the canonical markdown briefing.
// Layout follows PAI-352's spec (welcome → what this project is →
// external systems → guidelines → recent context → [agent role] →
// runbooks → where to look → reading list).
func renderBriefingMarkdown(in briefingInput, _ []briefingSection, rev string) string {
	var b strings.Builder
	// Header: one-line drift marker matching PAI-330's convention.
	fmt.Fprint(&b, buildOnboardHeader(in, rev), "\n\n")

	// Title + tagline.
	fmt.Fprintf(&b, "# Welcome to %s\n\n", displayName(in.project))
	if d := strings.TrimSpace(in.project.Description); d != "" {
		fmt.Fprintf(&b, "> %s\n\n", firstLine(d))
	}

	// What this project is.
	fmt.Fprintln(&b, "## What this project is")
	if d := strings.TrimSpace(in.project.Description); d != "" {
		fmt.Fprintln(&b, d)
		fmt.Fprintln(&b)
	} else {
		fmt.Fprintln(&b, "_No project description on file yet — ask the project owner to add one._")
		fmt.Fprintln(&b)
	}
	if related := topNRelatedProjects(in.bundle, 5); len(related) > 0 {
		fmt.Fprintln(&b, "Related projects:")
		fmt.Fprintln(&b)
		for _, e := range related {
			line := fmt.Sprintf("- **%s**", strings.TrimSpace(e.Title))
			if k := stringFromMeta(e.Metadata, "key"); k != "" {
				line = fmt.Sprintf("- **%s** (`%s`)", strings.TrimSpace(e.Title), k)
			}
			if role := stringFromMeta(e.Metadata, "role"); role != "" {
				line += " — " + role
			} else if rel := stringFromMeta(e.Metadata, "relationship"); rel != "" {
				line += " — " + rel
			}
			fmt.Fprintln(&b, line)
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, "  %s\n", firstLine(body))
			}
		}
		fmt.Fprintln(&b)
	}

	// External systems.
	if ext := topNExternalSystems(in.bundle, 10); len(ext) > 0 {
		fmt.Fprintln(&b, "## Key external systems")
		fmt.Fprintln(&b)
		for _, e := range ext {
			fmt.Fprintf(&b, "- **%s**", strings.TrimSpace(e.Title))
			if u := stringFromMeta(e.Metadata, "url"); u != "" {
				fmt.Fprintf(&b, " — <%s>", u)
			}
			fmt.Fprintln(&b)
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, "  %s\n", firstLine(body))
			}
		}
		fmt.Fprintln(&b)
	}

	// How we work — top guidelines.
	if gl := topNGuidelines(in.bundle, 10); len(gl) > 0 {
		fmt.Fprintln(&b, "## How we work")
		fmt.Fprintln(&b)
		for _, e := range gl {
			fmt.Fprintf(&b, "- **%s**", strings.TrimSpace(e.Title))
			if e.Source != nil && e.Source.Type == "inherited" {
				fmt.Fprintf(&b, " _(inherited from %s)_", e.Source.FromProject)
			}
			fmt.Fprintln(&b)
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, "  %s\n", firstLine(body))
			}
		}
		fmt.Fprintln(&b)
	}

	// Recent context.
	if len(in.recent) > 0 {
		fmt.Fprintln(&b, "## Recent context")
		fmt.Fprintln(&b)
		for _, r := range in.recent {
			fmt.Fprintf(&b, "- `%s` — %s _(%s, %s)_\n",
				strings.TrimSpace(r.IssueKey),
				strings.TrimSpace(r.Title),
				strings.TrimSpace(r.Status),
				shortTimestamp(r.UpdatedAt))
		}
		fmt.Fprintln(&b)
	}

	// Agent role section — only when --agent supplied.
	if in.agentName != "" {
		probe := decodeAgentArtifact(in.bundle.Agent)
		fmt.Fprintf(&b, "## If you're playing the %s role\n\n", in.agentName)
		if d := strings.TrimSpace(probe.Agent.Description); d != "" {
			fmt.Fprintf(&b, "%s\n\n", d)
		}
		if body := strings.TrimSpace(probe.Agent.Body); body != "" {
			fmt.Fprintln(&b, "### Excerpt")
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, excerpt(body, 600))
			fmt.Fprintln(&b)
		}
		if steps := probe.Agent.BootstrapSteps; len(steps) > 0 {
			fmt.Fprintln(&b, "### Bootstrap steps")
			fmt.Fprintln(&b)
			for i, s := range steps {
				title := strings.TrimSpace(s.Title)
				if title == "" {
					title = fmt.Sprintf("Step %d", i+1)
				}
				fmt.Fprintf(&b, "%d. **%s**\n", i+1, title)
				if cmd := strings.TrimSpace(s.Command); cmd != "" {
					fmt.Fprintf(&b, "   ```\n   %s\n   ```\n", cmd)
				}
				if r := strings.TrimSpace(s.Rationale); r != "" {
					fmt.Fprintf(&b, "   _%s_\n", r)
				}
			}
			fmt.Fprintln(&b)
		}
		if rules := probe.Agent.NonNegotiableRules; len(rules) > 0 {
			fmt.Fprintln(&b, "### Non-negotiable rules")
			fmt.Fprintln(&b)
			for _, r := range rules {
				fmt.Fprintf(&b, "- **%s**", strings.TrimSpace(r.Title))
				if ref := strings.TrimSpace(r.MemoryRef); ref != "" {
					fmt.Fprintf(&b, " _(memory: `%s`)_", ref)
				}
				fmt.Fprintln(&b)
				if body := strings.TrimSpace(r.Body); body != "" {
					fmt.Fprintf(&b, "  %s\n", firstLine(body))
				}
			}
			fmt.Fprintln(&b)
		}
	}

	// Runbooks.
	if rbs := in.bundle.Runbooks; len(rbs) > 0 {
		fmt.Fprintln(&b, "## Known runbooks")
		fmt.Fprintln(&b)
		for _, e := range rbs {
			fmt.Fprintf(&b, "- **%s**", strings.TrimSpace(e.Title))
			if e.Source != nil && e.Source.Type == "inherited" {
				fmt.Fprintf(&b, " _(inherited from %s)_", e.Source.FromProject)
			}
			fmt.Fprintln(&b)
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, "  %s\n", firstLine(body))
			}
		}
		fmt.Fprintln(&b)
	}

	// Where to look.
	fmt.Fprintln(&b, "## Where to look")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Local memory cache: `.paimos/cache/%s/` (run `paimos session start --bundle full`)\n", in.project.Key)
	fmt.Fprintln(&b, "- Issues, memory, runbooks: the paimos web UI for this project")
	fmt.Fprintf(&b, "- CLI quickstart: `paimos session start --project %s --agent %s`\n",
		in.project.Key, fallback(in.agentName, "<agent>"))
	fmt.Fprintln(&b)

	// Reading list.
	if rl := topNReadingList(in.bundle, in.readingListSize); len(rl) > 0 {
		fmt.Fprintln(&b, "## Reading list")
		fmt.Fprintln(&b)
		for _, e := range rl {
			conf := memoryConfidenceFrom(e.Metadata)
			fmt.Fprintf(&b, "- **%s** _(confidence: %s)_", strings.TrimSpace(e.Title), conf)
			if e.Source != nil && e.Source.Type == "inherited" {
				fmt.Fprintf(&b, " _(from %s)_", e.Source.FromProject)
			}
			fmt.Fprintln(&b)
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, "  %s\n", firstLine(body))
			}
		}
		fmt.Fprintln(&b)
	}

	return b.String()
}

// renderBriefingHTML produces the self-contained HTML briefing. Style
// is intentionally minimal — embedded CSS, no external dependency. We
// re-use the markdown body with light HTML transforms (escape + add
// section + list markup) rather than running a full markdown parser:
// avoids pulling in a parser dependency for one verb.
func renderBriefingHTML(in briefingInput, _ []briefingSection, rev string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "<!DOCTYPE html>")
	fmt.Fprintln(&b, `<html lang="en">`)
	fmt.Fprintln(&b, "<head>")
	fmt.Fprintf(&b, `<meta charset="utf-8">`+"\n")
	fmt.Fprintf(&b, "<title>%s — paimos onboarding</title>\n", html.EscapeString(displayName(in.project)))
	fmt.Fprint(&b, onboardCSS, "\n")
	fmt.Fprintln(&b, "</head>")
	fmt.Fprintln(&b, "<body>")
	fmt.Fprintln(&b, buildOnboardHeader(in, rev))

	fmt.Fprintf(&b, "<h1>Welcome to %s</h1>\n", html.EscapeString(displayName(in.project)))
	if d := strings.TrimSpace(in.project.Description); d != "" {
		fmt.Fprintf(&b, "<blockquote>%s</blockquote>\n", html.EscapeString(firstLine(d)))
	}

	fmt.Fprintln(&b, "<h2>What this project is</h2>")
	if d := strings.TrimSpace(in.project.Description); d != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(d))
	} else {
		fmt.Fprintln(&b, "<p><em>No project description on file yet.</em></p>")
	}
	if related := topNRelatedProjects(in.bundle, 5); len(related) > 0 {
		fmt.Fprintln(&b, "<p>Related projects:</p>")
		fmt.Fprintln(&b, "<ul>")
		for _, e := range related {
			fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(strings.TrimSpace(e.Title)))
			if k := stringFromMeta(e.Metadata, "key"); k != "" {
				fmt.Fprintf(&b, ` <code>%s</code>`, html.EscapeString(k))
			}
			if role := stringFromMeta(e.Metadata, "role"); role != "" {
				fmt.Fprintf(&b, ` — %s`, html.EscapeString(role))
			} else if rel := stringFromMeta(e.Metadata, "relationship"); rel != "" {
				fmt.Fprintf(&b, ` — %s`, html.EscapeString(rel))
			}
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, `<br><span class="muted">%s</span>`, html.EscapeString(firstLine(body)))
			}
			fmt.Fprintln(&b, "</li>")
		}
		fmt.Fprintln(&b, "</ul>")
	}

	if ext := topNExternalSystems(in.bundle, 10); len(ext) > 0 {
		fmt.Fprintln(&b, "<h2>Key external systems</h2>")
		fmt.Fprintln(&b, "<ul>")
		for _, e := range ext {
			fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(strings.TrimSpace(e.Title)))
			if u := stringFromMeta(e.Metadata, "url"); u != "" {
				fmt.Fprintf(&b, ` — <a href="%s">%s</a>`, html.EscapeString(u), html.EscapeString(u))
			}
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, `<br><span class="muted">%s</span>`, html.EscapeString(firstLine(body)))
			}
			fmt.Fprintln(&b, "</li>")
		}
		fmt.Fprintln(&b, "</ul>")
	}

	if gl := topNGuidelines(in.bundle, 10); len(gl) > 0 {
		fmt.Fprintln(&b, "<h2>How we work</h2>")
		fmt.Fprintln(&b, "<ul>")
		for _, e := range gl {
			fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(strings.TrimSpace(e.Title)))
			if e.Source != nil && e.Source.Type == "inherited" {
				fmt.Fprintf(&b, ` <em>(inherited from %s)</em>`, html.EscapeString(e.Source.FromProject))
			}
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, `<br><span class="muted">%s</span>`, html.EscapeString(firstLine(body)))
			}
			fmt.Fprintln(&b, "</li>")
		}
		fmt.Fprintln(&b, "</ul>")
	}

	if len(in.recent) > 0 {
		fmt.Fprintln(&b, "<h2>Recent context</h2>")
		fmt.Fprintln(&b, "<ul>")
		for _, r := range in.recent {
			fmt.Fprintf(&b, `<li><code>%s</code> — %s <span class="muted">(%s, %s)</span></li>`+"\n",
				html.EscapeString(strings.TrimSpace(r.IssueKey)),
				html.EscapeString(strings.TrimSpace(r.Title)),
				html.EscapeString(strings.TrimSpace(r.Status)),
				html.EscapeString(shortTimestamp(r.UpdatedAt)))
		}
		fmt.Fprintln(&b, "</ul>")
	}

	if in.agentName != "" {
		probe := decodeAgentArtifact(in.bundle.Agent)
		fmt.Fprintf(&b, "<h2>If you're playing the %s role</h2>\n", html.EscapeString(in.agentName))
		if d := strings.TrimSpace(probe.Agent.Description); d != "" {
			fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(d))
		}
		if body := strings.TrimSpace(probe.Agent.Body); body != "" {
			fmt.Fprintln(&b, "<h3>Excerpt</h3>")
			fmt.Fprintf(&b, "<pre>%s</pre>\n", html.EscapeString(excerpt(body, 600)))
		}
		if steps := probe.Agent.BootstrapSteps; len(steps) > 0 {
			fmt.Fprintln(&b, "<h3>Bootstrap steps</h3>")
			fmt.Fprintln(&b, "<ol>")
			for i, s := range steps {
				title := strings.TrimSpace(s.Title)
				if title == "" {
					title = fmt.Sprintf("Step %d", i+1)
				}
				fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(title))
				if cmd := strings.TrimSpace(s.Command); cmd != "" {
					fmt.Fprintf(&b, "<pre><code>%s</code></pre>", html.EscapeString(cmd))
				}
				if r := strings.TrimSpace(s.Rationale); r != "" {
					fmt.Fprintf(&b, `<p class="muted">%s</p>`, html.EscapeString(r))
				}
				fmt.Fprintln(&b, "</li>")
			}
			fmt.Fprintln(&b, "</ol>")
		}
		if rules := probe.Agent.NonNegotiableRules; len(rules) > 0 {
			fmt.Fprintln(&b, "<h3>Non-negotiable rules</h3>")
			fmt.Fprintln(&b, "<ul>")
			for _, r := range rules {
				fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(strings.TrimSpace(r.Title)))
				if ref := strings.TrimSpace(r.MemoryRef); ref != "" {
					fmt.Fprintf(&b, ` <em>(memory: <code>%s</code>)</em>`, html.EscapeString(ref))
				}
				if body := strings.TrimSpace(r.Body); body != "" {
					fmt.Fprintf(&b, `<br><span class="muted">%s</span>`, html.EscapeString(firstLine(body)))
				}
				fmt.Fprintln(&b, "</li>")
			}
			fmt.Fprintln(&b, "</ul>")
		}
	}

	if rbs := in.bundle.Runbooks; len(rbs) > 0 {
		fmt.Fprintln(&b, "<h2>Known runbooks</h2>")
		fmt.Fprintln(&b, "<ul>")
		for _, e := range rbs {
			fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(strings.TrimSpace(e.Title)))
			if e.Source != nil && e.Source.Type == "inherited" {
				fmt.Fprintf(&b, ` <em>(inherited from %s)</em>`, html.EscapeString(e.Source.FromProject))
			}
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, `<br><span class="muted">%s</span>`, html.EscapeString(firstLine(body)))
			}
			fmt.Fprintln(&b, "</li>")
		}
		fmt.Fprintln(&b, "</ul>")
	}

	fmt.Fprintln(&b, "<h2>Where to look</h2>")
	fmt.Fprintln(&b, "<ul>")
	fmt.Fprintf(&b, "<li>Local memory cache: <code>.paimos/cache/%s/</code> (run <code>paimos session start --bundle full</code>)</li>\n", html.EscapeString(in.project.Key))
	fmt.Fprintln(&b, "<li>Issues, memory, runbooks: the paimos web UI for this project</li>")
	fmt.Fprintf(&b, "<li>CLI quickstart: <code>paimos session start --project %s --agent %s</code></li>\n",
		html.EscapeString(in.project.Key), html.EscapeString(fallback(in.agentName, "&lt;agent&gt;")))
	fmt.Fprintln(&b, "</ul>")

	if rl := topNReadingList(in.bundle, in.readingListSize); len(rl) > 0 {
		fmt.Fprintln(&b, "<h2>Reading list</h2>")
		fmt.Fprintln(&b, "<ul>")
		for _, e := range rl {
			conf := memoryConfidenceFrom(e.Metadata)
			fmt.Fprintf(&b, "<li><strong>%s</strong> <span class=\"muted\">(confidence: %s)</span>",
				html.EscapeString(strings.TrimSpace(e.Title)), html.EscapeString(conf))
			if e.Source != nil && e.Source.Type == "inherited" {
				fmt.Fprintf(&b, ` <em>(from %s)</em>`, html.EscapeString(e.Source.FromProject))
			}
			if body := strings.TrimSpace(e.Body); body != "" {
				fmt.Fprintf(&b, `<br><span class="muted">%s</span>`, html.EscapeString(firstLine(body)))
			}
			fmt.Fprintln(&b, "</li>")
		}
		fmt.Fprintln(&b, "</ul>")
	}

	fmt.Fprintln(&b, "</body>")
	fmt.Fprintln(&b, "</html>")
	return b.String()
}

// onboardCSS is the embedded stylesheet for the HTML format. We pick
// a system-font stack + readable line height + a muted secondary
// colour for tertiary text. No external assets — the briefing must
// render offline.
const onboardCSS = `<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; line-height: 1.5; max-width: 760px; margin: 2rem auto; padding: 0 1rem; color: #1a1a1a; }
  h1 { border-bottom: 1px solid #ddd; padding-bottom: .25rem; }
  h2 { margin-top: 2rem; border-bottom: 1px solid #eee; padding-bottom: .15rem; }
  h3 { margin-top: 1.25rem; }
  blockquote { border-left: 3px solid #888; margin: 0; padding: .25rem 1rem; color: #555; background: #f8f8f8; }
  code { background: #f4f4f4; padding: 0 .25rem; border-radius: 3px; }
  pre { background: #f4f4f4; padding: .5rem .75rem; border-radius: 4px; overflow-x: auto; }
  ul, ol { padding-left: 1.5rem; }
  li { margin: .35rem 0; }
  .muted { color: #666; font-size: .9em; }
</style>`

// buildOnboardHeader returns the canonical drift-detection header
// line. Format mirrors PAI-330's adapter header (`<!-- paimos: ... -->`)
// so a future unified drift-detection surface can read both.
func buildOnboardHeader(in briefingInput, rev string) string {
	agent := strings.TrimSpace(in.agentName)
	if agent == "" {
		return fmt.Sprintf("%s%s@%s at %s -->",
			onboardHeaderPrefix, in.project.Key, rev, time.Now().UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("%s%s@%s [agent=%s] at %s -->",
		onboardHeaderPrefix, in.project.Key, rev, agent, time.Now().UTC().Format(time.RFC3339))
}

// computeOnboardRev returns a stable sha256 prefix over the bundle
// payload's content. We re-use computeBundleRev (PAI-340) so the
// briefing rev is identical to the bundle rev — which means a
// `session start --bundle full` then `onboard` cycle produces a
// briefing whose rev a later --check can verify against the
// then-current bundle without re-rendering.
func computeOnboardRev(b *bundlePayload) string {
	if b == nil {
		// Empty rev keeps the header well-formed even when the bundle
		// pipeline failed; --check can detect "no rev" as a special case.
		h := sha256.New()
		h.Write([]byte("empty-bundle"))
		return hex.EncodeToString(h.Sum(nil))[:12]
	}
	full := computeBundleRev(b)
	if len(full) >= 12 {
		return full[:12]
	}
	return full
}

// runOnboardCheck reads the existing on-disk briefing, parses the
// embedded header, and compares the rev field against the current
// canonical bundle rev. PAI-330's contract: 0 identical, 1 drift,
// 2 header missing.
func runOnboardCheck(path, currentRev string, format onboardFormat) error {
	if strings.TrimSpace(path) == "" {
		return &usageError{msg: "--check requires --out (path to existing briefing)"}
	}
	body, err := os.ReadFile(path) // #nosec G304 -- path comes from the CLI user's own --out flag.
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "paimos: %s does not exist (would be created on render)\n", path)
			return &checkExitCode{code: 1}
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	headerRev, agent, ok := parseOnboardHeader(string(body))
	if !ok {
		fmt.Fprintf(stderr, "paimos: %s has no paimos-managed header — out of management surface\n", path)
		return &checkExitCode{code: 2}
	}
	if headerRev == currentRev {
		if flagJSON {
			payload := map[string]any{
				"check": "identical",
				"path":  path,
				"rev":   currentRev,
				"agent": agent,
			}
			b, _ := json.Marshal(payload)
			fmt.Fprintln(stdout, string(b))
		} else {
			fmt.Fprintf(stdout, "%s: identical (rev=%s)\n", path, currentRev)
		}
		return nil
	}
	fmt.Fprintf(stderr, "paimos: %s differs from canonical bundle (header rev=%s, current rev=%s)\n",
		path, headerRev, currentRev)
	if format == onboardFormatHTML {
		// Hint: the HTML wrapper carries the header on its own line —
		// a regen just rewrites the file, no merge needed.
		fmt.Fprintln(stderr, "  run without --check to regenerate")
	} else {
		fmt.Fprintln(stderr, "  run without --check to regenerate")
	}
	return &checkExitCode{code: 1}
}

// parseOnboardHeader extracts the rev + agent out of the briefing's
// first paimos-managed line. Returns ok=false when no header is
// present (exit-code-2 case).
//
// Header forms accepted:
//
//	<!-- paimos: onboarded BON26@<rev> at <ts> -->
//	<!-- paimos: onboarded BON26@<rev> [agent=ops] at <ts> -->
//
// The HTML format embeds the same header verbatim so this parser is
// shared between both formats.
func parseOnboardHeader(body string) (rev, agent string, ok bool) {
	body = strings.TrimLeft(body, "\xef\xbb\xbf \t\r\n")
	if !strings.HasPrefix(body, onboardHeaderPrefix) {
		return "", "", false
	}
	end := strings.Index(body, "-->")
	if end < 0 {
		return "", "", false
	}
	inner := strings.TrimSpace(body[len(onboardHeaderPrefix):end])
	// `inner` is e.g. "BON26@abc123 [agent=ops] at 2026-..." — pluck
	// the rev (after '@', before next space) and the agent (between
	// "[agent=" and "]").
	at := strings.Index(inner, "@")
	if at < 0 {
		return "", "", false
	}
	tail := inner[at+1:]
	// rev runs until the next space.
	if sp := strings.IndexAny(tail, " \t"); sp >= 0 {
		rev = strings.TrimSpace(tail[:sp])
	} else {
		rev = strings.TrimSpace(tail)
	}
	if rev == "" {
		return "", "", false
	}
	if i := strings.Index(inner, "[agent="); i >= 0 {
		j := strings.Index(inner[i:], "]")
		if j > 0 {
			agent = strings.TrimSpace(inner[i+len("[agent=") : i+j])
		}
	}
	return rev, agent, true
}

// ── helpers ─────────────────────────────────────────────────────────

// displayName returns the project's preferred user-facing label —
// `name` when present, falling back to `key` so the briefing is
// always useful even on minimally-configured projects.
func displayName(p projectDetail) string {
	if n := strings.TrimSpace(p.Name); n != "" {
		return n
	}
	return strings.TrimSpace(p.Key)
}

// firstLine returns the first non-empty line of `s`, trimmed. The
// briefing uses one-line summaries pervasively (related projects,
// recent context, runbooks first paragraph) so this is the workhorse
// for "give me a one-liner from the body".
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// excerpt returns up to `n` characters of `s`, on a word boundary,
// suffixed with an ellipsis when truncation occurred. Used for the
// agent body excerpt — the full body is in the agent.json artifact.
func excerpt(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexAny(cut, " \t\n"); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \t\n.,;:") + "…"
}

// shortTimestamp returns a YYYY-MM-DD slice of an RFC3339 timestamp;
// failing parse, returns the raw string so the briefing still renders.
func shortTimestamp(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		// Common shapes: "2026-05-08 13:59:32" / "2026-05-08T13:59:32Z".
		return s[:10]
	}
	return s
}

// fallback returns `s` if non-empty (after trim); else `def`. Used
// throughout the rendering to keep "where to look" lines well-formed
// even when the caller didn't pass --agent.
func fallback(s, def string) string {
	if t := strings.TrimSpace(s); t != "" {
		return t
	}
	return def
}

// topNRelatedProjects returns the first `n` related-project entries
// from the bundle. The bundle's order follows the project's declared
// related_projects[] order, which is what we want for the briefing
// (the project owner orders by salience).
func topNRelatedProjects(b *bundlePayload, n int) []knowledgeEntry {
	if b == nil {
		return nil
	}
	out := b.RelatedProjects
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// topNExternalSystems returns the first `n` external-system entries
// from the bundle. External systems aren't ranked client-side; we
// trust the project's declared order.
func topNExternalSystems(b *bundlePayload, n int) []knowledgeEntry {
	if b == nil {
		return nil
	}
	out := b.ExternalSystems
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// topNGuidelines returns the top `n` guidelines, ranked by the same
// (confidence DESC, last_referenced_at DESC) policy as the reading
// list. Guidelines without confidence metadata fall to "medium" per
// PAI-347's backwards-compat rule.
func topNGuidelines(b *bundlePayload, n int) []knowledgeEntry {
	if b == nil {
		return nil
	}
	out := make([]knowledgeEntry, len(b.Guidelines))
	copy(out, b.Guidelines)
	sortByConfidenceAndRecency(out)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// topNReadingList returns the top `n` memory entries, sorted by
// confidence (high → medium → low) with last_referenced_at DESC as
// tiebreak (PAI-347 §"Confidence taxonomy"). Same ranking as the
// guidelines — the reading list is the agent's "what to read first"
// surface; recency reflects "what's been actively referenced".
func topNReadingList(b *bundlePayload, n int) []knowledgeEntry {
	if b == nil {
		return nil
	}
	out := make([]knowledgeEntry, len(b.Memory))
	copy(out, b.Memory)
	sortByConfidenceAndRecency(out)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// sortByConfidenceAndRecency applies PAI-347's confidence taxonomy as
// the primary sort key. Confidence ordering: high (3) > medium (2) >
// low (1). Tiebreak: last_referenced_at DESC (most recently used
// first); a missing timestamp falls back to updated_at, then to slug
// alphabetical so the sort stays deterministic.
func sortByConfidenceAndRecency(entries []knowledgeEntry) {
	score := func(e knowledgeEntry) int {
		switch memoryConfidenceFrom(e.Metadata) {
		case "high":
			return 3
		case "medium":
			return 2
		case "low":
			return 1
		}
		return 2
	}
	when := func(e knowledgeEntry) string {
		if t := stringFromMeta(e.Metadata, "last_referenced_at"); t != "" {
			return t
		}
		return e.UpdatedAt
	}
	sort.SliceStable(entries, func(i, j int) bool {
		si, sj := score(entries[i]), score(entries[j])
		if si != sj {
			return si > sj
		}
		wi, wj := when(entries[i]), when(entries[j])
		if wi != wj {
			return wi > wj
		}
		return entries[i].Slug < entries[j].Slug
	})
}

// decodeAgentArtifact safely probes the canonical agent JSON for the
// PAI-329 rendering fields. Returns a zero-valued probe (every field
// blank) on decode failure so the renderer just skips the agent
// section without erroring.
func decodeAgentArtifact(raw json.RawMessage) agentArtifactProbe {
	var probe agentArtifactProbe
	if len(raw) == 0 {
		return probe
	}
	_ = json.Unmarshal(raw, &probe)
	return probe
}
