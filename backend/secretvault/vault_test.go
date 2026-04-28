// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package secretvault_test

// PAI-261 phase-1 unit tests. Each subtest carries its own root-key
// reset so tests don't bleed package-level cached state into each
// other (the cache is a sync.Once + package-level []byte).

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/markus-barta/paimos/backend/secretvault"
)

// helper: set a known 32-byte key in PAIMOS_SECRET_KEY and reset
// the cache so the next RootKey() reads it. Returns the raw key
// bytes so tests that need to forge legacy v0 ciphertexts can use
// them directly.
func setEnvKey(t *testing.T) []byte {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	t.Setenv("PAIMOS_SECRET_KEY", base64.StdEncoding.EncodeToString(raw))
	t.Setenv("DATA_DIR", t.TempDir()) // never read in env-key mode
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)
	return raw
}

// TestRoundTrip_PerDomain pins the basic invariant: a plaintext
// encrypted under domain X decrypts to the same bytes under domain X.
// Run across multiple domains in one process so the per-domain
// subkey derivation is also exercised.
func TestRoundTrip_PerDomain(t *testing.T) {
	setEnvKey(t)
	cases := []struct {
		domain    string
		plaintext string
	}{
		{"crm:provider_configs", `{"token":"pat-na1-FAKE","portal_id":"42"}`},
		{"ai:openrouter", `sk-or-FAKEKEY-ABCDEF`},
		{"webhook:incoming", `whsec_FAKEHOOKSECRET`},
	}
	for _, c := range cases {
		t.Run(c.domain, func(t *testing.T) {
			ct, err := secretvault.Encrypt(c.domain, []byte(c.plaintext))
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if ct[0] != 0x01 {
				t.Errorf("expected v1 prefix 0x01 on new writes, got 0x%02x", ct[0])
			}
			pt, err := secretvault.Decrypt(c.domain, ct)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if string(pt) != c.plaintext {
				t.Errorf("round-trip mismatch: got %q, want %q", pt, c.plaintext)
			}
		})
	}
}

