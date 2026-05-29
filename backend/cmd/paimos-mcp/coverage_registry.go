package main

type coverageClassification string

const (
	coverageMCPTool              coverageClassification = "mcp_tool"
	coverageCoveredByTool        coverageClassification = "covered_by_tool"
	coverageAgentReadonlyContext coverageClassification = "agent_readonly_context"
	coverageHumanAdminOnly       coverageClassification = "human_admin_only"
	coverageFileOrStream         coverageClassification = "file_or_stream"
	coverageUIInternal           coverageClassification = "ui_internal"
	coverageDangerousDefault     coverageClassification = "dangerous_default"
	coverageDeferredGap          coverageClassification = "deferred_gap"
)

type routeCoverage struct {
	Method         string
	Path           string
	Classification coverageClassification
	Tool           string
	Reason         string
	Ticket         string
}

// routeCoverageRegistry is the first-pass MCP/API coverage registry
// for agent-facing routes. It deliberately classifies intent, not just
// one-tool-per-route parity: some routes are better represented through
// retrieve/schema/context tools, while broad bulk mutations stay out of
// the default MCP surface.
var routeCoverageRegistry = []routeCoverage{
	{Method: "GET", Path: "/api/schema", Classification: coverageMCPTool, Tool: "paimos_schema", Reason: "schema discovery is a direct agent primitive"},
	{Method: "GET", Path: "/api/projects", Classification: coverageMCPTool, Tool: "paimos_project_list", Reason: "project lookup and key resolution"},
	{Method: "POST", Path: "/api/projects", Classification: coverageMCPTool, Tool: "paimos_project_create", Reason: "explicitly scoped bootstrap write"},
	{Method: "POST", Path: "/api/projects/{id}/retrieve", Classification: coverageMCPTool, Tool: "paimos_retrieve", Reason: "primary mixed-context retrieval surface"},
	{Method: "GET", Path: "/api/projects/{id}/graph", Classification: coverageMCPTool, Tool: "paimos_graph", Reason: "bounded project graph traversal"},
	{Method: "GET", Path: "/api/projects/{id}/graph/blast-radius", Classification: coverageMCPTool, Tool: "paimos_blast_radius", Reason: "bounded impact analysis"},
	{Method: "GET", Path: "/api/search", Classification: coverageMCPTool, Tool: "paimos_search", Reason: "global text search"},
	{Method: "GET", Path: "/api/issues/{id}", Classification: coverageMCPTool, Tool: "paimos_issue_get", Reason: "single issue read"},
	{Method: "GET", Path: "/api/issues", Classification: coverageMCPTool, Tool: "paimos_issue_list", Reason: "filtered issue listing"},
	{Method: "POST", Path: "/api/projects/{id}/issues", Classification: coverageMCPTool, Tool: "paimos_issue_create", Reason: "single issue create with enum validation and idempotency"},
	{Method: "PUT", Path: "/api/issues/{id}", Classification: coverageMCPTool, Tool: "paimos_issue_update", Reason: "bounded single issue update"},
	{Method: "POST", Path: "/api/issues/{id}/relations", Classification: coverageMCPTool, Tool: "paimos_relation_add", Reason: "single relation add with enum validation and idempotency"},

	// PAI-506 — project-agent CRUD. get rides on the .json artifact
	// (peeled to .agent); the others map 1:1 to their REST routes.
	{Method: "GET", Path: "/api/projects/{id}/agents", Classification: coverageMCPTool, Tool: "paimos_agent_list", Reason: "list project agents"},
	{Method: "GET", Path: "/api/projects/{id}/agents/{name}.json", Classification: coverageMCPTool, Tool: "paimos_agent_get", Reason: "single agent read via the canonical .json artifact"},
	{Method: "POST", Path: "/api/projects/{id}/agents", Classification: coverageMCPTool, Tool: "paimos_agent_create", Reason: "single agent create with name validation"},
	{Method: "PUT", Path: "/api/projects/{id}/agents/{name}", Classification: coverageMCPTool, Tool: "paimos_agent_update", Reason: "single agent replace (full PUT)"},
	{Method: "DELETE", Path: "/api/projects/{id}/agents/{name}", Classification: coverageMCPTool, Tool: "paimos_agent_delete", Reason: "single agent delete"},

	{Method: "POST", Path: "/api/projects/{key}/issues/batch", Classification: coverageDangerousDefault, Reason: "bulk mutation belongs in CLI/apply flows, not default MCP context"},
	{Method: "PATCH", Path: "/api/issues", Classification: coverageDangerousDefault, Reason: "bulk update is too broad for the default MCP surface"},
	{Method: "POST", Path: "/api/projects/{id}/anchors", Classification: coverageCoveredByTool, Tool: "paimos_retrieve", Reason: "anchors are consumed through retrieve/graph context rather than direct raw ingestion"},
	{Method: "GET", Path: "/api/projects/{id}/knowledge", Classification: coverageAgentReadonlyContext, Tool: "paimos_retrieve", Reason: "knowledge is exposed through retrieve/schema/onboard context in the initial MCP surface"},
	{Method: "POST", Path: "/api/projects/{id}/knowledge", Classification: coverageDeferredGap, Reason: "knowledge authoring needs a dedicated tool design before exposing writes", Ticket: "PAI-492"},
	{Method: "POST", Path: "/api/issues/{id}/attachments", Classification: coverageFileOrStream, Reason: "multipart upload is not suitable for text-only MCP tools by default"},
	{Method: "GET", Path: "/api/projects/{id}/export/csv", Classification: coverageFileOrStream, Reason: "file export route"},
	{Method: "GET", Path: "/api/auth/api-keys", Classification: coverageHumanAdminOnly, Reason: "security administration stays out of MCP default tools"},
}

func routeCoverageKey(method, path string) string {
	return method + " " + path
}
