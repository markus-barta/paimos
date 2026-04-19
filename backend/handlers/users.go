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
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

func ListUsers(w http.ResponseWriter, r *http.Request) {
	// By default only active + inactive. Pass ?status=deleted for trash.
	status := r.URL.Query().Get("status")
	var rows interface{ Next() bool; Scan(...any) error; Close() error }
	var err error
	if status == "deleted" {
		rows, err = db.DB.Query(
			"SELECT "+userSelectColsWithTOTP+" FROM users WHERE status='deleted' ORDER BY username",
		)
	} else {
		rows, err = db.DB.Query(
			"SELECT "+userSelectColsWithTOTP+" FROM users WHERE status != 'deleted' ORDER BY username",
		)
	}
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	users := []models.User{}
	for rows.Next() {
		var u models.User
		if err := scanUserWithTOTP(rows, &u); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		users = append(users, u)
	}
	jsonOK(w, users)
}

func CreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		jsonError(w, "username and password required", http.StatusBadRequest)
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		jsonError(w, "hash failed", http.StatusInternalServerError)
		return
	}

	res, err := db.DB.Exec(
		"INSERT INTO users(username,password,role,status) VALUES(?,?,?,'active')",
		body.Username, hash, body.Role,
	)
	if handleDBError(w, err, "username") {
		return
	}
	id, _ := res.LastInsertId()
	var u models.User
	scanUser(db.DB.QueryRow("SELECT "+userSelectCols+" FROM users WHERE id=?", id), &u)
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, u)
}

func UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	var body struct {
		Username           *string  `json:"username"`
		Password           *string  `json:"password"`
		Role               *string  `json:"role"`
		Status             *string  `json:"status"`
		Nickname           *string  `json:"nickname"`
		Email              *string  `json:"email"`
		InternalRateHourly *float64 `json:"internal_rate_hourly"`
		Locale             *string  `json:"locale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	if body.Status != nil {
		s := *body.Status
		if s != "active" && s != "inactive" && s != "deleted" {
			jsonError(w, "status must be active, inactive, or deleted", http.StatusBadRequest)
			return
		}
	}

	// Validate nickname length
	if body.Nickname != nil && len([]rune(*body.Nickname)) > 3 {
		jsonError(w, "nickname must be 3 characters or fewer", http.StatusBadRequest)
		return
	}

	if body.Username != nil || body.Role != nil || body.Status != nil || body.Nickname != nil || body.Email != nil || body.InternalRateHourly != nil || body.Locale != nil {
		_, err = db.DB.Exec(`
			UPDATE users SET
				username             = COALESCE(?, username),
				role                 = COALESCE(?, role),
				status               = COALESCE(?, status),
				nickname             = COALESCE(?, nickname),
				email                = COALESCE(?, email),
				internal_rate_hourly = COALESCE(?, internal_rate_hourly),
				locale               = COALESCE(?, locale)
			WHERE id = ?
		`, body.Username, body.Role, body.Status, body.Nickname, body.Email, body.InternalRateHourly, body.Locale, id)
		if handleDBError(w, err, "user") {
			return
		}
	}

	if body.Password != nil && *body.Password != "" {
		hash, err := auth.HashPassword(*body.Password)
		if err != nil {
			jsonError(w, "hash failed", http.StatusInternalServerError)
			return
		}
		if _, err := db.DB.Exec("UPDATE users SET password=? WHERE id=?", hash, id); err != nil {
			jsonError(w, "password update failed", http.StatusInternalServerError)
			return
		}
	}

	var u models.User
	if err := scanUserWithTOTP(db.DB.QueryRow("SELECT "+userSelectColsWithTOTP+" FROM users WHERE id=?", id), &u); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, u)
}

// ResetUserTOTP disables 2FA for a user — admin only, no password required.
// POST /api/users/{id}/reset-totp
func ResetUserTOTP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if _, err := db.DB.Exec("UPDATE users SET totp_secret='', totp_enabled=0 WHERE id=?", id); err != nil {
		jsonError(w, "failed to reset 2FA", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]bool{"reset": true})
}

// DisableUser sets status to 'inactive' — account disabled, data preserved.
func DisableUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	caller := auth.GetUser(r)
	if caller != nil && caller.ID == id {
		jsonError(w, "cannot disable your own account", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec("UPDATE users SET status='inactive' WHERE id=? AND status='active'", id)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, "not found or not active", http.StatusNotFound)
		return
	}
	// invalidate sessions
	if _, err := db.DB.Exec("DELETE FROM sessions WHERE user_id=?", id); err != nil {
		log.Printf("DisableUser: delete sessions user_id=%d: %v", id, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteUser sets status to 'deleted' — hidden from UI, restorable via DB.
func DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	caller := auth.GetUser(r)
	if caller != nil && caller.ID == id {
		jsonError(w, "cannot delete your own account", http.StatusBadRequest)
		return
	}
	res, err := db.DB.Exec("UPDATE users SET status='deleted' WHERE id=? AND status != 'deleted'", id)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, "not found or already deleted", http.StatusNotFound)
		return
	}
	if _, err := db.DB.Exec("DELETE FROM sessions WHERE user_id=?", id); err != nil {
		log.Printf("DeleteUser: delete sessions user_id=%d: %v", id, err)
	}
	if _, err := db.DB.Exec("DELETE FROM api_keys WHERE user_id=?", id); err != nil {
		log.Printf("DeleteUser: delete api_keys user_id=%d: %v", id, err)
	}
	w.WriteHeader(http.StatusNoContent)
}
