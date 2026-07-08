package handlers

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

type aiCallArgs struct {
	RequestID        string
	UserID           *int64
	ActionKey        string
	SubAction        string
	Surface          string
	IssueID          *int64
	ProjectID        *int64
	CustomerID       *int64
	CooperationID    *int64
	Provider         string
	Model            string
	ProfileID        string
	Effort           string
	PromptPresetRef  string
	ContextPack      string
	PromptTokens     int
	CompletionTokens int
	CostMicroUSD     int64
	Outcome          string
	ErrorClass       string
	LatencyMs        int64
}

type aiCallListRow struct {
	ID               int64  `json:"id"`
	RequestID        string `json:"request_id"`
	UserID           *int64 `json:"user_id"`
	Username         string `json:"username"`
	ActionKey        string `json:"action_key"`
	SubAction        string `json:"sub_action"`
	Surface          string `json:"surface"`
	IssueID          *int64 `json:"issue_id"`
	ProjectID        *int64 `json:"project_id"`
	CustomerID       *int64 `json:"customer_id"`
	CooperationID    *int64 `json:"cooperation_id"`
	SubjectLabel     string `json:"subject_label"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	ProfileID        string `json:"profile_id,omitempty"`
	Effort           string `json:"effort,omitempty"`
	PromptPresetRef  string `json:"prompt_preset_ref,omitempty"`
	ContextPack      string `json:"context_pack,omitempty"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CostMicroUSD     int64  `json:"cost_micro_usd"`
	Outcome          string `json:"outcome"`
	ErrorClass       string `json:"error_class,omitempty"`
	LatencyMs        int64  `json:"latency_ms"`
	CreatedAt        string `json:"created_at"`
}

type aiCallListResponse struct {
	Rows              []aiCallListRow `json:"rows"`
	NextCursor        string          `json:"next_cursor"`
	TotalCount        int64           `json:"total_count"`
	TotalCostMicroUSD int64           `json:"total_cost_micro_usd"`
}

