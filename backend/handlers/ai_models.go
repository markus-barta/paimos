// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// PAI-160. GET /api/ai/models — server-cached top-3 OpenRouter
// models per category, served to the Settings → AI model picker.
//
// Why a server-side cache
// -----------------------
// The OpenRouter /api/v1/models response is ~1 MB and changes
// roughly daily. A 1 hour cache + manual "Refresh" button is the
// right tradeoff: admins never wait on the upstream during the
// usual settings workflow, but they CAN force a refresh when they
// know a new model just dropped. Going client-side would break the
// "hidden behind admin gate" model and force every admin tab open
// to hit the upstream.
//
// Why six categories
// ------------------
//   - free            (pricing == "0")
//   - open_weights    (hugging_face_id != "")
//   - frontier        (top of frontend top-weekly, $0.000005+/token)
//   - value           (>=128k ctx + tools support, sort by avg price)
//   - cheapest        (cheapest non-free)
//   - fastest         (frontend ?order=throughput-high-to-low)
//
// Why graceful fallback
// ---------------------
// OpenRouter is sometimes slow or rate-limits. We keep the last
// successful snapshot and serve it with `stale: true` so the picker
// never hard-empties; the UI shows a small "stale" pill so admins
// know to retry.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
)

const (
	// Source URLs. The frontend endpoint is undocumented but exposes
	// trending / throughput rankings that /v1/models doesn't. Treat
	// as best-effort: failures fall back to derived rankings.
	openRouterModelsURL  = "https://openrouter.ai/api/v1/models"
	openRouterFrontendFindURL = "https://openrouter.ai/api/frontend/models/find"

	// modelsCacheTTL is the soft-expiry of the cached snapshot. After
	// this we re-fetch on the next GET but still serve the stale copy
	// if the upstream call fails — a stale list beats a blank picker.
	modelsCacheTTL = 1 * time.Hour

	// modelsHTTPTimeout caps any one upstream call. Both endpoints are
	// chunky; a slow upstream is the most common failure mode.
	modelsHTTPTimeout = 12 * time.Second

	// modelsMaxBodyBytes guards memory in case OpenRouter returns
	// something pathological. Their /v1/models is ~1 MB; 4 MiB gives
	// headroom for growth.
	modelsMaxBodyBytes = 4 << 20

	// modelsPerCategory caps each category card grid. Setting this
	// here (not at the UI) keeps the wire payload tight and the API
	// contract explicit. PAI-178 raised it from 3 → 4 so the picker
	// shows a clean 4-per-row grid at typical tab widths.
	modelsPerCategory = 4

	// frontierPriceFloor is the per-token price (USD) used to keep
	// "Frontier" from filling with whatever happens to be trending.
	// 0.000005 USD/token = $5 / Mtok prompt — frontier-tier in 2026.
	frontierPriceFloor = 0.000005

	// valueMinContext is the min context window for the "Value" pick.
	// 128k separates "small embedded models" from "real workhorses".
	valueMinContext = 128_000
)

// orModel is the slice of /v1/models we read. Pricing fields are
// strings ("0" / "0.00000125") in OpenRouter's contract.
type orModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	ContextLength       int      `json:"context_length"`
	HuggingFaceID       string   `json:"hugging_face_id"`
	Pricing             orPricing `json:"pricing"`
	SupportedParameters []string `json:"supported_parameters"`
	Created             int64    `json:"created"`
}

type orPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

func (p orPricing) promptUSD() float64     { return parsePricing(p.Prompt) }
func (p orPricing) completionUSD() float64 { return parsePricing(p.Completion) }
func (p orPricing) avgUSD() float64 {
	return (p.promptUSD() + p.completionUSD()) / 2
}

func parsePricing(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// orModelsResponse is the /v1/models envelope.
type orModelsResponse struct {
	Data []orModel `json:"data"`
}

// orFrontendFindResponse is what the unofficial endpoint returns. Same
// model shape; we only need the top-N IDs from these calls — full
// pricing/context comes from the canonical /v1/models pull.
type orFrontendFindResponse struct {
	Data struct {
		Models []orModel `json:"models"`
	} `json:"data"`
}

// pickedModel is one entry in a category card. Pricing is normalized
// to USD/Mtok (multiply per-token USD by 1e6) for display purposes;
// raw pricing is omitted to keep the payload tight.
type pickedModel struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name"`
	ContextLength            int      `json:"context_length"`
	PricingPromptPerMtok     float64  `json:"pricing_prompt_per_mtok"`
	PricingCompletionPerMtok float64  `json:"pricing_completion_per_mtok"`
	Tags                     []string `json:"tags"`
}

