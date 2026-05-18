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
	AgentName    string
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
	ReportSummary      string   `json:"report_summary"`
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

type issueTagMutationSnapshot struct {
	IssueID int64 `json:"issue_id"`
	TagID   int64 `json:"tag_id"`
	Exists  bool  `json:"exists"`
}

type commentMutationSnapshot struct {
	ID        int64  `json:"id"`
	IssueID   int64  `json:"issue_id,omitempty"`
	AuthorID  *int64 `json:"author_id"`
	Body      string `json:"body,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Exists    bool   `json:"exists"`
}

type timeEntryMutationSnapshot struct {
	ID                 int64    `json:"id"`
	IssueID            int64    `json:"issue_id,omitempty"`
	UserID             int64    `json:"user_id,omitempty"`
	StartedAt          string   `json:"started_at,omitempty"`
	StoppedAt          *string  `json:"stopped_at"`
	Override           *float64 `json:"override"`
	Comment            string   `json:"comment,omitempty"`
	CreatedAt          string   `json:"created_at,omitempty"`
	InternalRateHourly *float64 `json:"internal_rate_hourly"`
	MiteID             *int64   `json:"mite_id"`
	Exists             bool     `json:"exists"`
}

type undoConflictError struct {
	Message string
}

func (e *undoConflictError) Error() string { return e.Message }

type undoLockedError struct {
	Message string
}

func (e *undoLockedError) Error() string { return e.Message }

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
	v := strings.TrimSpace(r.Header.Get(SessionHeader))
	if len(v) > agentAttrCap {
		v = v[:agentAttrCap]
	}
	return v
}

// agentNameFromRequest pulls the X-Paimos-Agent-Name header off the
// request, trims whitespace, and caps the result at agentAttrCap (64
// chars). Empty values become the empty string so recordMutation can
// store SQL NULL via nullableString. PAI-354 — the per-mutation
// attribution counterpart to issue_history's snapshot attribution.
func agentNameFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := strings.TrimSpace(r.Header.Get(AgentNameHeader))
	if len(v) > agentAttrCap {
		v = v[:agentAttrCap]
	}
	return v
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
	// PAI-354: agent_name is an additional attribution column on
	// mutation_log (added in M101). session_id has lived here since
	// M83 — both are application-side capped at agentAttrCap before
	// the INSERT. Empty values persist as SQL NULL.
	sessionVal := args.SessionID
	if len(sessionVal) > agentAttrCap {
		sessionVal = sessionVal[:agentAttrCap]
	}
	agentVal := args.AgentName
	if len(agentVal) > agentAttrCap {
		agentVal = agentVal[:agentAttrCap]
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO mutation_log(
			request_id, user_id, session_id, agent_name, mutation_type, subject_type, subject_id,
			batch_id, parent_log_id,
			inverse_op, before_state, after_state, before_hash, after_hash, undoable, on_user_stack
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		args.RequestID, args.UserID, nullableString(sessionVal), nullableString(agentVal), args.MutationType, args.SubjectType, args.SubjectID,
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
		       report_summary,
		       status, priority, cost_unit, release, billing_type, total_budget, rate_hourly, rate_lp,
		       start_date, end_date, group_state, sprint_state, jira_id, jira_version, jira_text,
		       estimate_hours, estimate_lp, ar_hours, ar_lp, time_override, color, assignee_id, deleted_at
		FROM issues WHERE id = ?
	`, issueID).Scan(
		&snap.ID, &projectID, &snap.Type, &parentID, &snap.Title, &snap.Description, &snap.AcceptanceCriteria, &snap.Notes,
		&snap.ReportSummary,
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

func fetchIssueTagMutationSnapshotTx(tx *sql.Tx, issueID, tagID int64) (issueTagMutationSnapshot, error) {
	snap := issueTagMutationSnapshot{IssueID: issueID, TagID: tagID}
	var exists int
	err := tx.QueryRow(`SELECT 1 FROM issue_tags WHERE issue_id=? AND tag_id=?`, issueID, tagID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return snap, nil
		}
		return snap, err
	}
	snap.Exists = true
	return snap, nil
}

func issueTagSnapshotFromRow(tx *sql.Tx, row mutationLogRow) (issueTagMutationSnapshot, error) {
	issueID, tagID, err := issueTagIDsFromRow(row)
	if err != nil {
		return issueTagMutationSnapshot{}, err
	}
	return fetchIssueTagMutationSnapshotTx(tx, issueID, tagID)
}

