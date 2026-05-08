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

// Package claudecode is the v1 reference adapter for `paimos skill render`
// (PAI-330). It consumes the canonical agent artifact emitted by
// /api/projects/:id/agents/:name.json and produces a Claude-Code-shaped
// markdown skill file matching today's hand-authored
// `<repo>/.claude/commands/<slash>.md` look-and-feel.
//
// Section layout (matches PAI-330 ticket):
//
//	You are operating as the **<agent.name> session** for <project.name>
//	(PMO project **<project.key>**).
//
//	## Your lane
//	<merged from agent.description + project.repos + project.environments>
//
//	## Bootstrap
//	<numbered list of bootstrap_steps[].title / command / rationale>
//
//	## Non-negotiable rules
//	<bulleted list of non_negotiable_rules[].title / body / memory_ref>
//
//	## Deploy cheat sheet
//	<deploy_recipes[]; metadata.deploy_recipes_used filtering happens
//	 server-side already, so we just render whatever made it through>
//
//	## Free body
//	<verbatim agent.body>
//
// Empty sections are skipped — the adapter degrades gracefully on
// PAI-326-era agents that haven't filled in the new PAI-329 fields
// (the section would just be a heading with nothing under it).
//
// memory_ref resolution is OUT OF SCOPE for PAI-330 — the ref is
// rendered as-is. PAI-353 / PAI-339 ship the resolution surface.
package claudecode

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/markus-barta/paimos/backend/cmd/paimos/adapters"
)

// Name is the registry key matched by `--harness claude-code`.
const Name = "claude-code"

// Version is this adapter's own semver. Bump on any output-format change.
const Version = "1.0.0"

// SupportsRange is the canonical-schema range this adapter consumes.
// PAI-329 shipped 1.0.0; we accept the full 1.x line.
const SupportsRange = ">=1.0.0 <2.0.0"

// Adapter implements adapters.Adapter for Claude Code.
type Adapter struct{}

// New returns a default-configured claude-code adapter.
func New() *Adapter { return &Adapter{} }

// Name returns the registry key.
func (a *Adapter) Name() string { return Name }

// Version returns the adapter's own semver.
func (a *Adapter) Version() string { return Version }

// Supports returns the canonical-schema range this adapter accepts.
func (a *Adapter) Supports() string { return SupportsRange }

// Describe is the one-line CLI help.
func (a *Adapter) Describe() string {
	return "Claude Code skill markdown — writes to .claude/commands/<slash>.md"
}

// canonicalArtifact mirrors the shape returned by
// /api/projects/:id/agents/:name.json. We re-declare it here (rather
// than importing from handlers/) to keep the adapter package self-
// contained — that's important for PAI-333's eventual extraction.
type canonicalArtifact struct {
	Project struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Key  string `json:"key"`
	} `json:"project"`
	Agent struct {
		Name             string   `json:"name"`
		Description      string   `json:"description"`
		SlashCommandName string   `json:"slash_command_name"`
		LaneTags         []string `json:"lane_tags"`
		Body             string   `json:"body"`
		BootstrapSteps   []struct {
			Title     string `json:"title"`
			Command   string `json:"command"`
			Rationale string `json:"rationale"`
		} `json:"bootstrap_steps"`
		NonNegotiableRules []struct {
			Title     string `json:"title"`
			Body      string `json:"body"`
			MemoryRef string `json:"memory_ref"`
		} `json:"non_negotiable_rules"`
		Metadata map[string]any `json:"metadata"`
	} `json:"agent"`
	Repos []struct {
		Label         string `json:"label"`
		URL           string `json:"url"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repos"`
	Environments []struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		HostAlias string `json:"host_alias"`
		HostIP    string `json:"host_ip"`
	} `json:"environments"`
	DeployRecipes []struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Summary string `json:"summary"`
	} `json:"deploy_recipes"`
}

// Render parses the canonical artifact and emits the Claude-Code skill
// body + suggested path. The dispatcher injects the drift-detection
// header — adapters never write it themselves.
func (a *Adapter) Render(canonical []byte) (adapters.RenderResult, error) {
	var art canonicalArtifact
	if err := json.Unmarshal(canonical, &art); err != nil {
		return adapters.RenderResult{}, fmt.Errorf("decode canonical artifact: %w", err)
	}
	if strings.TrimSpace(art.Agent.Name) == "" {
		return adapters.RenderResult{}, fmt.Errorf("canonical artifact missing agent.name")
	}
	body := renderBody(&art)
	suggested := suggestedPath(&art)
	return adapters.RenderResult{
		Content:       body,
		SuggestedPath: suggested,
	}, nil
}

// suggestedPath returns the conventional Claude Code commands file
// for this agent. Falls back to agent.name when slash_command_name is
// absent (matches the PAI-326 default-to-name semantics).
func suggestedPath(art *canonicalArtifact) string {
	slug := strings.TrimSpace(art.Agent.SlashCommandName)
	if slug == "" {
		slug = strings.TrimSpace(art.Agent.Name)
	}
	// Filename hygiene — slashes and backslashes would split the path.
	slug = strings.ReplaceAll(slug, string(filepath.Separator), "-")
	slug = strings.ReplaceAll(slug, "/", "-")
	return filepath.Join(".claude", "commands", slug+".md")
}

