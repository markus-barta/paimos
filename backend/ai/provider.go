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

// Package ai is the LLM-text-optimization layer for PAIMOS (PAI-146).
//
// The package is intentionally provider-agnostic. PAI-151 isolates every
// vendor-specific concern behind the Provider interface defined in this
// file so the editor flow, the prompt wrapper, the audit pipeline, and
// the HTTP handler do not depend on any particular vendor.
//
// What stays inside the interface boundary:
//
//   - Authentication (API key vs. bearer token vs. local socket).
//   - Wire format (OpenAI-compatible JSON, Anthropic native, Ollama
//     `/api/generate`, llama.cpp HTTP, etc.).
//   - Token-usage shape, finish reasons, error taxonomy.
//   - Request shaping that the provider needs but the rest of the system
//     should not care about (e.g. role tagging, system-vs-user split).
//
// What stays OUTSIDE the interface (and is shared across providers):
//
//   - The fixed PAIMOS-owned system wrapper (PAI-150): this is product
//     surface and cannot be delegated to a vendor's defaults.
//   - The admin-editable optimization instruction.
//   - Context assembly (issue title, project name, parent epic, etc.).
//   - Audit logging (PAI-153). Providers report metadata; logging is
//     centralised so every backend gets the same observability.
//
// The shape below is what PAI-122 needs to add a local-model provider
// without touching any of the rest of the feature: implement Optimize,
// register a name, done.
package ai

import (
	"context"
	"errors"
)

// Provider is the single seam every LLM backend implements. New backends
// land in this package as a sibling file (e.g. openrouter.go,
// ollama.go, lmstudio.go) and a registration entry in registry.go.
//
// Implementations MUST be safe for concurrent use — the optimize HTTP
// handler does not serialize requests.
type Provider interface {
	// Name returns the stable identifier used in admin settings and
	// audit logs. Should match the value stored in ai_settings.provider
	// (e.g. "openrouter", "ollama"). Lowercase, no whitespace.
	Name() string

	// Optimize sends one rewrite request to the backend and returns
	// the result. Implementations should respect ctx for cancellation
	// and timeouts; the caller (handlers/ai_optimize.go) owns the
	// timeout policy so providers don't reinvent it.
	//
	// Implementations MUST NOT log full prompt or full output bodies —
	// PAI-153 mandates audit-without-bodies, and the only sane place to
	// enforce that is the provider boundary.
	Optimize(ctx context.Context, req OptimizeRequest) (OptimizeResponse, error)
}

// OptimizeRequest is the provider-generic shape of one rewrite call.
// SystemPrompt and UserPrompt are pre-assembled by prompt.go so the
// provider does not assemble or interpret them.
//
// APIKey + BaseURL + Model are populated from ai_settings by the
// caller. Providers that don't need a key (e.g. local Ollama) ignore
// the field; providers that don't accept a custom BaseURL ignore that.
// Keeping all fields on the request — rather than per-provider config —
// makes the ai_settings shape provider-generic too.
type OptimizeRequest struct {
	// Model is the provider-scoped model identifier ("anthropic/claude-3.5-haiku"
	// for OpenRouter, "llama3.3:70b" for Ollama, etc.).
	Model string

	// APIKey is the provider auth secret, or empty for providers that
	// don't need one.
	APIKey string

	// BaseURL is an optional override (used by Ollama/LM Studio/llama.cpp
	// where the operator runs the daemon at a known URL). Empty means
	// the provider's documented default.
	BaseURL string

	// SystemPrompt is the assembled PAIMOS wrapper + admin instruction.
	// Treated as opaque by the provider.
	SystemPrompt string

	// UserPrompt is the field text plus assembled context.
	// Treated as opaque by the provider.
	UserPrompt string

	// MaxOutputTokens is a soft cap; providers that don't expose this
	// to their API may ignore it. The handler sets a sensible default.
	MaxOutputTokens int
}

// OptimizeResponse is the provider-generic result. Latency and tokens
// are best-effort: any field a provider can't fill stays at zero, and
// the audit layer treats zero as "not reported" rather than "0 tokens".
//
// FinishReason is informational only. The handler does not branch on
// it (a "length" finish on a text-rewrite request is still a usable
// rewrite, not an error). It exists so audit / future analytics can
// see the truncation rate without an extra call.
type OptimizeResponse struct {
	// Text is the rewritten field content. The handler returns this
	// verbatim to the SPA, which renders it in the diff overlay.
	Text string

	// Model is the actual model the provider served (may differ from
	// the request when the upstream silently fell back).
	Model string

	// PromptTokens / CompletionTokens are zero when the provider does
	// not report them.
	PromptTokens     int
	CompletionTokens int

	// FinishReason is the provider's stop reason ("stop", "length",
	// "content_filter", "tool_calls", …) lower-cased and unmapped.
	FinishReason string
}

// ErrProviderUnconfigured is returned when the caller asks a provider
// to run without the keys/URLs it needs. Treated as a 503 by the
// handler so the SPA can show "AI optimization is not configured" in
// place of a generic failure.
var ErrProviderUnconfigured = errors.New("ai provider not configured")

// ErrProviderUnavailable is returned when the provider was reachable
// but refused the request for a transient reason (rate limit, upstream
// outage, model not found). Distinct from ErrProviderUnconfigured so
// the handler can surface a "try again later" message rather than the
// "configure first" message.
var ErrProviderUnavailable = errors.New("ai provider temporarily unavailable")