func issueTagIDsFromRow(row mutationLogRow) (int64, int64, error) {
	for _, raw := range []string{row.AfterState, row.BeforeState} {
		var snap issueTagMutationSnapshot
		if err := json.Unmarshal([]byte(raw), &snap); err == nil && snap.IssueID > 0 && snap.TagID > 0 {
			return snap.IssueID, snap.TagID, nil
		}
	}
	var inv InverseOp
	if err := json.Unmarshal([]byte(row.InverseOp), &inv); err != nil {
		return 0, 0, err
	}
	if inv.Method == http.MethodDelete && strings.Contains(inv.Path, "/tags/") {
		return parseIssueTagIDsFromPath(inv.Path)
	}
	if inv.Method == http.MethodPost && strings.HasSuffix(inv.Path, "/tags") {
		issueID, err := parseIssueIDFromPath(inv.Path)
		if err != nil {
			return 0, 0, err
		}
		bodyBytes, _ := json.Marshal(inv.Body)
		var body struct {
			TagID int64 `json:"tag_id"`
		}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			return 0, 0, err
		}
		if body.TagID <= 0 {
			return 0, 0, fmt.Errorf("tag id not found in inverse op")
		}
		return issueID, body.TagID, nil
	}
	return 0, 0, fmt.Errorf("issue tag ids not found for mutation row %d", row.ID)
}

func fetchCommentMutationSnapshotTx(tx *sql.Tx, commentID int64) (commentMutationSnapshot, error) {
	snap := commentMutationSnapshot{ID: commentID}
	var authorID sql.NullInt64
	err := tx.QueryRow(`
		SELECT id, issue_id, author_id, body, created_at
		FROM comments
		WHERE id = ?
	`, commentID).Scan(&snap.ID, &snap.IssueID, &authorID, &snap.Body, &snap.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return snap, nil
		}
		return snap, err
	}
	snap.AuthorID = nullInt64Ptr(authorID)
	snap.Exists = true
	return snap, nil
}

func fetchTimeEntryMutationSnapshotTx(tx *sql.Tx, entryID int64) (timeEntryMutationSnapshot, error) {
	snap := timeEntryMutationSnapshot{ID: entryID}
	var stoppedAt sql.NullString
	var override sql.NullFloat64
	var internalRate sql.NullFloat64
	var miteID sql.NullInt64
	err := tx.QueryRow(`
		SELECT id, issue_id, user_id, started_at, stopped_at, override, comment, created_at, internal_rate_hourly, mite_id
		FROM time_entries
		WHERE id = ?
	`, entryID).Scan(
		&snap.ID, &snap.IssueID, &snap.UserID, &snap.StartedAt, &stoppedAt,
		&override, &snap.Comment, &snap.CreatedAt, &internalRate, &miteID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return snap, nil
		}
		return snap, err
	}
	snap.StoppedAt = nullStringPtr(stoppedAt)
	snap.Override = nullFloat64Ptr(override)
	snap.InternalRateHourly = nullFloat64Ptr(internalRate)
	snap.MiteID = nullInt64Ptr(miteID)
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
			report_summary = ?,
			status = ?, priority = ?, cost_unit = ?, release = ?, billing_type = ?, total_budget = ?,
			rate_hourly = ?, rate_lp = ?, start_date = ?, end_date = ?, group_state = ?, sprint_state = ?,
			jira_id = ?, jira_version = ?, jira_text = ?, estimate_hours = ?, estimate_lp = ?, ar_hours = ?,
			ar_lp = ?, time_override = ?, color = ?, assignee_id = ?, deleted_at = ?, updated_at = ?
		WHERE id = ?
	`,
		snap.Type, snap.ParentID, snap.Title, snap.Description, snap.AcceptanceCriteria, snap.Notes,
		snap.ReportSummary,
		snap.Status, snap.Priority, snap.CostUnit, snap.Release, snap.BillingType, snap.TotalBudget,
		snap.RateHourly, snap.RateLp, snap.StartDate, snap.EndDate, snap.GroupState, snap.SprintState,
		snap.JiraID, snap.JiraVersion, snap.JiraText, snap.EstimateHours, snap.EstimateLp, snap.ArHours,
		snap.ArLp, snap.TimeOverride, snap.Color, snap.AssigneeID, snap.DeletedAt, time.Now().UTC().Format("2006-01-02 15:04:05"),
		issueID,
	)
	return err
}

func applyIssueTagSnapshotTx(ctx context.Context, tx *sql.Tx, snap issueTagMutationSnapshot) error {
	if snap.IssueID <= 0 || snap.TagID <= 0 {
		return fmt.Errorf("issue tag snapshot missing ids")
	}
	if !snap.Exists {
		_, err := tx.ExecContext(ctx, `DELETE FROM issue_tags WHERE issue_id=? AND tag_id=?`, snap.IssueID, snap.TagID)
		return err
	}
	if !issueExistsActiveTx(tx, snap.IssueID) {
		return &undoConflictError{Message: fmt.Sprintf("tag parent issue %d is no longer active", snap.IssueID)}
	}
	if !tagExistsTx(tx, snap.TagID) {
		return &undoConflictError{Message: fmt.Sprintf("tag %d no longer exists", snap.TagID)}
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO issue_tags(issue_id, tag_id) VALUES(?,?)`, snap.IssueID, snap.TagID)
	return err
}