// renderBody assembles the markdown sections per the ticket's layout.
// Empty sections are skipped — graceful degrade on sparse agents.
func renderBody(art *canonicalArtifact) string {
	var b strings.Builder

	// Header sentence — always emitted; this is the "you are operating
	// as ..." preamble that grounds the session. The agent.description
	// belongs in `## Your lane`, not the preamble — keeping it in one
	// place avoids the doubled "Infra, deploys, secrets" we used to
	// emit while iterating on the layout.
	projectName := strOrFallback(art.Project.Name, art.Project.Key)
	projectKey := strOrFallback(art.Project.Key, fmt.Sprintf("id=%d", art.Project.ID))
	fmt.Fprintf(&b, "You are operating as the **%s session** for %s (PMO project **%s**).\n",
		art.Agent.Name, projectName, projectKey)

	// ## Your lane — describes scope: agent description + repos + envs.
	if hasLaneContent(art) {
		b.WriteString("\n## Your lane\n\n")
		writeLane(&b, art)
	}

	// ## Bootstrap — numbered, structured.
	if len(art.Agent.BootstrapSteps) > 0 {
		b.WriteString("\n## Bootstrap\n\n")
		writeBootstrap(&b, art)
	}

	// ## Non-negotiable rules — bulleted, with memory_ref pass-through.
	if len(art.Agent.NonNegotiableRules) > 0 {
		b.WriteString("\n## Non-negotiable rules\n\n")
		writeRules(&b, art)
	}

	// ## Deploy cheat sheet — server already filtered to the agent's
	// allow-list (see handlers/project_agent_artifact.go), so we just
	// render whatever's in art.DeployRecipes.
	if len(art.DeployRecipes) > 0 {
		b.WriteString("\n## Deploy cheat sheet\n\n")
		writeDeployRecipes(&b, art)
	}

	// ## Free body — verbatim, the bulk of hand-authored guidance.
	if body := strings.TrimSpace(art.Agent.Body); body != "" {
		b.WriteString("\n## Free body\n\n")
		b.WriteString(body)
		b.WriteString("\n")
	}

	return b.String()
}

func hasLaneContent(art *canonicalArtifact) bool {
	if strings.TrimSpace(art.Agent.Description) != "" {
		return true
	}
	if len(art.Repos) > 0 || len(art.Environments) > 0 {
		return true
	}
	if len(art.Agent.LaneTags) > 0 {
		return true
	}
	return false
}

func writeLane(b *strings.Builder, art *canonicalArtifact) {
	if desc := strings.TrimSpace(art.Agent.Description); desc != "" {
		b.WriteString(desc)
		b.WriteString("\n")
	}
	if len(art.Agent.LaneTags) > 0 {
		fmt.Fprintf(b, "\n**Lane tags:** %s\n", strings.Join(art.Agent.LaneTags, ", "))
	}
	if len(art.Repos) > 0 {
		b.WriteString("\n**Repos:**\n")
		for _, r := range art.Repos {
			label := strOrFallback(r.Label, r.URL)
			if r.DefaultBranch != "" {
				fmt.Fprintf(b, "- %s — %s (`%s`)\n", label, r.URL, r.DefaultBranch)
			} else {
				fmt.Fprintf(b, "- %s — %s\n", label, r.URL)
			}
		}
	}
	if len(art.Environments) > 0 {
		b.WriteString("\n**Environments:**\n")
		for _, e := range art.Environments {
			fmt.Fprintf(b, "- **%s**", e.Name)
			if e.URL != "" {
				fmt.Fprintf(b, " — %s", e.URL)
			}
			if host := formatHost(e.HostAlias, e.HostIP); host != "" {
				fmt.Fprintf(b, " (host: %s)", host)
			}
			b.WriteString("\n")
		}
	}
}

func writeBootstrap(b *strings.Builder, art *canonicalArtifact) {
	for i, s := range art.Agent.BootstrapSteps {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			title = fmt.Sprintf("Step %d", i+1)
		}
		fmt.Fprintf(b, "%d. **%s**\n", i+1, title)
		if cmd := strings.TrimSpace(s.Command); cmd != "" {
			fmt.Fprintf(b, "   ```sh\n   %s\n   ```\n", cmd)
		}
		if rat := strings.TrimSpace(s.Rationale); rat != "" {
			fmt.Fprintf(b, "   _%s_\n", rat)
		}
	}
}

func writeRules(b *strings.Builder, art *canonicalArtifact) {
	for i, r := range art.Agent.NonNegotiableRules {
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = fmt.Sprintf("Rule %d", i+1)
		}
		fmt.Fprintf(b, "- **%s**", title)
		// memory_ref pass-through (PAI-330 scope: render as-is, do not
		// resolve). PAI-353 / PAI-339 land the resolver.
		if ref := strings.TrimSpace(r.MemoryRef); ref != "" {
			fmt.Fprintf(b, " _(memory: `%s`)_", ref)
		}
		b.WriteString("\n")
		if body := strings.TrimSpace(r.Body); body != "" {
			for _, line := range strings.Split(body, "\n") {
				fmt.Fprintf(b, "  %s\n", line)
			}
		}
	}
}

func writeDeployRecipes(b *strings.Builder, art *canonicalArtifact) {
	for _, rec := range art.DeployRecipes {
		fmt.Fprintf(b, "### %s\n\n", rec.Name)
		if s := strings.TrimSpace(rec.Summary); s != "" {
			fmt.Fprintf(b, "%s\n\n", s)
		}
		if c := strings.TrimSpace(rec.Command); c != "" {
			fmt.Fprintf(b, "```sh\n%s\n```\n\n", c)
		}
	}
}

func formatHost(alias, ip string) string {
	switch {
	case alias != "" && ip != "":
		return alias + " (" + ip + ")"
	case alias != "":
		return alias
	case ip != "":
		return ip
	}
	return ""
}

func strOrFallback(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}