// modelsResponse is the body served to the SPA. `stale` flips when
// the upstream call failed but a previous snapshot is being served.
// `fastest_unofficial` is true when the Fastest category was sourced
// from the undocumented frontend endpoint — surfacing this in the
// payload lets the UI add a "beta source" tooltip honestly.
type modelsResponse struct {
	Categories struct {
		Free        []pickedModel `json:"free"`
		OpenWeights []pickedModel `json:"open_weights"`
		Frontier    []pickedModel `json:"frontier"`
		Value       []pickedModel `json:"value"`
		Cheapest    []pickedModel `json:"cheapest"`
		Fastest     []pickedModel `json:"fastest"`
	} `json:"categories"`
	FetchedAt          time.Time `json:"fetched_at"`
	Stale              bool      `json:"stale"`
	FastestUnofficial  bool      `json:"fastest_unofficial"`
	Source             string    `json:"source"`
	UpstreamLatencyMs  int64     `json:"upstream_latency_ms"`
}

// modelsCache is the package-level cache. A single struct + mutex is
// fine for our needs: at most a few admins refresh at once, and the
// cached payload itself is small (~3 KB JSON for 18 picks).
var modelsCache struct {
	mu        sync.RWMutex
	payload   *modelsResponse
	fetchedAt time.Time
}

// AIListModels handles GET /api/ai/models. Admin-only (mounted under
// the admin auth group). Query string `?force=1` forces a re-fetch
// even when the cache is warm.
func AIListModels(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1"

	if !force {
		modelsCache.mu.RLock()
		fresh := modelsCache.payload != nil && time.Since(modelsCache.fetchedAt) < modelsCacheTTL
		if fresh {
			cp := *modelsCache.payload
			cp.Stale = false
			modelsCache.mu.RUnlock()
			jsonOK(w, cp)
			return
		}
		modelsCache.mu.RUnlock()
	}

	// Cache miss / stale / forced refresh: do the upstream calls. Any
	// failure here falls back to the previous snapshot (if any) marked
	// `stale`, otherwise to a curated static fallback (the same list
	// SettingsAITab used to ship with).
	t0 := time.Now()
	pl, err := buildModelsPayload(r.Context())
	latency := time.Since(t0)

	if err != nil {
		log.Printf("ai_models: upstream fetch failed: %v", err)
		modelsCache.mu.RLock()
		prev := modelsCache.payload
		modelsCache.mu.RUnlock()
		if prev != nil {
			cp := *prev
			cp.Stale = true
			jsonOK(w, cp)
			return
		}
		// Cold-start failure: serve the static fallback. The picker
		// stays usable even on first-ever boot when OpenRouter is down.
		jsonOK(w, staticFallbackPayload(latency))
		return
	}

	pl.UpstreamLatencyMs = latency.Milliseconds()
	modelsCache.mu.Lock()
	modelsCache.payload = pl
	modelsCache.fetchedAt = pl.FetchedAt
	modelsCache.mu.Unlock()

	jsonOK(w, pl)
}

// buildModelsPayload makes both upstream calls and assembles the
// six categories. The frontend-find call is best-effort: if it
// fails we fall back to deriving "Fastest" from the canonical
// /v1/models response by sorting on context_length (a coarse but
// safe proxy when no throughput data is available).
func buildModelsPayload(parent context.Context) (*modelsResponse, error) {
	ctx, cancel := context.WithTimeout(parent, modelsHTTPTimeout)
	defer cancel()

	canonical, err := fetchOpenRouterModels(ctx)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return nil, errors.New("openrouter returned 0 models")
	}

	pl := &modelsResponse{
		FetchedAt: time.Now().UTC(),
		Source:    "openrouter",
	}
	pl.Categories.Free = pickFree(canonical)
	pl.Categories.OpenWeights = pickOpenWeights(canonical)
	pl.Categories.Value = pickValue(canonical)
	pl.Categories.Cheapest = pickCheapest(canonical)

	// Frontier: explicitly diversify across the four major frontier
	// vendors (Anthropic, OpenAI, xAI, Google) so admins always see
	// the top model from each — top-weekly alone tends to cluster on
	// whichever vendor happened to ship most recently. PAI-178.
	if frontier, err := fetchFrontendFind(ctx, "top-weekly"); err == nil && len(frontier) > 0 {
		joined := joinByID(frontier, canonical)
		pl.Categories.Frontier = pickFrontierByVendor(joined, canonical)
	} else {
		pl.Categories.Frontier = pickFrontierByVendor(nil, canonical)
	}

	// Fastest: throughput ranking is unofficial. Mark accordingly.
	if fastest, err := fetchFrontendFind(ctx, "throughput-high-to-low"); err == nil && len(fastest) > 0 {
		pl.Categories.Fastest = trim(toPicked(joinByID(fastest, canonical), "fast"), modelsPerCategory)
		pl.FastestUnofficial = true
	} else {
		// Fallback proxy: highest context_length tends to correlate
		// with hosted-on-fast-infra, but it's a weak proxy. Marking
		// `fastest_unofficial = false` is honest here: this is just
		// /v1/models data, not a real throughput ranking.
		pl.Categories.Fastest = pickFastestFallback(canonical)
		pl.FastestUnofficial = false
	}

	return pl, nil
}

