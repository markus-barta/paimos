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
	"strconv"
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

// PAI-322: 30-day sliding window with a 90-day absolute cap.
//
// - sessionDuration is the sliding window. Every authenticated request
//   that finds the remaining TTL below half of this value bumps
//   expires_at back to now+sessionDuration. So an active user never
//   gets logged out; an inactive user is logged out 30 days after
//   their last request.
//
// - sessionAbsoluteLifetime is a hard ceiling measured from the row's
//   created_at. Even a perpetually-active session is forced to
//   re-login once it crosses this. Catches stolen-cookie risk and
//   keeps a clean session-table when users churn devices.
//
// - sessionRenewThreshold is the "below half" decision: don't UPDATE
//   on every request — only when it earns us at least half a window.
//   For 30d sliding, that means we write at most every ~15 days per
//   session under normal use.
//
// The cookie's Expires attribute is set to sessionAbsoluteLifetime so
// the browser keeps the cookie alive for the full possible lifetime
// of the row; the DB row is the source of truth and renewal re-issues
// the cookie alongside the DB UPDATE.
const sessionDuration = 30 * 24 * time.Hour
const sessionAbsoluteLifetime = 90 * 24 * time.Hour
const sessionRenewThreshold = sessionDuration / 2

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

// devLoginKey is the context flag set by Middleware when the request's
// session was created via dev-login (PAI-267). MeHandler reads it to
// surface `via_dev_login: true` to the frontend banner.
type devLoginKeyType struct{}

var devLoginKey = devLoginKeyType{}

func withDevLoginFlag(ctx context.Context, v bool) context.Context {
	return context.WithValue(ctx, devLoginKey, v)
}

// IsViaDevLogin reports whether the current request authenticated via
// the dev-login route. Returns false for API-key auth, normal session
// auth, or unauthenticated requests.
func IsViaDevLogin(ctx context.Context) bool {
	v, _ := ctx.Value(devLoginKey).(bool)
	return v
}

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
			// PAI-320: API-key callers also get the epoch so any
			// frontend that talks via a key picks up role / membership
			// changes on the next request.
			w.Header().Set("X-Permissions-Epoch", strconv.FormatInt(GetPermissionsEpoch(user.ID), 10))
			ctx := context.WithValue(r.Context(), UserKey, user)
			ctx = WithAccessCache(ctx)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 2. Fall back to session cookie
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		rec, err := loadSession(cookie.Value)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// PAI-322: enforce the 90-day absolute cap. created_at can be
		// zero for sessions that pre-date the M89 migration in unusual
		// edge cases (the migration UPDATEs existing rows, but a zero
		// value is still defensive); skip the cap check in that case
		// rather than fail a legitimate session.
		if !rec.createdAt.IsZero() && time.Since(rec.createdAt) > sessionAbsoluteLifetime {
			if _, derr := db.DB.Exec("DELETE FROM sessions WHERE id=?", cookie.Value); derr != nil {
				log.Printf("Middleware: delete capped session %s: %v", cookie.Value, derr)
			}
			clearSessionCookie(w)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// PAI-322: sliding renewal — bump expires_at if remaining TTL
		// is below half. Throttling the write to "remaining < half"
		// means an active user triggers an UPDATE at most every
		// sessionRenewThreshold (≈15 days for the 30d window) per
		// session. The new expiry is also clamped to the absolute cap
		// so we never slide past it.
		remaining := time.Until(rec.expiresAt)
		if remaining < sessionRenewThreshold {
			newExpiry := time.Now().Add(sessionDuration)
			if !rec.createdAt.IsZero() {
				absoluteEnd := rec.createdAt.Add(sessionAbsoluteLifetime)
				if newExpiry.After(absoluteEnd) {
					newExpiry = absoluteEnd
				}
			}
			if _, uerr := db.DB.Exec(
				"UPDATE sessions SET expires_at = ? WHERE id = ?",
				newExpiry.UTC().Format("2006-01-02 15:04:05"), cookie.Value,
			); uerr != nil {
				// A renewal failure is recoverable — log it and let the
				// request proceed; the next request retries the slide.
				log.Printf("Middleware: slide renewal for %s: %v", cookie.Value, uerr)
			} else {
				rec.expiresAt = newExpiry
				http.SetCookie(w, &http.Cookie{
					Name:     sessionCookie,
					Value:    cookie.Value,
					Path:     "/",
					Expires:  time.Now().Add(sessionAbsoluteLifetime),
					HttpOnly: true,
					Secure:   cookieSecure,
					SameSite: http.SameSiteLaxMode,
				})
			}
		}

		// PAI-322: surface the session's current expiry to the SPA so
		// it can show a low-key "expires in N minutes" toast as the
		// absolute cap approaches. Sliding sessions almost never reach
		// this — by design, the toast is rare.
		w.Header().Set("X-Session-Expires-At", rec.expiresAt.UTC().Format(time.RFC3339))

		// PAI-320: surface the per-user permissions epoch so the SPA
		// re-fetches /auth/me when role / status / membership has
		// changed on another tab or via an admin action. Backend
		// permission checks already see fresh values per request; this
		// header exists to invalidate the SPA's local access cache.
		w.Header().Set("X-Permissions-Epoch", strconv.FormatInt(rec.permissionsEpoch, 10))

		// PAI-113: lazy-upgrade pre-M72 sessions that have no token yet.
		// The first authenticated request after deployment issues one and
		// sets the cookie. Avoids forcing every existing session to log in
		// again at deploy time; new sessions get a token at login.
		if rec.csrfTok == "" {
			if t, err := IssueCSRFForSession(w, cookie.Value); err == nil {
				rec.csrfTok = t
			}
		}
		ctx := context.WithValue(r.Context(), UserKey, rec.user)
		ctx = WithAccessCache(ctx)
		ctx = withSessionAuth(ctx, rec.csrfTok)
		ctx = withDevLoginFlag(ctx, rec.viaDevLogin)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clearSessionCookie writes a Set-Cookie that immediately expires the
// session cookie on the client. Used when the server kills a session
// (absolute cap, account disabled, etc.) so the browser doesn't keep
// presenting a value the server has already deleted.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookie,
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
		MaxAge:  -1,
	})
	ClearCSRFCookie(w)
}

