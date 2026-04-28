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

// Package secretvault is the single home for "encrypt this user-entered
// secret at rest" inside PAIMOS (PAI-261).
//
// Background
// ----------
// Before PAI-261, CRM provider creds were AES-GCM encrypted in the DB
// using a hand-rolled implementation in handlers/crm/config.go, while
// the AI api_key column was stored plaintext-with-a-comment-promising-
// to-fix-it. Every new feature that needed to persist a user-entered
// secret either re-implemented the CRM scheme or defaulted to plaintext.
// This package makes encryption-at-rest a single shared dependency.
//
// Envelope format
// ---------------
// New writes always emit v1:
//
//	v1:  0x01 || nonce(12) || ciphertext || gcm_tag
//
// Reads also accept v0 (the legacy CRM format):
//
//	v0:        nonce(12) || ciphertext || gcm_tag
//
// Detection at read-time is "try v1 first; fall back to v0 if that
// fails." The two formats can collide on the wire (a legacy v0 nonce
// might happen to begin with 0x01 — 1/256 chance per blob) but both
// paths verify the GCM auth tag, so the wrong path always errors
// rather than returning bogus plaintext. See Decrypt.
//
// Per-domain subkeys
// ------------------
// v1 ciphertexts are encrypted with an HKDF-SHA256-derived subkey,
// using the caller-supplied `domain` string as the HKDF `info`. This
// gives every secret-bearing surface (CRM provider configs, AI
// api_keys, future webhook tokens, …) its own subkey while still
// rooting at one master key. A ciphertext encrypted under
// `crm:provider_configs` cannot be replayed against `ai:openrouter`
// — the AEAD tag verifies under the wrong subkey and the read fails.
//
// Master-key sourcing
// -------------------
// Same model as the legacy CRM helper:
//
//  1. PAIMOS_SECRET_KEY env (base64 of 32 bytes) — operators that
//     manage the key out-of-band (k8s secret, sops, vault, …).
//  2. $DATA_DIR/.secret-key — auto-generated on first boot, mode 0600.
//     Stable across restarts; backing up the data dir backs it up too.
//
// Env wins; the disk path is the fallback. See HARDENING.md §3.6 for
// the operator-facing T1/T2 framing of these two modes.
package secretvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// versionV1 is the leading byte that identifies a v1 envelope. Anything
// without this prefix is treated as legacy v0 ciphertext (no version
// byte at all — the first byte is the random GCM nonce).
const versionV1 byte = 0x01

// nonceSize is GCM's standard 96-bit nonce length. Hard-coded rather
// than reading gcm.NonceSize() at decode-time so the format is
// independent of the cipher mode's parameter choices.
const nonceSize = 12

// rootKeyBytes is the master-key length. AES-256.
const rootKeyBytes = 32

var (
	// rootKeyOnce, rootKey, rootKeyErr cache the master key after the
	// first successful resolution. Tests can call ResetForTest to
	// re-read PAIMOS_SECRET_KEY / DATA_DIR between cases.
	rootKeyOnce sync.Once
	rootKey     []byte
	rootKeyErr  error
)

// ErrInvalidKey is returned when PAIMOS_SECRET_KEY is set but does not
// decode to exactly 32 bytes. Operators see this at the first
// secret-bearing call after boot, which is often the admin clicking
// "Test integration" — a clear failure mode beats a silent fallback.
var ErrInvalidKey = errors.New("PAIMOS_SECRET_KEY must be base64 of exactly 32 bytes")

// ErrCiphertext is the umbrella error wrapping any AEAD verification
// or format-parsing failure. Callers can `errors.Is(err, ErrCiphertext)`
// to distinguish "this ciphertext is bad" from infrastructure errors
// like key-file unreadable.
var ErrCiphertext = errors.New("ciphertext invalid or for a different domain")

// RootKey returns the master key, sourced per the package doc. Cached
// after the first call so repeat callers (CRM, AI, …) pay no extra
// I/O. Always returns the same []byte for the lifetime of the process.
func RootKey() ([]byte, error) {
	rootKeyOnce.Do(loadRootKey)
	return rootKey, rootKeyErr
}

// loadRootKey is the once-body. Extracted so it can be re-invoked from
// ResetForTest without fighting sync.Once.
func loadRootKey() {
	if envKey := os.Getenv("PAIMOS_SECRET_KEY"); envKey != "" {
		k, err := base64.StdEncoding.DecodeString(envKey)
		if err != nil || len(k) != rootKeyBytes {
			rootKeyErr = ErrInvalidKey
			return
		}
		rootKey = k
		return
	}
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "./data"
	}
	path := filepath.Join(dir, ".secret-key")
	if data, err := os.ReadFile(path); err == nil && len(data) == rootKeyBytes {
		rootKey = data
		return
	}
	// Generate + persist. First-boot path; subsequent boots read the
	// same bytes back from disk.
	k := make([]byte, rootKeyBytes)
	if _, err := rand.Read(k); err != nil {
		rootKeyErr = fmt.Errorf("generate secret key: %w", err)
		return
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		rootKeyErr = fmt.Errorf("mkdir data: %w", err)
		return
	}
	if err := os.WriteFile(path, k, 0o600); err != nil {
		rootKeyErr = fmt.Errorf("write secret key: %w", err)
		return
	}
	rootKey = k
}

