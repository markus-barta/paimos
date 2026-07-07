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

package models

type Issue struct {
	ID                 int64  `json:"id"`
	ProjectID          *int64 `json:"project_id"`
	IssueNumber        int    `json:"issue_number"`
	IssueKey           string `json:"issue_key"` // computed: project.key + "-" + issue_number
	Type               string `json:"type"`
	ParentID           *int64 `json:"parent_id"`
	Parent             *Issue `json:"parent,omitempty"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Notes              string `json:"notes"`
	// PAI-418: customer-facing report-text used by Projektbericht.
	// One field; the audience style (warm customer copy vs technical
	// exec TL;DR) is picked at AI-generation time.
	ReportSummary string `json:"report_summary"`
	Status        string `json:"status"`
	Priority      string `json:"priority"`
	// PAI-599: cost_unit/release are edge-sourced references to their
	// container issue ({id,label}), not free-text columns. nil when the
	// issue carries no such edge. Set via a string label on create/update
	// (the backend resolves/creates the container); returned as an object.
	CostUnit *LabelRef `json:"cost_unit"`
	Release  *LabelRef `json:"release"`
	// v2 group/sprint fields (nullable; only meaningful on epic/cost_unit/release/sprint)
	BillingType *string  `json:"billing_type"`
	TotalBudget *float64 `json:"total_budget"`
	RateHourly  *float64 `json:"rate_hourly"`
	RateLp      *float64 `json:"rate_lp"`
	StartDate   *string  `json:"start_date"`
	EndDate     *string  `json:"end_date"`
	// Estimation + AR fields (nullable)
	EstimateHours *float64 `json:"estimate_hours"`
	EstimateLp    *float64 `json:"estimate_lp"`
	ArHours       *float64 `json:"ar_hours"`
	ArLp          *float64 `json:"ar_lp"`
	TimeOverride  *float64 `json:"time_override"`
	GroupState    *string  `json:"group_state"`
	SprintState   *string  `json:"sprint_state"`
	JiraID        *string  `json:"jira_id"`
	JiraVersion   *string  `json:"jira_version"`
	JiraText      *string  `json:"jira_text"`
	// epic color — optional visual accent for epic badges
	Color *string `json:"color"`
	// sprint membership — IDs of sprints this issue belongs to (source_id in issue_relations type=sprint)
	SprintIDs []int64 `json:"sprint_ids"`
	// archived flag — used for sprints
	Archived bool `json:"archived"`
	// relations
	AssigneeID *int64  `json:"assignee_id"`
	Assignee   *User   `json:"assignee,omitempty"`
	Children   []Issue `json:"children,omitempty"`
	Tags       []Tag   `json:"tags"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	// creator
	CreatedBy     *int64 `json:"created_by"`
	CreatedByName string `json:"created_by_name,omitempty"`
	// last editor — derived from most recent issue_history entry
	LastChangedByName string `json:"last_changed_by_name,omitempty"`
	// billing lifecycle
	AcceptedAt    *string `json:"accepted_at"`
	AcceptedBy    *int64  `json:"accepted_by"`
	InvoicedAt    *string `json:"invoiced_at"`
	InvoiceNumber string  `json:"invoice_number"`
	// soft-delete: non-NULL on issues that live in the Trash.
	DeletedAt     *string `json:"deleted_at,omitempty"`
	DeletedBy     *int64  `json:"deleted_by,omitempty"`
	DeletedByName string  `json:"deleted_by_name,omitempty"`
	// computed: SUM of time entry hours (override or stopped_at - started_at)
	BookedHours float64 `json:"booked_hours"`
	// computed: budget in hours (estimate_hours or estimate_lp * rate conversion)
	BudgetHours *float64 `json:"budget_hours"`
	// Time tracking 4-field model
	TimeLogged float64 `json:"time_logged"` // direct time entries on this issue
	TimeRollup float64 `json:"time_rollup"` // sum of children's time_total
	TimeTotal  float64 `json:"time_total"`  // override ?? (logged + rollup)
	// Latest "Implement this" work state, when any agent run exists.
	AIWorkStatus *IssueAIWorkStatus `json:"ai_work_status,omitempty"`
}

type IssueAIWorkStatus struct {
	ID              int64   `json:"id"`
	Status          string  `json:"status"`
	AgentName       string  `json:"agent_name"`
	DeviceID        string  `json:"device_id"`
	ActionKey       string  `json:"action_key"`
	ProviderKind    string  `json:"provider_kind"`
	ProviderID      string  `json:"provider_id"`
	ProviderLabel   string  `json:"provider_label"`
	Model           string  `json:"model"`
	RunMode         string  `json:"run_mode"`
	ProfileID       string  `json:"profile_id"`
	Effort          string  `json:"effort"`
	PromptPresetRef string  `json:"prompt_preset_ref"`
	ContextPack     string  `json:"context_pack"`
	Version         string  `json:"version"`
	DeployTarget    string  `json:"deploy_target"`
	TestsSummary    *string `json:"tests_summary"`
	Error           string  `json:"error"`
	CreatedAt       string  `json:"created_at"`
	StartedAt       *string `json:"started_at"`
	FinishedAt      *string `json:"finished_at"`
}

// LabelRef is the edge-sourced representation of an issue's cost_unit or
// release (PAI-599): the container issue's id + its title. Replaces the
// former free-text string columns; nil when the issue has no such edge.
type LabelRef struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

// IssueRelation represents a row in issue_relations.
// Convention: source_id = container/owner, target_id = member/child.
// For sprint:     source = sprint issue, target = member issue.
// For groups:     source = epic/cost_unit/release, target = ticket.
// For depends_on: source = dependent issue, target = dependency.
// For impacts:    source = impacting issue, target = impacted issue.
type IssueRelation struct {
	SourceID    int64  `json:"source_id"`
	TargetID    int64  `json:"target_id"`
	Type        string `json:"type"`
	TargetKey   string `json:"target_key,omitempty"`
	TargetTitle string `json:"target_title,omitempty"`
	// Direction is "outgoing" when the issue named in the request URL
	// is this relation's source_id, "incoming" otherwise. Lets the UI
	// render inverse labels (e.g. "follows up on X" vs "followed up by Y")
	// without a second DB row. Added in PAI-89.
	Direction string `json:"direction,omitempty"`
}