// userScanDests returns the scan destination pointers for userSelectCols.
// Useful when the query has extra prefix/suffix columns around the user cols.
//
// IMPORTANT: this list must stay in lock-step with `userSelectCols` AND with the
// twin `handlers/userSelectCols` / `handlers/scanUser`. Adding a new user column
// requires updating BOTH packages — see Test_APIKeyAuth for the regression that
// caused this to be a recurring footgun.
func userScanDests(u *models.User) []any {
	return []any{
		&u.ID, &u.Username, &u.Role, &u.Status, &u.CreatedAt,
		&u.Nickname, &u.FirstName, &u.LastName, &u.Email, &u.AvatarPath,
		&u.MarkdownDefault, &u.MonospaceFields, &u.RecentProjectsLimit,
		&u.InternalRateHourly, &u.ShowAltUnitTable, &u.ShowAltUnitDetail, &u.Locale,
		&u.RecentTimersLimit, &u.Timezone, &u.PreviewHoverDelay,
		&u.IssueAutoRefreshEnabled, &u.IssueAutoRefreshIntervalSeconds, &u.LastLoginAt,
		&u.AccrualsStatsEnabled, &u.AccrualsExtraStatuses,
	}
}

// userSelectCols is the full qualified column list for the users table.
const userSelectCols = `u.id, u.username, u.role, u.status, u.created_at, u.nickname, u.first_name, u.last_name, u.email, u.avatar_path, u.markdown_default, u.monospace_fields, u.recent_projects_limit, u.internal_rate_hourly, u.show_alt_unit_table, u.show_alt_unit_detail, u.locale, u.recent_timers_limit, u.timezone, u.preview_hover_delay, u.issue_auto_refresh_enabled, u.issue_auto_refresh_interval_seconds, u.last_login_at, u.accruals_stats_enabled, u.accruals_extra_statuses`

// sessionRecord is the full row needed to make slide / cap / surface
// decisions in Middleware. Times are parsed from the SQLite TEXT
// columns; a zero time means the column was empty (pre-migration row
// or unparseable string) — callers must check .IsZero() before relying
// on the value.
type sessionRecord struct {
	user             *models.User
	csrfTok          string
	viaDevLogin      bool
	expiresAt        time.Time
	createdAt        time.Time
	permissionsEpoch int64
}

