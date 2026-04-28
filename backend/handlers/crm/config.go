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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/secretvault"
)

// crmSecretDomain is the HKDF info string used by the secretvault
// package to derive a per-domain subkey for CRM provider creds. PAI-261:
// changing this string silently bricks every existing CRM ciphertext on
// the next decrypt — treat it as part of the on-disk data contract.
const crmSecretDomain = "crm:provider_configs"

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
		// PAI-261: secretvault.DecryptJSON tries v1 (per-domain HKDF
		// subkey) first and falls back to v0 (legacy CRM root-key
		// direct), so existing pre-PAI-261 ciphertexts in production
		// keep decrypting on first read after the deploy without any
		// operator action.
		plain := map[string]string{}
		if err := secretvault.DecryptJSON(crmSecretDomain, secretBlob, &plain); err != nil {
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
		// PAI-261: every new write is a v1 envelope under the
		// `crm:provider_configs` domain subkey — see secretvault docs
		// for envelope shape + HKDF derivation. The previous v0
		// ciphertexts (root key direct, no version byte) are still
		// readable but are NEVER produced any more.
		secretBlob, err = secretvault.EncryptJSON(crmSecretDomain, rec.Secret)
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

// PAI-261 removed the hand-rolled secret-encryption helpers that used
// to live in this file (secretsKey / encryptSecrets / decryptSecrets).
// The shared backend/secretvault package now owns key resolution,
// per-domain HKDF subkey derivation, and the v0/v1 envelope read
// path. CRM is one of two consumers (the other is ai_settings).
