package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	RequestIDHeader       = "X-PAIMOS-Request-Id"
	AIRequestIDHeader     = "X-PAIMOS-AI-Request-Id"
	AIActionHeader        = "X-PAIMOS-AI-Action"
	AISubActionHeader     = "X-PAIMOS-AI-Sub-Action"
	defaultUndoStackDepth = 3
	snapshotStringCap     = 32 * 1024
)

type requestIDContextKey struct{}

type InverseOp struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Body   interface{} `json:"body,omitempty"`
}

type mutationRecordArgs struct {
	RequestID    string
	UserID       *int64
	SessionID    string
	MutationType string
	SubjectType  string
	SubjectID    int64
	BatchID      string
	ParentLogID  *int64
	InverseOp    InverseOp
	BeforeState  any
	AfterState   any
	Undoable     bool
}

type mutationLogRow struct {
	ID               int64
	RequestID        string
	UserID           *int64
	MutationType     string
	SubjectType      string
	SubjectID        int64
	BatchID          sql.NullString
	ParentLogID      sql.NullInt64
	InverseOp        string
	BeforeState      string
	AfterState       string
	BeforeHash       string
	AfterHash        string
	Undoable         bool
	OnUserStack      bool
	Redoable         bool
	UndoneAt         sql.NullString
	ResolutionChoice sql.NullString
}

type issueMutationSnapshot struct {
	ID                 int64    `json:"id"`
	ProjectID          *int64   `json:"project_id,omitempty"`
	Type               string   `json:"type"`
	ParentID           *int64   `json:"parent_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Notes              string   `json:"notes"`
	Status             string   `json:"status"`
	Priority           string   `json:"priority"`
	CostUnit           string   `json:"cost_unit"`
	Release            string   `json:"release"`
	BillingType        *string  `json:"billing_type"`
	TotalBudget        *float64 `json:"total_budget"`
	RateHourly         *float64 `json:"rate_hourly"`
	RateLp             *float64 `json:"rate_lp"`
	StartDate          *string  `json:"start_date"`
	EndDate            *string  `json:"end_date"`
	GroupState         *string  `json:"group_state"`
	SprintState        *string  `json:"sprint_state"`
	JiraID             *string  `json:"jira_id"`
	JiraVersion        *string  `json:"jira_version"`
	JiraText           *string  `json:"jira_text"`
	EstimateHours      *float64 `json:"estimate_hours"`
	EstimateLp         *float64 `json:"estimate_lp"`
	ArHours            *float64 `json:"ar_hours"`
	ArLp               *float64 `json:"ar_lp"`
	TimeOverride       *float64 `json:"time_override"`
	Color              *string  `json:"color"`
	AssigneeID         *int64   `json:"assignee_id"`
	DeletedAt          *string  `json:"deleted_at"`
}

type relationMutationSnapshot struct {
	SourceID int64  `json:"source_id"`
	TargetID int64  `json:"target_id"`
	Type     string `json:"type"`
	Rank     int    `json:"rank"`
	Exists   bool   `json:"exists"`
}

type undoConflictError struct {
	Message string
}

func (e *undoConflictError) Error() string { return e.Message }

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if requestID == "" {
			requestID = strings.TrimSpace(r.Header.Get(AIRequestIDHeader))
		}
		if requestID == "" {
			requestID = newAIRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		w.Header().Set(RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromRequest(r *http.Request) string {
	if v, ok := r.Context().Value(requestIDContextKey{}).(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if hdr := strings.TrimSpace(r.Header.Get(RequestIDHeader)); hdr != "" {
		return hdr
	}
	if hdr := strings.TrimSpace(r.Header.Get(AIRequestIDHeader)); hdr != "" {
		return hdr
	}
	return newAIRequestID()
}

func mutationTypeForRequest(r *http.Request, base string) string {
	action := strings.TrimSpace(r.Header.Get(AIActionHeader))
	if action == "" {
		return base
	}
	return "ai." + base
}

func sessionIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(SessionHeader))
}

func recordMutation(ctx context.Context, tx *sql.Tx, args mutationRecordArgs) (int64, error) {
	if tx == nil {
		return 0, errors.New("recordMutation requires transaction")
	}
	if strings.TrimSpace(args.RequestID) == "" {
		args.RequestID = newAIRequestID()
	}
	beforeJSON, beforeHash, err := canonicalState(args.BeforeState)
	if err != nil {
		return 0, err
	}
	afterJSON, afterHash, err := canonicalState(args.AfterState)
	if err != nil {
		return 0, err
	}
	inverseJSON, err := json.Marshal(args.InverseOp)
	if err != nil {
		return 0, err
	}
	undoable := 0
	if args.Undoable {
		undoable = 1
	}
	onUserStack := 0
	if args.Undoable && args.UserID != nil && *args.UserID > 0 {
		onUserStack = 1
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO mutation_log(
			request_id, user_id, session_id, mutation_type, subject_type, subject_id,
			batch_id, parent_log_id,
			inverse_op, before_state, after_state, before_hash, after_hash, undoable, on_user_stack
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		args.RequestID, args.UserID, nullableString(args.SessionID), args.MutationType, args.SubjectType, args.SubjectID,
		nullableString(args.BatchID), args.ParentLogID,
		string(inverseJSON), string(beforeJSON), string(afterJSON), beforeHash, afterHash, undoable, onUserStack,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if args.UserID != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE mutation_log SET redoable = 0 WHERE user_id = ? AND redoable = 1`, *args.UserID); err != nil {
			return 0, err
		}
	}
	if onUserStack == 1 && args.UserID != nil {
		if err := enforceUndoStackDepth(ctx, tx, *args.UserID); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func canonicalState(v any) ([]byte, string, error) {
	sanitized := sanitizeSnapshotValue(v)
	blob, err := json.Marshal(sanitized)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(blob)
	return blob, hex.EncodeToString(sum[:]), nil
}

func sanitizeSnapshotValue(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case bool,
		float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return x
	case json.Number:
		return x.String()
	case map[string]any:
		out := make(map[string]any, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out[k] = sanitizeSnapshotValue(x[k])
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = sanitizeSnapshotValue(x[i])
		}
		return out
	case string:
		if len(x) > snapshotStringCap {
			return x[:snapshotStringCap]
		}
		return x
	default:
		blob, err := json.Marshal(v)
		if err != nil {
			return v
		}
		var generic any
		if err := json.Unmarshal(blob, &generic); err != nil {
			return v
		}
		return sanitizeSnapshotValue(generic)
	}
}

