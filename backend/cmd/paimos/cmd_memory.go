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

// PAI-349 — `paimos memory propose` verb. The implementation is
// deliberately thin: the verb POSTs to /api/projects/:id/memory with
// `status: "proposed"` so it lands as a `proposed`-state knowledge
// entry. The agent's session attribution (PAI-324 headers) tags the
// proposal automatically — operators see "proposed by agent=<name>,
// session=<uuid>" in the existing history feed.
//
// Server-side gates (rate limit + opt-out) live in
// handlers/memory_propose.go; this CLI is just the convenience wrapper.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// memoryCmd is the parent for `paimos memory ...` verbs. v1 only
// ships `propose`; future siblings (e.g. `accept`, `reject`) can land
// here without reshaping the surface.
func memoryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "memory",
		Short: "Manage memory entries (PAI-349 propose verb + future siblings)",
		Long: `Memory verbs operate on the project's memory knowledge entries.

Today the only verb is ` + "`propose`" + `, which drafts a memory
entry in 'proposed' status pending operator review (PAI-349). The
draft is visible in the Knowledge tab's "Proposed" inbox; accept /
edit / reject from there.`,
	}
	c.AddCommand(memoryProposeCmd())
	return c
}

// memoryProposeCmd implements `paimos memory propose --project ... --type ... --title ... [--body-file ...]`.
// Mirrors the issue create flag style (multi-line body via --body or
// --body-file, with `-` for stdin) so existing CLI users feel at home.
func memoryProposeCmd() *cobra.Command {
	var (
		projectRef        string
		typ               string
		title             string
		body              string
		bodyFile          string
		originatingTicket string
		suggestedName     string
		confidence        string
		dryRun            bool
	)
	c := &cobra.Command{
		Use:   "propose",
		Short: "Draft a memory entry in 'proposed' status (PAI-349)",
		Long: `Drafts a memory entry as a 'proposed' knowledge plane row that
operators review in the Knowledge tab. The agent's
X-Paimos-Agent-Name and X-Paimos-Session-Id headers (PAI-324) are
forwarded automatically — operators see who drafted what.

Required:
  --project <key|id>   destination project (key like BON26 or numeric id)
  --title "<text>"     human-readable title

Recommended:
  --type <kind>           memory taxonomy (feedback|project|reference|user)
                          stored in metadata.type for the editor.
  --body-file <path>      multi-line body (or "-" for stdin); --body inline alt.
  --originating-ticket <key>
                          stamps category_metadata.originating_tickets[]
  --suggested-name <slug> stable slug under (project, memory). When omitted,
                          the verb derives a slug from the title (best-effort).
  --confidence <h|m|l>    sets metadata.confidence; defaults to 'medium'.

Server gates (PAI-349):
  - Per-(agent, session) rate limit (5 / 24h by default).
  - Operator opt-out via PAIMOS_PROPOSE_DISABLED → 503.

Use --dry-run to print the request payload without hitting the API.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(projectRef) == "" {
				return &usageError{msg: "--project is required"}
			}
			if strings.TrimSpace(title) == "" {
				return &usageError{msg: "--title is required"}
			}
			bodyContent, _, err := readMultilineInput(body, bodyFile, "body")
			if err != nil {
				return err
			}
			slug := strings.TrimSpace(suggestedName)
			if slug == "" {
				slug = suggestSlugFromTitle(title)
			}
			if slug == "" {
				return &usageError{msg: "--suggested-name is required when title cannot be auto-slugged"}
			}

			meta := map[string]any{}
			if t := strings.TrimSpace(typ); t != "" {
				meta["type"] = t
			}
			if conf := strings.TrimSpace(strings.ToLower(confidence)); conf != "" {
				switch conf {
				case "h", "high":
					meta["confidence"] = "high"
				case "m", "medium":
					meta["confidence"] = "medium"
				case "l", "low":
					meta["confidence"] = "low"
				default:
					return &usageError{msg: fmt.Sprintf("--confidence %q (expected high|medium|low)", confidence)}
				}
			}
			if ot := strings.TrimSpace(originatingTicket); ot != "" {
				meta["originating_tickets"] = []string{ot}
			}

			payload := map[string]any{
				"slug":     slug,
				"title":    strings.TrimSpace(title),
				"body":     bodyContent,
				"status":   "proposed",
				"metadata": meta,
			}

			if dryRun {
				return emitJSON(payload)
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			projectID, err := resolveProjectRefToID(client, projectRef)
			if err != nil {
				return reportError(err)
			}
			respBody, err := client.do("POST", fmt.Sprintf("/api/projects/%d/memory", projectID), payload)
			if err != nil {
				return reportError(err)
			}
			// Decode the response so --json renders the canonical shape
			// the convenience endpoints emit; pretty-mode prints a brief
			// confirmation so a human running the verb sees the slug it
			// landed under.
			var out map[string]any
			if err := json.Unmarshal(respBody, &out); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			if flagJSON {
				return emitJSON(out)
			}
			fmt.Fprintf(stdout, "proposed memory %q in project %s (status=proposed, awaiting review)\n",
				slug, projectRef)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id (required)")
	c.Flags().StringVar(&typ, "type", "", "memory taxonomy (feedback|project|reference|user)")
	c.Flags().StringVar(&title, "title", "", "memory title (required)")
	c.Flags().StringVar(&body, "body", "", "memory body (inline; mutex with --body-file)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "memory body file path (or - for stdin)")
	c.Flags().StringVar(&originatingTicket, "originating-ticket", "", "issue key the proposal originated from")
	c.Flags().StringVar(&suggestedName, "suggested-name", "", "slug for the new memory entry (auto-derived from --title when omitted)")
	c.Flags().StringVar(&confidence, "confidence", "", "confidence rating (high|medium|low)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the request payload and exit without hitting the API")
	return c
}

// suggestSlugFromTitle is the fallback slug generator. Mirrors the
// frontend's suggestSlug — lowercase, ASCII letters/digits/_-, max
// 64 chars, must start with a letter (otherwise prefixed with `m_`).
// Best-effort: when the title is purely non-ASCII the result may be
// empty and the caller falls back to a usage error.
func suggestSlugFromTitle(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" {
		return ""
	}
	head := out[0]
	if head >= '0' && head <= '9' || head == '-' || head == '_' {
		out = "m_" + out
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
