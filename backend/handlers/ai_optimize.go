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

// PAI-150 + PAI-152 + PAI-153. POST /api/ai/optimize handler.
//
// Why this lives in handlers/ rather than ai/:
//
//   - This file pulls together the prompt wrapper (ai), the provider
//     registry (ai), the database (db), and the auth context (auth).
//     Putting it inside the ai package would force ai to import db,
//     which is the wrong direction — ai stays vendor-shaped, handlers
//     are the integration seam.
//   - Audit logging (PAI-153) sits naturally with the other handlers
//     so operators see "audit:" lines from one place.
//
// Boundary checks (in order):
//
//   1. Caller is authenticated. The route is mounted in the auth-required
//      group so this is structurally guaranteed; we still pull the user
//      record for audit attribution.
//   2. Feature is configured + enabled. Returns 503 with a stable error
//      shape the SPA can show as "AI optimization is not configured".
//   3. Field name is one of the supported ones. PAI-146 lists three for
//      v1; we don't trust the client's field string for anything except
//      the per-field reminder, but a deny-list keeps the audit trail
//      clean and prevents future field types from leaking through.
//   4. Source text is non-empty and within the size cap. The cap covers
//      both runaway tokens cost AND a basic abuse vector (200KB textarea
//      submitted through the optimize endpoint).
//   5. Optional issue_id, when present, must reference an issue the
//      caller can view. Missing issue_id is fine — the optimize action
//      should also work on draft text that isn't saved yet.

package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/ai"
	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// optimizeRequest is the body the SPA sends.
type optimizeRequest struct {
	// Field is the multiline-field identifier the AI button is bound
	// to. Used for per-field prompt reminders and the audit trail.
	Field string `json:"field"`

	// Text is the current content of the field. May be unsaved (the
	// SPA can call optimize before the user clicks Save).
	Text string `json:"text"`

	// IssueID is optional. When set, the handler loads the surrounding
	// context for the prompt. When zero, the optimize works on the
	// text alone — which is correct for "new issue" forms where there
	// isn't an issue row yet.
	IssueID int64 `json:"issue_id,omitempty"`
}

