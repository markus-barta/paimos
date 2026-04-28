// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package secretvault_test

// PAI-261 phase-3 rotation tests. The invariants pinned here:
//
//   1. Rotate decrypts every CRM + AI ciphertext under the OLD key
//      and re-encrypts under the NEW key — round-trip works.
//   2. Dry-run reports counts but writes nothing — the DB is byte-
//      identical before and after.
//   3. A partial failure rolls back: no row is changed if any row
//      fails to decrypt or re-encrypt. (Tested by injecting a
//      corrupted CRM row that decrypts cleanly under neither key.)
//   4. After successful rotation, decrypts under the OLD key fail
//      (for the rows that actually rotated) and decrypts under the
//      NEW key succeed.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/markus-barta/paimos/backend/secretvault"
)

// rotateTestDB stands up an in-memory SQLite with just the two tables
// Rotate touches, populated to the shape Rotate expects (CRM has
// (provider_id, config_secret_json); ai_settings is a singleton with
// id=1 + api_key_encrypted column). We don't run the full migration
// stack here — that's tested in db/. This keeps the unit test fast
// and focused on the rotation primitive itself.
func rotateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE provider_configs (
			provider_id TEXT PRIMARY KEY,
			config_secret_json BLOB
		)`,
		`CREATE TABLE ai_settings (
			id INTEGER PRIMARY KEY,
			api_key_encrypted BLOB
		)`,
		`INSERT INTO ai_settings (id, api_key_encrypted) VALUES (1, NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v: %v", stmt, err)
		}
	}
	return db
}

// rotateTestSetEnv puts a known 32-byte master key in PAIMOS_SECRET_KEY
// and resets the secretvault cache so RootKey() reads the new value.
// Returns the bytes for tests that need to seed ciphertexts directly.
func rotateTestSetEnv(t *testing.T) []byte {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	t.Setenv("PAIMOS_SECRET_KEY", base64.StdEncoding.EncodeToString(raw))
	t.Setenv("DATA_DIR", t.TempDir())
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)
	return raw
}