func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func enforceUndoStackDepth(ctx context.Context, tx *sql.Tx, userID int64) error {
	depth, err := loadUndoStackDepth(tx)
	if err != nil {
		return err
	}
	if depth <= 0 {
		depth = defaultUndoStackDepth
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(batch_id, '')
		FROM mutation_log
		WHERE user_id = ? AND on_user_stack = 1 AND undone_at IS NULL
		ORDER BY created_at DESC, id DESC
	`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int64
	seenBatch := map[string]struct{}{}
	slots := 0
	for rows.Next() {
		var id int64
		var batchID string
		if err := rows.Scan(&id, &batchID); err != nil {
			return err
		}
		if batchID != "" {
			if _, ok := seenBatch[batchID]; ok {
				ids = append(ids, id)
				continue
			}
			seenBatch[batchID] = struct{}{}
		}
		slots++
		if slots > depth {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	ph := makePlaceholders(len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err = tx.ExecContext(ctx, `UPDATE mutation_log SET on_user_stack = 0 WHERE id IN (`+ph+`)`, args...)
	return err
}

type appSettingReader interface {
	QueryRow(query string, args ...any) *sql.Row
}

func loadUndoStackDepth(q appSettingReader) (int, error) {
	row := q.QueryRow(`SELECT value FROM app_settings WHERE key='undo_stack_depth'`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultUndoStackDepth, nil
		}
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return defaultUndoStackDepth, nil
	}
	return n, nil
}

func fetchIssueMutationSnapshotTx(tx *sql.Tx, issueID int64) (issueMutationSnapshot, error) {
	var snap issueMutationSnapshot
	var projectID sql.NullInt64
	var parentID sql.NullInt64
	var billingType, startDate, endDate, groupState, sprintState, jiraID, jiraVersion, jiraText, color, deletedAt sql.NullString
	var totalBudget, rateHourly, rateLp, estimateHours, estimateLp, arHours, arLp, timeOverride sql.NullFloat64
	var assigneeID sql.NullInt64
	err := tx.QueryRow(`
		SELECT id, project_id, type, parent_id, title, description, acceptance_criteria, notes,
		       status, priority, cost_unit, release, billing_type, total_budget, rate_hourly, rate_lp,
		       start_date, end_date, group_state, sprint_state, jira_id, jira_version, jira_text,
		       estimate_hours, estimate_lp, ar_hours, ar_lp, time_override, color, assignee_id, deleted_at
		FROM issues WHERE id = ?
	`, issueID).Scan(
		&snap.ID, &projectID, &snap.Type, &parentID, &snap.Title, &snap.Description, &snap.AcceptanceCriteria, &snap.Notes,
		&snap.Status, &snap.Priority, &snap.CostUnit, &snap.Release, &billingType, &totalBudget, &rateHourly, &rateLp,
		&startDate, &endDate, &groupState, &sprintState, &jiraID, &jiraVersion, &jiraText,
		&estimateHours, &estimateLp, &arHours, &arLp, &timeOverride, &color, &assigneeID, &deletedAt,
	)
	if err != nil {
		return snap, err
	}
	snap.ProjectID = nullInt64Ptr(projectID)
	snap.ParentID = nullInt64Ptr(parentID)
	snap.BillingType = nullStringPtr(billingType)
	snap.TotalBudget = nullFloat64Ptr(totalBudget)
	snap.RateHourly = nullFloat64Ptr(rateHourly)
	snap.RateLp = nullFloat64Ptr(rateLp)
	snap.StartDate = nullStringPtr(startDate)
	snap.EndDate = nullStringPtr(endDate)
	snap.GroupState = nullStringPtr(groupState)
	snap.SprintState = nullStringPtr(sprintState)
	snap.JiraID = nullStringPtr(jiraID)
	snap.JiraVersion = nullStringPtr(jiraVersion)
	snap.JiraText = nullStringPtr(jiraText)
	snap.EstimateHours = nullFloat64Ptr(estimateHours)
	snap.EstimateLp = nullFloat64Ptr(estimateLp)
	snap.ArHours = nullFloat64Ptr(arHours)
	snap.ArLp = nullFloat64Ptr(arLp)
	snap.TimeOverride = nullFloat64Ptr(timeOverride)
	snap.Color = nullStringPtr(color)
	snap.AssigneeID = nullInt64Ptr(assigneeID)
	snap.DeletedAt = nullStringPtr(deletedAt)
	return snap, nil
}

func fetchRelationMutationSnapshotTx(tx *sql.Tx, sourceID, targetID int64, relType string) (relationMutationSnapshot, error) {
	snap := relationMutationSnapshot{SourceID: sourceID, TargetID: targetID, Type: relType}
	err := tx.QueryRow(`SELECT rank FROM issue_relations WHERE source_id=? AND target_id=? AND type=?`, sourceID, targetID, relType).Scan(&snap.Rank)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return snap, nil
		}
		return snap, err
	}
	snap.Exists = true
	return snap, nil
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func nullFloat64Ptr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	n := v.Float64
	return &n
}

func applyIssueSnapshotTx(tx *sql.Tx, issueID int64, snap issueMutationSnapshot) error {
	_, err := tx.Exec(`
		UPDATE issues SET
			type = ?, parent_id = ?, title = ?, description = ?, acceptance_criteria = ?, notes = ?,
			status = ?, priority = ?, cost_unit = ?, release = ?, billing_type = ?, total_budget = ?,
			rate_hourly = ?, rate_lp = ?, start_date = ?, end_date = ?, group_state = ?, sprint_state = ?,
			jira_id = ?, jira_version = ?, jira_text = ?, estimate_hours = ?, estimate_lp = ?, ar_hours = ?,
			ar_lp = ?, time_override = ?, color = ?, assignee_id = ?, deleted_at = ?, updated_at = ?
		WHERE id = ?
	`,
		snap.Type, snap.ParentID, snap.Title, snap.Description, snap.AcceptanceCriteria, snap.Notes,
		snap.Status, snap.Priority, snap.CostUnit, snap.Release, snap.BillingType, snap.TotalBudget,
		snap.RateHourly, snap.RateLp, snap.StartDate, snap.EndDate, snap.GroupState, snap.SprintState,
		snap.JiraID, snap.JiraVersion, snap.JiraText, snap.EstimateHours, snap.EstimateLp, snap.ArHours,
		snap.ArLp, snap.TimeOverride, snap.Color, snap.AssigneeID, snap.DeletedAt, time.Now().UTC().Format("2006-01-02 15:04:05"),
		issueID,
	)
	return err
}

func loadUndoableMutation(tx *sql.Tx, logID int64, userID int64) (mutationLogRow, error) {
	var row mutationLogRow
	var uid sql.NullInt64
	var undoneBy sql.NullInt64
	var undoable, onUserStack, redoable int
	err := tx.QueryRow(`
		SELECT id, request_id, user_id, mutation_type, subject_type, subject_id, batch_id, parent_log_id, inverse_op,
		       before_state, after_state, before_hash, after_hash, undoable, on_user_stack, redoable, undone_at, undone_by, resolution_choice
		FROM mutation_log
		WHERE id = ? AND user_id = ?
	`, logID, userID).Scan(
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

func loadUndoableMutationByRequestID(tx *sql.Tx, requestID string, userID int64) (mutationLogRow, error) {
	var row mutationLogRow
	var uid sql.NullInt64
	var undoneBy sql.NullInt64
	var undoable, onUserStack, redoable int
	err := tx.QueryRow(`
		SELECT id, request_id, user_id, mutation_type, subject_type, subject_id, batch_id, parent_log_id, inverse_op,
		       before_state, after_state, before_hash, after_hash, undoable, on_user_stack, redoable, undone_at, undone_by, resolution_choice
		FROM mutation_log
		WHERE request_id = ? AND user_id = ? AND undoable = 1 AND on_user_stack = 1 AND undone_at IS NULL
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

func currentMutationHashTx(tx *sql.Tx, row mutationLogRow) (string, error) {
	switch row.SubjectType {
	case "issue":
		snap, err := fetchIssueMutationSnapshotTx(tx, row.SubjectID)
		if err != nil {
			return "", err
		}
		_, hash, err := canonicalState(snap)
		return hash, err
	default:
		var inv InverseOp
		if err := json.Unmarshal([]byte(row.InverseOp), &inv); err != nil {
			return "", err
		}
		if strings.HasSuffix(inv.Path, "/relations") {
			var body struct {
				TargetID int64  `json:"target_id"`
				Type     string `json:"type"`
			}
			bodyBytes, _ := json.Marshal(inv.Body)
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				return "", err
			}
			sourceID, err := parseIssueIDFromPath(inv.Path)
			if err != nil {
				return "", err
			}
			dbSource, dbTarget := sourceID, body.TargetID
			if body.Type == "sprint" {
				dbSource, dbTarget = body.TargetID, sourceID
			}
			snap, err := fetchRelationMutationSnapshotTx(tx, dbSource, dbTarget, body.Type)
			if err != nil {
				return "", err
			}
			_, hash, err := canonicalState(snap)
			return hash, err
		}
		return "", fmt.Errorf("unsupported subject type %s", row.SubjectType)
	}
}

func executeInverseOpTx(ctx context.Context, tx *sql.Tx, inv InverseOp) error {
	switch {
	case inv.Method == http.MethodPut && strings.Contains(inv.Path, "/issues/"):
		issueID, err := parseIssueIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		bodyBytes, err := json.Marshal(inv.Body)
		if err != nil {
			return err
		}
		var snap issueMutationSnapshot
		if err := json.Unmarshal(bodyBytes, &snap); err != nil {
			return err
		}
		return applyIssueSnapshotTx(tx, issueID, snap)
	case inv.Method == http.MethodDelete && strings.HasSuffix(inv.Path, "/relations"):
		sourceID, err := parseIssueIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		bodyBytes, err := json.Marshal(inv.Body)
		if err != nil {
			return err
		}
		var body struct {
			TargetID int64  `json:"target_id"`
			Type     string `json:"type"`
		}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			return err
		}
		dbSource, dbTarget := sourceID, body.TargetID
		if body.Type == "sprint" {
			dbSource, dbTarget = body.TargetID, sourceID
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM issue_relations WHERE source_id=? AND target_id=? AND type=?`, dbSource, dbTarget, body.Type)
		if err != nil {
			return err
		}
		deleteIssueEntityRelation(dbSource, dbTarget, body.Type)
		return nil
	case inv.Method == http.MethodPost && strings.HasSuffix(inv.Path, "/relations"):
		sourceID, err := parseIssueIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		bodyBytes, err := json.Marshal(inv.Body)
		if err != nil {
			return err
		}
		var body struct {
			TargetID int64  `json:"target_id"`
			Type     string `json:"type"`
			Rank     int    `json:"rank"`
		}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			return err
		}
		dbSource, dbTarget := sourceID, body.TargetID
		if body.Type == "sprint" {
			dbSource, dbTarget = body.TargetID, sourceID
		}
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO issue_relations(source_id, target_id, type, rank) VALUES(?,?,?,?)`, dbSource, dbTarget, body.Type, body.Rank)
		if err != nil {
			return err
		}
		upsertIssueEntityRelation(dbSource, dbTarget, body.Type)
		return nil
	default:
		return fmt.Errorf("unsupported inverse op %s %s", inv.Method, inv.Path)
	}
}

func parseIssueIDFromPath(path string) (int64, error) {
	path = strings.TrimSpace(path)
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "issues" && i+1 < len(parts) {
			return strconv.ParseInt(parts[i+1], 10, 64)
		}
	}
	return 0, fmt.Errorf("issue id not found in path %q", path)
}

func UndoMutation(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeUndo, false, false)
}

func UndoMutationByRequestID(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeUndo, true, false)
}
