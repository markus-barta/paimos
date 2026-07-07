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
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

func TestResolveAIActionOptionsDefaults(t *testing.T) {
	got, err := resolveAIActionOptions(AISettings{Provider: "openrouter", Model: "test/model"}, "spec_out", aiActionOptions{}, nil)
	if err != nil {
		t.Fatalf("resolve default options: %v", err)
	}
	want := resolvedAIActionOptions{
		ProfileID:       "balanced",
		Model:           "test/model",
		Effort:          "standard",
		PromptPresetRef: "default",
		ContextPack:     "issue",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved options = %+v, want %+v", got, want)
	}
}

func TestResolveAIActionOptionsPerActionDefaults(t *testing.T) {
	got, err := resolveAIActionOptions(AISettings{Provider: "openrouter", Model: "test/model"}, "detect_duplicates", aiActionOptions{}, nil)
	if err != nil {
		t.Fatalf("resolve duplicate defaults: %v", err)
	}
	if got.ProfileID != "deep" || got.Effort != "deep" {
		t.Fatalf("detect_duplicates defaults = (%q,%q), want (deep,deep)", got.ProfileID, got.Effort)
	}
}

func TestResolveAIActionOptionsRejectsUnavailableChoices(t *testing.T) {
	settings := AISettings{Provider: "openrouter", Model: "test/model"}
	cases := []struct {
		name string
		opts aiActionOptions
		want string
	}{
		{"profile", aiActionOptions{ProfileID: "unknown"}, "profile is not available"},
		{"model", aiActionOptions{ModelID: "other/model"}, "model is not available"},
		{"effort", aiActionOptions{Effort: "maximum"}, "effort is not supported"},
		{"prompt", aiActionOptions{PromptPresetRef: "project/runbook"}, "prompt preset is not available"},
		{"context", aiActionOptions{ContextPack: "not-a-pack"}, "context pack is not available"},
		{"project-context", aiActionOptions{ContextPack: "knowledge"}, "context pack requires a project-scoped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveAIActionOptions(settings, "spec_out", tc.opts, nil)
			if err == nil {
				t.Fatal("expected options error")
			}
			if !strings.Contains(err.msg, tc.want) {
				t.Fatalf("error msg=%q, want containing %q", err.msg, tc.want)
			}
		})
	}
}

func TestAIActionOptionsEnvelopeRecordedAsMetadata(t *testing.T) {
	const actionKey = "options_envelope_test"

	teardown := withTempDB(t)
	defer teardown()
	if _, err := db.DB.Exec(
		`INSERT INTO users(id, username, password, role, status) VALUES(42, 'admin', 'x', 'admin', 'active')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET enabled=1, provider='openrouter', model='test/model', api_key='test-key' WHERE id=1`,
	); err != nil {
		t.Fatalf("enable ai settings: %v", err)
	}

	prev, hadPrev := actionRegistry[actionKey]
	actionRegistry[actionKey] = actionDescriptor{
		Key:         actionKey,
		Label:       "Options envelope test",
		Surface:     "issue",
		Placement:   "issue",
		Implemented: true,
		Handler: func(ax *aiActionContext) (any, string, int, int, string, error) {
			if ax.Options.Effort != "standard" {
				t.Fatalf("handler options = %+v", ax.Options)
			}
			return map[string]bool{"ok": true}, "test/model", 13, 5, "stop", nil
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
		bytes.NewBufferString(`{
			"action":"`+actionKey+`",
			"options":{
				"profile_id":"default",
				"model_id":"test/model",
				"effort":"standard",
				"prompt_preset_ref":"default",
				"context_pack":"issue_only"
			}
		}`),
	)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserKey, &models.User{
		ID:       42,
		Username: "admin",
		Role:     "admin",
		Status:   "active",
	}))
	rec := httptest.NewRecorder()

	AIAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body actionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode action response: %v", err)
	}
	wantOptions := resolvedAIActionOptions{
		ProfileID:       "default",
		Model:           "test/model",
		Effort:          "standard",
		PromptPresetRef: "default",
		ContextPack:     "issue",
	}
	if !reflect.DeepEqual(body.Options, wantOptions) {
		t.Fatalf("response options = %+v", body.Options)
	}

	var profileID, effort, promptPresetRef, contextPack string
	if err := db.DB.QueryRow(
		`SELECT profile_id, effort, prompt_preset_ref, context_pack FROM ai_calls WHERE request_id=?`,
		body.RequestID,
	).Scan(&profileID, &effort, &promptPresetRef, &contextPack); err != nil {
		t.Fatalf("query ai_call options: %v", err)
	}
	if profileID != "default" || effort != "standard" || promptPresetRef != "default" || contextPack != "issue" {
		t.Fatalf("stored options = (%q,%q,%q,%q)", profileID, effort, promptPresetRef, contextPack)
	}
	logLine := buf.String()
	for _, want := range []string{`profile_id="default"`, `effort="standard"`, `prompt_preset_ref="default"`, `context_pack="issue"`} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("audit log missing %s: %s", want, logLine)
		}
	}
}

