// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-163. POST /api/ai/action — unified dispatcher for the
// multi-action AI dropdown (PAI-162).
//
// The single-purpose `/api/ai/optimize` (PAI-146) ports into this
// dispatcher in PAI-164 as the `optimize` action, but until that
// migrates we keep both endpoints live so the UI can demo the menu
// shell with the existing optimize flow as the default item.
//
// Design notes
// ------------
//   - Action handlers are registered at init() time into a package
//     map. New action = one new file with a registerAction() call.
//   - Each handler receives a fully populated `aiActionContext` —
//     the dispatcher does auth, settings, cap-check, provider
//     resolution, body decode, project-context load, and audit.
//     Handlers focus on one thing: build a prompt, call the
//     provider, shape the response.
//   - Audit shape (PAI-153 invariant):
//
//       audit: ai_action action=<key> sub_action=<sub>?
//              user_id=N field=X issue_id=Y model="..."
//              outcome=O latency_ms=N prompt_tokens=N
//              completion_tokens=N
//
//     Body content NEVER logged.

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

// actionRequest is the wire body. `Params` is action-specific JSON
// the handler decodes into its own struct — keeps the dispatcher
// generic without losing type safety inside each handler.
type actionRequest struct {
	Action    string          `json:"action"`
	SubAction string          `json:"sub_action,omitempty"`
	Field     string          `json:"field,omitempty"`
	IssueID   int64           `json:"issue_id,omitempty"`
	Text      string          `json:"text,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// actionResponse is what the dispatcher returns — body shape varies
// per action (optimize returns {optimized}, suggest returns
// {suggestions}, etc.) so we serialise it as raw JSON. Common
// metadata stays on the envelope so the frontend can render it
// uniformly.
type actionResponse struct {
	Action           string          `json:"action"`
	SubAction        string          `json:"sub_action,omitempty"`
	Body             json.RawMessage `json:"body"`
	Model            string          `json:"model,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	FinishReason     string          `json:"finish_reason,omitempty"`
}

// aiActionContext is what every action handler receives. The
// dispatcher populates it before calling the handler so each
// handler can stay focused on its own prompt + response shape.
type aiActionContext struct {
	Ctx       context.Context
	Request   *http.Request
	UserID    int64
	IsAdmin   bool
	Provider  ai.Provider
	Settings  AISettings
	IssueData ai.Context // pre-loaded issue context for prompt assembly
	IssueID   int64
	Field     string
	Text      string
	SubAction string
	Params    json.RawMessage
	DB        *sql.DB
}

// actionHandler is the contract every action implements. Returns the
// shaped JSON body, plus token counts for the meter and the model
// the provider actually served.
type actionHandler func(ax *aiActionContext) (body any, model string, promptTokens, completionTokens int, finishReason string, err error)

// actionDescriptor is what registerAction() puts into the registry.
// `Surface` is informational — the dispatcher doesn't switch on it,
// but the prompt-CRUD admin UI (PAI-176) reads it to group actions.
//
// PAI-179: `Placement` distinguishes text-level actions ("rewrite
// this paragraph") from issue-level actions ("operate on the whole
// record"). The default placement for each built-in action is set
// in ai_action_a_registry.go and overridable per-row by admins via
// the prompt-CRUD UI. The frontend AiActionMenu filters on this so
// text fields don't surface "Generate sub-tasks" and the issue
// header doesn't surface "Translate this textarea".
type actionDescriptor struct {
	Key         string
	Label       string
	Surface     string // "issue" | "customer"
	Placement   string // "text" | "issue" | "both" — PAI-179
	Handler     actionHandler
	SubKeys     []string // sub-action whitelist; empty means none required
	Implemented bool     // false → stub registered for menu shell only
}

// actionRegistry is the package-level map. Each action's source file
// calls registerAction() inside init() so the dispatcher can resolve
// keys without import-time wiring.
var actionRegistry = map[string]actionDescriptor{}

// registerAction is called by every action's init(). Duplicate keys
// panic at boot — same shape as the prompt-placeholder check in
// ai/prompt.go (PAI-157).
func registerAction(d actionDescriptor) {
	if d.Key == "" {
		panic("ai_action: registerAction called with empty key")
	}
	if d.Handler == nil {
		panic("ai_action: registerAction called with nil handler for " + d.Key)
	}
	if _, dup := actionRegistry[d.Key]; dup {
		panic("ai_action: duplicate action key " + d.Key)
	}
	actionRegistry[d.Key] = d
}

