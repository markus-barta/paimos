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
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/markus-barta/paimos/backend/handlers"
)

// Tool is the MCP-facing shape for one tool declaration: name, human
// description, and a JSON Schema for the expected arguments.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	handler     func(args map[string]any) (string, error)
}

func stringEnum(domain string) map[string]any {
	values := handlers.Schema.Enums[domain]
	return map[string]any{
		"type": "string",
		"enum": append([]string(nil), values...),
	}
}

func stringEnumForBinding(binding string) map[string]any {
	return stringEnum(enumDomainForBinding(binding))
}

func enumDomainForBinding(binding string) string {
	if domain := handlers.Schema.EnumFields[binding]; domain != "" {
		return domain
	}
	return binding
}

func validateStringEnum(field, binding, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	domain := enumDomainForBinding(binding)
	values := handlers.Schema.Enums[domain]
	for _, value := range values {
		if raw == value {
			return nil
		}
	}
	return fmt.Errorf("%s %q is not valid; expected one of: %s", field, raw, strings.Join(values, ", "))
}

// handleToolsList returns the v1 allowlist from the pickup doc:
// paimos_schema, paimos_issue_get, _list, _create, _update,
// _relation_add, plus the PAI-506 project-agent CRUD family
// paimos_agent_list / _get / _create / _update / _delete.
// Deliberately NO batch-update / apply — MCP context grows fast and
// these belong in the CLI.
func (s *Server) handleToolsList() any {
	tools := s.tools()
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return map[string]any{"tools": out}
}

// handleToolsCall looks up and runs a named tool. Errors from the
// tool body become MCP isError responses so the agent sees what went
// wrong without the JSON-RPC envelope eating the message.
func (s *Server) handleToolsCall(raw json.RawMessage) (any, *rpcError) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	tools := s.tools()
	for _, t := range tools {
		if t.Name == params.Name {
			result, err := t.handler(params.Arguments)
			if err != nil {
				return toolTextResult(err.Error(), true), nil
			}
			return toolTextResult(result, false), nil
		}
	}
	return nil, &rpcError{
		Code:    codeMethodNotFound,
		Message: "unknown tool: " + params.Name,
	}
}

// toolTextResult builds an MCP CallToolResult with a single text
// content block. Errors are flagged via isError=true so agents can
// distinguish them from normal output.
func toolTextResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	}
}

