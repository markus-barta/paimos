// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	"github.com/inspr-at/paimos/backend/db"

	_ "modernc.org/sqlite"
)

func resetModelsCacheForTest(t *testing.T) {
	t.Helper()
	modelsCache.mu.Lock()
	oldPayload := modelsCache.payload
	oldFetchedAt := modelsCache.fetchedAt
	modelsCache.payload = nil
	modelsCache.fetchedAt = time.Time{}
	modelsCache.mu.Unlock()
	t.Cleanup(func() {
		modelsCache.mu.Lock()
		modelsCache.payload = oldPayload
		modelsCache.fetchedAt = oldFetchedAt
		modelsCache.mu.Unlock()
	})
}

func setModelsCachePayloadForTest(t *testing.T, payload *modelsResponse) {
	t.Helper()
	modelsCache.mu.Lock()
	modelsCache.payload = payload
	modelsCache.fetchedAt = payload.FetchedAt
	modelsCache.mu.Unlock()
}

func TestLookupAICallCostMicroUSDColdCacheUsesStaticFallback(t *testing.T) {
	resetModelsCacheForTest(t)

	got := lookupAICallCostMicroUSD("openai/gpt-4o-mini", 1000, 100)
	if got != 210 {
		t.Fatalf("cost=%d, want 210 micro-USD", got)
	}
}

func TestLookupAICallCostMicroUSDUsesFullModelIndex(t *testing.T) {
	resetModelsCacheForTest(t)
	payload := &modelsResponse{
		FetchedAt: time.Now().UTC(),
		Source:    "test",
		allModels: indexCanonicalModels([]orModel{{
			ID:            "example/non-bucket-model",
			Name:          "Non Bucket Model",
			ContextLength: 128000,
			Pricing: orPricing{
				Prompt:     "0.00000125",
				Completion: "0.00000625",
			},
		}}),
	}
	payload.Categories.Value = []pickedModel{{
		ID:                       "example/curated-model",
		Name:                     "Curated Model",
		PricingPromptPerMtok:     0.1,
		PricingCompletionPerMtok: 0.2,
	}}
	setModelsCachePayloadForTest(t, payload)

	got := lookupAICallCostMicroUSD("example/non-bucket-model", 1000, 100)
	want := int64(math.Round(1.25*1000 + 6.25*100))
	if got != want {
		t.Fatalf("cost=%d, want %d micro-USD", got, want)
	}
}

func TestBackfillRecentAICallCostsUpdatesPricedZeroRows(t *testing.T) {
	resetModelsCacheForTest(t)
	payload := &modelsResponse{
		FetchedAt: time.Now().UTC(),
		Source:    "test",
		allModels: indexCanonicalModels([]orModel{{
			ID: "example/non-bucket-model",
			Pricing: orPricing{
				Prompt:     "0.00000125",
				Completion: "0.00000625",
			},
		}}),
	}
	setModelsCachePayloadForTest(t, payload)

	oldDB := db.DB
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.DB = sqlDB
	t.Cleanup(func() {
		sqlDB.Close()
		db.DB = oldDB
	})
	if _, err := db.DB.Exec(`
		CREATE TABLE ai_calls (
			id INTEGER PRIMARY KEY,
			model TEXT,
			prompt_tokens INTEGER,
			completion_tokens INTEGER,
			cost_micro_usd INTEGER,
			created_at TEXT
		)
	`); err != nil {
		t.Fatalf("create ai_calls: %v", err)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec(`
		INSERT INTO ai_calls(id, model, prompt_tokens, completion_tokens, cost_micro_usd, created_at)
		VALUES
			(1, 'example/non-bucket-model', 1000, 100, 0, ?),
			(2, 'example/unknown-model', 1000, 100, 0, ?)
	`, now, now); err != nil {
		t.Fatalf("insert ai_calls: %v", err)
	}

	updated, err := backfillRecentAICallCosts(context.Background(), 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated=%d, want 1", updated)
	}
	var priced, unknown int64
	if err := db.DB.QueryRow(`SELECT cost_micro_usd FROM ai_calls WHERE id=1`).Scan(&priced); err != nil {
		t.Fatalf("select priced: %v", err)
	}
	if err := db.DB.QueryRow(`SELECT cost_micro_usd FROM ai_calls WHERE id=2`).Scan(&unknown); err != nil {
		t.Fatalf("select unknown: %v", err)
	}
	if priced != int64(math.Round(1.25*1000+6.25*100)) {
		t.Fatalf("priced cost=%d", priced)
	}
	if unknown != 0 {
		t.Fatalf("unknown cost=%d, want unchanged 0", unknown)
	}
}