func fetchOpenRouterModels(ctx context.Context) ([]orModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("HTTP-Referer", "https://github.com/markus-barta/paimos")
	req.Header.Set("X-Title", "PAIMOS")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("openrouter /v1/models status " + strconv.Itoa(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, modelsMaxBodyBytes))
	if err != nil {
		return nil, err
	}
	var env orModelsResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func fetchFrontendFind(ctx context.Context, order string) ([]orModel, error) {
	url := openRouterFrontendFindURL + "?order=" + order + "&limit=20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("HTTP-Referer", "https://github.com/markus-barta/paimos")
	req.Header.Set("X-Title", "PAIMOS")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("openrouter frontend/find status " + strconv.Itoa(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, modelsMaxBodyBytes))
	if err != nil {
		return nil, err
	}
	var env orFrontendFindResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return env.Data.Models, nil
}

// joinByID enriches the (possibly thin) result of frontend/find with
// pricing/context from the canonical /v1/models pull. Frontend/find
// returns most fields, but the canonical source is the truth — and
// matching on ID is cheap.
func joinByID(thin, canonical []orModel) []orModel {
	byID := make(map[string]orModel, len(canonical))
	for _, m := range canonical {
		byID[m.ID] = m
	}
	out := make([]orModel, 0, len(thin))
	for _, t := range thin {
		if c, ok := byID[t.ID]; ok {
			out = append(out, c)
		} else {
			out = append(out, t)
		}
	}
	return out
}

// pickFree: pricing.prompt == "0" AND pricing.completion == "0".
// Sort by context_length desc so big-window free models surface first.
func pickFree(all []orModel) []pickedModel {
	var hits []orModel
	for _, m := range all {
		if m.Pricing.promptUSD() == 0 && m.Pricing.completionUSD() == 0 {
			hits = append(hits, m)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].ContextLength > hits[j].ContextLength
	})
	return trim(toPicked(hits, "free"), modelsPerCategory)
}

// pickOpenWeights: HuggingFace ID set. Sort by `created` desc — the
// open-weights world is moving fast and "what dropped most recently"
// is more useful than "what's the biggest".
func pickOpenWeights(all []orModel) []pickedModel {
	var hits []orModel
	for _, m := range all {
		if strings.TrimSpace(m.HuggingFaceID) != "" {
			hits = append(hits, m)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Created > hits[j].Created
	})
	return trim(toPicked(hits, "open_weights"), modelsPerCategory)
}

// pickValue: large context (>=128k) + tools support, sorted by
// average prompt+completion price ascending.
func pickValue(all []orModel) []pickedModel {
	var hits []orModel
	for _, m := range all {
		if m.ContextLength < valueMinContext {
			continue
		}
		if !contains(m.SupportedParameters, "tools") {
			continue
		}
		// Exclude free models from "value" — they have their own
		// category; keeping them out makes "Value" mean "best
		// price-to-power, not actually free".
		if m.Pricing.promptUSD() == 0 {
			continue
		}
		hits = append(hits, m)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Pricing.avgUSD() < hits[j].Pricing.avgUSD()
	})
	return trim(toPicked(hits, "value"), modelsPerCategory)
}

// pickCheapest: lowest combined prompt+completion price, excluding
// free (which is its own category).
func pickCheapest(all []orModel) []pickedModel {
	var hits []orModel
	for _, m := range all {
		total := m.Pricing.promptUSD() + m.Pricing.completionUSD()
		if total <= 0 {
			continue
		}
		hits = append(hits, m)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		ti := hits[i].Pricing.promptUSD() + hits[i].Pricing.completionUSD()
		tj := hits[j].Pricing.promptUSD() + hits[j].Pricing.completionUSD()
		return ti < tj
	})
	return trim(toPicked(hits, "cheap"), modelsPerCategory)
}

