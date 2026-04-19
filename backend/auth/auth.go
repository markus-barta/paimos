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
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/brand"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
	"golang.org/x/crypto/bcrypt"
)

// cookieSecure mirrors the COOKIE_SECURE env var.
// Set COOKIE_SECURE=true on live (HTTPS); leave unset for staging/local (HTTP).
var cookieSecure = os.Getenv("COOKIE_SECURE") == "true"

const totpPendingTTLAuth = 5 * time.Minute

const sessionCookie = "session"
const sessionDuration = 24 * time.Hour

const authRateLimitWindow = 10 * time.Minute
const authRateLimitMaxAttempts = 5

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Middleware — attaches *models.User to context if session valid.
type contextKey string

const UserKey contextKey = "user"

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Try API key: Authorization: Bearer <BRAND_API_KEY_PREFIX>...
		if hdr := r.Header.Get("Authorization"); strings.HasPrefix(hdr, "Bearer "+brand.Default.APIKeyPrefix) {
			rawKey := strings.TrimPrefix(hdr, "Bearer ")
			user, err := ResolveAPIKey(rawKey)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), UserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 2. Fall back to session cookie
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		user, err := sessionUser(cookie.Value)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), UserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}



// scanUser scans the standard user projection into a User struct.
//
// IMPORTANT: this list must stay in lock-step with `userSelectCols` AND with the
// twin `handlers/userSelectCols` / `handlers/scanUser`. Adding a new user column
// requires updating BOTH packages — see Test_APIKeyAuth for the regression that
// caused this to be a recurring footgun.
func scanUser(row interface{ Scan(...any) error }, u *models.User) error {
	return row.Scan(userScanDests(u)...)
}

// userScanDests returns the scan destination pointers for userSelectCols.
// Useful when the query has extra prefix/suffix columns around the user cols.
func userScanDests(u *models.User) []any {
	return []any{
		&u.ID, &u.Username, &u.Role, &u.Status, &u.CreatedAt,
		&u.Nickname, &u.FirstName, &u.LastName, &u.Email, &u.AvatarPath,
		&u.MarkdownDefault, &u.MonospaceFields, &u.RecentProjectsLimit,
		&u.InternalRateHourly, &u.ShowAltUnitTable, &u.ShowAltUnitDetail, &u.Locale,
		&u.RecentTimersLimit, &u.Timezone, &u.PreviewHoverDelay, &u.LastLoginAt,
		&u.AccrualsStatsEnabled, &u.AccrualsExtraStatuses,
	}
}

// userSelectCols is the full qualified column list for the users table.
const userSelectCols = `u.id, u.username, u.role, u.status, u.created_at, u.nickname, u.first_name, u.last_name, u.email, u.avatar_path, u.markdown_default, u.monospace_fields, u.recent_projects_limit, u.internal_rate_hourly, u.show_alt_unit_table, u.show_alt_unit_detail, u.locale, u.recent_timers_limit, u.timezone, u.preview_hover_delay, u.last_login_at, u.accruals_stats_enabled, u.accruals_extra_statuses`

func sessionUser(sessionID string) (*models.User, error) {
	row := db.DB.QueryRow(`
		SELECT `+userSelectCols+`
		FROM sessions s JOIN users u ON s.user_id = u.id
		WHERE s.id = ? AND s.expires_at > datetime('now')
	`, sessionID)
	u := &models.User{}
	if err := scanUser(row, u); err != nil {
		return nil, err
	}
	// Block inactive and deleted users even if session exists
	if u.Status == "inactive" || u.Status == "deleted" {
		if _, err := db.DB.Exec("DELETE FROM sessions WHERE id=?", sessionID); err != nil {
			log.Printf("sessionUser: delete session %s: %v", sessionID, err)
		}
		return nil, fmt.Errorf("account disabled")
	}
	return u, nil
}

