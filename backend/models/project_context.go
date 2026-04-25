package models

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
	ProjectID  int64  `json:"project_id"`
	Data       any    `json:"data"`
	UpdatedAt  string `json:"updated_at"`
	UpdatedBy  *int64 `json:"updated_by"`
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
