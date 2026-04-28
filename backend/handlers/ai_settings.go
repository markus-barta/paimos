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

// PAI-149: system settings for the LLM text-optimization feature.
//
// Three concerns share this file:
//
//   - GET /api/ai/settings — admin read of the full row, including the
//     API key. The non-admin status endpoint (/api/ai/status) lives next
//     to the optimize endpoint and only exposes whether the feature is
//     usable; this file's GET is admin-only.
//   - PUT /api/ai/settings — admin write. Empty api_key in the payload
//     leaves the stored key untouched, so admins can edit the model or
//     instruction without re-typing the secret every time.
//   - DefaultOptimizeInstruction — the seed text shown in the editor on
//     a fresh install. Lives here (not in a config file) because it is
//     part of the product surface that the prompt wrapper layers around.
//
// PAI-261: api_key is encrypted at rest under the `ai:openrouter`
// secretvault domain. The legacy plaintext column (`api_key`) stays as
// a transitional read fallback so deployments that haven't re-saved
// their key since the upgrade keep working untouched. On the next
// PutAISettings call the encrypted column is populated and the
// plaintext column is cleared — the migration is lazy by design.

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/secretvault"
)

// aiSecretDomain is the HKDF info string the secretvault package uses
// to derive a per-domain subkey for the AI provider api_key. PAI-261:
// changing this string silently bricks every existing encrypted
// api_key on next decrypt — treat it as part of the on-disk data
// contract.
const aiSecretDomain = "ai:openrouter"

// DefaultOptimizeInstruction is the admin-editable instruction layered
// inside the fixed wrapper. Phrased as "an editor's brief" rather than
// a system-prompt voice so admins can rewrite it without learning prompt
// syntax. Architecture-significant phrasing is called out explicitly so
// the model preserves it; that requirement comes from PAI-146.
const DefaultOptimizeInstruction = `You are an editor for software-project requirement text.

Goals, in order of priority:
1. Preserve technical meaning, intent, and any explicit decisions the author has made.
2. Preserve markdown structure: headings, lists, checklists, code blocks, and inline formatting.
3. Improve clarity, professional tone, and human readability.
4. Align wording with common software-engineering vocabulary used in this project.

Hard rules:
- Do NOT remove or normalize phrasing that signals architecture change, breaking change, schema change, infra change, new component, or a deliberate trade-off. Keep that intent visible.
- Do NOT add new requirements, scope, or commitments that are not in the source text.
- Do NOT translate the text into another language.
- Return ONLY the optimized text. No preamble, no explanation, no markdown fences around the whole reply.`

// AISettings is the shape persisted in the M74 singleton row. It is also
// the response body of GET /api/ai/settings; the handler clears the
// api_key in non-admin contexts (we only ever surface to admins anyway,
// but the JSON tag stays so admins see the saved value once).
type AISettings struct {
	Enabled             bool   `json:"enabled"`
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	APIKey              string `json:"api_key"`
	OptimizeInstruction string `json:"optimize_instruction"`
	UpdatedAt           string `json:"updated_at"`
}

// LoadAISettings reads the singleton row, applying defaults for fields
// that have never been set. Used by both the settings handler and the
// optimize handler — the latter only needs the resolved values, not the
// raw row.
//
// PAI-261: api_key resolution prefers the encrypted column. The
// transitional plaintext column is the fallback for deployments that
// have not re-saved their key since the upgrade. Once an admin
// re-saves, PutAISettings clears the plaintext column and only the
// encrypted column carries the value going forward.
func LoadAISettings() (AISettings, error) {
	var s AISettings
	var enabled int
	var plaintextKey string
	var encryptedKey []byte
	err := db.DB.QueryRow(
		`SELECT enabled, provider, model, api_key, api_key_encrypted, optimize_instruction, updated_at
		 FROM ai_settings WHERE id = 1`,
	).Scan(&enabled, &s.Provider, &s.Model, &plaintextKey, &encryptedKey, &s.OptimizeInstruction, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Migration M74 seeds id=1 on first apply, so this should not
		// happen — but if a hand-edited DB drops the row, we don't want
		// the whole optimize feature to 500. Return defaults instead.
		return AISettings{
			Provider:            "openrouter",
			OptimizeInstruction: DefaultOptimizeInstruction,
		}, nil
	}
	if err != nil {
		return AISettings{}, err
	}
	s.Enabled = enabled == 1
	// PAI-261: prefer the encrypted column. Failure to decrypt is an
	// operational error, not a data error — surface it loudly so the
	// admin sees something obviously wrong rather than silently
	// falling back to plaintext.
	switch {
	case len(encryptedKey) > 0:
		plain, derr := secretvault.Decrypt(aiSecretDomain, encryptedKey)
		if derr != nil {
			return AISettings{}, fmt.Errorf("ai_settings.api_key_encrypted decrypt: %w", derr)
		}
		s.APIKey = string(plain)
	case plaintextKey != "":
		s.APIKey = plaintextKey // legacy row, will get re-encrypted on next save
	}
	if s.OptimizeInstruction == "" {
		s.OptimizeInstruction = DefaultOptimizeInstruction
	}
	if s.Provider == "" {
		s.Provider = "openrouter"
	}
	return s, nil
}