// PAI-178: vendor-diverse Frontier picker. We always want admins to
// see the top frontier-tier model from each of the four major
// vendors — Anthropic, OpenAI, xAI, Google — rather than four
// Anthropic models because Claude was trending that week.
//
// Strategy:
//   1. Build a per-vendor candidate list from the canonical
//      /v1/models pull (filter by frontier price floor).
//   2. If we have trending data, RANK each vendor's list using the
//      trending order; otherwise sort by created desc (newest first).
//   3. Take the top 1 per vendor in a fixed order: Anthropic → OpenAI
//      → xAI → Google. The fixed order matches industry mindshare
//      and gives admins a consistent visual scan.
//   4. If a vendor has no qualifying model, the slot is filled by
//      the next-best frontier model from any vendor (so the row
//      always renders 4 cards on a busy day).
func pickFrontierByVendor(trending, canonical []orModel) []pickedModel {
	// Bucket canonical models by vendor; only frontier-priced are
	// eligible.
	buckets := map[string][]orModel{
		"anthropic": nil,
		"openai":    nil,
		"xai":       nil,
		"google":    nil,
	}
	rest := []orModel{}
	for _, m := range canonical {
		if m.Pricing.promptUSD() < frontierPriceFloor {
			continue
		}
		v := vendorOf(m.ID)
		if _, ok := buckets[v]; ok {
			buckets[v] = append(buckets[v], m)
		} else {
			rest = append(rest, m)
		}
	}

	// Within each bucket, rank by trending order if available,
	// otherwise newest-first.
	rank := buildTrendingRank(trending)
	sortByRank := func(in []orModel) {
		sort.SliceStable(in, func(i, j int) bool {
			ri, rj := rank[in[i].ID], rank[in[j].ID]
			if ri == 0 && rj == 0 {
				return in[i].Created > in[j].Created
			}
			if ri == 0 {
				return false
			}
			if rj == 0 {
				return true
			}
			return ri < rj
		})
	}
	for k := range buckets {
		sortByRank(buckets[k])
	}
	sortByRank(rest)

	// Pick one from each vendor in the fixed display order.
	out := []pickedModel{}
	displayOrder := []string{"anthropic", "openai", "xai", "google"}
	used := map[string]bool{}
	for _, v := range displayOrder {
		if len(buckets[v]) == 0 {
			continue
		}
		pick := buckets[v][0]
		used[pick.ID] = true
		p := toPicked([]orModel{pick}, "frontier")[0]
		// Tag with vendor so the UI can render a vendor pill if
		// it wants to — keeps the picker informative without a
		// second API call.
		p.Tags = append(p.Tags, "vendor:"+v)
		out = append(out, p)
	}

	// Backfill from rest (non-major vendors or unmatched) until we
	// reach modelsPerCategory.
	for _, m := range rest {
		if len(out) >= modelsPerCategory {
			break
		}
		if used[m.ID] {
			continue
		}
		p := toPicked([]orModel{m}, "frontier")[0]
		out = append(out, p)
	}

	// If we still don't have enough, take leftovers from the major
	// buckets (a vendor's #2 model if another vendor was missing).
	if len(out) < modelsPerCategory {
		for _, v := range displayOrder {
			for i := 1; i < len(buckets[v]) && len(out) < modelsPerCategory; i++ {
				m := buckets[v][i]
				if used[m.ID] {
					continue
				}
				used[m.ID] = true
				p := toPicked([]orModel{m}, "frontier")[0]
				p.Tags = append(p.Tags, "vendor:"+v)
				out = append(out, p)
			}
		}
	}
	return out
}

// vendorOf maps a slash-prefixed OpenRouter id ("anthropic/claude-…",
// "openai/gpt-…", "x-ai/grok-…", "google/gemini-…") to a
// short vendor key. Anything we don't recognise falls into "other"
// so the bucket logic stays simple.
func vendorOf(id string) string {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		prefix := strings.ToLower(id[:i])
		switch prefix {
		case "anthropic":
			return "anthropic"
		case "openai":
			return "openai"
		case "x-ai", "xai":
			return "xai"
		case "google", "google-vertex":
			return "google"
		}
		return prefix
	}
	return "other"
}