func applyTimeEntrySnapshotTx(ctx context.Context, tx *sql.Tx, entryID int64, snap timeEntryMutationSnapshot) error {
	if !snap.Exists {
		if snap.IssueID > 0 && issueInvoicedTx(tx, snap.IssueID) {
			return &undoLockedError{Message: fmt.Sprintf("time entry issue %d is invoiced", snap.IssueID)}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM time_entries WHERE id = ?`, entryID)
		return err
	}
	if snap.ID == 0 {
		snap.ID = entryID
	}
	if snap.ID != entryID {
		return fmt.Errorf("time entry snapshot id %d does not match path id %d", snap.ID, entryID)
	}
	if snap.IssueID <= 0 {
		return fmt.Errorf("time entry snapshot missing issue_id")
	}
	if !issueExistsActiveTx(tx, snap.IssueID) {
		return &undoConflictError{Message: fmt.Sprintf("time entry parent issue %d is no longer active", snap.IssueID)}
	}
	if issueInvoicedTx(tx, snap.IssueID) {
		return &undoLockedError{Message: fmt.Sprintf("time entry issue %d is invoiced", snap.IssueID)}
	}
	if !userExistsTx(tx, snap.UserID) {
		return &undoConflictError{Message: fmt.Sprintf("time entry user %d no longer exists", snap.UserID)}
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO time_entries(id, issue_id, user_id, started_at, stopped_at, override, comment, created_at, internal_rate_hourly, mite_id)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			issue_id = excluded.issue_id,
			user_id = excluded.user_id,
			started_at = excluded.started_at,
			stopped_at = excluded.stopped_at,
			override = excluded.override,
			comment = excluded.comment,
			created_at = excluded.created_at,
			internal_rate_hourly = excluded.internal_rate_hourly,
			mite_id = excluded.mite_id
	`, snap.ID, snap.IssueID, snap.UserID, snap.StartedAt, snap.StoppedAt, snap.Override, snap.Comment, snap.CreatedAt, snap.InternalRateHourly, snap.MiteID)
	return err
}

func applyCommentSnapshotTx(tx *sql.Tx, commentID int64, snap commentMutationSnapshot) error {
	if !snap.Exists {
		_, err := tx.Exec(`DELETE FROM comments WHERE id = ?`, commentID)
		return err
	}
	if snap.ID == 0 {
		snap.ID = commentID
	}
	if snap.ID != commentID {
		return fmt.Errorf("comment snapshot id %d does not match path id %d", snap.ID, commentID)
	}
	if snap.IssueID <= 0 {
		return fmt.Errorf("comment snapshot missing issue_id")
	}
	if !issueExistsActiveTx(tx, snap.IssueID) {
		return &undoConflictError{Message: fmt.Sprintf("comment parent issue %d is no longer active", snap.IssueID)}
	}
	if snap.AuthorID != nil && !userExistsTx(tx, *snap.AuthorID) {
		snap.AuthorID = nil
	}
	_, err := tx.Exec(`
		INSERT INTO comments(id, issue_id, author_id, body, created_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			issue_id = excluded.issue_id,
			author_id = excluded.author_id,
			body = excluded.body,
			created_at = excluded.created_at
	`, snap.ID, snap.IssueID, snap.AuthorID, snap.Body, snap.CreatedAt)
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

// loadUndoableMutationsByRequestID returns every undoable mutation_log
// row tied to a single user request. For non-batch mutations this is
// always one row; for bulk operations (PATCH /api/issues, etc.) where
// multiple subjects share one request_id AND one batch_id, it returns
// the whole batch in DESC order so the caller can revert them as one
// logical action (PAI-316).
//
// Subtle: enforceUndoStackDepth() marks all-but-one batch-mate as
// on_user_stack=0 to make the batch count as one stack slot. Filtering
// solely on on_user_stack=1 would therefore see only the representative
// row. We instead anchor on the on-stack row for the request, then
// expand by its batch_id so off-stack batch siblings come along.
func loadUndoableMutationsByRequestID(tx *sql.Tx, requestID string, userID int64) ([]mutationLogRow, error) {
	return loadMutationsByRequestID(tx, requestID, userID, undoModeUndo)
}

// loadRedoableMutationsByRequestID — symmetric to the undo loader.
// runUndoMode reverses the loaded DESC order to redo chronologically.
func loadRedoableMutationsByRequestID(tx *sql.Tx, requestID string, userID int64) ([]mutationLogRow, error) {
	return loadMutationsByRequestID(tx, requestID, userID, undoModeRedo)
}

// loadMutationsByRequestID is the shared anchor-then-expand loader for
// both undo and redo. It first finds the actionable representative row
// for the request_id, then — if that row is part of a batch — expands
// to all batch siblings; otherwise returns the single row.
func loadMutationsByRequestID(tx *sql.Tx, requestID string, userID int64, mode undoMode) ([]mutationLogRow, error) {
	requestID = strings.TrimSpace(requestID)
	var anchorBatchID sql.NullString
	var anchorWhere string
	if mode == undoModeUndo {
		anchorWhere = `request_id = ? AND user_id = ? AND undoable = 1 AND on_user_stack = 1 AND undone_at IS NULL`
	} else {
		anchorWhere = `request_id = ? AND user_id = ? AND undoable = 1 AND redoable = 1 AND undone_at IS NOT NULL`
	}
	err := tx.QueryRow(`
		SELECT batch_id FROM mutation_log
		WHERE `+anchorWhere+`
		ORDER BY created_at DESC, id DESC LIMIT 1
	`, requestID, userID).Scan(&anchorBatchID)
	if errors.Is(err, sql.ErrNoRows) {
		// No actionable row for this request — caller renders this as 404.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var (
		rows *sql.Rows
		qerr error
	)
	expandedBatch := anchorBatchID.Valid && anchorBatchID.String != ""
	if expandedBatch {
		// Batch case — expand by batch_id (ignore on_user_stack so
		// off-stack siblings come along). Still gate by undone_at and
		// the mode-specific predicates.
		var modeFilter string
		if mode == undoModeUndo {
			modeFilter = `undoable = 1 AND undone_at IS NULL`
		} else {
			modeFilter = `undoable = 1 AND redoable = 1 AND undone_at IS NOT NULL`
		}
		rows, qerr = tx.Query(`
			SELECT id, request_id, user_id, mutation_type, subject_type, subject_id, batch_id, parent_log_id, inverse_op,
			       before_state, after_state, before_hash, after_hash, undoable, on_user_stack, redoable, undone_at, undone_by, resolution_choice
			FROM mutation_log
			WHERE batch_id = ? AND user_id = ? AND `+modeFilter+`
			ORDER BY created_at DESC, id DESC
		`, anchorBatchID.String, userID)
	} else {
		// Single-row case — return just the anchor.
		rows, qerr = tx.Query(`
			SELECT id, request_id, user_id, mutation_type, subject_type, subject_id, batch_id, parent_log_id, inverse_op,
			       before_state, after_state, before_hash, after_hash, undoable, on_user_stack, redoable, undone_at, undone_by, resolution_choice
			FROM mutation_log
			WHERE `+anchorWhere+`
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, requestID, userID)
	}
	if qerr != nil {
		return nil, qerr
	}
	defer rows.Close()
	var out []mutationLogRow
	for rows.Next() {
		var row mutationLogRow
		var uid sql.NullInt64
		var undoneBy sql.NullInt64
		var undoable, onUserStack, redoable int
		if err := rows.Scan(
			&row.ID, &row.RequestID, &uid, &row.MutationType, &row.SubjectType, &row.SubjectID, &row.BatchID, &row.ParentLogID, &row.InverseOp,
			&row.BeforeState, &row.AfterState, &row.BeforeHash, &row.AfterHash, &undoable, &onUserStack, &redoable, &row.UndoneAt, &undoneBy, &row.ResolutionChoice,
		); err != nil {
			return nil, err
		}
		row.UserID = nullInt64Ptr(uid)
		row.Undoable = undoable == 1
		// Force OnUserStack=true for batch-expanded rows: their DB flag
		// is 0 because enforceUndoStackDepth marks all-but-one batch
		// member off-stack to count as one slot. For the purpose of
		// undoing the *batch as a whole* they're all in scope, and
		// applyMutationFlowTx's StatusGone guard otherwise rejects the
		// siblings.
		if expandedBatch {
			row.OnUserStack = true
		} else {
			row.OnUserStack = onUserStack == 1
		}
		row.Redoable = redoable == 1
		out = append(out, row)
	}
	return out, nil
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
	case "issue_tag":
		snap, err := issueTagSnapshotFromRow(tx, row)
		if err != nil {
			return "", err
		}
		_, hash, err := canonicalState(snap)
		return hash, err
	case "time_entry":
		snap, err := fetchTimeEntryMutationSnapshotTx(tx, row.SubjectID)
		if err != nil {
			return "", err
		}
		_, hash, err := canonicalState(snap)
		return hash, err
	case "comment":
		snap, err := fetchCommentMutationSnapshotTx(tx, row.SubjectID)
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
	case inv.Method == http.MethodPost && strings.HasSuffix(inv.Path, "/tags"):
		issueID, err := parseIssueIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		bodyBytes, err := json.Marshal(inv.Body)
		if err != nil {
			return err
		}
		var body struct {
			TagID int64 `json:"tag_id"`
		}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			return err
		}
		return applyIssueTagSnapshotTx(ctx, tx, issueTagMutationSnapshot{IssueID: issueID, TagID: body.TagID, Exists: true})
	case inv.Method == http.MethodPut && strings.Contains(inv.Path, "/time-entries/"):
		entryID, err := parseTimeEntryIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		bodyBytes, err := json.Marshal(inv.Body)
		if err != nil {
			return err
		}
		var snap timeEntryMutationSnapshot
		if err := json.Unmarshal(bodyBytes, &snap); err != nil {
			return err
		}
		return applyTimeEntrySnapshotTx(ctx, tx, entryID, snap)
	case inv.Method == http.MethodDelete && strings.Contains(inv.Path, "/time-entries/"):
		entryID, err := parseTimeEntryIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		snap, err := fetchTimeEntryMutationSnapshotTx(tx, entryID)
		if err != nil {
			return err
		}
		if !snap.Exists {
			return nil
		}
		snap.Exists = false
		return applyTimeEntrySnapshotTx(ctx, tx, entryID, snap)
	case inv.Method == http.MethodDelete && strings.Contains(inv.Path, "/tags/"):
		issueID, tagID, err := parseIssueTagIDsFromPath(inv.Path)
		if err != nil {
			return err
		}
		return applyIssueTagSnapshotTx(ctx, tx, issueTagMutationSnapshot{IssueID: issueID, TagID: tagID})
	case inv.Method == http.MethodPut && strings.Contains(inv.Path, "/comments/"):
		commentID, err := parseCommentIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		bodyBytes, err := json.Marshal(inv.Body)
		if err != nil {
			return err
		}
		var snap commentMutationSnapshot
		if err := json.Unmarshal(bodyBytes, &snap); err != nil {
			return err
		}
		return applyCommentSnapshotTx(tx, commentID, snap)
	case inv.Method == http.MethodDelete && strings.Contains(inv.Path, "/comments/"):
		commentID, err := parseCommentIDFromPath(inv.Path)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, commentID)
		return err
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

func parseTimeEntryIDFromPath(path string) (int64, error) {
	path = strings.TrimSpace(path)
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "time-entries" && i+1 < len(parts) {
			return strconv.ParseInt(parts[i+1], 10, 64)
		}
	}
	return 0, fmt.Errorf("time entry id not found in path %q", path)
}

func parseIssueTagIDsFromPath(path string) (int64, int64, error) {
	issueID, err := parseIssueIDFromPath(path)
	if err != nil {
		return 0, 0, err
	}
	path = strings.TrimSpace(path)
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "tags" && i+1 < len(parts) {
			tagID, err := strconv.ParseInt(parts[i+1], 10, 64)
			if err != nil {
				return 0, 0, err
			}
			return issueID, tagID, nil
		}
	}
	return 0, 0, fmt.Errorf("tag id not found in path %q", path)
}

func parseCommentIDFromPath(path string) (int64, error) {
	path = strings.TrimSpace(path)
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "comments" && i+1 < len(parts) {
			return strconv.ParseInt(parts[i+1], 10, 64)
		}
	}
	return 0, fmt.Errorf("comment id not found in path %q", path)
}

func UndoMutation(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeUndo, false, false)
}

func UndoMutationByRequestID(w http.ResponseWriter, r *http.Request) {
	runUndoMode(w, r, undoModeUndo, true, false)
}
