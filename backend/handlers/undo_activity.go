package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

type undoMode string

const (
	undoModeUndo undoMode = "undo"
	undoModeRedo undoMode = "redo"
)

type undoConflictOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

type undoFieldConflict struct {
	Pattern      string               `json:"pattern"`
	Field        string               `json:"field"`
	TheirValue   any                  `json:"their_value"`
	TargetValue  any                  `json:"target_value"`
	CurrentValue any                  `json:"current_value"`
	Options      []undoConflictOption `json:"options"`
}

type undoCascadeBlocker struct {
	Pattern     string               `json:"pattern"`
	TargetID    int64                `json:"target_id,omitempty"`
	Description string               `json:"description"`
	Options     []undoConflictOption `json:"options"`
}

type undoConflictResponse struct {
	Status            string               `json:"status"`
	LogID             int64                `json:"log_id"`
	RequestID         string               `json:"request_id"`
	Mode              string               `json:"mode"`
	MutationType      string               `json:"mutation_type"`
	Conflicts         []undoFieldConflict  `json:"conflicts"`
	CascadingBlockers []undoCascadeBlocker `json:"cascading_blockers"`
}

type undoResolutionPayload struct {
	FieldChoices   map[string]string `json:"field_choices"`
	CascadeChoices map[string]string `json:"cascade_choices"`
}

type mutationActivityRow struct {
	ID           int64  `json:"id"`
	RequestID    string `json:"request_id"`
	MutationType string `json:"mutation_type"`
	SubjectType  string `json:"subject_type"`
	SubjectID    int64  `json:"subject_id"`
	SubjectLabel string `json:"subject_label"`
	Summary      string `json:"summary"`
	ChangeDetail string `json:"change_detail"`
	ActorLabel   string `json:"actor_label"`
	OriginLabel  string `json:"origin_label,omitempty"`
	Undoable     bool   `json:"undoable"`
	OnUserStack  bool   `json:"on_user_stack"`
	Redoable     bool   `json:"redoable"`
	Undone       bool   `json:"undone"`
	CreatedAt    string `json:"created_at"`
}

type mutationActivityResponse struct {
	UndoRows    []mutationActivityRow `json:"undo_rows"`
	RedoRows    []mutationActivityRow `json:"redo_rows"`
	HistoryRows []mutationActivityRow `json:"history_rows"`
	StackDepth  int                   `json:"stack_depth"`
}

func decodeIssueSnapshot(raw string) (issueMutationSnapshot, error) {
	var snap issueMutationSnapshot
	err := json.Unmarshal([]byte(raw), &snap)
	return snap, err
}

func decodeRelationSnapshot(raw string) (relationMutationSnapshot, error) {
	var snap relationMutationSnapshot
	err := json.Unmarshal([]byte(raw), &snap)
	return snap, err
}

func decodeIssueTagSnapshot(raw string) (issueTagMutationSnapshot, error) {
	var snap issueTagMutationSnapshot
	err := json.Unmarshal([]byte(raw), &snap)
	return snap, err
}

func decodeTimeEntrySnapshot(raw string) (timeEntryMutationSnapshot, error) {
	var snap timeEntryMutationSnapshot
	err := json.Unmarshal([]byte(raw), &snap)
	return snap, err
}

func decodeCommentSnapshot(raw string) (commentMutationSnapshot, error) {
	var snap commentMutationSnapshot
	err := json.Unmarshal([]byte(raw), &snap)
	return snap, err
}

func snapshotMap(v any) map[string]any {
	blob, _ := json.Marshal(sanitizeSnapshotValue(v))
	var out map[string]any
	_ = json.Unmarshal(blob, &out)
	return out
}

func issueExistsActiveTx(tx *sql.Tx, issueID int64) bool {
	if issueID <= 0 {
		return false
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM issues WHERE id = ? AND deleted_at IS NULL`, issueID).Scan(&id)
	return err == nil
}

func userExistsTx(tx *sql.Tx, userID int64) bool {
	if userID <= 0 {
		return false
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM users WHERE id = ?`, userID).Scan(&id)
	return err == nil
}

