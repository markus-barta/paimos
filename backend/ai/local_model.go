// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// localModelProvider is the draft-provider contract for operator-owned local
// model servers that expose an OpenAI-compatible chat-completions endpoint.
// Examples include LM Studio, vLLM, llama.cpp server, and Ollama's /v1
// compatibility surface. It has no secret requirement; BaseURL is mandatory.
type localModelProvider struct {
	httpClient *http.Client
}

func init() {
	Register(&localModelProvider{httpClient: &http.Client{}})
}

func (p *localModelProvider) Name() string { return "local_model" }

func (p *localModelProvider) Optimize(ctx context.Context, req OptimizeRequest) (OptimizeResponse, error) {
	if strings.TrimSpace(req.BaseURL) == "" || strings.TrimSpace(req.Model) == "" {
		return OptimizeResponse{}, ErrProviderUnconfigured
	}
	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	body := chatRequest{
		Model: req.Model,
		Messages: []chatMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserPrompt},
		},
		MaxTokens:   req.MaxOutputTokens,
		Temperature: 0.2,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return OptimizeResponse{}, fmt.Errorf("ai/local_model: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return OptimizeResponse{}, fmt.Errorf("ai/local_model: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return OptimizeResponse{}, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, openRouterMaxBodyBytes))
	if err != nil {
		return OptimizeResponse{}, fmt.Errorf("%w: read body: %v", ErrProviderUnavailable, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return OptimizeResponse{}, ErrProviderUnconfigured
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode >= 500:
		return OptimizeResponse{}, fmt.Errorf("%w: upstream status %d", ErrProviderUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		var er chatResponse
		_ = json.Unmarshal(raw, &er)
		if er.Error != nil && er.Error.Message != "" {
			return OptimizeResponse{}, fmt.Errorf("ai/local_model: %s", er.Error.Message)
		}
		return OptimizeResponse{}, fmt.Errorf("ai/local_model: upstream status %d", resp.StatusCode)
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return OptimizeResponse{}, fmt.Errorf("%w: decode body: %v", ErrProviderUnavailable, err)
	}
	if len(cr.Choices) == 0 {
		return OptimizeResponse{}, fmt.Errorf("%w: empty choices", ErrProviderUnavailable)
	}
	text := strings.TrimSpace(cr.Choices[0].Message.Content)
	if text == "" {
		return OptimizeResponse{}, fmt.Errorf("%w: empty completion", ErrProviderUnavailable)
	}
	model := cr.Model
	if strings.TrimSpace(model) == "" {
		model = req.Model
	}
	return OptimizeResponse{
		Text:             text,
		Model:            model,
		PromptTokens:     cr.Usage.PromptTokens,
		CompletionTokens: cr.Usage.CompletionTokens,
		FinishReason:     strings.ToLower(cr.Choices[0].FinishReason),
	}, nil
}