// ResetForTest clears the cached root key so the next RootKey() call
// re-reads PAIMOS_SECRET_KEY / DATA_DIR. Test-only; production code
// must not call this. The function is exported (capital R) because
// test files in *_test packages can't reach unexported helpers.
func ResetForTest() {
	rootKeyOnce = sync.Once{}
	rootKey = nil
	rootKeyErr = nil
}

// deriveSubkey is HKDF-SHA256 with empty salt, using `domain` as the
// info parameter. Each domain gets a deterministic 32-byte subkey;
// changing the domain string changes the subkey, so cross-domain
// ciphertext replay fails AEAD verification.
//
// We implement HKDF inline (single Extract + single Expand block) to
// keep the dependency surface minimal — golang.org/x/crypto would
// pull a new top-level dep just for this 30-line primitive.
func deriveSubkey(rootKey []byte, domain string) []byte {
	// HKDF-Extract: PRK = HMAC-SHA256(salt, IKM). Salt is empty (RFC
	// 5869 recommends an all-zeros default of HashLen octets in that
	// case; HMAC's spec handles short keys identically).
	mac := hmac.New(sha256.New, nil)
	mac.Write(rootKey)
	prk := mac.Sum(nil)

	// HKDF-Expand: T(1) = HMAC-SHA256(PRK, info || 0x01). Single
	// block is sufficient since SHA256 outputs 32 bytes and we want
	// exactly 32 bytes (one AES-256 key).
	mac = hmac.New(sha256.New, prk)
	mac.Write([]byte(domain))
	mac.Write([]byte{0x01})
	return mac.Sum(nil)
}

// Encrypt seals plaintext under the per-domain subkey of the cached
// root key, emitting a v1 envelope. Domain MUST be a stable string
// chosen by the caller (e.g., "crm:provider_configs",
// "ai:openrouter") — changing it later silently bricks all
// previously-stored ciphertexts in that domain, so treat it as part
// of the data contract.
func Encrypt(domain string, plaintext []byte) ([]byte, error) {
	root, err := RootKey()
	if err != nil {
		return nil, err
	}
	return encryptWithKey(root, domain, plaintext)
}

// EncryptWithKey is the explicit-key variant used by the rotation CLI
// (PAI-261 phase 3). Production callers should use Encrypt; this is
// the seam for "decrypt with old key, re-encrypt with new key" flows.
func EncryptWithKey(rootKey []byte, domain string, plaintext []byte) ([]byte, error) {
	return encryptWithKey(rootKey, domain, plaintext)
}

func encryptWithKey(root []byte, domain string, plaintext []byte) ([]byte, error) {
	if len(root) != rootKeyBytes {
		return nil, ErrInvalidKey
	}
	subkey := deriveSubkey(root, domain)
	block, err := aes.NewCipher(subkey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// Layout: 0x01 || nonce || ciphertext || tag. gcm.Seal appends
	// ciphertext+tag to its first arg; we prepend the version byte
	// before nonce so detection is a single-byte peek at read time.
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, versionV1)
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt opens a v1 envelope OR a legacy v0 ciphertext. Detection is
// "try v1 first; fall back to v0 on failure." Both paths verify the
// GCM tag, so a wrong-domain or corrupted blob always errors rather
// than returning bogus plaintext.
func Decrypt(domain string, blob []byte) ([]byte, error) {
	root, err := RootKey()
	if err != nil {
		return nil, err
	}
	return decryptWithKey(root, domain, blob)
}

// DecryptWithKey is the explicit-key variant used by the rotation CLI.
func DecryptWithKey(rootKey []byte, domain string, blob []byte) ([]byte, error) {
	return decryptWithKey(rootKey, domain, blob)
}

func decryptWithKey(root []byte, domain string, blob []byte) ([]byte, error) {
	if len(root) != rootKeyBytes {
		return nil, ErrInvalidKey
	}
	// Path 1: v1 envelope. Requires version byte == 0x01 + a v1
	// payload long enough to hold a nonce + tag.
	if len(blob) >= 1+nonceSize+16 && blob[0] == versionV1 {
		subkey := deriveSubkey(root, domain)
		if pt, err := openGCM(subkey, blob[1:]); err == nil {
			return pt, nil
		}
		// fall through to v0 — could be a legacy ciphertext whose
		// random nonce starts with 0x01 (1/256 of all v0 blobs).
	}
	// Path 2: v0 ciphertext (legacy CRM format). Encrypted with the
	// ROOT key directly, no domain-derived subkey.
	if len(blob) >= nonceSize+16 {
		if pt, err := openGCM(root, blob); err == nil {
			return pt, nil
		}
	}
	return nil, ErrCiphertext
}

// openGCM splits `nonce(12) || ciphertext || tag` and AEAD-decrypts.
// Used by both the v1 (after stripping the leading 0x01) and v0 paths.
func openGCM(key []byte, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, ErrCiphertext
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// EncryptJSON is the JSON convenience wrapper used by callers whose
// plaintext is a struct or map. Equivalent to json.Marshal +
// Encrypt; exists so the CRM secret-map and AI api_key code paths
// don't each re-implement the marshalling.
func EncryptJSON(domain string, v any) ([]byte, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Encrypt(domain, plain)
}

// DecryptJSON decrypts and unmarshals into dst (which must be a
// pointer). Counterpart of EncryptJSON.
func DecryptJSON(domain string, blob []byte, dst any) error {
	plain, err := Decrypt(domain, blob)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, dst)
}
