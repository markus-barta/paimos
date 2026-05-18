// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-449. Pre-run cost estimate endpoint for the bulk
// generate-summary modal. Given an action_key + count, returns the
// expected cost range based on the active provider's model pricing
// and the rolling average prompt/completion token counts observed
// in past calls for the same action.
//
// Wire shape:
//   GET /api/ai/bulk-cost-estimate?action=customer_rewrite&n=585
//   →
//   {
//     "model": "anthropic/claude-sonnet-4.5",
//     "pricing_prompt_per_mtok": 3.0,
//     "pricing_completion_per_mtok": 15.0,
//     "avg_prompt_tokens": 700,
//     "avg_completion_tokens": 200,
//     "sample_size": 47,           // rows used to compute averages
//     "est_micro_usd_low": 320000, // -25% from baseline
//     "est_micro_usd_mid": 425000, // baseline
//     "est_micro_usd_high": 530000 // +25%
//   }
//
// Falls back to a fixed heuristic (700 prompt + 200 completion) when
// no history exists for this action+model combo.

package handlers

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
)

type bulkCostEstimateResponse struct {
	Model                    string  `json:"model"`
	PricingPromptPerMtok     float64 `json:"pricing_prompt_per_mtok"`
	PricingCompletionPerMtok float64 `json:"pricing_completion_per_mtok"`
	AvgPromptTokens          int     `json:"avg_prompt_tokens"`
	AvgCompletionTokens      int     `json:"avg_completion_tokens"`
	SampleSize               int     `json:"sample_size"`
	EstMicroUSDLow           int64   `json:"est_micro_usd_low"`
	EstMicroUSDMid           int64   `json:"est_micro_usd_mid"`
	EstMicroUSDHigh          int64   `json:"est_micro_usd_high"`
	HeuristicFallback        bool    `json:"heuristic_fallback"`
}

const (
	// Fallback per-call token heuristics used when ai_calls has no
	// history for this action+model. Order-of-magnitude estimates for
	// the typical Projektbericht customer-summary path (description +
	// title + AC + system prompt ≈ 700 prompt; 1-3 German sentences ≈
	// 200 completion). Updates here only affect cold-start estimates.
	bulkCostFallbackPromptTokens     = 700
	bulkCostFallbackCompletionTokens = 200
	// estimateUncertaintyPct is the ± band shown to the user around
	// the baseline computed from rolling averages. 25% absorbs
	// expected variance from description length / completion verbosity
	// without giving false precision.
	estimateUncertaintyPct = 0.25
)

// AIBulkCostEstimate is GET /api/ai/bulk-cost-estimate. Mounted in
// the auth group (any authenticated user can ask — the data exposed
// is pricing + rolling-average token counts, neither sensitive).
func AIBulkCostEstimate(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	if action == "" {
		jsonError(w, "action is required", http.StatusBadRequest)
		return
	}
	if _, ok := actionRegistry[action]; !ok {
		jsonError(w, "unknown action", http.StatusBadRequest)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 {
		jsonError(w, "n must be positive", http.StatusBadRequest)
		return
	}
	if n > 10_000 {
		jsonError(w, "n too large", http.StatusBadRequest)
		return
	}

	settings, err := LoadAISettings()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	model := strings.TrimSpace(settings.Model)
	if model == "" {
		jsonError(w, "no model configured", http.StatusServiceUnavailable)
		return
	}

	picked, ok := findModelByID(model)
	if !ok {
		// Model unknown to pricing cache (e.g. admin selected something
		// off-curated). Return the response with zero pricing so the UI
		// can render "estimate unavailable" instead of guessing.
		resp := bulkCostEstimateResponse{
			Model:               model,
			AvgPromptTokens:     bulkCostFallbackPromptTokens,
			AvgCompletionTokens: bulkCostFallbackCompletionTokens,
			HeuristicFallback:   true,
		}
		jsonOK(w, resp)
		return
	}

	avgPrompt, avgCompletion, sample := averageActionTokens(action, model)
	heuristic := false
	if sample == 0 {
		avgPrompt = bulkCostFallbackPromptTokens
		avgCompletion = bulkCostFallbackCompletionTokens
		heuristic = true
	}

	// Per-call cost in micro-USD (same unit math as
	// lookupAICallCostMicroUSD: USD-per-Mtok × tokens = micro-USD).
	perCallMicro := picked.PricingPromptPerMtok*float64(avgPrompt) +
		picked.PricingCompletionPerMtok*float64(avgCompletion)
	baseline := perCallMicro * float64(n)

	resp := bulkCostEstimateResponse{
		Model:                    model,
		PricingPromptPerMtok:     picked.PricingPromptPerMtok,
		PricingCompletionPerMtok: picked.PricingCompletionPerMtok,
		AvgPromptTokens:          avgPrompt,
		AvgCompletionTokens:      avgCompletion,
		SampleSize:               sample,
		EstMicroUSDLow:           int64(math.Round(baseline * (1 - estimateUncertaintyPct))),
		EstMicroUSDMid:           int64(math.Round(baseline)),
		EstMicroUSDHigh:          int64(math.Round(baseline * (1 + estimateUncertaintyPct))),
		HeuristicFallback:        heuristic,
	}
	jsonOK(w, resp)
}

// averageActionTokens reads the last 100 successful ai_calls rows for
// the given action_key + model and returns the rounded averages
// alongside the sample size. Returns (0, 0, 0) when no history exists.
//
// Restricted to outcome='ok' so failed/aborted calls (which can have
// truncated completions or 0 tokens) don't poison the average.
func averageActionTokens(action, model string) (avgPrompt, avgCompletion, sample int) {
	const q = `
SELECT prompt_tokens, completion_tokens
FROM ai_calls
WHERE action_key = ?
  AND model = ?
  AND outcome = 'ok'
  AND prompt_tokens > 0
ORDER BY id DESC
LIMIT 100
`
	rows, err := db.DB.Query(q, action, model)
	if err != nil {
		return 0, 0, 0
	}
	defer rows.Close()
	var sumP, sumC, n int
	for rows.Next() {
		var p, c int
		if err := rows.Scan(&p, &c); err != nil {
			continue
		}
		sumP += p
		sumC += c
		n++
	}
	if n == 0 {
		return 0, 0, 0
	}
	return int(math.Round(float64(sumP) / float64(n))),
		int(math.Round(float64(sumC) / float64(n))),
		n
}

// Compile-time check: encoding/json is used by the response body.
var _ = json.Marshal
