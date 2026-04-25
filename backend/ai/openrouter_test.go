// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// PAI-152. Provider-level tests for OpenRouter using httptest.
//
// We do not hit the real OpenRouter API in tests — that would be slow,
// flaky, expensive, and require a key that not every contributor has.
// The provider talks plain OpenAI-compatible JSON, so a small httptest
// server is enough to exercise the success and failure-mapping paths.

package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Get the registered OpenRouter provider rather than constructing one
// directly — this also covers init()-time registration.
func openrouter(t *testing.T) Provider {
	t.Helper()
	p, err := Get("openrouter")
	if err != nil {
		t.Fatalf("openrouter not registered: %v", err)
	}
	return p
}

func TestOpenRouter_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The handler should send a Bearer token + JSON body. Cheap
		// sanity checks here — we don't want this test to drift from
		// the upstream contract silently.
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("missing/malformed Authorization header: %q", got)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type: want application/json, got %q", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Errorf("expected system+user messages, got %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatResponse{
			Model: "anthropic/claude-3.5-haiku",
			Choices: []struct {
				Message      chatMessage `json:"message"`
				FinishReason string      `json:"finish_reason"`
			}{
				{
					Message:      chatMessage{Role: "assistant", Content: "polished text\n"},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			}{PromptTokens: 42, CompletionTokens: 7},
		})
	}))
	defer srv.Close()

	resp, err := openrouter(t).Optimize(context.Background(), OptimizeRequest{
		Model:        "anthropic/claude-3.5-haiku",
		APIKey:       "sk-or-test",
		BaseURL:      srv.URL,
		SystemPrompt: "wrapper",
		UserPrompt:   "raw text",
	})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if resp.Text != "polished text" {
		t.Errorf("Text: want %q, got %q", "polished text", resp.Text)
	}
	if resp.PromptTokens != 42 || resp.CompletionTokens != 7 {
		t.Errorf("usage: want 42/7, got %d/%d", resp.PromptTokens, resp.CompletionTokens)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason: want stop, got %q", resp.FinishReason)
	}
}

func TestOpenRouter_AuthErrorMappedAsUnconfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer srv.Close()

	_, err := openrouter(t).Optimize(context.Background(), OptimizeRequest{
		Model:   "x",
		APIKey:  "bad",
		BaseURL: srv.URL,
	})
	if !errors.Is(err, ErrProviderUnconfigured) {
		t.Fatalf("want ErrProviderUnconfigured, got %v", err)
	}
}

func TestOpenRouter_RateLimitMappedAsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := openrouter(t).Optimize(context.Background(), OptimizeRequest{
		Model:   "x",
		APIKey:  "k",
		BaseURL: srv.URL,
	})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("want ErrProviderUnavailable, got %v", err)
	}
}

func TestOpenRouter_BadRequestSurfacesUpstreamMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found: foo/bar"}}`))
	}))
	defer srv.Close()

	_, err := openrouter(t).Optimize(context.Background(), OptimizeRequest{
		Model: "foo/bar", APIKey: "k", BaseURL: srv.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "model not found: foo/bar") {
		t.Fatalf("expected upstream message in error, got %v", err)
	}
	// Crucially NOT mapped as unconfigured/unavailable — admins need to
	// see a model-not-found message to debug, not a generic banner.
	if errors.Is(err, ErrProviderUnconfigured) || errors.Is(err, ErrProviderUnavailable) {
		t.Errorf("4xx-other should not match a sentinel: %v", err)
	}
}

func TestOpenRouter_MissingConfig(t *testing.T) {
	_, err := openrouter(t).Optimize(context.Background(), OptimizeRequest{
		// Neither key nor model — the provider must short-circuit
		// before any HTTP call.
	})
	if !errors.Is(err, ErrProviderUnconfigured) {
		t.Fatalf("want ErrProviderUnconfigured, got %v", err)
	}
}

func TestOpenRouter_HonorsContextTimeout(t *testing.T) {
	// Server sleeps past the deadline; provider must abort.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := openrouter(t).Optimize(ctx, OptimizeRequest{
		Model: "x", APIKey: "k", BaseURL: srv.URL,
	})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("want ErrProviderUnavailable on timeout, got %v", err)
	}
}