type issueAIActivityRow struct {
	LogID            int64  `json:"log_id"`
	RunID            *int64 `json:"run_id,omitempty"`
	Kind             string `json:"kind"`
	RequestID        string `json:"request_id"`
	ActionKey        string `json:"action_key"`
	SubAction        string `json:"sub_action"`
	Surface          string `json:"surface"`
	UserID           *int64 `json:"user_id"`
	UserName         string `json:"user_name"`
	Outcome          string `json:"outcome"`
	Status           string `json:"status,omitempty"`
	ProviderKind     string `json:"provider_kind,omitempty"`
	ProviderID       string `json:"provider_id,omitempty"`
	ProviderLabel    string `json:"provider_label,omitempty"`
	RunMode          string `json:"run_mode,omitempty"`
	AgentName        string `json:"agent_name,omitempty"`
	DeviceID         string `json:"device_id,omitempty"`
	Version          string `json:"version,omitempty"`
	DeployTarget     string `json:"deploy_target,omitempty"`
	TestsSummary     string `json:"tests_summary,omitempty"`
	Error            string `json:"error,omitempty"`
	LatencyMs        int64  `json:"latency_ms"`
	Model            string `json:"model"`
	ProfileID        string `json:"profile_id,omitempty"`
	Effort           string `json:"effort,omitempty"`
	PromptPresetRef  string `json:"prompt_preset_ref,omitempty"`
	ContextPack      string `json:"context_pack,omitempty"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CostMicroUSD     int64  `json:"cost_micro_usd"`
	OnUserStack      bool   `json:"on_user_stack"`
	CreatedAt        string `json:"created_at"`
	FinishedAt       string `json:"finished_at,omitempty"`
	SourceDraftRunID *int64 `json:"source_draft_run_id,omitempty"`
	FollowupRunID    *int64 `json:"followup_run_id,omitempty"`
}

type issueAIActivityResponse struct {
	Rows          []issueAIActivityRow `json:"rows"`
	Count         int                  `json:"count"`
	LastWeekCount int                  `json:"last_week_count"`
}

type aiCallFilters struct {
	From      string
	To        string
	UserID    *int64
	ActionKey string
	Model     string
	Outcome   string
	Surface   string
	IssueID   *int64
	Limit     int
	Cursor    string
	SelfOnly  *int64
}

type issueAIActivityFilters struct {
	Kind      string
	ActionKey string
	Provider  string
	ProfileID string
	Agent     string
	Status    string
}

func newAIRequestID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString()
}

func recordAICall(ctx context.Context, args aiCallArgs) {
	if strings.TrimSpace(args.RequestID) == "" {
		args.RequestID = newAIRequestID()
	}
	if args.CostMicroUSD == 0 && args.Model != "" && (args.PromptTokens > 0 || args.CompletionTokens > 0) {
		if modelPricingCacheNeedsWarmup() {
			refreshModelsCacheAsync("ai_call_cost_lookup")
		}
		args.CostMicroUSD = lookupAICallCostMicroUSD(args.Model, args.PromptTokens, args.CompletionTokens)
	}
	totalTokens := args.PromptTokens + args.CompletionTokens
	_, err := db.DB.ExecContext(ctx, `
		INSERT INTO ai_calls(
			request_id, user_id, action_key, sub_action, surface,
			issue_id, project_id, customer_id, cooperation_id,
			provider, model, profile_id, effort, prompt_preset_ref, context_pack,
			prompt_tokens, completion_tokens, total_tokens,
			cost_micro_usd, outcome, error_class, latency_ms
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		args.RequestID,
		args.UserID,
		strings.TrimSpace(args.ActionKey),
		strings.TrimSpace(args.SubAction),
		strings.TrimSpace(args.Surface),
		args.IssueID,
		args.ProjectID,
		args.CustomerID,
		args.CooperationID,
		strings.TrimSpace(args.Provider),
		strings.TrimSpace(args.Model),
		strings.TrimSpace(args.ProfileID),
		strings.TrimSpace(args.Effort),
		strings.TrimSpace(args.PromptPresetRef),
		strings.TrimSpace(args.ContextPack),
		args.PromptTokens,
		args.CompletionTokens,
		totalTokens,
		args.CostMicroUSD,
		strings.TrimSpace(args.Outcome),
		strings.TrimSpace(args.ErrorClass),
		args.LatencyMs,
	)
	if err != nil {
		log.Printf("ai_calls: insert: %v", err)
		return
	}
	if args.CostMicroUSD == 0 && args.Model != "" && (args.PromptTokens > 0 || args.CompletionTokens > 0) {
		if modelPricingCacheNeedsWarmup() {
			refreshModelsCacheAsync("ai_call_zero_cost_insert")
		} else {
			backfillRecentAICallCostsAsync("ai_call_zero_cost_insert")
		}
	}
}

// lookupAICallCostMicroUSD returns the per-call cost in micro-USD for
// the given model + token counts. PAI-448: hardened against two
// historical bug-sources:
//
//  1. The old implementation walked only the six "curated" buckets
//     (Free / OpenWeights / Frontier / Value / Cheapest / Fastest).
//     Any model that ended up actually being called but wasn't in
//     a curated bucket (admin-pinned, rotated out, custom) silently
//     returned 0 cost.
//  2. modelsCache.payload was nil until the SPA first hit
//     /api/ai/models. AI calls issued during the cold-cache window
//     recorded 0 cost forever.
//
// We now build a private flat ID-keyed view from the full canonical
// /v1/models response, keep bucket scan as a compatibility fallback,
// and fall back to staticFallbackPayload when the live cache hasn't
// been hydrated. The unit math is unchanged — and worth restating:
// PricingPromptPerMtok is USD per million tokens, multiplied by raw
// token count gives micro-USD directly (USD/Mtok × tokens =
// USD × tokens/Mtok = USD × tokens/1e6 = 1e-6 USD = micro-USD).
func lookupAICallCostMicroUSD(model string, promptTokens, completionTokens int) int64 {
	model = strings.TrimSpace(model)
	if model == "" {
		return 0
	}
	m, ok := findModelByID(model)
	if !ok {
		return 0
	}
	cost := m.PricingPromptPerMtok*float64(promptTokens) + m.PricingCompletionPerMtok*float64(completionTokens)
	return int64(math.Round(cost))
}

