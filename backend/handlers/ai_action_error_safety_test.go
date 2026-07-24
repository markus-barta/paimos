// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/inspr-at/paimos/backend/ai"
	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
	"github.com/inspr-at/paimos/backend/models"
)

type invalidJSONActionProvider struct {
	text string
}

func (p invalidJSONActionProvider) Name() string { return "invalid-json-action-test" }

func (p invalidJSONActionProvider) Optimize(context.Context, ai.OptimizeRequest) (ai.OptimizeResponse, error) {
	return ai.OptimizeResponse{
		Text:             p.text,
		Model:            "test/model",
		PromptTokens:     11,
		CompletionTokens: 7,
		FinishReason:     "stop",
	}, nil
}

func TestCallJSONActionParseErrorDoesNotExposeModelOutput(t *testing.T) {
	const rawSentinel = "RAW_MODEL_OUTPUT_SENTINEL_DO_NOT_EXPOSE"

	var out struct {
		OK bool `json:"ok"`
	}
	model, ptok, ctok, finish, err := callJSONAction(
		context.Background(),
		&aiActionContext{
			Provider: invalidJSONActionProvider{text: "not json " + rawSentinel},
			Settings: AISettings{Model: "test/model"},
		},
		"system",
		"user",
		100,
		&out,
	)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !errors.Is(err, errAIActionJSONParse) {
		t.Fatalf("errors.Is(err, errAIActionJSONParse)=false; err=%v", err)
	}
	if strings.Contains(err.Error(), rawSentinel) {
		t.Fatalf("parse error leaked raw model output: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to parse model JSON") {
		t.Fatalf("parse error lost safe class: %v", err)
	}
	if model != "test/model" || ptok != 11 || ctok != 7 || finish != "stop" {
		t.Fatalf("metadata = (%q,%d,%d,%q), want provider metadata", model, ptok, ctok, finish)
	}
}

func TestAIActionParseErrorReturnsSafeProblemDetails(t *testing.T) {
	const actionKey = "parse_error_safety_test"
	const rawSentinel = "RAW_MODEL_OUTPUT_SENTINEL_DO_NOT_EXPOSE"

	teardown := withTempDB(t)
	defer teardown()
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET enabled=1, provider='openrouter', model='test/model', api_key='test-key' WHERE id=1`,
	); err != nil {
		t.Fatalf("enable ai settings: %v", err)
	}

	prev, hadPrev := actionRegistry[actionKey]
	actionRegistry[actionKey] = actionDescriptor{
		Key:         actionKey,
		Label:       "Parse error safety test",
		Surface:     "issue",
		Placement:   "issue",
		Implemented: true,
		Handler: func(ax *aiActionContext) (any, string, int, int, string, error) {
			var out struct {
				OK bool `json:"ok"`
			}
			testAx := *ax
			testAx.Provider = invalidJSONActionProvider{text: "not json " + rawSentinel}
			model, ptok, ctok, finish, err := callJSONAction(
				context.Background(),
				&testAx,
				"system",
				"user",
				100,
				&out,
			)
			return nil, model, ptok, ctok, finish, err
		},
	}
	t.Cleanup(func() {
		if hadPrev {
			actionRegistry[actionKey] = prev
			return
		}
		delete(actionRegistry, actionKey)
	})

	buf, restore := captureLog(t)
	defer restore()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/ai/action",
		bytes.NewBufferString(`{"action":"`+actionKey+`"}`),
	)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserKey, &models.User{
		ID:       42,
		Username: "admin",
		Role:     "admin",
		Status:   "active",
	}))
	rec := httptest.NewRecorder()

	AIAction(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	aiRequestID := rec.Header().Get(AIRequestIDHeader)
	if aiRequestID == "" {
		t.Fatal("missing AI request id header")
	}
	var problem ProblemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem details: %v", err)
	}
	if problem.Code != "ai_action_invalid_response" {
		t.Fatalf("problem.Code=%q", problem.Code)
	}
	if problem.RequestID != aiRequestID {
		t.Fatalf("problem.RequestID=%q header=%q", problem.RequestID, aiRequestID)
	}
	if strings.Contains(rec.Body.String(), rawSentinel) {
		t.Fatalf("problem details leaked raw model output: %s", rec.Body.String())
	}
	if strings.Contains(buf.String(), rawSentinel) {
		t.Fatalf("logs leaked raw model output: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "error_class=response_parse") {
		t.Fatalf("logs missing response_parse class: %s", buf.String())
	}
}