func TestAIActionContextPackAssemblesKnowledgeWithoutLeakingBody(t *testing.T) {
	const actionKey = "context_pack_knowledge_test"
	const sentinel = "CONTEXT_PACK_BODY_SENTINEL_DO_NOT_RETURN"

	teardown := withTempDB(t)
	defer teardown()
	if _, err := db.DB.Exec(
		`INSERT INTO users(id, username, password, role, status) VALUES(42, 'admin', 'x', 'admin', 'active')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET enabled=1, provider='openrouter', model='test/model', api_key='test-key' WHERE id=1`,
	); err != nil {
		t.Fatalf("enable ai settings: %v", err)
	}
	projectID := seedAIPromptPresetProject(t, "AICK")
	seedAIPromptPreset(t, projectID, "memory", "context_rule", "Context Rule", sentinel, "backlog", map[string]any{})
	res, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, description, status, priority)
		VALUES(?, ?, 'ticket', 'Context target', 'Needs context.', 'backlog', 'medium')
	`, projectID, 99)
	if err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	issueID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("issue id: %v", err)
	}

	prev, hadPrev := actionRegistry[actionKey]
	actionRegistry[actionKey] = actionDescriptor{
		Key:         actionKey,
		Label:       "Context pack test",
		Surface:     "issue",
		Placement:   "issue",
		Implemented: true,
		Handler: func(ax *aiActionContext) (any, string, int, int, string, error) {
			if ax.Options.ContextPack != "knowledge" {
				t.Fatalf("context pack=%q, want knowledge", ax.Options.ContextPack)
			}
			if !strings.Contains(ax.Options.contextPackBody, sentinel) {
				t.Fatalf("context pack body missing sentinel: %q", ax.Options.contextPackBody)
			}
			if len(ax.Options.ContextSources) != 1 || ax.Options.ContextSources[0].Kind != "knowledge" {
				t.Fatalf("context sources=%#v, want knowledge source", ax.Options.ContextSources)
			}
			return map[string]bool{"ok": true}, "test/model", 1, 1, "stop", nil
		},
	}
	t.Cleanup(func() {
		if hadPrev {
			actionRegistry[actionKey] = prev
			return
		}
		delete(actionRegistry, actionKey)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/ai/action",
		bytes.NewBufferString(`{"action":"`+actionKey+`","issue_id":`+fmt.Sprint(issueID)+`,"options":{"context_pack":"knowledge"}}`),
	)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserKey, &models.User{
		ID:       42,
		Username: "admin",
		Role:     "admin",
		Status:   "active",
	}))
	rec := httptest.NewRecorder()

	AIAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sentinel) {
		t.Fatalf("response leaked context body: %s", rec.Body.String())
	}
	var body actionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode action response: %v", err)
	}
	if body.Options.ContextPack != "knowledge" || body.Options.ContextPackLabel != "Project knowledge" {
		t.Fatalf("context options=%#v", body.Options)
	}
	if len(body.Options.ContextSources) != 1 || body.Options.ContextSources[0].Kind != "knowledge" {
		t.Fatalf("response context sources=%#v", body.Options.ContextSources)
	}
}

func TestAIActionRejectsInvalidOptionsBeforeHandler(t *testing.T) {
	const actionKey = "options_invalid_test"

	teardown := withTempDB(t)
	defer teardown()
	if _, err := db.DB.Exec(
		`INSERT INTO users(id, username, password, role, status) VALUES(42, 'admin', 'x', 'admin', 'active')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET enabled=1, provider='openrouter', model='test/model', api_key='test-key' WHERE id=1`,
	); err != nil {
		t.Fatalf("enable ai settings: %v", err)
	}

	called := false
	prev, hadPrev := actionRegistry[actionKey]
	actionRegistry[actionKey] = actionDescriptor{
		Key:         actionKey,
		Label:       "Invalid options test",
		Surface:     "issue",
		Placement:   "issue",
		Implemented: true,
		Handler: func(ax *aiActionContext) (any, string, int, int, string, error) {
			called = true
			return map[string]bool{"ok": true}, "test/model", 0, 0, "stop", nil
		},
	}
	t.Cleanup(func() {
		if hadPrev {
			actionRegistry[actionKey] = prev
			return
		}
		delete(actionRegistry, actionKey)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/ai/action",
		bytes.NewBufferString(`{"action":"`+actionKey+`","options":{"effort":"maximum"}}`),
	)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserKey, &models.User{
		ID:       42,
		Username: "admin",
		Role:     "admin",
		Status:   "active",
	}))
	rec := httptest.NewRecorder()

	AIAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler was called despite invalid options")
	}
	if !strings.Contains(rec.Body.String(), "effort is not supported") {
		t.Fatalf("body missing safe options error: %s", rec.Body.String())
	}
}