// AvailableForOptimize is the cheap precondition check used by both the
// optimize endpoint and the public-status endpoint. It mirrors the UI's
// "AI button is enabled" rule: feature flag on, key present, model set.
// Provider is intentionally not checked here — adding a new provider
// (PAI-122) later just needs its own readiness shape.
func (s AISettings) AvailableForOptimize() bool {
	if !s.Enabled {
		return false
	}
	if s.Provider == "openrouter" {
		return s.APIKey != "" && s.Model != ""
	}
	return false
}

// GetAISettings — admin read of the row. Mounted under RequireAdmin in
// main.go. Non-admins use AIStatus instead, which only returns the bool.
func GetAISettings(w http.ResponseWriter, r *http.Request) {
	s, err := LoadAISettings()
	if err != nil {
		log.Printf("ai_settings load: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, s)
}

// aiSettingsPayload is the PUT body. APIKey is *string so the client can
// distinguish "leave the key as-is" (omit / null) from "clear it"
// (empty string).
type aiSettingsPayload struct {
	Enabled             bool    `json:"enabled"`
	Provider            string  `json:"provider"`
	Model               string  `json:"model"`
	APIKey              *string `json:"api_key"`
	OptimizeInstruction string  `json:"optimize_instruction"`
}

// PutAISettings — admin write. Validation is deliberately light: the
// only callers are admins through the settings UI, and the optimize
// endpoint guards itself against missing config separately.
func PutAISettings(w http.ResponseWriter, r *http.Request) {
	var p aiSettingsPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if p.Provider == "" {
		p.Provider = "openrouter"
	}
	if p.Provider != "openrouter" {
		// PAI-151 reserves the provider field for future local backends.
		// Until those land, refuse anything unknown so a typo can't put
		// the feature into an unusable state.
		jsonError(w, "unsupported provider", http.StatusBadRequest)
		return
	}
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	if p.APIKey == nil {
		// "Leave the key as-is" path — admin is editing the model or
		// instruction without re-typing the secret. Don't touch either
		// api_key column. (A row that was carried in plaintext stays in
		// plaintext until an actual re-save.)
		_, err := db.DB.Exec(
			`UPDATE ai_settings
			 SET enabled = ?, provider = ?, model = ?,
			     optimize_instruction = ?, updated_at = datetime('now')
			 WHERE id = 1`,
			enabled, p.Provider, p.Model, p.OptimizeInstruction,
		)
		if err != nil {
			log.Printf("ai_settings update: %v", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		// PAI-261: every save of a new key value goes through the
		// secretvault under the `ai:openrouter` domain. We always
		// clear the plaintext column at the same time so the lazy
		// migration completes for legacy rows on the first re-save
		// after the deploy. Empty-string clears (admin "removes" the
		// key) wipe both columns.
		var encrypted []byte
		if *p.APIKey != "" {
			ct, eerr := secretvault.Encrypt(aiSecretDomain, []byte(*p.APIKey))
			if eerr != nil {
				log.Printf("ai_settings encrypt: %v", eerr)
				jsonError(w, "internal error", http.StatusInternalServerError)
				return
			}
			encrypted = ct
		}
		_, err := db.DB.Exec(
			`UPDATE ai_settings
			 SET enabled = ?, provider = ?, model = ?,
			     api_key = '', api_key_encrypted = ?,
			     optimize_instruction = ?, updated_at = datetime('now')
			 WHERE id = 1`,
			enabled, p.Provider, p.Model, encrypted, p.OptimizeInstruction,
		)
		if err != nil {
			log.Printf("ai_settings update: %v", err)
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	s, err := LoadAISettings()
	if err != nil {
		log.Printf("ai_settings reload: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, s)
}

// AIStatus is the public, non-admin endpoint the SPA polls to know
// whether to show the AI button enabled or disabled (with tooltip).
// Returns only the boolean availability flag — never any configuration.
func AIStatus(w http.ResponseWriter, r *http.Request) {
	s, err := LoadAISettings()
	if err != nil {
		log.Printf("ai_settings status: %v", err)
		jsonOK(w, map[string]bool{"available": false})
		return
	}
	jsonOK(w, map[string]bool{"available": s.AvailableForOptimize()})
}