// TestDecrypt_LegacyV0 forges a v0 ciphertext the way the pre-PAI-261
// CRM code did (AES-GCM with the ROOT key directly, no version byte,
// no per-domain HKDF) and verifies Decrypt opens it under the same
// API the new code uses. This is the backward-compat invariant — any
// regression here breaks every existing CRM provider config in
// production on the next deploy.
func TestDecrypt_LegacyV0(t *testing.T) {
	rootKey := setEnvKey(t)
	plaintext := []byte(`{"token":"legacy-cipher-from-v2.1.x"}`)

	v0 := forgeV0(t, rootKey, plaintext)
	if v0[0] == 0x01 {
		// 1/256 of the time the random nonce starts with 0x01. If
		// that happens, regenerate so the test deliberately exercises
		// the "first byte ≠ 0x01" branch — the more common case in
		// production.
		v0 = forgeV0(t, rootKey, plaintext)
	}

	got, err := secretvault.Decrypt("crm:provider_configs", v0)
	if err != nil {
		t.Fatalf("Decrypt v0: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

// TestDecrypt_LegacyV0_CollisionPath is the rare case where a v0
// blob's random nonce happens to start with 0x01. The decrypt path
// tries v1 first (which fails — the blob wasn't encrypted under the
// domain subkey), then falls back to v0 (which succeeds). We force
// the collision by regenerating until we get one.
func TestDecrypt_LegacyV0_CollisionPath(t *testing.T) {
	rootKey := setEnvKey(t)
	plaintext := []byte(`forced-v0-collision`)

	var v0 []byte
	for i := 0; i < 1000; i++ {
		v0 = forgeV0(t, rootKey, plaintext)
		if v0[0] == 0x01 {
			break
		}
	}
	if v0[0] != 0x01 {
		t.Skip("randomness didn't produce a 0x01 first byte in 1000 tries — skipping")
	}

	got, err := secretvault.Decrypt("crm:provider_configs", v0)
	if err != nil {
		t.Fatalf("Decrypt v0 with v1-shaped prefix: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

// TestDecrypt_CrossDomain pins the per-domain isolation invariant: a
// blob encrypted under domain A must NOT decrypt under domain B,
// even though both use the same root key. This is what makes "leaked
// CRM blob replayed against the AI store" fail closed.
func TestDecrypt_CrossDomain(t *testing.T) {
	setEnvKey(t)
	ct, err := secretvault.Encrypt("crm:provider_configs", []byte("secret-A"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := secretvault.Decrypt("ai:openrouter", ct); err == nil {
		t.Fatalf("cross-domain decrypt unexpectedly succeeded — domain isolation is broken")
	} else if !errors.Is(err, secretvault.ErrCiphertext) {
		t.Errorf("expected ErrCiphertext, got %v", err)
	}
}

// TestDecrypt_TamperedV1 — bit-flip in the ciphertext body must fail
// AEAD verification, not return garbage. Tests at the byte level
// since GCM's tag check is what makes this safe.
func TestDecrypt_TamperedV1(t *testing.T) {
	setEnvKey(t)
	ct, err := secretvault.Encrypt("ai:openrouter", []byte("important"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip a bit deep in the ciphertext (past the version byte +
	// nonce). Any single-bit mutation should trip the GCM tag.
	if len(ct) < 1+12+5 {
		t.Fatalf("ct too short to tamper (%d)", len(ct))
	}
	tampered := append([]byte{}, ct...)
	tampered[1+12+1] ^= 0x01

	if _, err := secretvault.Decrypt("ai:openrouter", tampered); err == nil {
		t.Fatalf("tampered ciphertext decrypted successfully — GCM tag check is broken")
	} else if !errors.Is(err, secretvault.ErrCiphertext) {
		t.Errorf("expected ErrCiphertext, got %v", err)
	}
}

// TestDecrypt_MalformedShort — blobs shorter than nonce+tag are
// structurally invalid and must error rather than panic. Edge case
// for a corrupted-on-disk row.
func TestDecrypt_MalformedShort(t *testing.T) {
	setEnvKey(t)
	for _, blob := range [][]byte{
		nil,
		{},
		{0x01},
		make([]byte, 10),                // too short to be a v0 nonce
		append([]byte{0x01}, make([]byte, 11)...), // v1 prefix + short nonce, no tag
	} {
		if _, err := secretvault.Decrypt("ai:openrouter", blob); err == nil {
			t.Errorf("malformed blob (len %d) decrypted successfully", len(blob))
		}
	}
}

// TestEncrypt_AlwaysV1 pins that new writes never accidentally emit
// the old v0 format. Catches a regression where someone reorders the
// envelope-build code and drops the version byte.
func TestEncrypt_AlwaysV1(t *testing.T) {
	setEnvKey(t)
	for i := 0; i < 32; i++ {
		ct, err := secretvault.Encrypt("ai:openrouter", []byte("x"))
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		if ct[0] != 0x01 {
			t.Fatalf("iteration %d: new write does not start with v1 prefix (got 0x%02x)", i, ct[0])
		}
	}
}

// TestRootKey_EnvOverridesDisk pins the AC: when both a disk
// .secret-key file and PAIMOS_SECRET_KEY env var are present, the
// env wins. Operators move from T1 (disk) to T2 (env) by setting
// the env var; their existing data must keep decrypting under the
// SAME bytes, not whatever was on disk.
func TestRootKey_EnvOverridesDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)

	// Write a different 32-byte value to disk.
	diskKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, diskKey); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".secret-key"), diskKey, 0o600); err != nil {
		t.Fatalf("write disk key: %v", err)
	}

	// Set a DIFFERENT key in env.
	envKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, envKey); err != nil {
		t.Fatalf("rand: %v", err)
	}
	t.Setenv("PAIMOS_SECRET_KEY", base64.StdEncoding.EncodeToString(envKey))

	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	got, err := secretvault.RootKey()
	if err != nil {
		t.Fatalf("RootKey: %v", err)
	}
	if string(got) != string(envKey) {
		t.Errorf("env did not override disk: got %x, want %x", got[:8], envKey[:8])
	}
}

// TestRootKey_DiskFallback — without env, the disk file is read
// (and auto-generated on first call if missing).
func TestRootKey_DiskFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("PAIMOS_SECRET_KEY", "")
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	k1, err := secretvault.RootKey()
	if err != nil {
		t.Fatalf("RootKey first call: %v", err)
	}
	if len(k1) != 32 {
		t.Fatalf("k1 len = %d, want 32", len(k1))
	}
	// Disk file should now exist with mode 0600. The mode check is
	// platform-dependent (Windows lacks Unix mode bits) so we only
	// assert the file exists.
	if _, err := os.Stat(filepath.Join(dir, ".secret-key")); err != nil {
		t.Errorf("disk key not persisted: %v", err)
	}

	// Re-resetting and re-reading must return the SAME bytes — i.e.
	// disk reads on subsequent boots are stable across restarts.
	secretvault.ResetForTest()
	k2, err := secretvault.RootKey()
	if err != nil {
		t.Fatalf("RootKey re-read: %v", err)
	}
	if string(k1) != string(k2) {
		t.Errorf("disk key not stable across resets: first %x, second %x", k1[:8], k2[:8])
	}
}

// TestRootKey_InvalidEnv — env var present but malformed (not 32
// bytes after base64-decode) must surface ErrInvalidKey, not silently
// fall through to the disk path. Operators rotate by setting env;
// silent fallback would mean a typo'd new key keeps decrypting old
// data under the old disk key — which is exactly the kind of stealth
// failure we want to catch.
func TestRootKey_InvalidEnv(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_SECRET_KEY", base64.StdEncoding.EncodeToString([]byte("too-short")))
	secretvault.ResetForTest()
	t.Cleanup(secretvault.ResetForTest)

	if _, err := secretvault.RootKey(); !errors.Is(err, secretvault.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

// TestEncryptJSON_RoundTrip — JSON convenience wrapper. Just enough
// coverage to catch a marshaller swap.
func TestEncryptJSON_RoundTrip(t *testing.T) {
	setEnvKey(t)
	type creds struct {
		Token   string `json:"token"`
		Portal  string `json:"portal"`
	}
	in := creds{Token: "pat-na1-FAKE", Portal: "12345678"}

	ct, err := secretvault.EncryptJSON("crm:provider_configs", in)
	if err != nil {
		t.Fatalf("EncryptJSON: %v", err)
	}
	var out creds
	if err := secretvault.DecryptJSON("crm:provider_configs", ct, &out); err != nil {
		t.Fatalf("DecryptJSON: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got %+v, want %+v", out, in)
	}
}

// forgeV0 builds a legacy CRM-style ciphertext using the same shape
// the pre-PAI-261 encryptSecrets() emitted: AES-GCM with the ROOT
// key directly (no HKDF), no version byte, layout
// `nonce(12) || ciphertext || tag`. The test fixture exists so we
// can pin backward-compat without digging up an actual production
// row.
func forgeV0(t *testing.T, rootKey, plaintext []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(rootKey)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil)
}