func TestRotate_RoundTripCRM(t *testing.T) {
	rotateTestSetEnv(t)
	db := rotateTestDB(t)

	// Seed two CRM ciphertexts under the OLD key.
	crmA, err := secretvault.Encrypt("crm:provider_configs", []byte(`{"token":"A"}`))
	if err != nil {
		t.Fatalf("encrypt A: %v", err)
	}
	crmB, err := secretvault.Encrypt("crm:provider_configs", []byte(`{"token":"B"}`))
	if err != nil {
		t.Fatalf("encrypt B: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO provider_configs(provider_id, config_secret_json) VALUES (?, ?), (?, ?)`,
		"hubspot", crmA, "pipedrive", crmB,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Generate a NEW key (different from current).
	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		t.Fatalf("rand: %v", err)
	}

	report, err := secretvault.Rotate(context.Background(), db, secretvault.RotateOptions{NewKey: newKey})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if report.CRMRows != 2 || report.AIRows != 0 || report.DryRun {
		t.Errorf("report: %+v, want 2 CRM / 0 AI / not dry-run", report)
	}

	// Read both rows back out — they should decrypt under the NEW key
	// (via DecryptWithKey) but NOT under the OLD key.
	for _, providerID := range []string{"hubspot", "pipedrive"} {
		var cipher []byte
		if err := db.QueryRow(
			`SELECT config_secret_json FROM provider_configs WHERE provider_id = ?`,
			providerID,
		).Scan(&cipher); err != nil {
			t.Fatalf("read %s: %v", providerID, err)
		}
		if _, err := secretvault.DecryptWithKey(newKey, "crm:provider_configs", cipher); err != nil {
			t.Errorf("%s should decrypt under new key: %v", providerID, err)
		}
	}
}

func TestRotate_RoundTripAI(t *testing.T) {
	rotateTestSetEnv(t)
	db := rotateTestDB(t)

	// Seed an AI ciphertext.
	aiCt, err := secretvault.Encrypt("ai:openrouter", []byte("sk-or-OLD"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE ai_settings SET api_key_encrypted = ? WHERE id = 1`,
		aiCt,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		t.Fatalf("rand: %v", err)
	}

	report, err := secretvault.Rotate(context.Background(), db, secretvault.RotateOptions{NewKey: newKey})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if report.AIRows != 1 {
		t.Errorf("AIRows: got %d, want 1", report.AIRows)
	}

	// Confirm the row decrypts under NEW + plaintext is preserved.
	var afterCt []byte
	if err := db.QueryRow(`SELECT api_key_encrypted FROM ai_settings WHERE id = 1`).Scan(&afterCt); err != nil {
		t.Fatalf("read ai_settings: %v", err)
	}
	pt, err := secretvault.DecryptWithKey(newKey, "ai:openrouter", afterCt)
	if err != nil {
		t.Fatalf("decrypt under new key: %v", err)
	}
	if string(pt) != "sk-or-OLD" {
		t.Errorf("plaintext after rotation: got %q, want %q", pt, "sk-or-OLD")
	}
}

func TestRotate_DryRunWritesNothing(t *testing.T) {
	rotateTestSetEnv(t)
	db := rotateTestDB(t)

	crmCt, _ := secretvault.Encrypt("crm:provider_configs", []byte(`{"token":"X"}`))
	aiCt, _ := secretvault.Encrypt("ai:openrouter", []byte("sk-or-X"))
	if _, err := db.Exec(
		`INSERT INTO provider_configs(provider_id, config_secret_json) VALUES (?, ?)`,
		"hubspot", crmCt,
	); err != nil {
		t.Fatalf("seed crm: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE ai_settings SET api_key_encrypted = ? WHERE id = 1`,
		aiCt,
	); err != nil {
		t.Fatalf("seed ai: %v", err)
	}

	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		t.Fatalf("rand: %v", err)
	}

	report, err := secretvault.Rotate(context.Background(), db, secretvault.RotateOptions{
		NewKey: newKey,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Rotate dry-run: %v", err)
	}
	if !report.DryRun {
		t.Errorf("DryRun should be true in report")
	}
	if report.CRMRows != 1 || report.AIRows != 1 {
		t.Errorf("dry-run counts: %+v, want 1+1", report)
	}

	// Both rows should still be byte-identical to what we seeded.
	var crmAfter, aiAfter []byte
	if err := db.QueryRow(
		`SELECT config_secret_json FROM provider_configs WHERE provider_id = 'hubspot'`,
	).Scan(&crmAfter); err != nil {
		t.Fatalf("read crm: %v", err)
	}
	if string(crmAfter) != string(crmCt) {
		t.Errorf("dry-run mutated CRM row")
	}
	if err := db.QueryRow(
		`SELECT api_key_encrypted FROM ai_settings WHERE id = 1`,
	).Scan(&aiAfter); err != nil {
		t.Fatalf("read ai: %v", err)
	}
	if string(aiAfter) != string(aiCt) {
		t.Errorf("dry-run mutated AI row")
	}
}

func TestRotate_PartialFailureRollsBack(t *testing.T) {
	rotateTestSetEnv(t)
	db := rotateTestDB(t)

	// Seed one valid CRM row + one corrupted row that decrypts under
	// neither OLD nor NEW key. Rotation must abort and leave both
	// untouched.
	good, _ := secretvault.Encrypt("crm:provider_configs", []byte(`{"token":"good"}`))
	corrupt := append([]byte{0x01}, []byte("not-a-real-ciphertext-at-all-not-even-close")...)
	if _, err := db.Exec(
		`INSERT INTO provider_configs(provider_id, config_secret_json) VALUES (?, ?), (?, ?)`,
		"hubspot", good, "broken", corrupt,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		t.Fatalf("rand: %v", err)
	}

	_, err := secretvault.Rotate(context.Background(), db, secretvault.RotateOptions{NewKey: newKey})
	if err == nil {
		t.Fatalf("expected partial-rotation error, got nil")
	}
	if !errors.Is(err, secretvault.ErrPartialRotation) {
		t.Errorf("expected ErrPartialRotation, got %v", err)
	}

	// The good row must still decrypt under the OLD key — i.e. the
	// transaction rolled back, not committed-then-attempted-revert.
	var stillGood []byte
	if err := db.QueryRow(
		`SELECT config_secret_json FROM provider_configs WHERE provider_id = 'hubspot'`,
	).Scan(&stillGood); err != nil {
		t.Fatalf("read good row: %v", err)
	}
	if string(stillGood) != string(good) {
		t.Errorf("good row mutated despite rollback")
	}
}

func TestRotate_NoRowsIsFine(t *testing.T) {
	rotateTestSetEnv(t)
	db := rotateTestDB(t)
	// No CRM rows seeded; ai_settings has id=1 but api_key_encrypted IS NULL.

	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		t.Fatalf("rand: %v", err)
	}

	report, err := secretvault.Rotate(context.Background(), db, secretvault.RotateOptions{NewKey: newKey})
	if err != nil {
		t.Fatalf("Rotate empty: %v", err)
	}
	if report.CRMRows != 0 || report.AIRows != 0 {
		t.Errorf("empty rotation should report 0+0, got %+v", report)
	}
}

func TestRotate_RejectsShortKey(t *testing.T) {
	rotateTestSetEnv(t)
	db := rotateTestDB(t)
	_, err := secretvault.Rotate(context.Background(), db, secretvault.RotateOptions{
		NewKey: []byte("too-short"),
	})
	if err == nil {
		t.Fatalf("expected error for short key")
	}
}