// findModelByID returns the pickedModel for an ID across the full
// private pricing index first, then the curated buckets, with a
// static-fallback safety net for the cold-cache window. Exported
// (to the package) so other handlers — e.g. the bulk cost-estimate
// endpoint — share the same lookup.
func findModelByID(id string) (pickedModel, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return pickedModel{}, false
	}
	modelsCache.mu.RLock()
	payload := modelsCache.payload
	modelsCache.mu.RUnlock()
	if payload == nil {
		payload = staticFallbackPayload(0)
	}
	if payload.allModels != nil {
		if m, ok := payload.allModels[id]; ok {
			return m, true
		}
	}
	for _, bucket := range [][]pickedModel{
		payload.Categories.Free,
		payload.Categories.OpenWeights,
		payload.Categories.Frontier,
		payload.Categories.Value,
		payload.Categories.Cheapest,
		payload.Categories.Fastest,
	} {
		for _, m := range bucket {
			if m.ID == id {
				return m, true
			}
		}
	}
	return pickedModel{}, false
}

func backfillRecentAICallCosts(ctx context.Context, since time.Duration, limit int) (int, error) {
	if db.DB == nil {
		return 0, nil
	}
	if since <= 0 {
		since = 90 * 24 * time.Hour
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	cutoff := time.Now().UTC().Add(-since).Format("2006-01-02 15:04:05")
	rows, err := db.DB.QueryContext(ctx, `
		SELECT id, model, prompt_tokens, completion_tokens
		FROM ai_calls
		WHERE cost_micro_usd = 0
		  AND TRIM(COALESCE(model, '')) != ''
		  AND (prompt_tokens > 0 OR completion_tokens > 0)
		  AND created_at >= ?
		ORDER BY id DESC
		LIMIT ?
	`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	type row struct {
		id               int64
		model            string
		promptTokens     int
		completionTokens int
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.model, &r.promptTokens, &r.completionTokens); err != nil {
			rows.Close()
			return 0, err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	updated := 0
	for _, r := range pending {
		cost := lookupAICallCostMicroUSD(r.model, r.promptTokens, r.completionTokens)
		if cost <= 0 {
			continue
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE ai_calls
			SET cost_micro_usd = ?
			WHERE id = ? AND cost_micro_usd = 0
		`, cost, r.id)
		if err != nil {
			return updated, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated++
		}
	}
	if err := tx.Commit(); err != nil {
		return updated, err
	}
	return updated, nil
}

func parseAICallContext(raw json.RawMessage) (projectID, customerID, cooperationID *int64) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	var body struct {
		ProjectID     *int64 `json:"project_id"`
		CustomerID    *int64 `json:"customer_id"`
		CooperationID *int64 `json:"cooperation_id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, nil, nil
	}
	return body.ProjectID, body.CustomerID, body.CooperationID
}

func nullableInt64(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func issueProjectID(issueID int64) (int64, error) {
	var projectID int64
	err := db.DB.QueryRow(`SELECT COALESCE(project_id, 0) FROM issues WHERE id=?`, issueID).Scan(&projectID)
	return projectID, err
}

func parseAICallFilters(r *http.Request, selfOnly *int64) (aiCallFilters, error) {
	q := r.URL.Query()
	f := aiCallFilters{
		From:      normalizeDateBound(q.Get("from"), false),
		To:        normalizeDateBound(q.Get("to"), true),
		ActionKey: strings.TrimSpace(q.Get("action_key")),
		Model:     strings.TrimSpace(q.Get("model")),
		Outcome:   strings.TrimSpace(q.Get("outcome")),
		Surface:   strings.TrimSpace(q.Get("surface")),
		Cursor:    strings.TrimSpace(q.Get("cursor")),
		Limit:     50,
		SelfOnly:  selfOnly,
	}
	if lim, err := strconv.Atoi(q.Get("limit")); err == nil && lim > 0 {
		if lim > 200 {
			lim = 200
		}
		f.Limit = lim
	}
	if v := strings.TrimSpace(q.Get("user_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid user_id")
		}
		f.UserID = &id
	}
	if v := strings.TrimSpace(q.Get("issue_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid issue_id")
		}
		f.IssueID = &id
	}
	return f, nil
}

func normalizeDateBound(raw string, end bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) == 10 {
		if end {
			return raw + " 23:59:59.999"
		}
		return raw + " 00:00:00.000"
	}
	return strings.ReplaceAll(raw, "T", " ")
}

func buildAICallWhere(f aiCallFilters) (string, []any, error) {
	var parts []string
	var args []any
	if f.From != "" {
		parts = append(parts, "c.created_at >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		parts = append(parts, "c.created_at <= ?")
		args = append(args, f.To)
	}
	if f.SelfOnly != nil {
		parts = append(parts, "c.user_id = ?")
		args = append(args, *f.SelfOnly)
	}
	if f.UserID != nil {
		parts = append(parts, "c.user_id = ?")
		args = append(args, *f.UserID)
	}
	if f.ActionKey != "" {
		parts = append(parts, "c.action_key = ?")
		args = append(args, f.ActionKey)
	}
	if f.Model != "" {
		parts = append(parts, "c.model = ?")
		args = append(args, f.Model)
	}
	if f.Outcome != "" {
		parts = append(parts, "c.outcome = ?")
		args = append(args, f.Outcome)
	}
	if f.Surface != "" {
		parts = append(parts, "c.surface = ?")
		args = append(args, f.Surface)
	}
	if f.IssueID != nil {
		parts = append(parts, "c.issue_id = ?")
		args = append(args, *f.IssueID)
	}
	if f.Cursor != "" {
		dec, err := url.QueryUnescape(f.Cursor)
		if err != nil {
			return "", nil, err
		}
		pieces := strings.Split(dec, "|")
		if len(pieces) != 2 {
			return "", nil, fmt.Errorf("invalid cursor")
		}
		id, err := strconv.ParseInt(pieces[1], 10, 64)
		if err != nil {
			return "", nil, fmt.Errorf("invalid cursor")
		}
		parts = append(parts, "(c.created_at < ? OR (c.created_at = ? AND c.id < ?))")
		args = append(args, pieces[0], pieces[0], id)
	}
	if len(parts) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(parts, " AND "), args, nil
}

func listAICalls(f aiCallFilters) (aiCallListResponse, error) {
	resp := aiCallListResponse{Rows: []aiCallListRow{}}
	where, args, err := buildAICallWhere(f)
	if err != nil {
		return resp, err
	}

	countSQL := `SELECT COUNT(*), COALESCE(SUM(c.cost_micro_usd), 0) FROM ai_calls c` + where
	// #nosec G701 -- countSQL is fixed SQL plus buildAICallWhere, which emits fixed fragments; user values are placeholders.
	if err := db.DB.QueryRow(countSQL, args...).Scan(&resp.TotalCount, &resp.TotalCostMicroUSD); err != nil {
		return resp, err
	}

	// #nosec G202 -- where comes from buildAICallWhere: fixed fragments with user values as placeholders.
	query := `
		SELECT
			c.id, c.request_id, c.user_id, COALESCE(u.username, 'deleted user'),
			c.action_key, c.sub_action, c.surface,
			c.issue_id, c.project_id, c.customer_id, c.cooperation_id,
			CASE
				WHEN c.issue_id IS NOT NULL THEN COALESCE(ip.key || '-' || i.issue_number || ' — ' || i.title, 'Issue #' || c.issue_id)
				WHEN c.project_id IS NOT NULL THEN COALESCE(pr.key || ' — ' || pr.name, 'Project #' || c.project_id)
				WHEN c.customer_id IS NOT NULL THEN COALESCE(cu.name, 'Customer #' || c.customer_id)
				WHEN c.cooperation_id IS NOT NULL THEN COALESCE(cp.key || ' cooperation', 'Cooperation #' || c.cooperation_id)
				ELSE ''
			END AS subject_label,
			c.provider, c.model, COALESCE(c.profile_id, ''), COALESCE(c.effort, ''),
			COALESCE(c.prompt_preset_ref, ''), COALESCE(c.context_pack, ''),
			c.prompt_tokens, c.completion_tokens, c.total_tokens,
			c.cost_micro_usd, c.outcome, COALESCE(c.error_class, ''), c.latency_ms, c.created_at
		FROM ai_calls c
		LEFT JOIN users u ON u.id = c.user_id
		LEFT JOIN issues i ON i.id = c.issue_id
		LEFT JOIN projects ip ON ip.id = i.project_id
		LEFT JOIN projects pr ON pr.id = c.project_id
		LEFT JOIN customers cu ON cu.id = c.customer_id
		LEFT JOIN project_cooperation pc ON pc.id = c.cooperation_id
		LEFT JOIN projects cp ON cp.id = pc.project_id
	` + where + `
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT ?
	`
	// #nosec G701 -- query is assembled from fixed SQL fragments; user values are placeholders.
	rows, err := db.DB.Query(query, append(args, f.Limit)...)
	if err != nil {
		return resp, err
	}
	defer rows.Close()
	for rows.Next() {
		var row aiCallListRow
		var username string
		var errorClass string
		if err := rows.Scan(
			&row.ID, &row.RequestID, &row.UserID, &username,
			&row.ActionKey, &row.SubAction, &row.Surface,
			&row.IssueID, &row.ProjectID, &row.CustomerID, &row.CooperationID,
			&row.SubjectLabel,
			&row.Provider, &row.Model, &row.ProfileID, &row.Effort, &row.PromptPresetRef, &row.ContextPack,
			&row.PromptTokens, &row.CompletionTokens, &row.TotalTokens,
			&row.CostMicroUSD, &row.Outcome, &errorClass, &row.LatencyMs, &row.CreatedAt,
		); err != nil {
			return resp, err
		}
		row.Username = username
		row.ErrorClass = errorClass
		resp.Rows = append(resp.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return resp, err
	}
	if len(resp.Rows) > 0 {
		last := resp.Rows[len(resp.Rows)-1]
		resp.NextCursor = url.QueryEscape(last.CreatedAt + "|" + strconv.FormatInt(last.ID, 10))
	}
	return resp, nil
}

func getAICallByID(id int64, selfOnly *int64) (aiCallListRow, error) {
	f := aiCallFilters{Limit: 1, SelfOnly: selfOnly}
	where, args, err := buildAICallWhere(f)
	if err != nil {
		return aiCallListRow{}, err
	}
	where = " WHERE c.id = ?"
	args = []any{id}
	if selfOnly != nil {
		where += " AND c.user_id = ?"
		args = append(args, *selfOnly)
	}
	query := `
		SELECT
			c.id, c.request_id, c.user_id, COALESCE(u.username, 'deleted user'),
			c.action_key, c.sub_action, c.surface,
			c.issue_id, c.project_id, c.customer_id, c.cooperation_id,
			CASE
				WHEN c.issue_id IS NOT NULL THEN COALESCE(ip.key || '-' || i.issue_number || ' — ' || i.title, 'Issue #' || c.issue_id)
				WHEN c.project_id IS NOT NULL THEN COALESCE(pr.key || ' — ' || pr.name, 'Project #' || c.project_id)
				WHEN c.customer_id IS NOT NULL THEN COALESCE(cu.name, 'Customer #' || c.customer_id)
				WHEN c.cooperation_id IS NOT NULL THEN COALESCE(cp.key || ' cooperation', 'Cooperation #' || c.cooperation_id)
				ELSE ''
			END AS subject_label,
			c.provider, c.model, COALESCE(c.profile_id, ''), COALESCE(c.effort, ''),
			COALESCE(c.prompt_preset_ref, ''), COALESCE(c.context_pack, ''),
			c.prompt_tokens, c.completion_tokens, c.total_tokens,
			c.cost_micro_usd, c.outcome, COALESCE(c.error_class, ''), c.latency_ms, c.created_at
		FROM ai_calls c
		LEFT JOIN users u ON u.id = c.user_id
		LEFT JOIN issues i ON i.id = c.issue_id
		LEFT JOIN projects ip ON ip.id = i.project_id
		LEFT JOIN projects pr ON pr.id = c.project_id
		LEFT JOIN customers cu ON cu.id = c.customer_id
		LEFT JOIN project_cooperation pc ON pc.id = c.cooperation_id
		LEFT JOIN projects cp ON cp.id = pc.project_id
	` + where + ` LIMIT 1`
	var row aiCallListRow
	var username string
	var errorClass string
	err = db.DB.QueryRow(query, args...).Scan(
		&row.ID, &row.RequestID, &row.UserID, &username,
		&row.ActionKey, &row.SubAction, &row.Surface,
		&row.IssueID, &row.ProjectID, &row.CustomerID, &row.CooperationID,
		&row.SubjectLabel,
		&row.Provider, &row.Model, &row.ProfileID, &row.Effort, &row.PromptPresetRef, &row.ContextPack,
		&row.PromptTokens, &row.CompletionTokens, &row.TotalTokens,
		&row.CostMicroUSD, &row.Outcome, &errorClass, &row.LatencyMs, &row.CreatedAt,
	)
	if err != nil {
		return aiCallListRow{}, err
	}
	row.Username = username
	row.ErrorClass = errorClass
	return row, nil
}

func AIListCalls(w http.ResponseWriter, r *http.Request) {
	f, err := parseAICallFilters(r, nil)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := listAICalls(f)
	if err != nil {
		log.Printf("ai_calls: list: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, rows)
}

func AIListMyCalls(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	f, err := parseAICallFilters(r, &user.ID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := listAICalls(f)
	if err != nil {
		log.Printf("ai_calls: list self: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, rows)
}

func AIGetCall(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	row, err := getAICallByID(id, nil)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("ai_calls: get: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, row)
}

func AIExportCallsCSV(w http.ResponseWriter, r *http.Request) {
	exportAICallsCSV(w, r, nil)
}

func AIExportMyCallsCSV(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	exportAICallsCSV(w, r, &user.ID)
}

func exportAICallsCSV(w http.ResponseWriter, r *http.Request, selfOnly *int64) {
	f, err := parseAICallFilters(r, nil)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.SelfOnly = selfOnly
	where, args, err := buildAICallWhere(f)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	// #nosec G202 -- where comes from buildAICallWhere: fixed fragments with user values as placeholders.
	query := `
		SELECT
			c.created_at, COALESCE(u.username, 'deleted user'),
			c.action_key, c.sub_action, c.surface,
			CASE
				WHEN c.issue_id IS NOT NULL THEN COALESCE(ip.key || '-' || i.issue_number || ' — ' || i.title, 'Issue #' || c.issue_id)
				WHEN c.project_id IS NOT NULL THEN COALESCE(pr.key || ' — ' || pr.name, 'Project #' || c.project_id)
				WHEN c.customer_id IS NOT NULL THEN COALESCE(cu.name, 'Customer #' || c.customer_id)
				WHEN c.cooperation_id IS NOT NULL THEN COALESCE(cp.key || ' cooperation', 'Cooperation #' || c.cooperation_id)
				ELSE ''
			END AS subject_label,
			c.model, COALESCE(c.profile_id, ''), COALESCE(c.effort, ''),
			COALESCE(c.prompt_preset_ref, ''), COALESCE(c.context_pack, ''),
			c.prompt_tokens, c.completion_tokens, c.total_tokens,
			c.cost_micro_usd, c.outcome, COALESCE(c.error_class, ''), c.latency_ms, c.request_id
		FROM ai_calls c
		LEFT JOIN users u ON u.id = c.user_id
		LEFT JOIN issues i ON i.id = c.issue_id
		LEFT JOIN projects ip ON ip.id = i.project_id
		LEFT JOIN projects pr ON pr.id = c.project_id
		LEFT JOIN customers cu ON cu.id = c.customer_id
		LEFT JOIN project_cooperation pc ON pc.id = c.cooperation_id
		LEFT JOIN projects cp ON cp.id = pc.project_id
	` + where + `
		ORDER BY c.created_at DESC, c.id DESC
	`
	// #nosec G701 -- query is assembled from fixed SQL fragments; user values are placeholders.
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		log.Printf("ai_calls: export: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ai-calls.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"time", "user", "action", "sub_action", "surface", "subject",
		"model", "profile_id", "effort", "prompt_preset_ref", "context_pack",
		"prompt_tokens", "completion_tokens", "total_tokens",
		"cost_usd", "outcome", "error_class", "latency_ms", "request_id",
	})
	for rows.Next() {
		var createdAt, username, actionKey, subAction, surface, subject, model, profileID, effort, promptPresetRef, contextPack, outcome, errorClass, requestID string
		var promptTokens, completionTokens, totalTokens int
		var costMicroUSD, latencyMs int64
		if err := rows.Scan(
			&createdAt, &username,
			&actionKey, &subAction, &surface,
			&subject,
			&model, &profileID, &effort, &promptPresetRef, &contextPack,
			&promptTokens, &completionTokens, &totalTokens,
			&costMicroUSD, &outcome, &errorClass, &latencyMs, &requestID,
		); err != nil {
			log.Printf("ai_calls: export scan: %v", err)
			continue
		}
		_ = cw.Write([]string{
			createdAt, username, actionKey, subAction, surface, subject,
			model, profileID, effort, promptPresetRef, contextPack,
			strconv.Itoa(promptTokens), strconv.Itoa(completionTokens), strconv.Itoa(totalTokens),
			fmt.Sprintf("%.4f", float64(costMicroUSD)/1_000_000.0), outcome, errorClass, strconv.FormatInt(latencyMs, 10), requestID,
		})
		cw.Flush()
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}
	cw.Flush()
}

func AIListIssueCalls(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	f, err := parseAICallFilters(r, nil)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.IssueID = &id
	if f.Limit == 0 {
		f.Limit = 100
	}
	rows, err := listAICalls(f)
	if err != nil {
		log.Printf("ai_calls: issue list: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, rows)
}

func AIListIssueActivity(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	filters := parseIssueAIActivityFilters(r)
	rowsOut, lastWeekCount, err := listIssueAIActivity(id, filters)
	if err != nil {
		log.Printf("ai_activity: list issue: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := issueAIActivityResponse{Rows: rowsOut, Count: len(rowsOut), LastWeekCount: lastWeekCount}
	jsonOK(w, resp)
}

func parseIssueAIActivityFilters(r *http.Request) issueAIActivityFilters {
	q := r.URL.Query()
	return issueAIActivityFilters{
		Kind:      strings.TrimSpace(q.Get("kind")),
		ActionKey: strings.TrimSpace(q.Get("action_key")),
		Provider:  strings.TrimSpace(q.Get("provider")),
		ProfileID: strings.TrimSpace(q.Get("profile_id")),
		Agent:     strings.TrimSpace(q.Get("agent")),
		Status:    strings.TrimSpace(q.Get("status")),
	}
}

func listIssueAIActivity(issueID int64, filters issueAIActivityFilters) ([]issueAIActivityRow, int, error) {
	rowsOut := make([]issueAIActivityRow, 0, 16)
	rows, err := db.DB.Query(`
		SELECT
			m.id,
			m.request_id,
			COALESCE(c.action_key, m.mutation_type),
			COALESCE(c.sub_action, ''),
			COALESCE(c.surface, ''),
			m.user_id,
			COALESCE(u.username, 'deleted user'),
			CASE
				WHEN m.undone_at IS NOT NULL THEN 'undone'
				ELSE COALESCE(c.outcome, 'ok')
			END,
			COALESCE(c.latency_ms, 0),
			COALESCE(c.provider, ''),
			COALESCE(c.model, ''),
			COALESCE(c.profile_id, ''),
			COALESCE(c.effort, ''),
			COALESCE(c.prompt_preset_ref, ''),
			COALESCE(c.context_pack, ''),
			COALESCE(c.prompt_tokens, 0),
			COALESCE(c.completion_tokens, 0),
			COALESCE(c.cost_micro_usd, 0),
			m.on_user_stack,
			m.created_at
		FROM mutation_log m
		LEFT JOIN ai_calls c ON c.request_id = m.request_id
		LEFT JOIN users u ON u.id = m.user_id
		WHERE m.subject_type IN ('issue', 'issue_relation')
		  AND m.subject_id = ?
		  AND m.mutation_type LIKE 'ai.%'
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 50
	`, issueID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var row issueAIActivityRow
		var onUserStack int
		if err := rows.Scan(
			&row.LogID, &row.RequestID, &row.ActionKey, &row.SubAction, &row.Surface,
			&row.UserID, &row.UserName,
			&row.Outcome, &row.LatencyMs, &row.ProviderID, &row.Model, &row.ProfileID, &row.Effort, &row.PromptPresetRef,
			&row.ContextPack, &row.PromptTokens,
			&row.CompletionTokens, &row.CostMicroUSD, &onUserStack, &row.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		row.Kind = "ai_action"
		row.Status = row.Outcome
		row.ProviderLabel = row.ProviderID
		row.OnUserStack = onUserStack == 1
		if issueAIActivityRowMatches(row, filters) {
			rowsOut = append(rowsOut, row)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	runRows, err := listIssueAIActivityRuns(issueID, filters)
	if err != nil {
		return nil, 0, err
	}
	rowsOut = append(rowsOut, runRows...)
	sort.SliceStable(rowsOut, func(i, j int) bool {
		if rowsOut[i].CreatedAt == rowsOut[j].CreatedAt {
			return rowsOut[i].LogID > rowsOut[j].LogID
		}
		return rowsOut[i].CreatedAt > rowsOut[j].CreatedAt
	})
	if len(rowsOut) > 50 {
		rowsOut = rowsOut[:50]
	}
	var actionLastWeek int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*)
		FROM mutation_log
		WHERE subject_type IN ('issue', 'issue_relation')
		  AND subject_id = ?
		  AND mutation_type LIKE 'ai.%'
		  AND created_at >= datetime('now', '-7 days')
	`, issueID).Scan(&actionLastWeek); err != nil {
		return nil, 0, err
	}
	var runLastWeek int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*)
		  FROM agent_runs
		 WHERE issue_id = ?
		   AND created_at >= datetime('now', '-7 days')
	`, issueID).Scan(&runLastWeek); err != nil {
		return nil, 0, err
	}
	return rowsOut, actionLastWeek + runLastWeek, nil
}

func listIssueAIActivityRuns(issueID int64, filters issueAIActivityFilters) ([]issueAIActivityRow, error) {
	rows, err := db.DB.Query(`
		SELECT
			ar.id,
			ar.requested_by,
			COALESCE(u.username, 'deleted user'),
			ar.action_key,
			ar.provider_kind,
			ar.provider_id,
			ar.provider_label,
			ar.model,
			ar.run_mode,
			ar.profile_id,
			ar.effort,
			ar.prompt_preset_ref,
			ar.context_pack,
			ar.prompt_tokens,
			ar.completion_tokens,
			ar.status,
			ar.version,
			COALESCE(ar.tests_summary, ''),
			ar.deploy_target,
			ar.error,
			ar.agent_name,
			ar.device_id,
			ar.created_at,
			COALESCE(ar.finished_at, ''),
			ar.source_draft_run_id,
			ar.followup_run_id
		FROM agent_runs ar
		LEFT JOIN users u ON u.id = ar.requested_by
		WHERE ar.issue_id = ?
		ORDER BY ar.created_at DESC, ar.id DESC
		LIMIT 100
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []issueAIActivityRow{}
	for rows.Next() {
		var row issueAIActivityRow
		var runID int64
		var sourceDraftRunID, followupRunID sql.NullInt64
		if err := rows.Scan(
			&runID, &row.UserID, &row.UserName, &row.ActionKey, &row.ProviderKind,
			&row.ProviderID, &row.ProviderLabel, &row.Model, &row.RunMode,
			&row.ProfileID, &row.Effort, &row.PromptPresetRef, &row.ContextPack,
			&row.PromptTokens, &row.CompletionTokens, &row.Status, &row.Version,
			&row.TestsSummary, &row.DeployTarget, &row.Error, &row.AgentName,
			&row.DeviceID, &row.CreatedAt, &row.FinishedAt, &sourceDraftRunID, &followupRunID,
		); err != nil {
			return nil, err
		}
		row.Kind = "agent_run"
		row.RunID = &runID
		row.RequestID = "run:" + strconv.FormatInt(runID, 10)
		row.Surface = "issue"
		row.Outcome = row.Status
		if sourceDraftRunID.Valid {
			row.SourceDraftRunID = &sourceDraftRunID.Int64
		}
		if followupRunID.Valid {
			row.FollowupRunID = &followupRunID.Int64
		}
		if issueAIActivityRowMatches(row, filters) {
			out = append(out, row)
		}
	}
	return out, rows.Err()
}

func issueAIActivityRowMatches(row issueAIActivityRow, filters issueAIActivityFilters) bool {
	if filters.Kind != "" && filters.Kind != row.Kind {
		return false
	}
	if filters.ActionKey != "" && filters.ActionKey != row.ActionKey {
		return false
	}
	if filters.Provider != "" {
		provider := strings.ToLower(filters.Provider)
		if !strings.Contains(strings.ToLower(row.ProviderID), provider) &&
			!strings.Contains(strings.ToLower(row.ProviderLabel), provider) &&
			!strings.Contains(strings.ToLower(row.ProviderKind), provider) {
			return false
		}
	}
	if filters.ProfileID != "" && filters.ProfileID != row.ProfileID {
		return false
	}
	if filters.Agent != "" && !strings.Contains(strings.ToLower(row.AgentName), strings.ToLower(filters.Agent)) {
		return false
	}
	if filters.Status != "" {
		status := row.Status
		if status == "" {
			status = row.Outcome
		}
		if filters.Status != status {
			return false
		}
	}
	return true
}
