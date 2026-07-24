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

// PAI-329 — canonical agent artifact endpoints.
//
// `/api/projects/:id/agents/:name.json` returns a single merged document:
//   - Every column from the agent row.
//   - The project-level inventories the agent inherits (repos[],
//     environments[], deploy_recipes[]).
//
// Adapters (e.g. the Claude Skills adapter, the AGENTS.md generator)
// consume this artifact as their single input. Server-side merge
// avoids each adapter re-implementing inheritance — and avoids drift
// between adapters.
//
// Filtering of deploy_recipes:
//   If the agent's metadata contains a `deploy_recipes_used` array
//   (string[]), only recipes whose name appears in that allow-list
//   are inlined. Otherwise all project recipes are included. This
//   matches the ticket's description ("agents reference recipes by
//   name") without requiring a new dedicated column today.
//
// `/api/projects/:id/agents/:name.md` renders the same artifact as
// default unstyled markdown for human inspection. Production skill
// formatting is the adapter's job; this endpoint is purely "show me
// what's in the canonical artifact, plain". It skips empty sections
// so existing PAI-326-era agents (no body, no rules) render as a
// short header rather than a sea of empty headings.

package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/models"
)

// AgentArtifact is the merged-and-flattened shape returned by
// /api/projects/:id/agents/:name.json. The `agent` block is the
// per-agent record; the project-level inventory blocks are inlined
// next to it so adapters never have to make a second call.
type AgentArtifact struct {
	Project       AgentArtifactProject         `json:"project"`
	Agent         models.ProjectAgent          `json:"agent"`
	Repos         []models.ProjectRepo         `json:"repos"`
	Environments  []models.ProjectEnvironment  `json:"environments"`
	DeployRecipes []models.ProjectDeployRecipe `json:"deploy_recipes"`
}

// AgentArtifactProject is the minimal project shape inlined into the
// artifact. Adapters that need the full Project (rates, customer, etc.)
// can still call /api/projects/:id; the artifact carries only what's
// load-bearing for skill rendering.
type AgentArtifactProject struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

// artifactBuildError is a typed (status, message) error so the two
// http handlers can render their own response in their own format
// (json vs markdown wrapper) without duplicating the resolution path.
type artifactBuildError struct {
	status int
	msg    string
}

func (e *artifactBuildError) Error() string { return e.msg }

// GetProjectAgentArtifact returns the canonical artifact JSON.
// Path: /api/projects/{id}/agents/{name}.json — the .json suffix is
// part of the literal route path, so chi binds {name} to the slug
// alone (see main.go wiring).
func GetProjectAgentArtifact(w http.ResponseWriter, r *http.Request) {
	artifact, buildErr := buildProjectAgentArtifact(r)
	if buildErr != nil {
		jsonError(w, buildErr.msg, buildErr.status)
		return
	}
	jsonOK(w, artifact)
}