// Login handler
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	if allowed, retryAfter := allowAuthAttempt("login", r, body.Username); !allowed {
		log.Printf("audit: login_throttled username=%q ip=%s retry_after=%s", body.Username, clientIP(r), retryAfter.Round(time.Second))
		setRetryAfter(w, retryAfter)
		http.Error(w, `{"error":"too many login attempts"}`, http.StatusTooManyRequests)
		return
	}

	var hash string
	var totpEnabled int
	var loginUser models.User
	dests := append([]any{&hash, &totpEnabled}, userScanDests(&loginUser)...)
	err := db.DB.QueryRow(
		"SELECT password, totp_enabled, "+userSelectCols+" FROM users u WHERE u.username=?", body.Username,
	).Scan(dests...)
	if err != nil || !CheckPassword(hash, body.Password) {
		recordAuthFailure("login", r, body.Username)
		log.Printf("audit: login_failed username=%q ip=%s", body.Username, clientIP(r))
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	if loginUser.Status == "inactive" || loginUser.Status == "deleted" {
		log.Printf("audit: login_blocked username=%q ip=%s reason=account_disabled", body.Username, clientIP(r))
		http.Error(w, `{"error":"account disabled"}`, http.StatusForbidden)
		return
	}
	resetAuthFailures("login", r, body.Username)

	// If 2FA is enabled, issue a short-lived pending token instead of a session
	if totpEnabled == 1 {
		tok, err := newSessionID()
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		exp := time.Now().Add(totpPendingTTLAuth).UTC().Format("2006-01-02 15:04:05")
		if _, err := db.DB.Exec(
			"INSERT INTO totp_pending(token,user_id,expires_at) VALUES(?,?,?)", tok, loginUser.ID, exp,
		); err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"totp_required": true,
			"totp_token":    tok,
		})
		return
	}

	sid, err := newSessionID()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	expiresAt := time.Now().Add(sessionDuration)
	if _, err := db.DB.Exec(
		"INSERT INTO sessions(id,user_id,expires_at) VALUES(?,?,?)",
		sid, loginUser.ID, expiresAt.UTC().Format("2006-01-02 15:04:05"),
	); err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sid,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	// Update last_login_at
	if _, err := db.DB.Exec("UPDATE users SET last_login_at=datetime('now') WHERE id=?", loginUser.ID); err != nil {
		log.Printf("LoginHandler: update last_login_at user_id=%d: %v", loginUser.ID, err)
	}

	log.Printf("audit: login_ok username=%q ip=%s", body.Username, clientIP(r))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loginUser)
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		if _, err := db.DB.Exec("DELETE FROM sessions WHERE id=?", cookie.Value); err != nil {
			log.Printf("LogoutHandler: delete session: %v", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookie,
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
		MaxAge:  -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func MeHandler(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// ChangePassword — authenticated user changes their own password.
// POST /api/auth/password  { "current_password": "...", "new_password": "..." }
func ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.CurrentPassword == "" || body.NewPassword == "" {
		http.Error(w, `{"error":"current_password and new_password required"}`, http.StatusBadRequest)
		return
	}
	if len(body.NewPassword) < 6 {
		http.Error(w, `{"error":"new password must be at least 6 characters"}`, http.StatusBadRequest)
		return
	}

	var hash string
	if err := db.DB.QueryRow(
		"SELECT password FROM users WHERE id=?", user.ID,
	).Scan(&hash); err != nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}
	if !CheckPassword(hash, body.CurrentPassword) {
		http.Error(w, `{"error":"current password is incorrect"}`, http.StatusUnauthorized)
		return
	}

	newHash, err := HashPassword(body.NewPassword)
	if err != nil {
		http.Error(w, `{"error":"hash failed"}`, http.StatusInternalServerError)
		return
	}
	if _, err := db.DB.Exec("UPDATE users SET password=? WHERE id=?", newHash, user.ID); err != nil {
		http.Error(w, `{"error":"update failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil || user.Role != "admin" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BlockExternal blocks external users from internal API routes (403).
func BlockExternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user != nil && user.Role == "external" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePortalAccess allows only external users and admins (for testing).
func RequirePortalAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if user.Role != "external" && user.Role != "admin" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetAccessibleProjectIDs returns the project IDs an external user can access.
// Returns nil for admin/member (meaning all projects).
func GetAccessibleProjectIDs(r *http.Request) []int64 {
	user := GetUser(r)
	if user == nil || user.Role != "external" {
		return nil // admin/member = all projects
	}
	rows, err := db.DB.Query("SELECT project_id FROM user_project_access WHERE user_id=?", user.ID)
	if err != nil {
		return []int64{}
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		ids = append(ids, id)
	}
	if ids == nil {
		ids = []int64{}
	}
	return ids
}

// HasProjectAccess checks if the current user has access to a specific project.
// Admin/member always has access. External users need user_project_access entry.
func HasProjectAccess(r *http.Request, projectID int64) bool {
	user := GetUser(r)
	if user == nil {
		return false
	}
	if user.Role != "external" {
		return true
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM user_project_access WHERE user_id=? AND project_id=?",
		user.ID, projectID).Scan(&count); err != nil {
		log.Printf("HasProjectAccess: scan error: %v", err)
		return false
	}
	return count > 0
}
