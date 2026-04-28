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

package handlers_test

// PAI-261 phase-2 tests: ai_settings.api_key migration to encrypted-
// at-rest. The invariants pinned here are what makes the lazy-
// migration deploy-safe:
//
//   1. A row that pre-dates PAI-261 (api_key plaintext, api_key_encrypted
//      NULL) keeps decrypting on read.
//   2. A new save populates api_key_encrypted AND clears the plaintext
//      column, so the second read uses the encrypted path.
//   3. When both columns are populated (transitional / pathological
//      state), the encrypted column wins.
//
// Without these, the upgrade either breaks existing deployments on
// deploy day (1) or leaks plaintext past the migration window (2).

import (
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers"
	"github.com/markus-barta/paimos/backend/secretvault"
)

func Test_AISettings_ReadsPlaintextLegacy(t *testing.T) {
	_ = newTestServer(t)
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	// Simulate a row that existed before the PAI-261 migration: the
	// api_key column carries the value, api_key_encrypted is NULL.
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET enabled=1, provider='openrouter', model='m', api_key='sk-or-LEGACY' WHERE id=1`,
	); err != nil {
		t.Fatalf("seed plaintext row: %v", err)
	}

	got, err := handlers.LoadAISettings()
	if err != nil {
		t.Fatalf("LoadAISettings: %v", err)
	}
	if got.APIKey != "sk-or-LEGACY" {
		t.Errorf("plaintext fallback: got %q, want %q", got.APIKey, "sk-or-LEGACY")
	}
}

func Test_AISettings_PutEncryptsAndClearsPlaintext(t *testing.T) {
	ts := newTestServer(t)
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	// Start from the legacy plaintext state, mirroring a real upgrade.
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET enabled=1, provider='openrouter', model='m', api_key='sk-or-LEGACY' WHERE id=1`,
	); err != nil {
		t.Fatalf("seed plaintext row: %v", err)
	}

	// Admin re-saves with a fresh key.
	resp := ts.put(t, "/api/ai/settings", ts.adminCookie, map[string]any{
		"enabled":              true,
		"provider":             "openrouter",
		"model":                "m",
		"api_key":              "sk-or-NEW",
		"optimize_instruction": "x",
	})
	assertStatus(t, resp, http.StatusOK)

	// The DB row must now have:
	//   - api_key cleared to ''  (lazy-migration completion)
	//   - api_key_encrypted non-empty BLOB
	var apiKey string
	var encrypted []byte
	if err := db.DB.QueryRow(
		`SELECT api_key, api_key_encrypted FROM ai_settings WHERE id=1`,
	).Scan(&apiKey, &encrypted); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if apiKey != "" {
		t.Errorf("plaintext column should be cleared after encrypted save, got %q", apiKey)
	}
	if len(encrypted) == 0 {
		t.Fatalf("encrypted column should be populated after save")
	}
	if encrypted[0] != 0x01 {
		t.Errorf("encrypted column should carry v1 envelope (0x01 prefix), got 0x%02x", encrypted[0])
	}

	// LoadAISettings should now return the new key via the encrypted path.
	got, err := handlers.LoadAISettings()
	if err != nil {
		t.Fatalf("LoadAISettings: %v", err)
	}
	if got.APIKey != "sk-or-NEW" {
		t.Errorf("after save: got %q, want %q", got.APIKey, "sk-or-NEW")
	}
}

func Test_AISettings_EncryptedWinsWhenBothColumnsSet(t *testing.T) {
	_ = newTestServer(t)
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	// Forge a transitional state: BOTH columns populated. This isn't a
	// state PutAISettings will produce on its own (it always clears
	// plaintext on encrypted writes), but pins the read precedence
	// against any future caller that might leave a stale plaintext
	// row behind.
	enc, err := secretvault.Encrypt("ai:openrouter", []byte("sk-or-FROM-ENCRYPTED"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET api_key='sk-or-STALE-PLAINTEXT', api_key_encrypted=? WHERE id=1`,
		enc,
	); err != nil {
		t.Fatalf("seed both columns: %v", err)
	}

	got, err := handlers.LoadAISettings()
	if err != nil {
		t.Fatalf("LoadAISettings: %v", err)
	}
	if got.APIKey != "sk-or-FROM-ENCRYPTED" {
		t.Errorf("encrypted should win when both set: got %q, want %q",
			got.APIKey, "sk-or-FROM-ENCRYPTED")
	}
}

func Test_AISettings_PutNilKeyPreservesExisting(t *testing.T) {
	ts := newTestServer(t)
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	// Seed an encrypted row.
	enc, err := secretvault.Encrypt("ai:openrouter", []byte("sk-or-PRE"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET enabled=1, provider='openrouter', model='m', api_key='', api_key_encrypted=? WHERE id=1`,
		enc,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// PUT without api_key field → leaves both columns alone.
	resp := ts.put(t, "/api/ai/settings", ts.adminCookie, map[string]any{
		"enabled":              true,
		"provider":             "openrouter",
		"model":                "m-changed",
		"optimize_instruction": "y",
	})
	assertStatus(t, resp, http.StatusOK)

	got, err := handlers.LoadAISettings()
	if err != nil {
		t.Fatalf("LoadAISettings: %v", err)
	}
	if got.APIKey != "sk-or-PRE" {
		t.Errorf("api_key should be preserved when nil in payload: got %q", got.APIKey)
	}
	if got.Model != "m-changed" {
		t.Errorf("model should still update: got %q", got.Model)
	}
}

func Test_AISettings_PutEmptyKeyClearsBothColumns(t *testing.T) {
	ts := newTestServer(t)
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	// Seed both columns populated to maximise what we're clearing.
	enc, _ := secretvault.Encrypt("ai:openrouter", []byte("sk-or-WILL-BE-WIPED"))
	if _, err := db.DB.Exec(
		`UPDATE ai_settings SET api_key='legacy-also', api_key_encrypted=? WHERE id=1`,
		enc,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	emptyKey := ""
	resp := ts.put(t, "/api/ai/settings", ts.adminCookie, map[string]any{
		"enabled":              false,
		"provider":             "openrouter",
		"model":                "m",
		"api_key":              emptyKey,
		"optimize_instruction": "z",
	})
	assertStatus(t, resp, http.StatusOK)

	var apiKey string
	var encryptedAfter []byte
	if err := db.DB.QueryRow(
		`SELECT api_key, api_key_encrypted FROM ai_settings WHERE id=1`,
	).Scan(&apiKey, &encryptedAfter); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if apiKey != "" {
		t.Errorf("plaintext column should be cleared, got %q", apiKey)
	}
	if len(encryptedAfter) != 0 {
		t.Errorf("encrypted column should be cleared (got %d bytes)", len(encryptedAfter))
	}
}