// tools returns the v1 tool set. Recomputed each call so any handler
// change is picked up without restart (cheap — these are pointers to
// methods).
func (s *Server) tools() []Tool {
	return []Tool{
		{
			Name:        "paimos_retrieve",
			Description: "Retrieve mixed project context hits for one PAIMOS project.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q":       map[string]any{"type": "string"},
					"project": map[string]any{"type": "string"},
					"k":       map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
				},
				"required": []string{"q"},
			},
			handler: s.toolProjectRetrieve,
		},
		{
			Name:        "paimos_graph",
			Description: "Traverse the typed project graph from a root entity.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root":    map[string]any{"type": "string"},
					"project": map[string]any{"type": "string"},
					"depth":   map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
				},
				"required": []string{"root"},
			},
			handler: s.toolProjectGraph,
		},
		{
			Name:        "paimos_blast_radius",
			Description: "Return grouped blast-radius results for an issue in one project.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue":   map[string]any{"type": "string"},
					"project": map[string]any{"type": "string"},
					"depth":   map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
				},
				"required": []string{"issue"},
			},
			handler: s.toolBlastRadius,
		},
		{
			Name:        "paimos_search",
			Description: "Run the global PAIMOS search endpoint.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q": map[string]any{"type": "string"},
				},
				"required": []string{"q"},
			},
			handler: s.toolSearch,
		},
		{
			Name:        "paimos_schema",
			Description: "Returns the PAIMOS API schema (enums, transitions, entity shapes). Use this before choosing status/type/priority values to avoid typos.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			handler: s.toolSchema,
		},
		{
			Name:        "paimos_issue_get",
			Description: "Fetches a single issue by key (e.g. PAI-83) or numeric id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref": map[string]any{
						"type":        "string",
						"description": "Issue key (PAI-83) or numeric id",
					},
				},
				"required": []string{"ref"},
			},
			handler: s.toolIssueGet,
		},
		{
			Name:        "paimos_issue_list",
			Description: "Lists issues with optional filters. Use project_key, status, type, priority. Returns up to 100 per call.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key": map[string]any{"type": "string"},
					"status":      stringEnumForBinding("issue.status"),
					"type":        stringEnumForBinding("issue.type"),
					"priority":    stringEnumForBinding("issue.priority"),
					"limit":       map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			handler: s.toolIssueList,
		},
		{
			Name:        "paimos_issue_create",
			Description: "Creates one issue. title + project_key required; all other fields optional. Markdown fields (description, acceptance_criteria, notes) are passed verbatim.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key":         map[string]any{"type": "string"},
					"title":               map[string]any{"type": "string"},
					"type":                stringEnumForBinding("issue.type"),
					"status":              stringEnumForBinding("issue.status"),
					"priority":            stringEnumForBinding("issue.priority"),
					"description":         map[string]any{"type": "string"},
					"acceptance_criteria": map[string]any{"type": "string"},
					"notes":               map[string]any{"type": "string"},
					"parent":              map[string]any{"type": "string", "description": "parent ref (key or id)"},
				},
				"required": []string{"project_key", "title"},
			},
			handler: s.toolIssueCreate,
		},
		{
			// PAI-379: agent-accessible project listing. Mirrors `paimos
			// project list` so agents can confirm a key exists / fetch
			// project metadata without falling back to /api/projects raw.
			Name:        "paimos_project_list",
			Description: "Lists projects on the active instance. Returns array of { id, key, name, description, ... } sorted by name.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			handler: s.toolProjectList,
		},
		{
			// PAI-379: agent-accessible project bootstrap. Requires an
			// api-key with the `projects:write` scope on an admin-owned
			// account (catalog rule, enforced server-side). The
			// previously hand-off-to-the-UI step is now end-to-end.
			Name:        "paimos_project_create",
			Description: "Creates a new project. name + key required; optional description. Requires an api-key with `projects:write` scope on an admin account (see paimos_schema → scopes).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string"},
					"key":         map[string]any{"type": "string", "description": "short project key, e.g. PAI"},
					"description": map[string]any{"type": "string"},
				},
				"required": []string{"name", "key"},
			},
			handler: s.toolProjectCreate,
		},
		{
			Name:        "paimos_issue_update",
			Description: "Partial-updates one issue. Provide ref + only the fields to change.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref":                 map[string]any{"type": "string"},
					"title":               map[string]any{"type": "string"},
					"type":                stringEnumForBinding("issue.type"),
					"status":              stringEnumForBinding("issue.status"),
					"priority":            stringEnumForBinding("issue.priority"),
					"description":         map[string]any{"type": "string"},
					"acceptance_criteria": map[string]any{"type": "string"},
					"notes":               map[string]any{"type": "string"},
				},
				"required": []string{"ref"},
			},
			handler: s.toolIssueUpdate,
		},
		{
			Name:        "paimos_relation_add",
			Description: "Adds a relation between two issues (convention: source = container/parent, target = member/child). Types: parent (issue hierarchy epic⊃ticket, ticket⊃task — the SSOT for parentage; source=parent, target=child, one parent per child), cost_unit / release (container membership: source = cost_unit/release container issue, target = ticket — the SSOT for those dimensions), groups (legacy generic membership, being retired), sprint, depends_on, impacts, follows_from, blocks, related. To place a ticket under an epic use type=parent (source=epic, target=ticket); legacy type=groups with an epic source is auto-translated to parent.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{"type": "string", "description": "source issue ref"},
					"type":   stringEnumForBinding("relation.type"),
					"target": map[string]any{"type": "string", "description": "target issue ref"},
				},
				"required": []string{"source", "type", "target"},
			},
			handler: s.toolRelationAdd,
		},
		{
			// PAI-506 — project-agent CRUD. Agents are project-scoped
			// (project_key resolves to the numeric id used in the path).
			Name:        "paimos_agent_list",
			Description: "Lists agents declared on a project. Returns array of agent records (id, name, description, slash_command_name, lane_tags, metadata, body, bootstrap_steps, non_negotiable_rules), sorted by name. Empty array when none.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key": map[string]any{"type": "string"},
				},
				"required": []string{"project_key"},
			},
			handler: s.toolAgentList,
		},
		{
			Name:        "paimos_agent_get",
			Description: "Fetches a single project agent by name. Returns the agent record alone (the project/repos/environments artifact wrapper is dropped).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key": map[string]any{"type": "string"},
					"name":        map[string]any{"type": "string", "description": "agent name (lowercase slug)"},
				},
				"required": []string{"project_key", "name"},
			},
			handler: s.toolAgentGet,
		},
		{
			Name:        "paimos_agent_create",
			Description: "Creates one project agent. project_key + name required; name is a lowercase slug ([a-z][a-z0-9_-]*, max 32; 'web-ui' reserved). Requires an api-key with admin + project-view scope.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key":          map[string]any{"type": "string"},
					"name":                 map[string]any{"type": "string"},
					"description":          map[string]any{"type": "string"},
					"slash_command_name":   map[string]any{"type": "string"},
					"lane_tags":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"metadata":             map[string]any{"type": "object"},
					"body":                 map[string]any{"type": "string"},
					"bootstrap_steps":      agentBootstrapStepsSchema(),
					"non_negotiable_rules": agentRulesSchema(),
				},
				"required": []string{"project_key", "name"},
			},
			handler: s.toolAgentCreate,
		},
		{
			Name:        "paimos_agent_update",
			Description: "Replaces a project agent in-place (full PUT). project_key + name (current name) required; pass `name` again to rename. Fields omitted are sent as the JSON zero value — fetch first with paimos_agent_get if you need to preserve them.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key":          map[string]any{"type": "string"},
					"name":                 map[string]any{"type": "string", "description": "current agent name; also the rename target"},
					"description":          map[string]any{"type": "string"},
					"slash_command_name":   map[string]any{"type": "string"},
					"lane_tags":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"metadata":             map[string]any{"type": "object"},
					"body":                 map[string]any{"type": "string"},
					"bootstrap_steps":      agentBootstrapStepsSchema(),
					"non_negotiable_rules": agentRulesSchema(),
				},
				"required": []string{"project_key", "name"},
			},
			handler: s.toolAgentUpdate,
		},
		{
			Name:        "paimos_agent_delete",
			Description: "Deletes a project agent by name. Returns a success message; 404 if no agent matched.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key": map[string]any{"type": "string"},
					"name":        map[string]any{"type": "string"},
				},
				"required": []string{"project_key", "name"},
			},
			handler: s.toolAgentDelete,
		},
	}
}