// GetProjectAgentArtifactMarkdown is the debug / inspection endpoint
// that renders the same artifact as default-styling markdown. NOT
// the production skill format — adapters own that.
func GetProjectAgentArtifactMarkdown(w http.ResponseWriter, r *http.Request) {
	artifact, buildErr := buildProjectAgentArtifact(r)
	if buildErr != nil {
		// Errors stay JSON even on the .md endpoint — keeping a
		// uniform machine-parseable error envelope across both
		// surfaces is more useful than a markdown 404 page.
		jsonError(w, buildErr.msg, buildErr.status)
		return
	}
	body := renderArtifactMarkdown(artifact)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// buildProjectAgentArtifact resolves the project + agent + project-
// level inventories and returns the merged artifact, or a typed
// error the caller can render in its own format.
func buildProjectAgentArtifact(r *http.Request) (*AgentArtifact, *artifactBuildError) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		return nil, &artifactBuildError{http.StatusBadRequest, "invalid project id"}
	}

	rawName := chi.URLParam(r, "name")
	agentName := strings.TrimSpace(rawName)
	if agentName == "" {
		return nil, &artifactBuildError{http.StatusBadRequest, "agent name required"}
	}

	// Project lookup — short-circuit 404 before the agent lookup so
	// callers see the more-specific error on a missing project (vs.
	// the more confusing "agent not found" when the project itself
	// is the problem).
	var proj AgentArtifactProject
	if err := db.DB.QueryRow(`SELECT id, name, key FROM projects WHERE id=?`, projectID).
		Scan(&proj.ID, &proj.Name, &proj.Key); err != nil {
		return nil, &artifactBuildError{http.StatusNotFound, "project not found"}
	}

	agent := getProjectAgentByProjectAndName(projectID, agentName)
	if agent == nil {
		return nil, &artifactBuildError{http.StatusNotFound, "agent not found"}
	}

	repos, err := listProjectReposData(projectID)
	if err != nil {
		return nil, &artifactBuildError{http.StatusInternalServerError, "query failed (repos)"}
	}
	envs, err := loadProjectEnvironments(projectID)
	if err != nil {
		return nil, &artifactBuildError{http.StatusInternalServerError, "query failed (environments)"}
	}
	recipes, err := loadProjectDeployRecipes(projectID)
	if err != nil {
		return nil, &artifactBuildError{http.StatusInternalServerError, "query failed (deploy_recipes)"}
	}

	// Filter recipes to the agent's deploy_recipes_used allow-list,
	// if it declares one. Repos and environments are always inlined
	// whole — they're cheap and adapters can sub-filter if needed.
	recipes = filterRecipesByAgentMetadata(agent.Metadata, recipes)

	return &AgentArtifact{
		Project:       proj,
		Agent:         *agent,
		Repos:         repos,
		Environments:  envs,
		DeployRecipes: recipes,
	}, nil
}

// filterRecipesByAgentMetadata returns the subset of `all` that the
// agent's metadata.deploy_recipes_used field names. If the field is
// absent, empty, or not a string-array, returns `all` unchanged so
// the default behaviour ("agent inherits everything") matches the
// ticket's "filtered to ones referenced by agents[].deploy_recipes_used
// if that field exists, else all".
func filterRecipesByAgentMetadata(metadata map[string]any, all []models.ProjectDeployRecipe) []models.ProjectDeployRecipe {
	raw, ok := metadata["deploy_recipes_used"]
	if !ok {
		return all
	}
	asArr, ok := raw.([]any)
	if !ok {
		return all
	}
	allow := map[string]struct{}{}
	for _, v := range asArr {
		if s, ok := v.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				allow[s] = struct{}{}
			}
		}
	}
	// Empty array OR all-empty entries → treat as "no filter declared"
	// so you can declare deploy_recipes_used: [] without accidentally
	// hiding everything; you'd remove the key to opt out, not pass
	// an empty array.
	if len(allow) == 0 {
		return all
	}
	out := make([]models.ProjectDeployRecipe, 0, len(all))
	for _, r := range all {
		if _, ok := allow[r.Name]; ok {
			out = append(out, r)
		}
	}
	return out
}

