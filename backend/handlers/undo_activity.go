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
			return nil, nil, http.StatusConflict, opErr
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

func loadRedoableMutationByRequestID(tx *sql.Tx, requestID string, userID int64) (mutationLogRow, error) {
	var row mutationLogRow
	var uid sql.NullInt64
	var undoneBy sql.NullInt64
	var undoable, onUserStack, redoable int
	err := tx.QueryRow(`
		SELECT id, request_id, user_id, mutation_type, subject_type, subject_id, batch_id, parent_log_id, inverse_op,
		       before_state, after_state, before_hash, after_hash, undoable, on_user_stack, redoable, undone_at, undone_by, resolution_choice
		FROM mutation_log
		WHERE request_id = ? AND user_id = ? AND redoable = 1 AND undone_at IS NOT NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, strings.TrimSpace(requestID), userID).Scan(
		&row.ID, &row.RequestID, &uid, &row.MutationType, &row.SubjectType, &row.SubjectID, &row.BatchID, &row.ParentLogID, &row.InverseOp,
		&row.BeforeState, &row.AfterState, &row.BeforeHash, &row.AfterHash, &undoable, &onUserStack, &redoable, &row.UndoneAt, &undoneBy, &row.ResolutionChoice,
	)
	if err != nil {
		return row, err
	}
	row.UserID = nullInt64Ptr(uid)
	row.Undoable = undoable == 1
	row.OnUserStack = onUserStack == 1
	row.Redoable = redoable == 1
	return row, nil
}

func issueLabel(id int64) string {
	var key string
	err := db.DB.QueryRow(`SELECT issue_key FROM issues WHERE id = ?`, id).Scan(&key)
	if err == nil && strings.TrimSpace(key) != "" {
		return key
	}
	return fmt.Sprintf("Issue %d", id)
}

func mutationSummary(row mutationLogRow) string {
	switch row.MutationType {
	case "ai.issue.update", "issue.update":
		return "Updated issue fields"
	case "ai.issue.relation.create", "issue.relation.create":
		return "Added issue relation"
	case "ai.issue.relation.delete", "issue.relation.delete":
		return "Removed issue relation"
	default:
		return strings.ReplaceAll(row.MutationType, ".", " → ")
	}
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
		SELECT id, request_id, mutation_type, subject_type, subject_id, undoable, on_user_stack, redoable, undone_at, created_at
		FROM mutation_log
		WHERE user_id = ?`
	args := []any{userID}
	if subjectType != "" && subjectID > 0 {
		if subjectType == "issue" {
			query += ` AND ((subject_type = 'issue' AND subject_id = ?) OR (subject_type = 'issue_relation' AND subject_id = ?))`
			args = append(args, subjectID, subjectID)
		} else {
			query += ` AND subject_type = ? AND subject_id = ?`
			args = append(args, subjectType, subjectID)
		}
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT 30`
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return resp, err
	}
	defer rows.Close()
	for rows.Next() {
		var row mutationActivityRow
		var undoable, onUserStack, redoable int
		var undoneAt sql.NullString
		if err := rows.Scan(&row.ID, &row.RequestID, &row.MutationType, &row.SubjectType, &row.SubjectID, &undoable, &onUserStack, &redoable, &undoneAt, &row.CreatedAt); err != nil {
			return resp, err
		}
		row.Undoable = undoable == 1
		row.OnUserStack = onUserStack == 1 && !undoneAt.Valid
		row.Redoable = redoable == 1 && undoneAt.Valid
		row.Undone = undoneAt.Valid
		row.SubjectLabel = issueLabel(row.SubjectID)
		row.Summary = mutationSummary(mutationLogRow{MutationType: row.MutationType})
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

	var row mutationLogRow
	if byRequest {
		requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
		if requestID == "" {
			jsonError(w, "invalid request id", http.StatusBadRequest)
			return
		}
		if mode == undoModeUndo {
			row, err = loadUndoableMutationByRequestID(tx, requestID, user.ID)
		} else {
			row, err = loadRedoableMutationByRequestID(tx, requestID, user.ID)
		}
	} else {
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
