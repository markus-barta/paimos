// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-159. POST /api/ai/test — admin-only, single-shot ping that
// verifies a (provider, model, api_key) triple actually works against
// the upstream LLM, before the admin commits the values via PUT
// /api/ai/settings.
//
// Why a fixed prompt with literal OK/FAIL marker
// ----------------------------------------------
// Free-form completions are hard to grade deterministically. Asking
// the model for a one-line funny answer that contains the literal
// token "OK" turns a soft "did it work?" into a hard string check:
// the response either contains the marker (and the call returned, so
// auth + routing + token quota all worked) or it doesn't. The
// presence of "FAIL" in the response flips ok=false — useful for
// chaos tests, and a good early-warning if a model is being
// pessimistic about its own output.
//
// The prompt is intentionally cheap (50-token completion budget) and
// playful — admins click this often during onboarding, and a dry
// "ping ok" feels worse than a one-liner about coffee.
//
// Audit shape
// -----------
// Same invariant as PAI-153 (no body content in audit lines):
//
//   audit: ai_test user_id=N model="..." outcome=test_ok|test_fail
//          latency_ms=N prompt_tokens=N completion_tokens=N

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/ai"
	"github.com/markus-barta/paimos/backend/auth"
)

// aiTestRequest is the form values the admin clicked Test Connection
// with. Critically, this is NOT loaded from ai_settings — the whole
// point is to test unsaved values.
type aiTestRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