// loadSession resolves a session id to its full record. Enforces the
// expires_at > now sliding-window check in SQL (so a long-expired
// session is invisible to the rest of the code) and disables the
// session inline if the user has been deactivated.
func loadSession(sessionID string) (*sessionRecord, error) {
	rec := &sessionRecord{user: &models.User{}}
	var csrfTok string
	var viaDevLoginInt int
	var expiresStr, createdStr string
	var epoch int64
	dests := append(
		[]any{&csrfTok, &viaDevLoginInt, &expiresStr, &createdStr, &epoch},
		userScanDests(rec.user)...,
	)
	row := db.DB.QueryRow(`
		SELECT s.csrf_token, s.via_dev_login, s.expires_at, s.created_at,
		       u.permissions_epoch, `+userSelectCols+`
		FROM sessions s JOIN users u ON s.user_id = u.id
		WHERE s.id = ? AND s.expires_at > datetime('now')
	`, sessionID)
	if err := row.Scan(dests...); err != nil {
		return nil, err
	}
	rec.permissionsEpoch = epoch
	if rec.user.Status == "inactive" || rec.user.Status == "deleted" {
		if _, err := db.DB.Exec("DELETE FROM sessions WHERE id=?", sessionID); err != nil {
			log.Printf("loadSession: delete session %s: %v", sessionID, err)
		}
		return nil, fmt.Errorf("account disabled")
	}
	rec.csrfTok = csrfTok
	rec.viaDevLogin = viaDevLoginInt != 0
	// SQLite stores timestamps as "YYYY-MM-DD HH:MM:SS" (UTC). Parse
	// errors leave the times zero, which the cap/slide logic tolerates.
	if t, err := time.Parse("2006-01-02 15:04:05", expiresStr); err == nil {
		rec.expiresAt = t.UTC()
	}
	if createdStr != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", createdStr); err == nil {
			rec.createdAt = t.UTC()
		}
	}
	return rec, nil
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
	now := time.Now()
	expiresAt := now.Add(sessionDuration)
	// PAI-322: created_at is the anchor for the absolute cap. We write
	// it explicitly here (rather than relying on a SQL default) so the
	// row is unambiguously stamped with the login moment in app-time,
	// matching whatever the server clock says when expires_at is read.
	if _, err := db.DB.Exec(
		"INSERT INTO sessions(id,user_id,expires_at,created_at) VALUES(?,?,?,?)",
		sid, loginUser.ID,
		expiresAt.UTC().Format("2006-01-02 15:04:05"),
		now.UTC().Format("2006-01-02 15:04:05"),
	); err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// PAI-322: cookie outlives the sliding window — expires at the
	// absolute cap so the browser keeps the cookie even when the
	// backend has slid expires_at past the current value. The DB row
	// is the source of truth; the cookie just has to survive long
	// enough for the next request to renew it.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sid,
		Path:     "/",
		Expires:  now.Add(sessionAbsoluteLifetime),
		HttpOnly: true,
		Secure:   cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	// PAI-113: bind a fresh CSRF token to the new session and expose it
	// to the SPA via a non-HttpOnly cookie. Do not fail the login if this
	// fails — the lazy-upgrade path in Middleware will retry.
	if _, err := IssueCSRFForSession(w, sid); err != nil {
		log.Printf("LoginHandler: issue csrf token: %v", err)
	}

	// Update last_login_at
	if _, err := db.DB.Exec("UPDATE users SET last_login_at=datetime('now') WHERE id=?", loginUser.ID); err != nil {
		log.Printf("LoginHandler: update last_login_at user_id=%d: %v", loginUser.ID, err)
	}

	log.Printf("audit: login_ok username=%q ip=%s", body.Username, clientIP(r))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeResponse{
		User:   &loginUser,
		Access: BuildAccessResponse(&loginUser),
	})
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
	ClearCSRFCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// MeResponse wraps the authenticated user and their per-project access
// summary. Returned by /auth/me, /auth/login (on success), and
// /auth/totp/verify so the client can hydrate its access cache in one
// round-trip instead of issuing a separate request per project.
//
// PAI-267: ViaDevLogin is true iff the current request authenticated
// via the dev-login route. The frontend renders a non-dismissable
// red banner whenever this is set so the operator can never confuse
// a dev session for a real one. Always false on production builds
// (the dev_login_prod stub never sets the context flag).
type MeResponse struct {
	User        *models.User   `json:"user"`
	Access      AccessResponse `json:"access"`
	ViaDevLogin bool           `json:"via_dev_login,omitempty"`
}

func MeHandler(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeResponse{
		User:        user,
		Access:      BuildAccessResponse(user),
		ViaDevLogin: IsViaDevLogin(r.Context()),
	})
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

	// PAI-322: kill every other session for this user so a stolen or
	// abandoned cookie on another device stops working immediately.
	// The current session is preserved by id-exclusion so the user
	// stays logged in here. API-key-authenticated callers don't carry
	// a session cookie — for them, all sessions are nuked, which is
	// the conservative choice (an automated client doesn't have a
	// "this one" to keep).
	currentSID := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		currentSID = c.Value
	}
	if currentSID != "" {
		if _, err := db.DB.Exec(
			"DELETE FROM sessions WHERE user_id=? AND id != ?",
			user.ID, currentSID,
		); err != nil {
			log.Printf("ChangePassword: prune sessions user_id=%d: %v", user.ID, err)
		}
	} else {
		if _, err := db.DB.Exec(
			"DELETE FROM sessions WHERE user_id=?", user.ID,
		); err != nil {
			log.Printf("ChangePassword: prune all sessions user_id=%d: %v", user.ID, err)
		}
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

// GetAccessibleProjectIDs returns the project IDs the user can at least
// view. Returns nil for admins (meaning all projects). Backed by the
// project_members table (see access.go).
//
// Deprecated: prefer AccessibleProjectIDs — kept for backwards compatibility
// with portal code.
func GetAccessibleProjectIDs(r *http.Request) []int64 {
	return AccessibleProjectIDs(r)
}

// HasProjectAccess reports whether the user can at least view projectID.
// Backed by project_members via CanViewProject.
//
// Deprecated: prefer CanViewProject / CanEditProject.
func HasProjectAccess(r *http.Request, projectID int64) bool {
	return CanViewProject(r, projectID)
}
