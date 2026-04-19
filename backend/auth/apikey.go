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

package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// ResolveAPIKey looks up a raw API key (full "paimos_..." string), verifies it
// against the stored hash, updates last_used_at, and returns the owning user.
func ResolveAPIKey(rawKey string) (*models.User, error) {
	sum := sha256.Sum256([]byte(rawKey))
	hash := hex.EncodeToString(sum[:])

	var keyID int64
	u := &models.User{}
	dests := append([]any{&keyID}, userScanDests(u)...)
	err := db.DB.QueryRow(`
		SELECT ak.id, `+userSelectCols+`
		FROM api_keys ak JOIN users u ON u.id = ak.user_id
		WHERE ak.key_hash = ?
	`, hash).Scan(dests...)
	if err != nil {
		return nil, fmt.Errorf("invalid api key")
	}
	if u.Status == "inactive" || u.Status == "deleted" {
		return nil, fmt.Errorf("account disabled")
	}

	// Best-effort last_used_at update
	if _, err := db.DB.Exec("UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?", keyID); err != nil {
		log.Printf("ResolveAPIKey: update last_used_at key_id=%d: %v", keyID, err)
	}

	return u, nil
}
