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

package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/brand"
	"github.com/inspr-at/paimos/backend/db"
)

// generateAPIKey returns a full key (shown once) and its prefix + hash for storage.
// Format: {BRAND_API_KEY_PREFIX}<64 hex chars>; prefix stored = first 13 chars.
func generateAPIKey() (full, prefix, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	full = brand.Default.APIKeyPrefix + hex.EncodeToString(b)
	prefix = full[:len(brand.Default.APIKeyPrefix)+8]
	sum := sha256.Sum256([]byte(full))
	hash = hex.EncodeToString(sum[:])
	return
}

// GET /api/auth/api-keys
func ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	rows, err := db.DB.Query(`
		SELECT id, name, key_prefix, created_at, last_used_at, scopes
		FROM api_keys WHERE user_id = ?
		ORDER BY created_at DESC
	`, user.ID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type APIKey struct {
		ID         int64    `json:"id"`
		Name       string   `json:"name"`
		KeyPrefix  string   `json:"key_prefix"`
		CreatedAt  string   `json:"created_at"`
		LastUsedAt *string  `json:"last_used_at"`
		Scopes     []string `json:"scopes"`
	}
	keys := []APIKey{}
	for rows.Next() {
		var k APIKey
		var scopesCSV string
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt, &scopesCSV); err == nil {
			k.Scopes = scopesSetToSortedSlice(auth.ParseScopes(scopesCSV))
			keys = append(keys, k)
		}
	}
	jsonOK(w, keys)
}

// scopesSetToSortedSlice serializes a ScopeSet to a stable, JSON-friendly
// array. The sentinel "*" sorts first because it has special meaning.
func scopesSetToSortedSlice(s auth.ScopeSet) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// POST /api/auth/api-keys  { "name": "My script", "scopes": ["projects:write"] }
// Returns the full key ONCE — not stored, not retrievable again.
//
// PAI-379: `scopes` is optional. Omitting it (or passing an empty array)
// stores the sentinel `*` so the key inherits the owner's full role —
// the long-standing behavior. Named scopes narrow the key; the caller's
// role must be permitted by the catalog to attach each named scope
// (see auth.ValidateScopesForRole).
func CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}

	// Build the scope set: empty / missing → ScopeAll (back-compat).
	scopeSet := auth.ScopeSet{}
	for _, s := range body.Scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		scopeSet[s] = struct{}{}
	}
	if len(scopeSet) == 0 {
		scopeSet[auth.ScopeAll] = struct{}{}
	}
	if err := auth.ValidateScopesForRole(scopeSet, user.Role); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	scopesCSV := auth.FormatScopes(scopeSet)

	full, prefix, hash, err := generateAPIKey()
	if err != nil {
		jsonError(w, "key generation failed", http.StatusInternalServerError)
		return
	}

	var id int64
	res, err := db.DB.Exec(`
		INSERT INTO api_keys(user_id, name, key_hash, key_prefix, scopes)
		VALUES (?, ?, ?, ?, ?)
	`, user.ID, body.Name, hash, prefix, scopesCSV)
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ = res.LastInsertId()

	log.Printf("audit: api_key_created username=%q key_prefix=%s name=%q scopes=%q",
		user.Username, prefix, body.Name, scopesCSV)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"name":       body.Name,
		"key_prefix": prefix,
		"key":        full, // shown ONCE
		"scopes":     scopesSetToSortedSlice(scopeSet),
	})
}

// DELETE /api/auth/api-keys/{id}
func DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Only delete own keys (admins may delete any)
	var query string
	var args []any
	if auth.IsAdmin(user) {
		query = "DELETE FROM api_keys WHERE id = ?"
		args = []any{id}
	} else {
		query = "DELETE FROM api_keys WHERE id = ? AND user_id = ?"
		args = []any{id, user.ID}
	}

	res, err := db.DB.Exec(query, args...)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, "key not found", http.StatusNotFound)
		return
	}
	log.Printf("audit: api_key_deleted username=%q key_id=%d", user.Username, id)
	w.WriteHeader(http.StatusNoContent)
}
