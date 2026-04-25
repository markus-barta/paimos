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

// PAI-152. OpenRouter Provider implementation.
//
// OpenRouter exposes an OpenAI-compatible chat-completions API at
// /api/v1/chat/completions. Using that contract here (rather than
// OpenRouter's optional OpenAI SDK) keeps the surface tiny and the
// dependency footprint zero — net/http + encoding/json only.
//
// Error mapping rules:
//
//   - 401 / 403 → ErrProviderUnconfigured (key wrong or revoked).
//     The admin needs to fix configuration; a user-facing retry won't
//     help. Same code path as "no key set at all".
//   - 429 / 5xx → ErrProviderUnavailable. Transient; SPA shows "try
//     again". We do NOT auto-retry: the user is in an editor with a
//     diff overlay waiting, so a quick "try again" beats a long wait.
//   - 4xx other → wrapped error with the upstream message. Surfaces
//     things like "model not found" or "request too large" verbatim
//     so admins can debug from the UI banner.
//
// Body capping: we intentionally do NOT trust the upstream Content-Length
// and read with io.LimitReader at 256 KiB. A short rewrite reply is
// usually <4 KiB; 256 KiB is a comfortable ceiling that prevents a
// rogue or buggy upstream from blowing memory.

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

const (
	openRouterDefaultBaseURL = "https://openrouter.ai/api/v1"
	openRouterMaxBodyBytes   = 256 << 10 // 256 KiB — see file header note
)

// openRouterProvider is the concrete implementation. Stateless beyond
// the shared http.Client, so a single package-level instance is enough.
type openRouterProvider struct {
	httpClient *http.Client
}

func init() {
	Register(&openRouterProvider{
		// We do not set a Timeout here — the handler controls that via
		// context.WithTimeout so the whole request (DNS + connect + TLS
		// + read) is bounded uniformly. A double-bound (handler + client)
		// makes timeouts confusing to reason about during incidents.
		httpClient: &http.Client{},
	})
}

func (p *openRouterProvider) Name() string { return "openrouter" }

// chatRequest is the OpenAI-compatible payload OpenRouter accepts.
// Trimmed to what we actually need; extra fields would just be noise
// in the wire log.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the slice of the OpenAI-compatible response we read.
// Anything we don't read (logprobs, system_fingerprint, …) stays
// unparsed.
type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *openRouterProvider) Optimize(ctx context.Context, req OptimizeRequest) (OptimizeResponse, error) {
	if req.APIKey == "" || req.Model == "" {
		return OptimizeResponse{}, ErrProviderUnconfigured
	}

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = openRouterDefaultBaseURL
	}

	body := chatRequest{
		Model: req.Model,
		Messages: []chatMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserPrompt},
		},
		MaxTokens: req.MaxOutputTokens,
		// Editorial rewrites benefit from low temperature: we want
		// fidelity to the source, not creative reinterpretation.
		Temperature: 0.2,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return OptimizeResponse{}, fmt.Errorf("ai/openrouter: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(encoded),
	)
	if err != nil {
		return OptimizeResponse{}, fmt.Errorf("ai/openrouter: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	// HTTP-Referer + X-Title are OpenRouter conventions for app
	// attribution. They're optional but help operators identify which
	// app a key is being used by in their OpenRouter dashboard.
	httpReq.Header.Set("HTTP-Referer", "https://github.com/markus-barta/paimos")
	httpReq.Header.Set("X-Title", "PAIMOS")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		// net/http returns context.DeadlineExceeded as the wrapped err
		// when the handler-imposed timeout fires. Propagate that as
		// "unavailable" rather than "unconfigured" so the SPA shows
		// "try again" instead of "configure first".
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
		// Try to decode the structured error body so admins see a
		// useful message; fall back to the raw HTTP status when the
		// body isn't OpenAI-compatible JSON.
		var er chatResponse
		_ = json.Unmarshal(raw, &er)
		if er.Error != nil && er.Error.Message != "" {
			return OptimizeResponse{}, fmt.Errorf("ai/openrouter: %s", er.Error.Message)
		}
		return OptimizeResponse{}, fmt.Errorf("ai/openrouter: upstream status %d", resp.StatusCode)
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

	return OptimizeResponse{
		Text:             text,
		Model:            cr.Model,
		PromptTokens:     cr.Usage.PromptTokens,
		CompletionTokens: cr.Usage.CompletionTokens,
		FinishReason:     strings.ToLower(cr.Choices[0].FinishReason),
	}, nil
}