// renderArtifactMarkdown emits a default unstyled markdown rendering
// of the canonical artifact. Sections are skipped when empty so
// PAI-326-era agents (no body, no rules) produce a tight document
// rather than a sea of empty headings.
//
// This is intentionally NOT the production skill format — adapters
// own that. The renderer here is for human inspection.
func renderArtifactMarkdown(a *AgentArtifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Agent: %s — %s\n\n", a.Agent.Name, a.Project.Name)
	if a.Agent.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", a.Agent.Description)
	}

	// Identity block — only emit if we have something to show.
	if a.Agent.SlashCommandName != "" || len(a.Agent.LaneTags) > 0 {
		b.WriteString("## Identity\n\n")
		if a.Agent.SlashCommandName != "" {
			fmt.Fprintf(&b, "- Slash command: `/%s`\n", a.Agent.SlashCommandName)
		}
		if len(a.Agent.LaneTags) > 0 {
			fmt.Fprintf(&b, "- Lane tags: %s\n", strings.Join(a.Agent.LaneTags, ", "))
		}
		b.WriteString("\n")
	}

	if a.Agent.Body != "" {
		b.WriteString("## Body\n\n")
		b.WriteString(strings.TrimSpace(a.Agent.Body))
		b.WriteString("\n\n")
	}

	if len(a.Agent.BootstrapSteps) > 0 {
		b.WriteString("## Bootstrap steps\n\n")
		for i, s := range a.Agent.BootstrapSteps {
			title := strings.TrimSpace(s.Title)
			if title == "" {
				title = fmt.Sprintf("Step %d", i+1)
			}
			fmt.Fprintf(&b, "%d. **%s**\n", i+1, title)
			if cmd := strings.TrimSpace(s.Command); cmd != "" {
				fmt.Fprintf(&b, "   ```sh\n   %s\n   ```\n", cmd)
			}
			if rat := strings.TrimSpace(s.Rationale); rat != "" {
				fmt.Fprintf(&b, "   _%s_\n", rat)
			}
		}
		b.WriteString("\n")
	}

	if len(a.Agent.NonNegotiableRules) > 0 {
		b.WriteString("## Non-negotiable rules\n\n")
		for i, r := range a.Agent.NonNegotiableRules {
			title := strings.TrimSpace(r.Title)
			if title == "" {
				title = fmt.Sprintf("Rule %d", i+1)
			}
			fmt.Fprintf(&b, "- **%s**", title)
			if ref := strings.TrimSpace(r.MemoryRef); ref != "" {
				// memory_ref is unresolved here — PAI-330 will resolve it.
				// Mark it explicitly so adapters / readers can see the
				// reference even pre-resolution.
				fmt.Fprintf(&b, " _(memory_ref: %s)_", ref)
			}
			b.WriteString("\n")
			if body := strings.TrimSpace(r.Body); body != "" {
				for _, line := range strings.Split(body, "\n") {
					fmt.Fprintf(&b, "  %s\n", line)
				}
			}
		}
		b.WriteString("\n")
	}

	if len(a.Repos) > 0 {
		b.WriteString("## Repos\n\n")
		for _, r := range a.Repos {
			label := r.Label
			if label == "" {
				label = r.URL
			}
			if r.DefaultBranch != "" {
				fmt.Fprintf(&b, "- **%s** — %s (`%s`)\n", label, r.URL, r.DefaultBranch)
			} else {
				fmt.Fprintf(&b, "- **%s** — %s\n", label, r.URL)
			}
		}
		b.WriteString("\n")
	}

	if len(a.Environments) > 0 {
		b.WriteString("## Environments\n\n")
		for _, e := range a.Environments {
			fmt.Fprintf(&b, "- **%s**", e.Name)
			if e.URL != "" {
				fmt.Fprintf(&b, " — %s", e.URL)
			}
			if e.HostAlias != "" || e.HostIP != "" {
				host := e.HostAlias
				if host == "" {
					host = e.HostIP
				} else if e.HostIP != "" {
					host = host + " (" + e.HostIP + ")"
				}
				fmt.Fprintf(&b, " — host: %s", host)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(a.DeployRecipes) > 0 {
		b.WriteString("## Deploy recipes\n\n")
		for _, rec := range a.DeployRecipes {
			fmt.Fprintf(&b, "### %s\n\n", rec.Name)
			if rec.Summary != "" {
				fmt.Fprintf(&b, "%s\n\n", rec.Summary)
			}
			if rec.Command != "" {
				fmt.Fprintf(&b, "```sh\n%s\n```\n\n", rec.Command)
			}
		}
	}

	return b.String()
}