// allowedActionFields restricts which field identifiers an action
// can target. Matches ai_optimize's allow-list — actions that need
// to operate on a field MUST use one of these keys, so the audit
// trail and prompt reminders stay consistent.
var allowedActionFields = map[string]bool{
	"description":             true,
	"acceptance_criteria":     true,
	"notes":                   true,
	"project_description":     true,
	"customer_notes":          true,
	"cooperation_sla_details": true,
	"cooperation_notes":       true,
	// Some actions don't have a single target field (e.g. find_parent,
	// detect_duplicates) — they pass field="" and handle context
	// themselves. The dispatcher treats empty field as "skip the
	// allow-list check".
}

// AIAction is POST /api/ai/action. Mounted in the auth+CSRF group
// in main.go (NOT admin-only — non-admins use AI buttons too).
func AIAction(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	var userID int64
	if user != nil {
		userID = user.ID
	}
	isAdmin := user != nil && user.Role == "admin"

	if user == nil {
		// Defensive only — middleware should have rejected.
		auditAction(0, "", "", "", 0, "", "unauth", 0, 0, 0)
		jsonError(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	// PAI-161 cap check, before any provider work.
	if ok, _, _, bypass := CheckUsageCap(userID, isAdmin); !ok {
		auditAction(userID, "", "", "", 0, "", "bad_request", 0, 0, 0)
		jsonError(w, "Daily AI limit reached. Ask an admin to raise the cap.", http.StatusTooManyRequests)
		return
	} else if bypass {
		w.Header().Set("X-AI-Over-Cap", "true")
	}

	settings, err := LoadAISettings()
	if err != nil {
		log.Printf("ai_action: load settings: %v", err)
		auditAction(userID, "", "", "", 0, "", "cfg_load_fail", 0, 0, 0)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !settings.AvailableForOptimize() {
		auditAction(userID, "", "", "", 0, settings.Model, "unconfigured", 0, 0, 0)
		jsonError(w, "AI is not configured", http.StatusServiceUnavailable)
		return
	}

	var body actionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		auditAction(userID, "", "", "", 0, settings.Model, "bad_request", 0, 0, 0)
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	body.Action = strings.TrimSpace(body.Action)
	body.SubAction = strings.TrimSpace(body.SubAction)
	body.Field = strings.TrimSpace(body.Field)

	desc, ok := actionRegistry[body.Action]
	if !ok {
		auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, settings.Model, "bad_request", 0, 0, 0)
		jsonError(w, "unknown action: "+body.Action, http.StatusBadRequest)
		return
	}

	if body.Field != "" && !allowedActionFields[body.Field] {
		auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, settings.Model, "bad_request", 0, 0, 0)
		jsonError(w, "field is not enabled for AI actions", http.StatusBadRequest)
		return
	}

	// Sub-action whitelist — when the action declares them, the
	// caller MUST pick one of the listed keys. Lets handlers stay
	// thin and rely on the dispatcher to reject typos.
	if len(desc.SubKeys) > 0 {
		matched := false
		for _, k := range desc.SubKeys {
			if k == body.SubAction {
				matched = true
				break
			}
		}
		if !matched {
			auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, settings.Model, "bad_request", 0, 0, 0)
			jsonError(w, "missing or invalid sub_action for "+body.Action, http.StatusBadRequest)
			return
		}
	}

	provider, err := ai.Get(settings.Provider)
	if err != nil {
		log.Printf("ai_action: provider %q not registered: %v", settings.Provider, err)
		auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, settings.Model, "provider_missing", 0, 0, 0)
		jsonError(w, "AI provider unavailable", http.StatusServiceUnavailable)
		return
	}

	issueData, ctxErr := loadOptimizeContext(r, body.IssueID, body.Field)
	if ctxErr != nil {
		var ue *userError
		if errors.As(ctxErr, &ue) {
			auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, settings.Model, "denied", 0, 0, 0)
			jsonError(w, ue.msg, ue.status)
			return
		}
		log.Printf("ai_action: context: %v", ctxErr)
		auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, settings.Model, "ctx_fail", 0, 0, 0)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	ax := &aiActionContext{
		Ctx:       r.Context(),
		Request:   r,
		UserID:    userID,
		IsAdmin:   isAdmin,
		Provider:  provider,
		Settings:  settings,
		IssueData: issueData,
		IssueID:   body.IssueID,
		Field:     body.Field,
		Text:      body.Text,
		SubAction: body.SubAction,
		Params:    body.Params,
		DB:        db.DB,
	}

	t0 := time.Now()
	respBody, model, ptok, ctok, finish, err := desc.Handler(ax)
	latency := time.Since(t0)

	if err != nil {
		outcome := "fail_upstream"
		if errors.Is(ax.Ctx.Err(), context.DeadlineExceeded) {
			outcome = "fail_timeout"
		}
		auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, model, outcome, latency, ptok, ctok)
		switch {
		case errors.Is(err, ai.ErrProviderUnconfigured):
			jsonError(w, "AI provider rejected the request — check API key and model", http.StatusServiceUnavailable)
		case errors.Is(err, ai.ErrProviderUnavailable):
			jsonError(w, "AI provider temporarily unavailable", http.StatusServiceUnavailable)
		case errors.Is(err, errActionNotImplemented):
			jsonError(w, fmt.Sprintf("Action %q is not implemented yet — see PAI-162.", body.Action), http.StatusNotImplemented)
		default:
			jsonError(w, err.Error(), http.StatusBadGateway)
		}
		log.Printf("ai_action: handler error (%s): %v", body.Action, err)
		return
	}

	auditAction(userID, body.Action, body.SubAction, body.Field, body.IssueID, model, "ok", latency, ptok, ctok)
	// PAI-161 meter increments AFTER audit so log + DB row agree.
	RecordUsage(userID, ptok, ctok)

	// Encode the action-specific body. We use json.RawMessage to keep
	// the dispatcher generic; a handler that returns nil body gets a
	// `null` field in the payload, which is fine for actions like
	// estimate_effort that signal completion via the meta fields.
	var rawBody json.RawMessage
	if respBody != nil {
		raw, err := json.Marshal(respBody)
		if err != nil {
			log.Printf("ai_action: marshal body: %v", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		rawBody = raw
	} else {
		rawBody = json.RawMessage("null")
	}

	jsonOK(w, actionResponse{
		Action:           body.Action,
		SubAction:        body.SubAction,
		Body:             rawBody,
		Model:            model,
		PromptTokens:     ptok,
		CompletionTokens: ctok,
		FinishReason:     finish,
	})
}

// auditAction writes the structured stdout audit line for one
// action call. Same shape as ai_optimize plus the action / sub_action
// dimensions so dashboards can group by action.
func auditAction(userID int64, action, subAction, field string, issueID int64, model, outcome string, latency time.Duration, promptTokens, completionTokens int) {
	log.Printf("audit: ai_action action=%s sub_action=%s user_id=%d field=%s issue_id=%d model=%q outcome=%s latency_ms=%d prompt_tokens=%d completion_tokens=%d",
		action, subAction, userID, field, issueID, model, outcome, latency.Milliseconds(), promptTokens, completionTokens)
}

// errActionNotImplemented is returned by stub handlers so the
// dispatcher can surface 501 instead of a vague upstream error.
// Real handlers (PAI-164–172) replace the stubs.
var errActionNotImplemented = errors.New("ai_action: not implemented")

// stubHandler is registered for every action key the dispatcher
// knows about but doesn't yet implement. The frontend menu still
// renders the item; clicking it surfaces a "Coming soon" banner
// (or a 501 toast) instead of a confusing blank modal.
func stubHandler(ax *aiActionContext) (any, string, int, int, string, error) {
	return nil, "", 0, 0, "", errActionNotImplemented
}

// AIListActions handles GET /api/ai/actions — surface the action
// catalog so the frontend menu can render itself without a giant
// hard-coded list. Includes per-action availability flags so the
// menu can render unimplemented items disabled / "Coming soon".
//
// PAI-179: placement is read from ai_prompts (admin-overridable)
// per row, falling back to the registry default. We do this in
// one query rather than N+1 lookups because the catalogue is
// fetched on every page that mounts an AI menu — keeping it
// server-side cheap matters.
type aiActionListItem struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Surface     string   `json:"surface"`
	Placement   string   `json:"placement"`
	SubKeys     []string `json:"sub_keys,omitempty"`
	Implemented bool     `json:"implemented"`
}

func AIListActions(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"actions": listAIActionCatalog(db.DB)})
}
