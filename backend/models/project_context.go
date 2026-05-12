package models

// ProjectAgent is a declarable agent attached to a project (PAI-326,
// extended in PAI-329 for skill scaffolding generation).
//
// `name` is the canonical key — slug-style, project-scoped unique.
// The PAI-326 base (description, slash_command_name, lane_tags,
// metadata) covers attribution. PAI-329 adds the rendering shape:
//
//   - Body: free-text markdown — the bulk of the rendered skill body.
//   - BootstrapSteps: ordered list of {title, command, rationale}
//     ("do these once at session start" actions).
//   - NonNegotiableRules: ordered list of {title, body, memory_ref}.
//     memory_ref is a free string here; resolution into an actual
//     memory entry happens at render time (PAI-330) — pass-through.
//
// All structured-list columns are stored as JSON blobs in TEXT and
// exposed as native shapes here. Empty arrays / blank body round-trip
// as []/"" (never null) so consumers can iterate safely.
type ProjectAgent struct {
	ID                 int64                `json:"id"`
	ProjectID          int64                `json:"project_id"`
	Name               string               `json:"name"`
	Description        string               `json:"description"`
	SlashCommandName   string               `json:"slash_command_name"`
	LaneTags           []string             `json:"lane_tags"`
	Metadata           map[string]any       `json:"metadata"`
	Body               string               `json:"body"`
	BootstrapSteps     []AgentBootstrapStep `json:"bootstrap_steps"`
	NonNegotiableRules []AgentRule          `json:"non_negotiable_rules"`
	CreatedAt          string               `json:"created_at"`
	UpdatedAt          string               `json:"updated_at"`
}

// AgentBootstrapStep is a single ordered step the agent should run
// at session start (e.g. probing environment, sourcing secrets).
// Title is the human-readable label, command is the shell text the
// agent should execute, rationale explains why (renders as a comment
// in the produced skill markdown).
type AgentBootstrapStep struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Rationale string `json:"rationale"`
}

// AgentRule is one of the agent's non-negotiable rules. Title is the
// short headline ("Don't push to main without PR"), Body is the
// expanded explanation, MemoryRef is an optional pointer into the
// project's memory inventory (resolved at render time by PAI-330).
type AgentRule struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	MemoryRef string `json:"memory_ref"`
}

// ProjectEnvironment is one of the project's deployment environments
// (PAI-329). Shared across agents so all rendered artifacts reference
// the same canonical truth.
type ProjectEnvironment struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	HostAlias string `json:"host_alias"`
	HostIP    string `json:"host_ip"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ProjectDeployRecipe is a named, reusable deployment command (PAI-329).
// Agents reference recipes by name (via agents[].metadata or body
// references) rather than copy-pasting the command text.
type ProjectDeployRecipe struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Summary   string `json:"summary"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ProjectRepo struct {
	ID            int64  `json:"id"`
	ProjectID     int64  `json:"project_id"`
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
	Label         string `json:"label"`
	SortOrder     int    `json:"sort_order"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type IssueAnchor struct {
	ID            int64   `json:"id"`
	ProjectID     int64   `json:"project_id"`
	IssueID       int64   `json:"issue_id"`
	IssueKey      string  `json:"issue_key,omitempty"`
	RepoID        int64   `json:"repo_id"`
	RepoLabel     string  `json:"repo_label"`
	RepoURL       string  `json:"repo_url"`
	DefaultBranch string  `json:"default_branch"`
	FilePath      string  `json:"file_path"`
	Line          int     `json:"line"`
	Label         string  `json:"label"`
	Confidence    string  `json:"confidence"`
	SymbolJSON    string  `json:"symbol_json"`
	SchemaVersion string  `json:"schema_version"`
	RepoRevision  string  `json:"repo_revision"`
	GeneratedAt   string  `json:"generated_at"`
	Hidden        bool    `json:"hidden"`
	Stale         bool    `json:"stale"`
	DeepLink      *string `json:"deep_link,omitempty"`
	UpdatedAt     string  `json:"updated_at"`
}

type ProjectManifest struct {
	ProjectID int64  `json:"project_id"`
	Data      any    `json:"data"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy *int64 `json:"updated_by"`
}

type EntityRelation struct {
	ID         int64  `json:"id"`
	ProjectID  int64  `json:"project_id"`
	SourceType string `json:"source_type"`
	SourceID   int64  `json:"source_id"`
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	EdgeType   string `json:"edge_type"`
	Confidence string `json:"confidence"`
	Metadata   string `json:"metadata"`
	CreatedAt  string `json:"created_at"`
}
