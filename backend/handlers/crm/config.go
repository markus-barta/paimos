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

package crm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-104. Provider config storage with secret hygiene.
//
// Two storage paths per provider:
//   - config_json (TEXT): non-secret values, JSON-encoded plain map.
//     Anyone who can read the DB can read these.
//   - config_secret_json (BLOB): secret values, AES-GCM encrypted with
//     a key from PAIMOS_SECRET_KEY (or auto-generated on first boot).
//     Tokens, API keys, and anything ConfigField marks as Type="secret"
//     lives here.
//
// API responses NEVER carry decrypted secret values — only a HasValue
// flag per field so the UI can render `••••• has value` without
// echoing back what the server stored.

// configRecord is the persisted shape (decoded from the DB).
type configRecord struct {
	ProviderID  string
	Enabled     bool
	NonSecret   map[string]string
	Secret      map[string]string // only ever in-memory; never serialised to API
}

// LoadConfig reads the merged (non-secret + decrypted secrets) config
// for a provider from the DB. Returns an empty record (zero-value) if
// the row doesn't exist yet — providers should treat that as
// "configure me first".
func LoadConfig(providerID string) (configRecord, error) {
	rec := configRecord{ProviderID: providerID, NonSecret: map[string]string{}, Secret: map[string]string{}}
	var enabledInt int
	var nonSecretJSON string
	var secretBlob []byte
	err := db.DB.QueryRow(`
		SELECT enabled, config_json, config_secret_json
		FROM provider_configs WHERE provider_id=?
	`, providerID).Scan(&enabledInt, &nonSecretJSON, &secretBlob)
	if err != nil {
		// No row yet = empty config. Not an error.
		if errors.Is(err, sql.ErrNoRows) {
			return rec, nil
		}
		return rec, fmt.Errorf("load provider_config: %w", err)
	}
	rec.Enabled = enabledInt != 0
	if nonSecretJSON != "" && nonSecretJSON != "{}" {
		if err := json.Unmarshal([]byte(nonSecretJSON), &rec.NonSecret); err != nil {
			return rec, fmt.Errorf("decode non-secret: %w", err)
		}
	}
	if len(secretBlob) > 0 {
		plain, err := decryptSecrets(secretBlob)
		if err != nil {
			return rec, fmt.Errorf("decrypt secrets: %w", err)
		}
		rec.Secret = plain
	}
	return rec, nil
}

// MergedValues returns non-secret + secret combined into a single map
// suitable for handing to a Provider call. Secrets shadow non-secrets
// if a key collides (shouldn't happen in practice — ConfigSchema keys
// are flat).
func (r configRecord) MergedValues() map[string]string {
	out := make(map[string]string, len(r.NonSecret)+len(r.Secret))
	for k, v := range r.NonSecret {
		out[k] = v
	}
	for k, v := range r.Secret {
		out[k] = v
	}
	return out
}

// SaveConfig persists the merged config back to the DB. Secrets are
// re-encrypted; non-secrets stored as plain JSON. updated_by may be 0
// when called from a test harness.
func SaveConfig(rec configRecord, updatedBy int64) error {
	nonSecretJSON, err := json.Marshal(rec.NonSecret)
	if err != nil {
		return fmt.Errorf("encode non-secret: %w", err)
	}
	var secretBlob []byte
	if len(rec.Secret) > 0 {
		secretBlob, err = encryptSecrets(rec.Secret)
		if err != nil {
			return fmt.Errorf("encrypt secrets: %w", err)
		}
	}
	enabledInt := 0
	if rec.Enabled {
		enabledInt = 1
	}
	// updated_by may be 0; map to NULL so the FK doesn't trip.
	var updatedByArg any
	if updatedBy > 0 {
		updatedByArg = updatedBy
	}
	_, err = db.DB.Exec(`
		INSERT INTO provider_configs(provider_id, enabled, config_json, config_secret_json, updated_at, updated_by)
		VALUES (?, ?, ?, ?, datetime('now'), ?)
		ON CONFLICT(provider_id) DO UPDATE SET
			enabled            = excluded.enabled,
			config_json        = excluded.config_json,
			config_secret_json = excluded.config_secret_json,
			updated_at         = excluded.updated_at,
			updated_by         = excluded.updated_by
	`, rec.ProviderID, enabledInt, string(nonSecretJSON), secretBlob, updatedByArg)
	return err
}

// ── Secret encryption ───────────────────────────────────────────────

var (
	secretKeyOnce sync.Once
	secretKey     []byte
	secretKeyErr  error
)

// secretsKey returns the 32-byte AES-256 key used for at-rest secret
// encryption. Resolved lazily and cached for the process lifetime:
//
//   1. PAIMOS_SECRET_KEY env (base64 of 32 bytes) — for ops that want
//      to manage the key themselves (k8s secret, vault, etc).
//   2. $DATA_DIR/.secret-key — auto-generated on first boot, 0600.
//      Stable across restarts.
//
// A fresh install with no env var generates a new key on first save
// and persists it to disk. Backing the data dir backs the key too.
func secretsKey() ([]byte, error) {
	secretKeyOnce.Do(func() {
		if envKey := os.Getenv("PAIMOS_SECRET_KEY"); envKey != "" {
			k, err := base64.StdEncoding.DecodeString(envKey)
			if err != nil || len(k) != 32 {
				secretKeyErr = fmt.Errorf("PAIMOS_SECRET_KEY must be base64 of exactly 32 bytes")
				return
			}
			secretKey = k
			return
		}
		dir := os.Getenv("DATA_DIR")
		if dir == "" {
			dir = "./data"
		}
		path := filepath.Join(dir, ".secret-key")
		if data, err := os.ReadFile(path); err == nil && len(data) == 32 {
			secretKey = data
			return
		}
		// Generate + persist.
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			secretKeyErr = fmt.Errorf("generate secret key: %w", err)
			return
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			secretKeyErr = fmt.Errorf("mkdir data: %w", err)
			return
		}
		if err := os.WriteFile(path, k, 0o600); err != nil {
			secretKeyErr = fmt.Errorf("write secret key: %w", err)
			return
		}
		secretKey = k
	})
	return secretKey, secretKeyErr
}

// encryptSecrets serialises the secret map as JSON and AES-GCM-encrypts
// it. Output layout: 12-byte nonce || ciphertext || GCM tag.
func encryptSecrets(secrets map[string]string) ([]byte, error) {
	plain, err := json.Marshal(secrets)
	if err != nil {
		return nil, err
	}
	key, err := secretsKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
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
	out := gcm.Seal(nonce, nonce, plain, nil)
	return out, nil
}

func decryptSecrets(blob []byte) (map[string]string, error) {
	key, err := secretsKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}
	return out, nil
}