func tagExistsTx(tx *sql.Tx, tagID int64) bool {
	if tagID <= 0 {
		return false
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM tags WHERE id = ?`, tagID).Scan(&id)
	return err == nil
}

func issueInvoicedTx(tx *sql.Tx, issueID int64) bool {
	if issueID <= 0 {
		return false
	}
	var status string
	var invoicedAt sql.NullString
	err := tx.QueryRow(`SELECT status, invoiced_at FROM issues WHERE id = ?`, issueID).Scan(&status, &invoicedAt)
	return err == nil && (status == "invoiced" || invoicedAt.Valid)
}

func classifyIssueConflictsTx(tx *sql.Tx, row mutationLogRow, mode undoMode) ([]undoFieldConflict, []undoCascadeBlocker, error) {
	before, err := decodeIssueSnapshot(row.BeforeState)
	if err != nil {
		return nil, nil, err
	}
	after, err := decodeIssueSnapshot(row.AfterState)
	if err != nil {
		return nil, nil, err
	}
	current, err := fetchIssueMutationSnapshotTx(tx, row.SubjectID)
	if err != nil {
		return nil, nil, err
	}

	desired := before
	baseline := after
	if mode == undoModeRedo {
		desired = after
		baseline = before
	}
	desiredMap := snapshotMap(desired)
	baselineMap := snapshotMap(baseline)
	currentMap := snapshotMap(current)

	keys := make([]string, 0, len(desiredMap))
	for k := range desiredMap {
		if k == "id" || k == "project_id" || k == "deleted_at" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	conflicts := make([]undoFieldConflict, 0)
	blockers := make([]undoCascadeBlocker, 0)
	for _, key := range keys {
		cur := currentMap[key]
		base := baselineMap[key]
		target := desiredMap[key]
		if valuesEqual(cur, base) || valuesEqual(cur, target) {
			continue
		}
		if key == "parent_id" {
			if targetID, ok := int64FromAny(target); ok && targetID > 0 && !issueExistsActiveTx(tx, targetID) {
				blockers = append(blockers, undoCascadeBlocker{
					Pattern:     "parent-deleted",
					TargetID:    targetID,
					Description: fmt.Sprintf("Parent issue %d no longer exists in active state.", targetID),
					Options: []undoConflictOption{
						{ID: "orphan", Label: "Make this issue top-level", Default: true},
						{ID: "cancel", Label: "Cancel"},
					},
				})
				continue
			}
		}
		conflicts = append(conflicts, undoFieldConflict{
			Pattern:      "field-changed-by-other",
			Field:        key,
			TheirValue:   cur,
			TargetValue:  target,
			CurrentValue: cur,
			Options: []undoConflictOption{
				{ID: "overwrite", Label: "Use my target value", Default: true},
				{ID: "keep_theirs", Label: "Keep the newer value"},
			},
		})
	}
	return conflicts, blockers, nil
}

func classifyRelationConflictsTx(tx *sql.Tx, row mutationLogRow, mode undoMode) ([]undoFieldConflict, []undoCascadeBlocker, error) {
	before, err := decodeRelationSnapshot(row.BeforeState)
	if err != nil {
		return nil, nil, err
	}
	after, err := decodeRelationSnapshot(row.AfterState)
	if err != nil {
		return nil, nil, err
	}
	desired := before
	if mode == undoModeRedo {
		desired = after
	}
	if desired.TargetID > 0 && !issueExistsActiveTx(tx, desired.TargetID) {
		return nil, []undoCascadeBlocker{{
			Pattern:     "target-deleted",
			TargetID:    desired.TargetID,
			Description: fmt.Sprintf("Target issue %d is no longer active.", desired.TargetID),
			Options: []undoConflictOption{
				{ID: "skip_relation", Label: "Skip the relation change", Default: true},
				{ID: "cancel", Label: "Cancel"},
			},
		}}, nil
	}
	return nil, nil, nil
}

func classifyIssueTagConflictsTx(tx *sql.Tx, row mutationLogRow, mode undoMode) ([]undoFieldConflict, []undoCascadeBlocker, error) {
	before, err := decodeIssueTagSnapshot(row.BeforeState)
	if err != nil {
		return nil, nil, err
	}
	after, err := decodeIssueTagSnapshot(row.AfterState)
	if err != nil {
		return nil, nil, err
	}
	current, err := issueTagSnapshotFromRow(tx, row)
	if err != nil {
		return nil, nil, err
	}

	desired := before
	baseline := after
	if mode == undoModeRedo {
		desired = after
		baseline = before
	}

	blockers := make([]undoCascadeBlocker, 0)
	if desired.Exists && !issueExistsActiveTx(tx, desired.IssueID) {
		blockers = append(blockers, undoCascadeBlocker{
			Pattern:     "parent-deleted",
			TargetID:    desired.IssueID,
			Description: fmt.Sprintf("Parent issue %d is no longer active.", desired.IssueID),
			Options: []undoConflictOption{
				{ID: "skip_tag", Label: "Skip the tag change", Default: true},
				{ID: "cancel", Label: "Cancel"},
			},
		})
	}
	if desired.Exists && !tagExistsTx(tx, desired.TagID) {
		return []undoFieldConflict{{
			Pattern:      "field-set-deleted",
			Field:        "tag",
			TheirValue:   tagLabel(desired.TagID),
			TargetValue:  "present",
			CurrentValue: "deleted",
			Options: []undoConflictOption{
				{ID: "skip_tag", Label: "Skip the tag change", Default: true},
				{ID: "cancel", Label: "Cancel"},
			},
		}}, blockers, nil
	}

	if valuesEqual(snapshotMap(current), snapshotMap(desired)) || valuesEqual(snapshotMap(current), snapshotMap(baseline)) {
		return nil, blockers, nil
	}
	conflict := undoFieldConflict{
		Pattern:      "field-changed-by-other",
		Field:        "tag",
		TheirValue:   issueTagStatePreview(current),
		TargetValue:  issueTagStatePreview(desired),
		CurrentValue: issueTagStatePreview(current),
		Options: []undoConflictOption{
			{ID: "overwrite", Label: "Use my target value", Default: true},
			{ID: "keep_theirs", Label: "Keep the newer value"},
		},
	}
	return []undoFieldConflict{conflict}, blockers, nil
}

func classifyTimeEntryConflictsTx(tx *sql.Tx, row mutationLogRow, mode undoMode) ([]undoFieldConflict, []undoCascadeBlocker, error) {
	before, err := decodeTimeEntrySnapshot(row.BeforeState)
	if err != nil {
		return nil, nil, err
	}
	after, err := decodeTimeEntrySnapshot(row.AfterState)
	if err != nil {
		return nil, nil, err
	}
	current, err := fetchTimeEntryMutationSnapshotTx(tx, row.SubjectID)
	if err != nil {
		return nil, nil, err
	}

	desired := before
	baseline := after
	if mode == undoModeRedo {
		desired = after
		baseline = before
	}

	blockers := make([]undoCascadeBlocker, 0)
	if desired.Exists && !issueExistsActiveTx(tx, desired.IssueID) {
		blockers = append(blockers, undoCascadeBlocker{
			Pattern:     "parent-deleted",
			TargetID:    desired.IssueID,
			Description: fmt.Sprintf("Parent issue %d is no longer active.", desired.IssueID),
			Options: []undoConflictOption{
				{ID: "skip_time_entry", Label: "Skip the time-entry change", Default: true},
				{ID: "cancel", Label: "Cancel"},
			},
		})
	}
	if desired.Exists && !userExistsTx(tx, desired.UserID) {
		return []undoFieldConflict{{
			Pattern:      "field-set-deleted",
			Field:        "user",
			TheirValue:   fmt.Sprintf("User %d", desired.UserID),
			TargetValue:  "present",
			CurrentValue: "deleted",
			Options: []undoConflictOption{
				{ID: "skip_time_entry", Label: "Skip the time-entry change", Default: true},
				{ID: "cancel", Label: "Cancel"},
			},
		}}, blockers, nil
	}

	if valuesEqual(snapshotMap(current), snapshotMap(desired)) || valuesEqual(snapshotMap(current), snapshotMap(baseline)) {
		return nil, blockers, nil
	}
	conflict := undoFieldConflict{
		Pattern:      "field-changed-by-other",
		Field:        "time_entry",
		TheirValue:   timeEntryStatePreview(current),
		TargetValue:  timeEntryStatePreview(desired),
		CurrentValue: timeEntryStatePreview(current),
		Options: []undoConflictOption{
			{ID: "overwrite", Label: "Use my target value", Default: true},
			{ID: "keep_theirs", Label: "Keep the newer value"},
		},
	}
	return []undoFieldConflict{conflict}, blockers, nil
}

func classifyCommentConflictsTx(tx *sql.Tx, row mutationLogRow, mode undoMode) ([]undoFieldConflict, []undoCascadeBlocker, error) {
	before, err := decodeCommentSnapshot(row.BeforeState)
	if err != nil {
		return nil, nil, err
	}
	after, err := decodeCommentSnapshot(row.AfterState)
	if err != nil {
		return nil, nil, err
	}
	current, err := fetchCommentMutationSnapshotTx(tx, row.SubjectID)
	if err != nil {
		return nil, nil, err
	}

	desired := before
	baseline := after
	if mode == undoModeRedo {
		desired = after
		baseline = before
	}

	blockers := make([]undoCascadeBlocker, 0)
	if desired.Exists && !issueExistsActiveTx(tx, desired.IssueID) {
		blockers = append(blockers, undoCascadeBlocker{
			Pattern:     "parent-deleted",
			TargetID:    desired.IssueID,
			Description: fmt.Sprintf("Parent issue %d is no longer active.", desired.IssueID),
			Options: []undoConflictOption{
				{ID: "skip_comment", Label: "Skip the comment change", Default: true},
				{ID: "cancel", Label: "Cancel"},
			},
		})
	}

	if valuesEqual(snapshotMap(current), snapshotMap(desired)) || valuesEqual(snapshotMap(current), snapshotMap(baseline)) {
		return nil, blockers, nil
	}
	conflict := undoFieldConflict{
		Pattern:      "field-changed-by-other",
		Field:        "comment",
		TheirValue:   commentStatePreview(current),
		TargetValue:  commentStatePreview(desired),
		CurrentValue: commentStatePreview(current),
		Options: []undoConflictOption{
			{ID: "overwrite", Label: "Use my target value", Default: true},
			{ID: "keep_theirs", Label: "Keep the newer value"},
		},
	}
	return []undoFieldConflict{conflict}, blockers, nil
}

func classifyMutationConflictTx(tx *sql.Tx, row mutationLogRow, mode undoMode) (undoConflictResponse, error) {
	resp := undoConflictResponse{
		Status:       "conflict",
		LogID:        row.ID,
		RequestID:    row.RequestID,
		Mode:         string(mode),
		MutationType: row.MutationType,
	}
	switch row.SubjectType {
	case "issue":
		conflicts, blockers, err := classifyIssueConflictsTx(tx, row, mode)
		if err != nil {
			return resp, err
		}
		resp.Conflicts = conflicts
		resp.CascadingBlockers = blockers
	case "issue_relation":
		conflicts, blockers, err := classifyRelationConflictsTx(tx, row, mode)
		if err != nil {
			return resp, err
		}
		resp.Conflicts = conflicts
		resp.CascadingBlockers = blockers
	case "issue_tag":
		conflicts, blockers, err := classifyIssueTagConflictsTx(tx, row, mode)
		if err != nil {
			return resp, err
		}
		resp.Conflicts = conflicts
		resp.CascadingBlockers = blockers
	case "time_entry":
		conflicts, blockers, err := classifyTimeEntryConflictsTx(tx, row, mode)
		if err != nil {
			return resp, err
		}
		resp.Conflicts = conflicts
		resp.CascadingBlockers = blockers
	case "comment":
		conflicts, blockers, err := classifyCommentConflictsTx(tx, row, mode)
		if err != nil {
			return resp, err
		}
		resp.Conflicts = conflicts
		resp.CascadingBlockers = blockers
	default:
		resp.Conflicts = []undoFieldConflict{{
			Pattern:      "field-changed-by-other",
			Field:        "state",
			TheirValue:   "changed",
			TargetValue:  "restore",
			CurrentValue: "changed",
			Options: []undoConflictOption{
				{ID: "overwrite", Label: "Overwrite current state", Default: true},
				{ID: "keep_theirs", Label: "Keep current state"},
			},
		}}
	}
	return resp, nil
}

func valuesEqual(a, b any) bool {
	ab, _ := json.Marshal(sanitizeSnapshotValue(a))
	bb, _ := json.Marshal(sanitizeSnapshotValue(b))
	return string(ab) == string(bb)
}

func int64FromAny(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case float32:
		return int64(x), true
	case int:
		return int64(x), true
	case int64:
		return x, true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func applyIssueResolutionTx(tx *sql.Tx, row mutationLogRow, mode undoMode, res undoResolutionPayload) error {
	before, err := decodeIssueSnapshot(row.BeforeState)
	if err != nil {
		return err
	}
	after, err := decodeIssueSnapshot(row.AfterState)
	if err != nil {
		return err
	}
	current, err := fetchIssueMutationSnapshotTx(tx, row.SubjectID)
	if err != nil {
		return err
	}
	desired := before
	baseline := after
	if mode == undoModeRedo {
		desired = after
		baseline = before
	}
	target := current
	desiredMap := snapshotMap(desired)
	baselineMap := snapshotMap(baseline)
	currentMap := snapshotMap(current)

	// Parent conflict first.
	if choice := res.CascadeChoices["parent-deleted"]; choice == "orphan" {
		target.ParentID = nil
	}
	if choice := res.CascadeChoices["parent-deleted"]; choice == "cancel" {
		return nil
	}

	for field, desiredValue := range desiredMap {
		if field == "id" || field == "project_id" || field == "deleted_at" {
			continue
		}
		cur := currentMap[field]
		base := baselineMap[field]
		if valuesEqual(cur, base) || valuesEqual(cur, desiredValue) {
			assignIssueField(&target, field, desiredValue)
			continue
		}
		choice := res.FieldChoices[field]
		if choice == "" {
			choice = "overwrite"
		}
		if choice == "overwrite" {
			assignIssueField(&target, field, desiredValue)
		}
	}
	return applyIssueSnapshotTx(tx, row.SubjectID, target)
}

func assignIssueField(target *issueMutationSnapshot, field string, value any) {
	blob, _ := json.Marshal(value)
	switch field {
	case "type":
		_ = json.Unmarshal(blob, &target.Type)
	case "parent_id":
		var v *int64
		_ = json.Unmarshal(blob, &v)
		target.ParentID = v
	case "title":
		_ = json.Unmarshal(blob, &target.Title)
	case "description":
		_ = json.Unmarshal(blob, &target.Description)
	case "acceptance_criteria":
		_ = json.Unmarshal(blob, &target.AcceptanceCriteria)
	case "notes":
		_ = json.Unmarshal(blob, &target.Notes)
	case "status":
		_ = json.Unmarshal(blob, &target.Status)
	case "priority":
		_ = json.Unmarshal(blob, &target.Priority)
	case "cost_unit":
		_ = json.Unmarshal(blob, &target.CostUnit)
	case "release":
		_ = json.Unmarshal(blob, &target.Release)
	case "billing_type":
		_ = json.Unmarshal(blob, &target.BillingType)
	case "total_budget":
		_ = json.Unmarshal(blob, &target.TotalBudget)
	case "rate_hourly":
		_ = json.Unmarshal(blob, &target.RateHourly)
	case "rate_lp":
		_ = json.Unmarshal(blob, &target.RateLp)
	case "start_date":
		_ = json.Unmarshal(blob, &target.StartDate)
	case "end_date":
		_ = json.Unmarshal(blob, &target.EndDate)
	case "group_state":
		_ = json.Unmarshal(blob, &target.GroupState)
	case "sprint_state":
		_ = json.Unmarshal(blob, &target.SprintState)
	case "jira_id":
		_ = json.Unmarshal(blob, &target.JiraID)
	case "jira_version":
		_ = json.Unmarshal(blob, &target.JiraVersion)
	case "jira_text":
		_ = json.Unmarshal(blob, &target.JiraText)
	case "estimate_hours":
		_ = json.Unmarshal(blob, &target.EstimateHours)
	case "estimate_lp":
		_ = json.Unmarshal(blob, &target.EstimateLp)
	case "ar_hours":
		_ = json.Unmarshal(blob, &target.ArHours)
	case "ar_lp":
		_ = json.Unmarshal(blob, &target.ArLp)
	case "time_override":
		_ = json.Unmarshal(blob, &target.TimeOverride)
	case "color":
		_ = json.Unmarshal(blob, &target.Color)
	case "assignee_id":
		_ = json.Unmarshal(blob, &target.AssigneeID)
	}
}

func applyRelationResolutionTx(ctx context.Context, tx *sql.Tx, row mutationLogRow, mode undoMode, res undoResolutionPayload) error {
	if choice := res.CascadeChoices["target-deleted"]; choice == "skip_relation" || choice == "cancel" {
		return nil
	}
	var inv InverseOp
	if err := json.Unmarshal([]byte(row.InverseOp), &inv); err != nil {
		return err
	}
	if mode == undoModeUndo {
		return executeInverseOpTx(ctx, tx, inv)
	}
	redo, err := redoOpForRow(row)
	if err != nil {
		return err
	}
	return executeInverseOpTx(ctx, tx, redo)
}

func applyIssueTagResolutionTx(ctx context.Context, tx *sql.Tx, row mutationLogRow, mode undoMode, res undoResolutionPayload) error {
	if choice := res.CascadeChoices["parent-deleted"]; choice == "skip_tag" || choice == "cancel" {
		return nil
	}
	if choice := res.FieldChoices["tag"]; choice == "keep_theirs" || choice == "skip_tag" || choice == "cancel" {
		return nil
	}
	if mode == undoModeUndo {
		var inv InverseOp
		if err := json.Unmarshal([]byte(row.InverseOp), &inv); err != nil {
			return err
		}
		return executeInverseOpTx(ctx, tx, inv)
	}
	redo, err := redoOpForRow(row)
	if err != nil {
		return err
	}
	return executeInverseOpTx(ctx, tx, redo)
}

func applyTimeEntryResolutionTx(ctx context.Context, tx *sql.Tx, row mutationLogRow, mode undoMode, res undoResolutionPayload) error {
	if choice := res.CascadeChoices["parent-deleted"]; choice == "skip_time_entry" || choice == "cancel" {
		return nil
	}
	for _, field := range []string{"time_entry", "user"} {
		choice := res.FieldChoices[field]
		if choice == "keep_theirs" || choice == "skip_time_entry" || choice == "cancel" {
			return nil
		}
	}
	if mode == undoModeUndo {
		var inv InverseOp
		if err := json.Unmarshal([]byte(row.InverseOp), &inv); err != nil {
			return err
		}
		return executeInverseOpTx(ctx, tx, inv)
	}
	redo, err := redoOpForRow(row)
	if err != nil {
		return err
	}
	return executeInverseOpTx(ctx, tx, redo)
}

func applyCommentResolutionTx(ctx context.Context, tx *sql.Tx, row mutationLogRow, mode undoMode, res undoResolutionPayload) error {
	if choice := res.CascadeChoices["parent-deleted"]; choice == "skip_comment" || choice == "cancel" {
		return nil
	}
	if choice := res.FieldChoices["comment"]; choice == "keep_theirs" {
		return nil
	}
	if mode == undoModeUndo {
		var inv InverseOp
		if err := json.Unmarshal([]byte(row.InverseOp), &inv); err != nil {
			return err
		}
		return executeInverseOpTx(ctx, tx, inv)
	}
	redo, err := redoOpForRow(row)
	if err != nil {
		return err
	}
	return executeInverseOpTx(ctx, tx, redo)
}

func redoOpForRow(row mutationLogRow) (InverseOp, error) {
	switch row.SubjectType {
	case "issue":
		var after issueMutationSnapshot
		if err := json.Unmarshal([]byte(row.AfterState), &after); err != nil {
			return InverseOp{}, err
		}
		return InverseOp{
			Method: http.MethodPut,
			Path:   fmt.Sprintf("/issues/%d", row.SubjectID),
			Body:   after,
		}, nil
	case "issue_relation":
		var after relationMutationSnapshot
		if err := json.Unmarshal([]byte(row.AfterState), &after); err != nil {
			return InverseOp{}, err
		}
		sourceID := row.SubjectID
		body := map[string]any{"target_id": after.TargetID, "type": after.Type, "rank": after.Rank}
		if after.Type == "sprint" {
			sourceID = after.TargetID
			body["target_id"] = after.SourceID
		}
		if after.Exists {
			return InverseOp{Method: http.MethodPost, Path: fmt.Sprintf("/issues/%d/relations", sourceID), Body: body}, nil
		}
		deleteBody := map[string]any{"target_id": body["target_id"], "type": after.Type}
		return InverseOp{Method: http.MethodDelete, Path: fmt.Sprintf("/issues/%d/relations", sourceID), Body: deleteBody}, nil
	case "issue_tag":
		var after issueTagMutationSnapshot
		if err := json.Unmarshal([]byte(row.AfterState), &after); err != nil {
			return InverseOp{}, err
		}
		if after.Exists {
			return InverseOp{Method: http.MethodPost, Path: fmt.Sprintf("/issues/%d/tags", after.IssueID), Body: map[string]any{"tag_id": after.TagID}}, nil
		}
		return InverseOp{Method: http.MethodDelete, Path: fmt.Sprintf("/issues/%d/tags/%d", after.IssueID, after.TagID)}, nil
	case "time_entry":
		var after timeEntryMutationSnapshot
		if err := json.Unmarshal([]byte(row.AfterState), &after); err != nil {
			return InverseOp{}, err
		}
		if after.Exists {
			return InverseOp{Method: http.MethodPut, Path: fmt.Sprintf("/time-entries/%d", row.SubjectID), Body: after}, nil
		}
		return InverseOp{Method: http.MethodDelete, Path: fmt.Sprintf("/time-entries/%d", row.SubjectID)}, nil
	case "comment":
		var after commentMutationSnapshot
		if err := json.Unmarshal([]byte(row.AfterState), &after); err != nil {
			return InverseOp{}, err
		}
		if after.Exists {
			return InverseOp{Method: http.MethodPut, Path: fmt.Sprintf("/comments/%d", row.SubjectID), Body: after}, nil
		}
		return InverseOp{Method: http.MethodDelete, Path: fmt.Sprintf("/comments/%d", row.SubjectID)}, nil
	default:
		return InverseOp{}, fmt.Errorf("unsupported redo subject type %s", row.SubjectType)
	}
}

func applyMutationFlowTx(ctx context.Context, tx *sql.Tx, row mutationLogRow, userID int64, mode undoMode, resolution *undoResolutionPayload) (map[string]any, *undoConflictResponse, int, error) {
	if !row.Undoable {
		return nil, nil, http.StatusLocked, nil
	}
	if mode == undoModeUndo {
		if !row.OnUserStack || row.UndoneAt.Valid {
			return nil, nil, http.StatusGone, nil
		}
	} else {
		if !row.Redoable || !row.UndoneAt.Valid {
			return nil, nil, http.StatusGone, nil
		}
	}

	currentHash, err := currentMutationHashTx(tx, row)
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	expectedHash := row.AfterHash
	if mode == undoModeRedo {
		expectedHash = row.BeforeHash
	}
	if currentHash != expectedHash {
		conflict, err := classifyMutationConflictTx(tx, row, mode)
		if err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		if resolution == nil {
			return nil, &conflict, http.StatusConflict, nil
		}
		switch row.SubjectType {
		case "issue":
			if err := applyIssueResolutionTx(tx, row, mode, *resolution); err != nil {
				return nil, nil, http.StatusInternalServerError, err
			}
		case "issue_relation":
			if err := applyRelationResolutionTx(ctx, tx, row, mode, *resolution); err != nil {
				return nil, nil, http.StatusInternalServerError, err
			}
		case "issue_tag":
			if err := applyIssueTagResolutionTx(ctx, tx, row, mode, *resolution); err != nil {
				return nil, nil, http.StatusInternalServerError, err
			}
		case "time_entry":
			if err := applyTimeEntryResolutionTx(ctx, tx, row, mode, *resolution); err != nil {
				var locked *undoLockedError
				if errors.As(err, &locked) {
					return nil, nil, http.StatusLocked, err
				}
				return nil, nil, http.StatusInternalServerError, err
			}
		case "comment":
			if err := applyCommentResolutionTx(ctx, tx, row, mode, *resolution); err != nil {
				return nil, nil, http.StatusInternalServerError, err
			}
		default:
			return nil, nil, http.StatusLocked, nil
		}
		choiceJSON, _ := json.Marshal(resolution)
		if mode == undoModeUndo {
			if _, err := tx.ExecContext(ctx, `
				UPDATE mutation_log
				SET undone_at = ?, undone_by = ?, on_user_stack = 0, redoable = 1, resolution_choice = ?
				WHERE id = ?
			`, time.Now().UTC().Format("2006-01-02 15:04:05.000"), userID, string(choiceJSON), row.ID); err != nil {
				return nil, nil, http.StatusInternalServerError, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE mutation_log
				SET undone_at = NULL, undone_by = NULL, on_user_stack = 1, redoable = 0, resolution_choice = ?
				WHERE id = ?
			`, string(choiceJSON), row.ID); err != nil {
				return nil, nil, http.StatusInternalServerError, err
			}
			if err := enforceUndoStackDepth(ctx, tx, userID); err != nil {
				return nil, nil, http.StatusInternalServerError, err
			}
		}
		return map[string]any{"applied": true, "resolved": true, "log_id": row.ID, "request_id": row.RequestID}, nil, http.StatusOK, nil
	}

	var opErr error
	if mode == undoModeUndo {
		var inv InverseOp
		if err := json.Unmarshal([]byte(row.InverseOp), &inv); err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		opErr = executeInverseOpTx(ctx, tx, inv)
	} else {
		redo, err := redoOpForRow(row)
		if err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		opErr = executeInverseOpTx(ctx, tx, redo)
	}
	if opErr != nil {
		var conflict *undoConflictError
		if errors.As(opErr, &conflict) {
			resp, err := classifyMutationConflictTx(tx, row, mode)
			if err == nil && (len(resp.Conflicts) > 0 || len(resp.CascadingBlockers) > 0) {
				return nil, &resp, http.StatusConflict, nil
			}
			return nil, nil, http.StatusConflict, opErr
		}
		var locked *undoLockedError
		if errors.As(opErr, &locked) {
			return nil, nil, http.StatusLocked, opErr
		}
		return nil, nil, http.StatusInternalServerError, opErr
	}
	if mode == undoModeUndo {
		if _, err := tx.ExecContext(ctx, `
			UPDATE mutation_log
			SET undone_at = ?, undone_by = ?, on_user_stack = 0, redoable = 1
			WHERE id = ?
		`, time.Now().UTC().Format("2006-01-02 15:04:05.000"), userID, row.ID); err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		return map[string]any{"undone": true, "log_id": row.ID, "request_id": row.RequestID}, nil, http.StatusOK, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mutation_log
		SET undone_at = NULL, undone_by = NULL, on_user_stack = 1, redoable = 0
		WHERE id = ?
	`, row.ID); err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	if err := enforceUndoStackDepth(ctx, tx, userID); err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	return map[string]any{"redone": true, "log_id": row.ID, "request_id": row.RequestID}, nil, http.StatusOK, nil
}

func loadRedoableMutation(tx *sql.Tx, logID int64, userID int64) (mutationLogRow, error) {
	row, err := loadUndoableMutation(tx, logID, userID)
	if err != nil {
		return row, err
	}
	if !row.Redoable {
		return row, sql.ErrNoRows
	}
	return row, nil
}

func issueLabel(id int64) string {
	var key, title string
	err := db.DB.QueryRow(`
		SELECT
			COALESCE(
				CASE
					WHEN p.key IS NOT NULL AND i.issue_number > 0
					THEN p.key || '-' || CAST(i.issue_number AS TEXT)
					ELSE ''
				END,
				''
			),
			COALESCE(i.title, '')
		FROM issues i
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE i.id = ?
	`, id).Scan(&key, &title)
	if err == nil {
		label := strings.TrimSpace(key)
		if label == "" {
			label = fmt.Sprintf("Issue %d", id)
		}
		if preview := plainPreview(title, 52); preview != "" {
			return label + " - " + preview
		}
		return label
	}
	return fmt.Sprintf("Issue %d", id)
}

func commentIssueID(row mutationLogRow) int64 {
	for _, raw := range []string{row.AfterState, row.BeforeState} {
		snap, err := decodeCommentSnapshot(raw)
		if err == nil && snap.IssueID > 0 {
			return snap.IssueID
		}
	}
	return 0
}

func timeEntryIssueID(row mutationLogRow) int64 {
	for _, raw := range []string{row.AfterState, row.BeforeState} {
		snap, err := decodeTimeEntrySnapshot(raw)
		if err == nil && snap.IssueID > 0 {
			return snap.IssueID
		}
	}
	return 0
}

func runUndoMutationSideEffects(rows []mutationLogRow) {
	seenTimeEntryIssues := make(map[int64]bool)
	for _, row := range rows {
		if row.SubjectType != "time_entry" {
			continue
		}
		issueID := timeEntryIssueID(row)
		if issueID <= 0 || seenTimeEntryIssues[issueID] {
			continue
		}
		seenTimeEntryIssues[issueID] = true
		EvaluateSystemTags(issueID)
	}
}

func subjectLabel(row mutationLogRow) string {
	switch row.SubjectType {
	case "issue":
		return issueLabel(row.SubjectID)
	case "issue_relation":
		return issueLabel(row.SubjectID)
	case "issue_tag":
		issueID, _, err := issueTagIDsFromRow(row)
		if err == nil && issueID > 0 {
			return fmt.Sprintf("Tag on %s", issueLabel(issueID))
		}
		return fmt.Sprintf("Tag on %s", issueLabel(row.SubjectID))
	case "comment":
		if issueID := commentIssueID(row); issueID > 0 {
			return fmt.Sprintf("Comment on %s", issueLabel(issueID))
		}
		return fmt.Sprintf("Comment %d", row.SubjectID)
	case "time_entry":
		if issueID := timeEntryIssueID(row); issueID > 0 {
			return fmt.Sprintf("Time entry on %s", issueLabel(issueID))
		}
		return fmt.Sprintf("Time entry %d", row.SubjectID)
	default:
		return fmt.Sprintf("%s %d", strings.ReplaceAll(row.SubjectType, "_", " "), row.SubjectID)
	}
}

func mutationSummary(row mutationLogRow) string {
	switch row.MutationType {
	case "ai.issue.update", "issue.update":
		return "Updated issue fields"
	case "ai.issue.relation.create", "issue.relation.create":
		return "Added issue relation"
	case "ai.issue.relation.delete", "issue.relation.delete":
		return "Removed issue relation"
	case "ai.issue.tag.add", "issue.tag.add", "issue.tag.bulk_add":
		return "Added tag"
	case "ai.issue.tag.remove", "issue.tag.remove", "issue.tag.bulk_remove":
		return "Removed tag"
	case "ai.issue.comment.create", "issue.comment.create":
		return "Added comment"
	case "ai.issue.comment.delete", "issue.comment.delete":
		return "Removed comment"
	case "ai.issue.time_entry.create", "issue.time_entry.create":
		return "Added time entry"
	case "ai.issue.time_entry.update", "issue.time_entry.update":
		return "Updated time entry"
	case "ai.issue.time_entry.delete", "issue.time_entry.delete":
		return "Removed time entry"
	default:
		return strings.ReplaceAll(row.MutationType, ".", " → ")
	}
}

func mutationDetail(row mutationLogRow) string {
	switch row.SubjectType {
	case "issue":
		return issueMutationDetail(row)
	case "issue_relation":
		return relationMutationDetail(row)
	case "issue_tag":
		return issueTagMutationDetail(row)
	case "comment":
		return commentMutationDetail(row)
	case "time_entry":
		return timeEntryMutationDetail(row)
	default:
		return ""
	}
}

func issueMutationDetail(row mutationLogRow) string {
	before, err := decodeIssueSnapshot(row.BeforeState)
	if err != nil {
		return ""
	}
	after, err := decodeIssueSnapshot(row.AfterState)
	if err != nil {
		return ""
	}
	beforeMap := snapshotMap(before)
	afterMap := snapshotMap(after)
	keys := make([]string, 0, len(afterMap))
	for key := range afterMap {
		if key == "id" || key == "project_id" || key == "deleted_at" {
			continue
		}
		if !valuesEqual(beforeMap[key], afterMap[key]) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, min(len(keys), 3))
	for _, key := range keys {
		if len(parts) == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s: %s -> %s", fieldLabel(key), valuePreview(beforeMap[key]), valuePreview(afterMap[key])))
	}
	if len(keys) > len(parts) {
		parts = append(parts, fmt.Sprintf("+%d more", len(keys)-len(parts)))
	}
	return strings.Join(parts, "; ")
}

func relationMutationDetail(row mutationLogRow) string {
	before, berr := decodeRelationSnapshot(row.BeforeState)
	after, aerr := decodeRelationSnapshot(row.AfterState)
	if berr != nil || aerr != nil {
		return ""
	}
	sourceID := after.SourceID
	if sourceID == 0 {
		sourceID = before.SourceID
	}
	targetID := after.TargetID
	if targetID == 0 {
		targetID = before.TargetID
	}
	relType := after.Type
	if relType == "" {
		relType = before.Type
	}
	beforeState := "absent"
	if before.Exists {
		beforeState = "present"
	}
	afterState := "absent"
	if after.Exists {
		afterState = "present"
	}
	return fmt.Sprintf("%s %s %s: %s -> %s", issueLabel(sourceID), strings.ReplaceAll(relType, "_", " "), issueLabel(targetID), beforeState, afterState)
}

func issueTagMutationDetail(row mutationLogRow) string {
	before, berr := decodeIssueTagSnapshot(row.BeforeState)
	after, aerr := decodeIssueTagSnapshot(row.AfterState)
	if berr != nil || aerr != nil {
		return ""
	}
	tagID := after.TagID
	if tagID == 0 {
		tagID = before.TagID
	}
	return fmt.Sprintf("%s: %s -> %s", tagLabel(tagID), issueTagStatePreview(before), issueTagStatePreview(after))
}

func issueTagStatePreview(snap issueTagMutationSnapshot) string {
	if snap.Exists {
		return "present"
	}
	return "absent"
}

func tagLabel(tagID int64) string {
	if tagID <= 0 {
		return "Tag"
	}
	var name string
	err := db.DB.QueryRow(`SELECT name FROM tags WHERE id = ?`, tagID).Scan(&name)
	if err == nil && strings.TrimSpace(name) != "" {
		return "#" + strings.TrimSpace(name)
	}
	return fmt.Sprintf("Tag %d", tagID)
}

func commentMutationDetail(row mutationLogRow) string {
	before, berr := decodeCommentSnapshot(row.BeforeState)
	after, aerr := decodeCommentSnapshot(row.AfterState)
	if berr != nil || aerr != nil {
		return ""
	}
	return fmt.Sprintf("%s -> %s", commentStatePreview(before), commentStatePreview(after))
}

func commentStatePreview(snap commentMutationSnapshot) string {
	if !snap.Exists {
		return "absent"
	}
	return valuePreview(snap.Body)
}

func timeEntryMutationDetail(row mutationLogRow) string {
	before, berr := decodeTimeEntrySnapshot(row.BeforeState)
	after, aerr := decodeTimeEntrySnapshot(row.AfterState)
	if berr != nil || aerr != nil {
		return ""
	}
	stateChange := fmt.Sprintf("%s -> %s", timeEntryStatePreview(before), timeEntryStatePreview(after))
	if !before.Exists || !after.Exists {
		return stateChange
	}

	beforeMap := snapshotMap(before)
	afterMap := snapshotMap(after)
	keys := []string{"user_id", "started_at", "stopped_at", "override", "comment", "internal_rate_hourly", "mite_id"}
	parts := []string{stateChange}
	for _, key := range keys {
		if len(parts) == 4 {
			break
		}
		if valuesEqual(beforeMap[key], afterMap[key]) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s -> %s", fieldLabel(key), valuePreview(beforeMap[key]), valuePreview(afterMap[key])))
	}
	return strings.Join(parts, "; ")
}

func timeEntryStatePreview(snap timeEntryMutationSnapshot) string {
	if !snap.Exists {
		return "absent"
	}
	duration := "running"
	switch {
	case snap.Override != nil:
		duration = formatHoursPreview(*snap.Override)
	case snap.StoppedAt != nil:
		if hours, ok := timeEntryHours(snap.StartedAt, *snap.StoppedAt); ok {
			duration = formatHoursPreview(hours)
		} else {
			duration = "stopped"
		}
	}
	parts := []string{duration}
	if label := userLabel(snap.UserID); label != "" {
		parts = append(parts, "for "+label)
	}
	if strings.TrimSpace(snap.Comment) != "" {
		parts = append(parts, quotePreview(snap.Comment))
	}
	return strings.Join(parts, " ")
}

func timeEntryHours(startedAt, stoppedAt string) (float64, bool) {
	layouts := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	var start, stop time.Time
	for _, layout := range layouts {
		if t, err := time.Parse(layout, startedAt); err == nil {
			start = t
			break
		}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, stoppedAt); err == nil {
			stop = t
			break
		}
	}
	if start.IsZero() || stop.IsZero() || !stop.After(start) {
		return 0, false
	}
	return stop.Sub(start).Hours(), true
}

func formatHoursPreview(hours float64) string {
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", hours), "0"), ".")
	return s + "h"
}

func userLabel(userID int64) string {
	if userID <= 0 {
		return ""
	}
	var name string
	err := db.DB.QueryRow(`SELECT COALESCE(NULLIF(nickname,''), username, '') FROM users WHERE id = ?`, userID).Scan(&name)
	if err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return fmt.Sprintf("User %d", userID)
}

func fieldLabel(field string) string {
	return strings.ReplaceAll(field, "_", " ")
}

func valuePreview(v any) string {
	if v == nil {
		return "empty"
	}
	switch x := v.(type) {
	case string:
		return quotePreview(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		blob, _ := json.Marshal(sanitizeSnapshotValue(v))
		return truncatePreview(string(blob), 72)
	}
}

func quotePreview(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "empty"
	}
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.Join(strings.Fields(v), " ")
	return `"` + truncatePreview(v, 64) + `"`
}

func plainPreview(v string, limit int) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.Join(strings.Fields(v), " ")
	return truncatePreview(v, limit)
}

func truncatePreview(v string, limit int) string {
	if len(v) <= limit {
		return v
	}
	if limit <= 3 {
		return v[:limit]
	}
	return v[:limit-3] + "..."
}

func actorLabel(actorName, agentName sql.NullString) string {
	actor := "Unknown user"
	if actorName.Valid && strings.TrimSpace(actorName.String) != "" {
		actor = strings.TrimSpace(actorName.String)
	}
	if agentName.Valid && strings.TrimSpace(agentName.String) != "" {
		return fmt.Sprintf("%s via %s", strings.TrimSpace(agentName.String), actor)
	}
	return actor
}

func originLabel(sessionID sql.NullString) string {
	if sessionID.Valid && strings.TrimSpace(sessionID.String) != "" {
		return "session " + strings.TrimSpace(sessionID.String)
	}
	return ""
}

func listMutationActivity(userID int64, subjectType string, subjectID int64) (mutationActivityResponse, error) {
	resp := mutationActivityResponse{
		UndoRows:    []mutationActivityRow{},
		RedoRows:    []mutationActivityRow{},
		HistoryRows: []mutationActivityRow{},
		StackDepth:  defaultUndoStackDepth,
	}
	depth, err := loadUndoStackDepth(db.DB)
	if err == nil && depth > 0 {
		resp.StackDepth = depth
	}
	query := `
		SELECT m.id, m.request_id, m.mutation_type, m.subject_type, m.subject_id,
		       m.undoable, m.on_user_stack, m.redoable, m.undone_at, m.created_at,
		       m.before_state, m.after_state,
		       COALESCE(NULLIF(u.nickname,''), u.username), m.agent_name, m.session_id
		FROM mutation_log m
		LEFT JOIN users u ON u.id = m.user_id
		WHERE m.user_id = ?`
	args := []any{userID}
	if subjectType != "" && subjectID > 0 {
		if subjectType == "issue" {
			query += ` AND (
				(m.subject_type = 'issue' AND m.subject_id = ?)
				OR (m.subject_type = 'issue_relation' AND m.subject_id = ?)
				OR (m.subject_type = 'issue_tag' AND m.subject_id = ?)
				OR (
					m.subject_type = 'comment'
					AND (
						json_extract(m.before_state, '$.issue_id') = ?
						OR json_extract(m.after_state, '$.issue_id') = ?
					)
				)
				OR (
					m.subject_type = 'time_entry'
					AND (
						json_extract(m.before_state, '$.issue_id') = ?
						OR json_extract(m.after_state, '$.issue_id') = ?
					)
				)
			)`
			args = append(args, subjectID, subjectID, subjectID, subjectID, subjectID, subjectID, subjectID)
		} else {
			query += ` AND m.subject_type = ? AND m.subject_id = ?`
			args = append(args, subjectType, subjectID)
		}
	}
	query += ` ORDER BY m.created_at DESC, m.id DESC LIMIT 30`
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return resp, err
	}
	defer rows.Close()
	for rows.Next() {
		var row mutationActivityRow
		var mrow mutationLogRow
		var undoable, onUserStack, redoable int
		var undoneAt sql.NullString
		var actorName, agentName, sessionID sql.NullString
		if err := rows.Scan(
			&mrow.ID, &mrow.RequestID, &mrow.MutationType, &mrow.SubjectType, &mrow.SubjectID,
			&undoable, &onUserStack, &redoable, &undoneAt, &row.CreatedAt,
			&mrow.BeforeState, &mrow.AfterState, &actorName, &agentName, &sessionID,
		); err != nil {
			return resp, err
		}
		row.ID = mrow.ID
		row.RequestID = mrow.RequestID
		row.MutationType = mrow.MutationType
		row.SubjectType = mrow.SubjectType
		row.SubjectID = mrow.SubjectID
		row.Undoable = undoable == 1
		row.OnUserStack = onUserStack == 1 && !undoneAt.Valid
		row.Redoable = redoable == 1 && undoneAt.Valid
		row.Undone = undoneAt.Valid
		mrow.Undoable = row.Undoable
		mrow.OnUserStack = row.OnUserStack
		mrow.Redoable = row.Redoable
		mrow.UndoneAt = undoneAt
		row.SubjectLabel = subjectLabel(mrow)
		row.Summary = mutationSummary(mrow)
		row.ChangeDetail = mutationDetail(mrow)
		row.ActorLabel = actorLabel(actorName, agentName)
		row.OriginLabel = originLabel(sessionID)
		switch {
		case row.OnUserStack:
			resp.UndoRows = append(resp.UndoRows, row)
		case row.Redoable:
			resp.RedoRows = append(resp.RedoRows, row)
		default:
			resp.HistoryRows = append(resp.HistoryRows, row)
		}
	}
	return resp, nil
}

func ListMyMutationActivity(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	resp, err := listMutationActivity(user.ID, "", 0)
	if err != nil {
		log.Printf("mutation activity: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, resp)
}

func ListIssueMutationActivity(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	resp, err := listMutationActivity(user.ID, "issue", id)
	if err != nil {
		log.Printf("issue mutation activity: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, resp)
}

func runUndoMode(w http.ResponseWriter, r *http.Request, mode undoMode, byRequest bool, resolve bool) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// byRequest expands to all mutation_log rows that share the request_id —
	// for non-batch mutations this is a single row; for bulk operations
	// (PATCH /api/issues) every subject in the batch shares one request_id
	// AND one batch_id, and a single user-facing undo must revert them
	// all atomically (PAI-316).
	if byRequest {
		requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
		if requestID == "" {
			jsonError(w, "invalid request id", http.StatusBadRequest)
			return
		}
		var batchRows []mutationLogRow
		if mode == undoModeUndo {
			batchRows, err = loadUndoableMutationsByRequestID(tx, requestID, user.ID)
		} else {
			batchRows, err = loadRedoableMutationsByRequestID(tx, requestID, user.ID)
		}
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		if len(batchRows) == 0 {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		// Redo replays in chronological order; undo unwinds in reverse.
		// Loaded order is DESC (newest first), so flip for redo.
		if mode == undoModeRedo {
			for i, j := 0, len(batchRows)-1; i < j; i, j = i+1, j-1 {
				batchRows[i], batchRows[j] = batchRows[j], batchRows[i]
			}
		}

		var resolution *undoResolutionPayload
		if resolve {
			var payload undoResolutionPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				jsonError(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			resolution = &payload
		}

		var lastBody map[string]any
		var rowConflict *undoConflictResponse
		var rowConflictStatus int
		for _, row := range batchRows {
			body, conflict, status, err := applyMutationFlowTx(r.Context(), tx, row, user.ID, mode, resolution)
			if err != nil {
				log.Printf("undo flow: %v", err)
				if status == http.StatusLocked {
					jsonError(w, "irreversible mutation", status)
					return
				}
				jsonError(w, "undo failed", status)
				return
			}
			if conflict != nil {
				// First conflict in the batch aborts the whole tx —
				// the user resolves the conflicting row via /undo/{id}/resolve
				// and re-issues the request.
				rowConflict = conflict
				rowConflictStatus = status
				break
			}
			if status == http.StatusLocked {
				jsonError(w, "irreversible mutation", http.StatusLocked)
				return
			}
			if status == http.StatusGone {
				jsonError(w, "mutation is no longer on your active stack", http.StatusGone)
				return
			}
			lastBody = body
		}
		if rowConflict != nil {
			w.WriteHeader(rowConflictStatus)
			_ = json.NewEncoder(w).Encode(rowConflict)
			return
		}
		if err := tx.Commit(); err != nil {
			jsonError(w, "undo failed", http.StatusInternalServerError)
			return
		}
		runUndoMutationSideEffects(batchRows)
		// Aggregate response: keep the single-row body shape (caller
		// expectations) and add batch_size so callers that care can
		// tell how many rows were affected.
		if lastBody == nil {
			lastBody = map[string]any{}
		}
		lastBody["batch_size"] = len(batchRows)
		jsonOK(w, lastBody)
		return
	}

	var row mutationLogRow
	logID, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if mode == undoModeUndo {
		row, err = loadUndoableMutation(tx, logID, user.ID)
	} else {
		row, err = loadRedoableMutation(tx, logID, user.ID)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var resolution *undoResolutionPayload
	if resolve {
		var payload undoResolutionPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		resolution = &payload
	}
	body, conflict, status, err := applyMutationFlowTx(r.Context(), tx, row, user.ID, mode, resolution)
	if err != nil {
		log.Printf("undo flow: %v", err)
		if status == http.StatusLocked {
			jsonError(w, "irreversible mutation", status)
			return
		}
		jsonError(w, "undo failed", status)
		return
	}
	if conflict != nil {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(conflict)
		return
	}
	if status == http.StatusLocked {
		jsonError(w, "irreversible mutation", http.StatusLocked)
		return
	}
	if status == http.StatusGone {
		jsonError(w, "mutation is no longer on your active stack", http.StatusGone)
		return
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "undo failed", http.StatusInternalServerError)
		return
	}
	runUndoMutationSideEffects([]mutationLogRow{row})
	jsonOK(w, body)
}

func RedoMutation(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeRedo, false, false)
}

func RedoMutationByRequestID(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeRedo, true, false)
}

func ResolveUndoMutation(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeUndo, false, true)
}

func ResolveRedoMutation(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeRedo, false, true)
}
