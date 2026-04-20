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
	ID                 int64   `json:"id"`
	ProjectID          *int64  `json:"project_id"`
	IssueNumber        int     `json:"issue_number"`
	IssueKey           string  `json:"issue_key"` // computed: project.key + "-" + issue_number
	Type               string  `json:"type"`
	ParentID           *int64  `json:"parent_id"`
	Parent             *Issue  `json:"parent,omitempty"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	AcceptanceCriteria string  `json:"acceptance_criteria"`
	Notes              string  `json:"notes"`
	Status             string  `json:"status"`
	Priority           string  `json:"priority"`
	// Grouping free-text fields (still used for filter/export)
	CostUnit string `json:"cost_unit"`
	Release  string `json:"release"`
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
	GroupState  *string  `json:"group_state"`
	SprintState *string  `json:"sprint_state"`
	JiraID      *string  `json:"jira_id"`
	JiraVersion *string  `json:"jira_version"`
	JiraText    *string  `json:"jira_text"`
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
	TimeLogged  float64 `json:"time_logged"`  // direct time entries on this issue
	TimeRollup  float64 `json:"time_rollup"`  // sum of children's time_total
	TimeTotal   float64 `json:"time_total"`   // override ?? (logged + rollup)
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
}
