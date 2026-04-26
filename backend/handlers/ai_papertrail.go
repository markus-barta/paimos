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
	"strconv"
	"strings"

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
	RequestID        string `json:"request_id"`
	ActionKey        string `json:"action_key"`
	SubAction        string `json:"sub_action"`
	Surface          string `json:"surface"`
	UserID           *int64 `json:"user_id"`
	UserName         string `json:"user_name"`
	Outcome          string `json:"outcome"`
	LatencyMs        int64  `json:"latency_ms"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CostMicroUSD     int64  `json:"cost_micro_usd"`
	OnUserStack      bool   `json:"on_user_stack"`
	CreatedAt        string `json:"created_at"`
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
		args.CostMicroUSD = lookupAICallCostMicroUSD(args.Model, args.PromptTokens, args.CompletionTokens)
	}
	totalTokens := args.PromptTokens + args.CompletionTokens
	_, err := db.DB.ExecContext(ctx, `
		INSERT INTO ai_calls(
			request_id, user_id, action_key, sub_action, surface,
			issue_id, project_id, customer_id, cooperation_id,
			provider, model, prompt_tokens, completion_tokens, total_tokens,
			cost_micro_usd, outcome, error_class, latency_ms
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
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
	}
}

func lookupAICallCostMicroUSD(model string, promptTokens, completionTokens int) int64 {
	model = strings.TrimSpace(model)
	if model == "" {
		return 0
	}
	modelsCache.mu.RLock()
	payload := modelsCache.payload
	modelsCache.mu.RUnlock()
	if payload == nil {
		return 0
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
			if m.ID != model {
				continue
			}
			cost := m.PricingPromptPerMtok*float64(promptTokens) + m.PricingCompletionPerMtok*float64(completionTokens)
			return int64(math.Round(cost))
		}
	}
	return 0
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
	if err := db.DB.QueryRow(countSQL, args...).Scan(&resp.TotalCount, &resp.TotalCostMicroUSD); err != nil {
		return resp, err
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
			c.provider, c.model, c.prompt_tokens, c.completion_tokens, c.total_tokens,
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
			&row.Provider, &row.Model, &row.PromptTokens, &row.CompletionTokens, &row.TotalTokens,
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
			c.provider, c.model, c.prompt_tokens, c.completion_tokens, c.total_tokens,
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
		&row.Provider, &row.Model, &row.PromptTokens, &row.CompletionTokens, &row.TotalTokens,
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
			c.model, c.prompt_tokens, c.completion_tokens, c.total_tokens,
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
		"model", "prompt_tokens", "completion_tokens", "total_tokens",
		"cost_usd", "outcome", "error_class", "latency_ms", "request_id",
	})
	for rows.Next() {
		var createdAt, username, actionKey, subAction, surface, subject, model, outcome, errorClass, requestID string
		var promptTokens, completionTokens, totalTokens int
		var costMicroUSD, latencyMs int64
		if err := rows.Scan(
			&createdAt, &username,
			&actionKey, &subAction, &surface,
			&subject,
			&model, &promptTokens, &completionTokens, &totalTokens,
			&costMicroUSD, &outcome, &errorClass, &latencyMs, &requestID,
		); err != nil {
			log.Printf("ai_calls: export scan: %v", err)
			continue
		}
		_ = cw.Write([]string{
			createdAt, username, actionKey, subAction, surface, subject,
			model, strconv.Itoa(promptTokens), strconv.Itoa(completionTokens), strconv.Itoa(totalTokens),
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
			COALESCE(c.model, ''),
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
	`, id)
	if err != nil {
		log.Printf("ai_activity: list issue: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	resp := issueAIActivityResponse{Rows: []issueAIActivityRow{}}
	for rows.Next() {
		var row issueAIActivityRow
		var onUserStack int
		if err := rows.Scan(
			&row.LogID, &row.RequestID, &row.ActionKey, &row.SubAction, &row.Surface,
			&row.UserID, &row.UserName,
			&row.Outcome, &row.LatencyMs, &row.Model, &row.PromptTokens,
			&row.CompletionTokens, &row.CostMicroUSD, &onUserStack, &row.CreatedAt,
		); err != nil {
			log.Printf("ai_activity: scan issue: %v", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		row.OnUserStack = onUserStack == 1
		resp.Rows = append(resp.Rows, row)
	}
	resp.Count = len(resp.Rows)
	if err := db.DB.QueryRow(`
		SELECT COUNT(*)
		FROM mutation_log
		WHERE subject_type IN ('issue', 'issue_relation')
		  AND subject_id = ?
		  AND mutation_type LIKE 'ai.%'
		  AND created_at >= datetime('now', '-7 days')
	`, id).Scan(&resp.LastWeekCount); err != nil {
		log.Printf("ai_activity: count issue: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, resp)
}