// agentBootstrapStepsSchema is the JSON-schema array shape for the
// agent bootstrap_steps field ([]{title,command,rationale}).
func agentBootstrapStepsSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":     map[string]any{"type": "string"},
				"command":   map[string]any{"type": "string"},
				"rationale": map[string]any{"type": "string"},
			},
			"required": []string{"title", "command"},
		},
	}
}

// agentRulesSchema is the JSON-schema array shape for the agent
// non_negotiable_rules field ([]{title,body,memory_ref}).
func agentRulesSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":      map[string]any{"type": "string"},
				"body":       map[string]any{"type": "string"},
				"memory_ref": map[string]any{"type": "string"},
			},
			"required": []string{"title", "body"},
		},
	}
}

// toolSchema → GET /api/schema.
func (s *Server) toolSchema(args map[string]any) (string, error) {
	raw, err := s.client.Do("GET", "/api/schema", nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolIssueGet → GET /api/issues/{ref}.
func (s *Server) toolIssueGet(args map[string]any) (string, error) {
	ref, _ := args["ref"].(string)
	if ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	raw, err := s.client.Do("GET", "/api/issues/"+url.PathEscape(ref), nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolIssueList → GET /api/issues with optional filters.
func (s *Server) toolIssueList(args map[string]any) (string, error) {
	q := url.Values{}
	if pk, _ := args["project_key"].(string); pk != "" {
		// Server's /issues endpoint uses project_ids (numeric). Resolve.
		projectsRaw, err := s.client.Do("GET", "/api/projects", nil)
		if err != nil {
			return "", fmt.Errorf("resolve project_key: %w", err)
		}
		var list []struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		}
		if err := json.Unmarshal(projectsRaw, &list); err != nil {
			return "", err
		}
		found := false
		for _, p := range list {
			if p.Key == pk {
				q.Set("project_ids", fmt.Sprintf("%d", p.ID))
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("project_key %q not found", pk)
		}
	}
	for _, k := range []string{"status", "type", "priority"} {
		if v, _ := args[k].(string); v != "" {
			if err := validateStringEnum(k, "issue."+k, v); err != nil {
				return "", err
			}
			q.Set(k, v)
		}
	}
	if l, ok := args["limit"].(float64); ok && l > 0 {
		q.Set("limit", fmt.Sprintf("%d", int(l)))
	} else {
		q.Set("limit", "50")
	}
	path := "/api/issues"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	raw, err := s.client.Do("GET", path, nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolProjectList → GET /api/projects.
func (s *Server) toolProjectList(args map[string]any) (string, error) {
	raw, err := s.client.Do("GET", "/api/projects", nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolProjectCreate → POST /api/projects.
// PAI-379: name + key required. The server requires admin role on the
// underlying user AND the api-key must carry `projects:write`. Missing
// either gate returns a 403 the MCP surfaces as an isError result.
func (s *Server) toolProjectCreate(args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("name is required")
	}
	key, _ := args["key"].(string)
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("key is required")
	}
	body := map[string]any{"name": name, "key": key}
	if desc, _ := args["description"].(string); desc != "" {
		body["description"] = desc
	}
	raw, err := s.client.Do("POST", "/api/projects", body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolIssueCreate → POST /api/projects/{key}/issues.
func (s *Server) toolIssueCreate(args map[string]any) (string, error) {
	projectKey, _ := args["project_key"].(string)
	if projectKey == "" {
		return "", fmt.Errorf("project_key is required")
	}
	title, _ := args["title"].(string)
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("title is required")
	}
	body := map[string]any{"title": title}
	for _, k := range []string{"type", "status", "priority", "description", "acceptance_criteria", "notes"} {
		if v, _ := args[k].(string); v != "" {
			switch k {
			case "type":
				if err := validateStringEnum("type", "issue.type", v); err != nil {
					return "", err
				}
			case "status":
				if err := validateStringEnum("status", "issue.status", v); err != nil {
					return "", err
				}
			case "priority":
				if err := validateStringEnum("priority", "issue.priority", v); err != nil {
					return "", err
				}
			}
			body[k] = v
		}
	}
	if parent, _ := args["parent"].(string); parent != "" {
		// Resolve to numeric id via GET /api/issues/{ref}.
		raw, err := s.client.Do("GET", "/api/issues/"+url.PathEscape(parent), nil)
		if err != nil {
			return "", fmt.Errorf("resolve parent %q: %w", parent, err)
		}
		var iss struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &iss); err != nil {
			return "", err
		}
		body["parent_id"] = iss.ID
	}
	// Resolve project key → id (CreateIssue uses numeric id in path).
	projectsRaw, err := s.client.Do("GET", "/api/projects", nil)
	if err != nil {
		return "", fmt.Errorf("list projects: %w", err)
	}
	var plist []struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(projectsRaw, &plist); err != nil {
		return "", err
	}
	var pid int64
	for _, p := range plist {
		if p.Key == projectKey {
			pid = p.ID
			break
		}
	}
	if pid == 0 {
		return "", fmt.Errorf("project_key %q not found", projectKey)
	}
	raw, err := s.client.Do("POST", fmt.Sprintf("/api/projects/%d/issues", pid), body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Server) toolProjectRetrieve(args map[string]any) (string, error) {
	q, _ := args["q"].(string)
	if strings.TrimSpace(q) == "" {
		return "", fmt.Errorf("q is required")
	}
	projectID, err := s.resolveProjectID(args)
	if err != nil {
		return "", err
	}
	k := 20
	if raw, ok := args["k"].(float64); ok && raw > 0 {
		k = int(raw)
	}
	raw, err := s.client.Do("POST", fmt.Sprintf("/api/projects/%d/retrieve", projectID), map[string]any{"q": q, "k": k})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Server) toolProjectGraph(args map[string]any) (string, error) {
	root, _ := args["root"].(string)
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("root is required")
	}
	projectID, err := s.resolveProjectID(args)
	if err != nil {
		return "", err
	}
	depth := 2
	if raw, ok := args["depth"].(float64); ok && raw > 0 {
		depth = int(raw)
	}
	path := fmt.Sprintf("/api/projects/%d/graph?root=%s&depth=%d", projectID, url.QueryEscape(root), depth)
	raw, err := s.client.Do("GET", path, nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Server) toolBlastRadius(args map[string]any) (string, error) {
	issue, _ := args["issue"].(string)
	if strings.TrimSpace(issue) == "" {
		return "", fmt.Errorf("issue is required")
	}
	projectID, err := s.resolveProjectID(args)
	if err != nil {
		return "", err
	}
	depth := 3
	if raw, ok := args["depth"].(float64); ok && raw > 0 {
		depth = int(raw)
	}
	path := fmt.Sprintf("/api/projects/%d/graph/blast-radius?issue=%s&depth=%d", projectID, url.QueryEscape(issue), depth)
	raw, err := s.client.Do("GET", path, nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Server) toolSearch(args map[string]any) (string, error) {
	q, _ := args["q"].(string)
	if strings.TrimSpace(q) == "" {
		return "", fmt.Errorf("q is required")
	}
	raw, err := s.client.Do("GET", "/api/search?q="+url.QueryEscape(q), nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Server) resolveProjectID(args map[string]any) (int64, error) {
	if project, _ := args["project"].(string); strings.TrimSpace(project) != "" {
		return s.lookupProjectID(project)
	}
	if project := strings.TrimSpace(os.Getenv("PAIMOS_PROJECT")); project != "" {
		return s.lookupProjectID(project)
	}
	remote, err := gitRemoteURL()
	if err != nil {
		return 0, fmt.Errorf("project not specified and git remote detection failed: %w", err)
	}
	projectsRaw, err := s.client.Do("GET", "/api/projects", nil)
	if err != nil {
		return 0, err
	}
	var projects []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		return 0, err
	}
	want := normalizeRepoURL(remote)
	for _, project := range projects {
		reposRaw, err := s.client.Do("GET", fmt.Sprintf("/api/projects/%d/repos", project.ID), nil)
		if err != nil {
			continue
		}
		var repos []struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(reposRaw, &repos) != nil {
			continue
		}
		for _, repo := range repos {
			if normalizeRepoURL(repo.URL) == want {
				return project.ID, nil
			}
		}
	}
	return 0, fmt.Errorf("could not infer project from git remote %q", remote)
}

func (s *Server) lookupProjectID(ref string) (int64, error) {
	projectsRaw, err := s.client.Do("GET", "/api/projects", nil)
	if err != nil {
		return 0, err
	}
	var projects []struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		return 0, err
	}
	for _, project := range projects {
		if project.Key == ref {
			return project.ID, nil
		}
	}
	return 0, fmt.Errorf("project %q not found", ref)
}

func gitRemoteURL() (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir, _ = os.Getwd()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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

// toolIssueUpdate → PUT /api/issues/{ref}.
func (s *Server) toolIssueUpdate(args map[string]any) (string, error) {
	ref, _ := args["ref"].(string)
	if ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	body := map[string]any{}
	for _, k := range []string{"title", "type", "status", "priority", "description", "acceptance_criteria", "notes"} {
		if v, ok := args[k].(string); ok {
			switch k {
			case "type":
				if err := validateStringEnum("type", "issue.type", v); err != nil {
					return "", err
				}
			case "status":
				if err := validateStringEnum("status", "issue.status", v); err != nil {
					return "", err
				}
			case "priority":
				if err := validateStringEnum("priority", "issue.priority", v); err != nil {
					return "", err
				}
			}
			body[k] = v
		}
	}
	if len(body) == 0 {
		return "", fmt.Errorf("no fields to update")
	}
	raw, err := s.client.Do("PUT", "/api/issues/"+url.PathEscape(ref), body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolRelationAdd → POST /api/issues/{source}/relations.
func (s *Server) toolRelationAdd(args map[string]any) (string, error) {
	src, _ := args["source"].(string)
	typ, _ := args["type"].(string)
	tgt, _ := args["target"].(string)
	if src == "" || typ == "" || tgt == "" {
		return "", fmt.Errorf("source, type, target all required")
	}
	if err := validateStringEnum("type", "relation.type", typ); err != nil {
		return "", err
	}
	// Resolve target ref → numeric id.
	tgtRaw, err := s.client.Do("GET", "/api/issues/"+url.PathEscape(tgt), nil)
	if err != nil {
		return "", fmt.Errorf("resolve target %q: %w", tgt, err)
	}
	var iss struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(tgtRaw, &iss); err != nil {
		return "", err
	}
	raw, err := s.client.Do("POST",
		"/api/issues/"+url.PathEscape(src)+"/relations",
		map[string]any{"target_id": iss.ID, "type": typ},
	)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ── PAI-506: project-agent CRUD ─────────────────────────────────────

// agentProjectID resolves the required project_key arg to a numeric id.
func (s *Server) agentProjectID(args map[string]any) (int64, error) {
	projectKey, _ := args["project_key"].(string)
	if strings.TrimSpace(projectKey) == "" {
		return 0, fmt.Errorf("project_key is required")
	}
	return s.lookupProjectID(strings.TrimSpace(projectKey))
}

// agentWriteBody copies the agent write fields from the tool args into a
// request body, preserving JSON types (arrays/objects pass through
// unchanged since the MCP args are already decoded JSON). `name` is set
// by the caller (it's required) so this only handles the optional rest.
func agentWriteBody(args map[string]any) map[string]any {
	body := map[string]any{}
	for _, k := range []string{"description", "slash_command_name", "body"} {
		if v, ok := args[k].(string); ok {
			body[k] = v
		}
	}
	for _, k := range []string{"lane_tags", "metadata", "bootstrap_steps", "non_negotiable_rules"} {
		if v, ok := args[k]; ok {
			body[k] = v
		}
	}
	return body
}

// toolAgentList → GET /api/projects/{id}/agents.
func (s *Server) toolAgentList(args map[string]any) (string, error) {
	projectID, err := s.agentProjectID(args)
	if err != nil {
		return "", err
	}
	raw, err := s.client.Do("GET", fmt.Sprintf("/api/projects/%d/agents", projectID), nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolAgentGet → GET /api/projects/{id}/agents/{name}.json, returning
// the `.agent` sub-object (there is no plain GET for a single agent).
func (s *Server) toolAgentGet(args map[string]any) (string, error) {
	projectID, err := s.agentProjectID(args)
	if err != nil {
		return "", err
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("name is required")
	}
	raw, err := s.client.Do("GET", fmt.Sprintf("/api/projects/%d/agents/%s.json", projectID, url.PathEscape(name)), nil)
	if err != nil {
		return "", err
	}
	var artifact struct {
		Agent json.RawMessage `json:"agent"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return "", fmt.Errorf("decode agent artifact: %w", err)
	}
	if len(artifact.Agent) == 0 {
		return "", fmt.Errorf("agent artifact missing `agent` field")
	}
	return string(artifact.Agent), nil
}

// toolAgentCreate → POST /api/projects/{id}/agents.
func (s *Server) toolAgentCreate(args map[string]any) (string, error) {
	projectID, err := s.agentProjectID(args)
	if err != nil {
		return "", err
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("name is required")
	}
	body := agentWriteBody(args)
	body["name"] = name
	raw, err := s.client.Do("POST", fmt.Sprintf("/api/projects/%d/agents", projectID), body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolAgentUpdate → PUT /api/projects/{id}/agents/{name}.
func (s *Server) toolAgentUpdate(args map[string]any) (string, error) {
	projectID, err := s.agentProjectID(args)
	if err != nil {
		return "", err
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("name is required")
	}
	body := agentWriteBody(args)
	// The server treats the URL {name} as the current identifier and the
	// body `name` as the (optional) rename target; default it to the URL
	// name so a no-rename update keeps the same slug.
	body["name"] = name
	raw, err := s.client.Do("PUT", fmt.Sprintf("/api/projects/%d/agents/%s", projectID, url.PathEscape(name)), body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolAgentDelete → DELETE /api/projects/{id}/agents/{name}.
func (s *Server) toolAgentDelete(args map[string]any) (string, error) {
	projectID, err := s.agentProjectID(args)
	if err != nil {
		return "", err
	}
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("name is required")
	}
	if _, err := s.client.Do("DELETE", fmt.Sprintf("/api/projects/%d/agents/%s", projectID, url.PathEscape(name)), nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("deleted agent %q on project %d", name, projectID), nil
}
