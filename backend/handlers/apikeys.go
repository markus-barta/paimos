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
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/brand"
	"github.com/markus-barta/paimos/backend/db"
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
		SELECT id, name, key_prefix, created_at, last_used_at
		FROM api_keys WHERE user_id = ?
		ORDER BY created_at DESC
	`, user.ID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type APIKey struct {
		ID          int64   `json:"id"`
		Name        string  `json:"name"`
		KeyPrefix   string  `json:"key_prefix"`
		CreatedAt   string  `json:"created_at"`
		LastUsedAt  *string `json:"last_used_at"`
	}
	keys := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt); err == nil {
			keys = append(keys, k)
		}
	}
	jsonOK(w, keys)
}

// POST /api/auth/api-keys  { "name": "My script" }
// Returns the full key ONCE — not stored, not retrievable again.
func CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}

	full, prefix, hash, err := generateAPIKey()
	if err != nil {
		jsonError(w, "key generation failed", http.StatusInternalServerError)
		return
	}

	var id int64
	res, err := db.DB.Exec(`
		INSERT INTO api_keys(user_id, name, key_hash, key_prefix)
		VALUES (?, ?, ?, ?)
	`, user.ID, body.Name, hash, prefix)
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ = res.LastInsertId()

	log.Printf("audit: api_key_created username=%q key_prefix=%s name=%q", user.Username, prefix, body.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"name":       body.Name,
		"key_prefix": prefix,
		"key":        full, // shown ONCE
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
	if user.Role == "admin" {
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