// optimizeResponse is the body returned to the SPA. The SPA renders
// "optimized" in the diff overlay; everything else is metadata used
// for the per-call success banner.
type optimizeResponse struct {
	Optimized        string `json:"optimized"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	FinishReason     string `json:"finish_reason"`
}

// allowedOptimizeFields is the v1 scope for PAI-146. Other multiline
// fields can be added one at a time as the rollout progresses; doing
// it as an explicit allow-list (rather than a reject-list) means a new
// field doesn't accidentally become AI-optimisable until it's been
// vetted.
var allowedOptimizeFields = map[string]bool{
	"description":         true,
	"acceptance_criteria": true,
	"notes":               true,
}

const (
	// optimizeMaxInputBytes caps a single optimize call. PAIMOS multiline
	// fields are typically <8 KiB; the cap is generous to allow long
	// epics with embedded code blocks. Anything larger is almost always
	// a bug or an attempt to abuse the operator's token budget.
	optimizeMaxInputBytes = 32 << 10 // 32 KiB

	// optimizeMaxOutputTokens is the soft cap requested from the
	// provider. ~3000 tokens at ~4 chars each is roughly 12 KiB, more
	// than enough for the rewrites we expect.
	optimizeMaxOutputTokens = 3000

	// optimizeRequestTimeout bounds the whole call. The router has its
	// own ~30s default but rewrite calls can take longer on bigger
	// models; 60s is the band most providers stay inside while still
	// catching obvious wedges.
	optimizeRequestTimeout = 60 * time.Second
)

// Audit outcome enum (PAI-153). Stable strings used by `audit:
// ai_optimize ... outcome=<value>` lines so operators / log analysis
// can group every attempt — successful, failed, denied, or rejected
// before the provider was called — into the same dashboard.
//
// New values must be added here, never inlined at the call site, so a
// grep for `outcome=` in this file lists the entire taxonomy.
const (
	// Provider returned a rewrite. Token counts populated.
	outcomeOK = "ok"
	// Our handler-imposed deadline (optimizeRequestTimeout) fired
	// before the provider produced a response. Distinct from
	// fail_upstream so dashboards can spot timeout patterns without
	// scraping latency_ms — a recurring fail_timeout on a specific
	// model usually means the operator should pin a faster model
	// or raise the cap.
	outcomeFailTimeout = "fail_timeout"
	// Provider was reached and replied with a non-success status
	// (4xx / 5xx) or a structurally invalid body. Token counts NOT
	// populated; latency_ms is the wall-clock attempt to the
	// upstream's response.
	outcomeFailUpstream = "fail_upstream"
	// Caller asked to optimize text on an issue they cannot view.
	// Mapped to HTTP 403 / 404 from loadOptimizeContext.
	outcomeDenied = "denied"
	// Defensive: middleware-bypass route would land here.
	outcomeUnauth = "unauth"
	// Settings row failed to load (DB error). Distinct from
	// `unconfigured` so operators can tell "DB sick" from "admin
	// hasn't enabled the feature".
	outcomeCfgLoadFail = "cfg_load_fail"
	// AvailableForOptimize returned false: feature flag off, missing
	// key, or missing model.
	outcomeUnconfigured = "unconfigured"
	// Body decode failed, field not in allow-list, text empty, or
	// text exceeds optimizeMaxInputBytes. The HTTP status varies
	// (400 vs 413) but the audit bucket is the same — these are all
	// "client sent input we won't process".
	outcomeBadRequest = "bad_request"
	// The configured provider name has no entry in the registry.
	// Distinguishes a PAIMOS-side bug ("provider was removed") from
	// a transient provider error.
	outcomeProviderMissing = "provider_missing"
	// loadOptimizeContext returned a non-userError (DB/SQL failure).
	// Distinct from `denied` so the access-control vs. infra signal
	// stays clean.
	outcomeCtxFail = "ctx_fail"
)

// AIOptimize is POST /api/ai/optimize. Mounted in the authenticated
// group; CSRF middleware (PAI-113) covers the cookie-auth path.
//
// PAI-153: every exit path emits exactly one audit line with a stable
// `outcome` string so the operator can grep stdout to see every
// attempt — successes, failures, denials, and validation rejections.
// Outcomes are an enum maintained at the top of this file.
func AIOptimize(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	// userID is 0 for the defensive-unauthenticated branch; downstream
	// audit lines treat user_id=0 as "no actor recorded" rather than
	// silently dropping the line.
	var userID int64
	if user != nil {
		userID = user.ID
	}

	if user == nil {
		// Defensive only — the route is mounted in the auth group so
		// this should be unreachable in practice. Keep the branch so
		// any future routing reshuffle fails loud.
		auditOptimize(0, "", 0, "", outcomeUnauth, 0, 0, 0)
		jsonError(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	settings, err := LoadAISettings()
	if err != nil {
		log.Printf("ai_optimize: load settings: %v", err)
		auditOptimize(userID, "", 0, "", outcomeCfgLoadFail, 0, 0, 0)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !settings.AvailableForOptimize() {
		// 503 (rather than 400) so the SPA can distinguish "you sent
		// bad input" from "the operator hasn't configured this yet"
		// and show the right banner.
		auditOptimize(userID, "", 0, settings.Model, outcomeUnconfigured, 0, 0, 0)
		jsonError(w, "AI optimization is not configured", http.StatusServiceUnavailable)
		return
	}

	var body optimizeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		auditOptimize(userID, "", 0, settings.Model, outcomeBadRequest, 0, 0, 0)
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	body.Field = strings.TrimSpace(body.Field)
	body.Text = strings.TrimRight(body.Text, " \t")
	if body.Field == "" || !allowedOptimizeFields[body.Field] {
		auditOptimize(userID, body.Field, body.IssueID, settings.Model, outcomeBadRequest, 0, 0, 0)
		jsonError(w, "field must be one of: description, acceptance_criteria, notes", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		auditOptimize(userID, body.Field, body.IssueID, settings.Model, outcomeBadRequest, 0, 0, 0)
		jsonError(w, "text must not be empty", http.StatusBadRequest)
		return
	}
	if len(body.Text) > optimizeMaxInputBytes {
		auditOptimize(userID, body.Field, body.IssueID, settings.Model, outcomeBadRequest, 0, 0, 0)
		jsonError(w, fmt.Sprintf("text exceeds %d byte cap", optimizeMaxInputBytes), http.StatusRequestEntityTooLarge)
		return
	}

	// Resolve provider before context lookup so we fail fast on a
	// misconfigured provider name. providerName comes from settings,
	// which the admin curated, so an unknown provider here means a
	// PAIMOS upgrade dropped the named provider — surface as 503.
	provider, err := ai.Get(settings.Provider)
	if err != nil {
		log.Printf("ai_optimize: provider %q not registered: %v", settings.Provider, err)
		auditOptimize(userID, body.Field, body.IssueID, settings.Model, outcomeProviderMissing, 0, 0, 0)
		jsonError(w, "AI provider unavailable", http.StatusServiceUnavailable)
		return
	}

	ctxData, ctxErr := loadOptimizeContext(r, body.IssueID, body.Field)
	if ctxErr != nil {
		// loadOptimizeContext returns a *userError when the caller
		// can't see the issue; bubble that up as 403/404 so the audit
		// trail records the attempt clearly. Other errors are
		// internal and we log + 500.
		var ue *userError
		if errors.As(ctxErr, &ue) {
			auditOptimize(userID, body.Field, body.IssueID, settings.Model, outcomeDenied, 0, 0, 0)
			jsonError(w, ue.msg, ue.status)
			return
		}
		log.Printf("ai_optimize: context: %v", ctxErr)
		auditOptimize(userID, body.Field, body.IssueID, settings.Model, outcomeCtxFail, 0, 0, 0)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	systemPrompt := ai.BuildSystemPrompt(settings.OptimizeInstruction)
	userPrompt := ai.BuildUserPrompt(body.Text, ctxData)

	callCtx, cancel := context.WithTimeout(r.Context(), optimizeRequestTimeout)
	defer cancel()
	t0 := time.Now()
	resp, err := provider.Optimize(callCtx, ai.OptimizeRequest{
		Model:           settings.Model,
		APIKey:          settings.APIKey,
		SystemPrompt:    systemPrompt,
		UserPrompt:      userPrompt,
		MaxOutputTokens: optimizeMaxOutputTokens,
	})
	latency := time.Since(t0)

	if err != nil {
		// Distinguish our own timeout from an upstream-driven failure.
		// callCtx.Err() == DeadlineExceeded means our optimizeRequestTimeout
		// fired before the provider could respond — distinct signal from
		// "provider replied with 5xx" because the remediation is different
		// (raise the cap / pick a faster model vs. retry / wait).
		outcome := outcomeFailUpstream
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			outcome = outcomeFailTimeout
		}
		auditOptimize(userID, body.Field, body.IssueID, settings.Model, outcome, latency, 0, 0)
		// Map sentinel errors to stable HTTP statuses so the SPA can
		// branch on the response. The error message stays generic for
		// non-admins; admins can find the upstream message in stdout.
		switch {
		case errors.Is(err, ai.ErrProviderUnconfigured):
			jsonError(w, "AI provider rejected the request — check API key and model", http.StatusServiceUnavailable)
		case errors.Is(err, ai.ErrProviderUnavailable):
			jsonError(w, "AI provider temporarily unavailable", http.StatusServiceUnavailable)
		default:
			// Surface the upstream message verbatim. It's safe — the
			// caller is an authenticated user, and OpenRouter messages
			// like "model not found: foo/bar" are exactly what an
			// admin needs to debug a misconfigured model slug.
			jsonError(w, err.Error(), http.StatusBadGateway)
		}
		log.Printf("ai_optimize: provider error: %v", err)
		return
	}

	// Strip a wrap-everything fence echo if the model couldn't resist.
	cleaned := ai.StripFenceEcho(resp.Text)

	auditOptimize(userID, body.Field, body.IssueID, resp.Model, outcomeOK, latency, resp.PromptTokens, resp.CompletionTokens)

	jsonOK(w, optimizeResponse{
		Optimized:        cleaned,
		Model:            resp.Model,
		PromptTokens:     resp.PromptTokens,
		CompletionTokens: resp.CompletionTokens,
		FinishReason:     resp.FinishReason,
	})
}

// userError lets loadOptimizeContext signal "client problem, not server
// bug" without wrapping http.Error patterns into the helper. Caller
// inspects via errors.As.
type userError struct {
	status int
	msg    string
}

func (e *userError) Error() string { return e.msg }

// loadOptimizeContext fetches the issue / project / parent epic for the
// prompt context block. Authorization re-uses auth.CanViewProject so
// the optimize endpoint enforces the same project-visibility rule the
// rest of the SPA does.
//
// Missing issueID returns an empty Context with the FieldName filled
// in — the prompt still works, just without the surrounding metadata.
func loadOptimizeContext(r *http.Request, issueID int64, field string) (ai.Context, error) {
	c := ai.Context{FieldName: field}
	if issueID == 0 {
		return c, nil
	}

	// We pull issue + project key/name + parent (key + title) in one
	// query. SQLite handles the LEFT JOIN to `parent` cheaply because
	// id is the rowid in our schema.
	const q = `
SELECT
  i.project_id,
  COALESCE(p.key,  '')                               AS project_key,
  COALESCE(p.name, '')                               AS project_name,
  i.issue_number,
  i.type,
  i.title,
  i.parent_id,
  COALESCE(pp.key, '') || CASE WHEN pp.id IS NULL THEN '' ELSE '-' END
    || COALESCE(parent.issue_number, '')             AS parent_key,
  COALESCE(parent.title, '')                         AS parent_title
FROM issues i
LEFT JOIN projects p      ON p.id = i.project_id
LEFT JOIN issues   parent ON parent.id = i.parent_id
LEFT JOIN projects pp     ON pp.id = parent.project_id
WHERE i.id = ? AND i.deleted_at IS NULL
`
	var (
		projectID                 sql.NullInt64
		projectKey, projectName   string
		issueNum                  int
		issueType, issueTitle     string
		parentID                  sql.NullInt64
		parentKey, parentTitle    string
	)
	err := db.DB.QueryRowContext(r.Context(), q, issueID).Scan(
		&projectID, &projectKey, &projectName,
		&issueNum, &issueType, &issueTitle,
		&parentID, &parentKey, &parentTitle,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ai.Context{}, &userError{status: http.StatusNotFound, msg: "issue not found"}
	}
	if err != nil {
		return ai.Context{}, fmt.Errorf("scan issue: %w", err)
	}

	// Project-access check. Admins bypass via auth.CanViewProject's
	// admin short-circuit; non-admins must have at least viewer rights
	// on the issue's project. We do this here (not via
	// auth.RequireIssueAccess middleware) because the optimize route
	// is not /issues/{id}/* — the issue ID rides in the JSON body, so
	// the chi-route-param middleware doesn't fit.
	if projectID.Valid {
		if !auth.CanViewProject(r, projectID.Int64) {
			return ai.Context{}, &userError{status: http.StatusForbidden, msg: "issue not accessible"}
		}
	}

	if projectKey != "" {
		c.IssueKey = fmt.Sprintf("%s-%d", projectKey, issueNum)
	}
	c.IssueType = issueType
	c.IssueTitle = issueTitle
	c.ProjectName = projectName
	if parentID.Valid {
		switch {
		case parentKey != "" && parentTitle != "":
			c.ParentEpic = fmt.Sprintf("%s — %s", parentKey, parentTitle)
		case parentTitle != "":
			c.ParentEpic = parentTitle
		}
	}
	return c, nil
}

// auditOptimize writes the structured stdout audit line for one
// optimize attempt. PAI-153 mandates audit-without-bodies: this line
// captures actor, target, model, outcome, and usage metadata. It MUST
// NOT contain prompt or response text.
func auditOptimize(userID int64, field string, issueID int64, model, outcome string, latency time.Duration, promptTokens, completionTokens int) {
	log.Printf("audit: ai_optimize user_id=%d field=%s issue_id=%d model=%q outcome=%s latency_ms=%d prompt_tokens=%d completion_tokens=%d",
		userID, field, issueID, model, outcome, latency.Milliseconds(), promptTokens, completionTokens)
}