// aiTestResponse is rendered next to the button. The funny line goes
// in response_text; the UI may truncate but should not transform it
// (the joke is part of the success signal).
type aiTestResponse struct {
	OK               bool   `json:"ok"`
	Message          string `json:"message"`           // human-readable banner
	ResponseText     string `json:"response_text"`     // model's funny line
	Model            string `json:"model"`             // upstream's reported model
	LatencyMs        int64  `json:"latency_ms"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Marker           string `json:"marker"`            // "OK" | "FAIL" | ""
}

const (
	// aiTestTimeout is intentionally tighter than the optimize timeout.
	// A test ping that takes 60s defeats the purpose ("did the model
	// work?") — anything beyond 15s and the UX is bad enough that we'd
	// rather declare failure and let the admin pick a faster model.
	aiTestTimeout = 15 * time.Second

	// aiTestMaxTokens is the completion budget. The fixed prompt asks
	// for one short line; 50 tokens is plenty (a haiku is ~17 tokens).
	aiTestMaxTokens = 50
)

// aiTestSystemPrompt is the system message. We do NOT use the
// optimize wrapper here — a test connection is not an editorial
// rewrite, and dragging the wrapper in would make the test prompt
// 800 tokens for no reason.
const aiTestSystemPrompt = `You are a smoke-test assistant for an issue-tracker called PAIMOS. You will receive one prompt and must reply with one short line. Stay under 100 characters.`

// aiTestUserPrompt is what makes the response gradeable. We require
// the literal token "OK" if everything is fine — and explicitly tell
// the model to use "FAIL" if it has refused / errored anything,
// so a model that decides to comment on its own status leaves a
// machine-readable trail instead of a soft "I cannot do that".
const aiTestUserPrompt = `Reply with exactly one short, witty, single-line sentence about coffee, sleep, or rubber ducks (your pick). The sentence MUST contain the literal uppercase token OK as a standalone whole word — not in parentheses, not lower-case, not as part of "okay" or "OKAY". Do not add a preamble or explanation. Examples of acceptable replies:
- The build is green and so is my mug, OK?
- Two espressos in, OK, now I can read the stack trace.
- The rubber duck nodded OK and went back to debugging.

If for any reason you must refuse, reply with one short sentence containing the literal uppercase token FAIL instead.`

// markerOK / markerFail must be matched as standalone whole words
// (surrounded by non-letter characters). A naive Contains check is
// wrong because "OK" is a substring of "OKAY" and "BLOCK" — both
// would falsely flip the success bit.
const (
	markerOK   = "OK"
	markerFAIL = "FAIL"
)

// AITestConnection is POST /api/ai/test. Admin-only, mounted in the
// admin auth group (CSRF covered).
func AITestConnection(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	var userID int64
	if user != nil {
		userID = user.ID
	}

	var req aiTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditTest(userID, "", "test_fail", 0, 0, 0)
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.APIKey = strings.TrimSpace(req.APIKey)

	if req.Provider == "" {
		req.Provider = "openrouter"
	}

	// PAI-178: fall back to the saved settings when the form leaves a
	// field blank. Admins typically don't re-type the API key (the
	// SPA never echoes it back) — making them paste it just to run a
	// smoke test was unnecessary friction. Same fallback for model
	// and provider so "Test connection" works on a settings page that
	// was opened, examined, and immediately tested.
	if req.Model == "" || req.APIKey == "" {
		if saved, lerr := LoadAISettings(); lerr == nil {
			if req.APIKey == "" {
				req.APIKey = saved.APIKey
			}
			if req.Model == "" {
				req.Model = saved.Model
			}
			if req.Provider == "" && saved.Provider != "" {
				req.Provider = saved.Provider
			}
		}
	}

	if req.Model == "" || req.APIKey == "" {
		auditTest(userID, req.Model, "test_fail", 0, 0, 0)
		jsonOK(w, aiTestResponse{
			OK:      false,
			Message: "No saved API key or model — paste one in the form (or save it first) and retry.",
		})
		return
	}

	provider, err := ai.Get(req.Provider)
	if err != nil {
		auditTest(userID, req.Model, "test_fail", 0, 0, 0)
		jsonOK(w, aiTestResponse{
			OK:      false,
			Message: "Unknown provider — pick one PAIMOS knows about.",
		})
		return
	}

	callCtx, cancel := context.WithTimeout(r.Context(), aiTestTimeout)
	defer cancel()
	t0 := time.Now()
	resp, err := provider.Optimize(callCtx, ai.OptimizeRequest{
		Model:           req.Model,
		APIKey:          req.APIKey,
		SystemPrompt:    aiTestSystemPrompt,
		UserPrompt:      aiTestUserPrompt,
		MaxOutputTokens: aiTestMaxTokens,
	})
	latency := time.Since(t0)

	if err != nil {
		auditTest(userID, req.Model, "test_fail", latency, 0, 0)
		// Return 200 with a structured failure body — the UI renders
		// this inline, not as an HTTP error toast. The admin needs
		// the upstream message to know what to fix (key vs model vs
		// rate limit).
		msg := "Connection test failed."
		switch {
		case errors.Is(err, ai.ErrProviderUnconfigured):
			msg = "Provider rejected the credentials. Check the API key."
		case errors.Is(err, ai.ErrProviderUnavailable):
			msg = "Provider unavailable right now. Try again or pick another model."
		case errors.Is(callCtx.Err(), context.DeadlineExceeded):
			msg = "Test call timed out (15s). The chosen model is too slow for a smoke test — pick a faster one and retry."
		default:
			// Surface upstream message verbatim for admins.
			msg = err.Error()
		}
		jsonOK(w, aiTestResponse{
			OK:        false,
			Message:   msg,
			LatencyMs: latency.Milliseconds(),
		})
		return
	}

	cleaned := ai.StripFenceEcho(resp.Text)
	hasOK := containsWholeWord(cleaned, markerOK)
	hasFAIL := containsWholeWord(cleaned, markerFAIL)

	// FAIL beats OK if both happen — defensive in case a model echoes
	// the example phrasing ("If you must refuse, reply FAIL...") into
	// its OK line. Better to surface a possible-fail than to claim
	// success on an ambiguous response.
	ok := hasOK && !hasFAIL

	marker := ""
	switch {
	case hasFAIL:
		marker = markerFAIL
	case hasOK:
		marker = markerOK
	}

	outcome := "test_fail"
	msg := "Test reply did not contain the expected OK marker. The connection works, but the model didn't follow instructions."
	if ok {
		outcome = "test_ok"
		msg = "Connection works."
	}
	if hasFAIL {
		msg = "Model returned a FAIL marker — connection works but the model declined the test prompt. Check rate limits or pick another model."
	}

	auditTest(userID, resp.Model, outcome, latency, resp.PromptTokens, resp.CompletionTokens)

	jsonOK(w, aiTestResponse{
		OK:               ok,
		Message:          msg,
		ResponseText:     cleaned,
		Model:            resp.Model,
		LatencyMs:        latency.Milliseconds(),
		PromptTokens:     resp.PromptTokens,
		CompletionTokens: resp.CompletionTokens,
		Marker:           marker,
	})
}

// containsWholeWord checks whether `marker` appears in `s` as a
// standalone token — i.e. surrounded by non-letter characters (or
// at the start/end of the string). This is what makes "OK" not
// match inside "OKAY" or "BLOCK".
//
// Lower-cased letters are also considered "letters" for boundary
// purposes so "ok" inside "ok," doesn't flip the marker.
func containsWholeWord(s, marker string) bool {
	if marker == "" {
		return false
	}
	idx := 0
	for {
		i := strings.Index(s[idx:], marker)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(marker)
		// Boundary at start: either at position 0, or previous rune
		// is not an ASCII letter.
		leftOK := start == 0 || !isLetter(s[start-1])
		rightOK := end == len(s) || !isLetter(s[end])
		if leftOK && rightOK {
			return true
		}
		idx = end
	}
}

func isLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// auditTest writes the structured stdout audit line for one test
// connection. Same shape as ai_optimize but a different verb so
// dashboards can separate user-driven optimize calls from admin
// smoke tests.
func auditTest(userID int64, model, outcome string, latency time.Duration, promptTokens, completionTokens int) {
	log.Printf("audit: ai_test user_id=%d model=%q outcome=%s latency_ms=%d prompt_tokens=%d completion_tokens=%d",
		userID, model, outcome, latency.Milliseconds(), promptTokens, completionTokens)
}