// buildTrendingRank turns a trending list into an "ID → 1-based
// rank" map. Models not in the trending list get rank 0, which the
// sort treats as "later than any ranked model".
func buildTrendingRank(trending []orModel) map[string]int {
	r := make(map[string]int, len(trending))
	for i, m := range trending {
		r[m.ID] = i + 1
	}
	return r
}

// pickFastestFallback proxies "fastest" by largest context window,
// since /v1/models doesn't expose a throughput field and the
// frontend endpoint that does was unavailable.
func pickFastestFallback(all []orModel) []pickedModel {
	c := append([]orModel(nil), all...)
	sort.SliceStable(c, func(i, j int) bool {
		return c[i].ContextLength > c[j].ContextLength
	})
	return trim(toPicked(c, "fast"), modelsPerCategory)
}

func toPicked(in []orModel, tag string) []pickedModel {
	out := make([]pickedModel, 0, len(in))
	for _, m := range in {
		p := pickedModel{
			ID:                       m.ID,
			Name:                     m.Name,
			ContextLength:            m.ContextLength,
			PricingPromptPerMtok:     m.Pricing.promptUSD() * 1_000_000,
			PricingCompletionPerMtok: m.Pricing.completionUSD() * 1_000_000,
			Tags:                     []string{tag},
		}
		// Add a secondary tag if obviously applicable: free + open.
		if m.Pricing.promptUSD() == 0 && m.Pricing.completionUSD() == 0 && tag != "free" {
			p.Tags = append(p.Tags, "free")
		}
		if strings.TrimSpace(m.HuggingFaceID) != "" && tag != "open_weights" {
			p.Tags = append(p.Tags, "open_weights")
		}
		out = append(out, p)
	}
	return out
}

func trim[T any](s []T, n int) []T {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func contains(s []string, want string) bool {
	for _, x := range s {
		if x == want {
			return true
		}
	}
	return false
}

// staticFallbackPayload mirrors the curated list SettingsAITab shipped
// before this endpoint existed. Only used on cold-start with a dead
// upstream — keeps the picker from being empty during onboarding when
// OpenRouter is unreachable.
func staticFallbackPayload(latency time.Duration) *modelsResponse {
	pl := &modelsResponse{
		FetchedAt: time.Now().UTC(),
		Stale:     true,
		Source:    "static-fallback",
	}
	mkPick := func(id, name string, ctx int, prompt, completion float64, tags ...string) pickedModel {
		return pickedModel{
			ID: id, Name: name, ContextLength: ctx,
			PricingPromptPerMtok:     prompt,
			PricingCompletionPerMtok: completion,
			Tags:                     tags,
		}
	}
	pl.Categories.Free = []pickedModel{
		mkPick("meta-llama/llama-3.1-8b-instruct:free", "Llama 3.1 8B (free)", 131_072, 0, 0, "free"),
	}
	pl.Categories.OpenWeights = []pickedModel{
		mkPick("meta-llama/llama-3.3-70b-instruct", "Llama 3.3 70B", 131_072, 0.13, 0.4, "open_weights"),
	}
	pl.Categories.Frontier = []pickedModel{
		mkPick("anthropic/claude-sonnet-4.5", "Claude Sonnet 4.5", 200_000, 3.0, 15.0, "frontier", "quality"),
		mkPick("openai/gpt-4o", "GPT-4o", 128_000, 2.5, 10.0, "frontier", "quality"),
	}
	pl.Categories.Value = []pickedModel{
		mkPick("anthropic/claude-3.5-haiku", "Claude 3.5 Haiku", 200_000, 0.8, 4.0, "value"),
		mkPick("openai/gpt-4o-mini", "GPT-4o mini", 128_000, 0.15, 0.6, "value", "cheap"),
	}
	pl.Categories.Cheapest = []pickedModel{
		mkPick("openai/gpt-4o-mini", "GPT-4o mini", 128_000, 0.15, 0.6, "cheap"),
	}
	pl.Categories.Fastest = []pickedModel{
		mkPick("anthropic/claude-3.5-haiku", "Claude 3.5 Haiku", 200_000, 0.8, 4.0, "fast"),
	}
	pl.UpstreamLatencyMs = latency.Milliseconds()
	return pl
}

// requireAdminFromCtx is a small helper for use in tests. The real
// handler is mounted under auth.RequireAdmin in main.go.
func requireAdminFromCtx(r *http.Request) bool {
	user := auth.GetUser(r)
	return user != nil && user.Role == "admin"
}

// (kept private to avoid an unused-helper warning if tests import this
// package without exercising the helper)
var _ = requireAdminFromCtx
