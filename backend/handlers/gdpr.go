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

// PAI-117: GDPR ops pack — retention sweep + per-subject export and
// erase endpoints. Keeps the surface tight on purpose:
//
//   - Retention is configured via env vars rather than a runtime UI; the
//     operator-facing knobs are documented in CONFIGURATION.md and the
//     defaults match the audit's "what would a careful operator pick"
//     baseline, not a regulator-mandated maximum.
//   - Erase is a soft anonymisation, not a hard delete. The DB has FK
//     references from time entries / comments / issues / audit rows back
//     to users(id); cascade-deleting those would falsify history. We
//     replace the PII columns with placeholders, drop sessions and API
//     keys, and rely on the existing status='deleted' guard to keep the
//     account out of the UI.

package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// retentionDays returns the configured retention window for a class of
// rows. Env-var driven so an operator can tune without a code change;
// each class gets its own knob because the right answer is very different
// for, say, a one-shot reset token (hours) and a session audit row (months).
func retentionDays(name string, def int) int {
	v := os.Getenv("PAIMOS_RETENTION_DAYS_" + name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// retentionPolicy is the public-facing description of what the cleaner
// will do — emitted by GET /api/gdpr/retention so the operator can sanity
// check their config without grepping logs.
type retentionPolicy struct {
	Sessions          int `json:"sessions_days"`
	ResetTokens       int `json:"reset_tokens_days"`
	AccessAudit       int `json:"access_audit_days"`
	SessionActivity   int `json:"session_activity_days"`
	IncidentClosed    int `json:"incident_closed_days"`
	TOTPPending       int `json:"totp_pending_minutes"`
}

func currentPolicy() retentionPolicy {
	return retentionPolicy{
		Sessions:        retentionDays("SESSIONS", 30),
		ResetTokens:     retentionDays("RESET_TOKENS", 7),
		AccessAudit:     retentionDays("ACCESS_AUDIT", 365),
		SessionActivity: retentionDays("SESSION_ACTIVITY", 90),
		IncidentClosed:  retentionDays("INCIDENT_CLOSED", 730),
		TOTPPending:     retentionDays("TOTP_PENDING_MIN", 60), // minutes, not days
	}
}

var retentionStartOnce sync.Once

// StartRetentionSweeper launches the background cleanup loop. Idempotent
// — wired from main.go at boot. The first sweep runs after a short delay
// so a cold start doesn't immediately spike DB activity.
func StartRetentionSweeper() {
	retentionStartOnce.Do(func() {
		go retentionLoop()
	})
}

func retentionLoop() {
	// Initial delay so a cold start is quiet.
	time.Sleep(30 * time.Second)
	for {
		runRetentionSweep()
		time.Sleep(24 * time.Hour)
	}
}

// runRetentionSweep deletes rows older than the configured retention
// window for each class. Bounded by a single transaction per class to
// keep lock contention low on busy instances.
func runRetentionSweep() {
	p := currentPolicy()
	sweepOlderThan("sessions",
		"DELETE FROM sessions WHERE expires_at < datetime('now')", p.Sessions)
	sweepOlderThan("password_reset_tokens",
		"DELETE FROM password_reset_tokens WHERE created_at < datetime('now', ?)",
		p.ResetTokens)
	sweepOlderThan("access_audit",
		"DELETE FROM access_audit WHERE occurred_at < datetime('now', ?)",
		p.AccessAudit)
	sweepOlderThan("session_activity",
		"DELETE FROM session_activity WHERE occurred_at < datetime('now', ?)",
		p.SessionActivity)
	sweepOlderThan("incident_log_closed",
		"DELETE FROM incident_log WHERE status='closed' AND updated_at < datetime('now', ?)",
		p.IncidentClosed)
	// TOTP pending is measured in minutes — already gated by expires_at;
	// this just trims rows that the verify path never got around to.
	if _, err := db.DB.Exec(
		"DELETE FROM totp_pending WHERE expires_at < datetime('now')",
	); err != nil {
		log.Printf("retention: totp_pending: %v", err)
	}
}

// sweepOlderThan runs a parameterised DELETE. The parameter form depends
// on whether the SQL has a ? placeholder; sessions are gated by their
// own expires_at column without a parameter, the rest take "-N days".
func sweepOlderThan(label, sqlText string, days int) {
	var args []any
	if hasParam := containsRune(sqlText, '?'); hasParam {
		args = []any{negDays(days)}
	}
	res, err := db.DB.Exec(sqlText, args...)
	if err != nil {
		log.Printf("retention: %s: %v", label, err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("retention: %s removed %d rows", label, n)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func negDays(n int) string {
	return "-" + strconv.Itoa(n) + " days"
}

// ── HTTP surface ─────────────────────────────────────────────────────

// GetRetentionPolicy — GET /api/gdpr/retention
//
// Admin-only. Returns the policy currently in effect so an operator can
// confirm their env-var changes were picked up.
func GetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, currentPolicy())
}

// ExportSubject — GET /api/users/{id}/gdpr-export
//
// Admin-only. Returns a JSON dump of every row that references the user.
// Designed to satisfy a Subject Access Request — the response is a
// downloadable file rather than something rendered in the UI, since it
// can run to several megabytes for active users.
func ExportSubject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	user := scanGDPRUser(id)
	if user == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	out := map[string]any{
		"export_format":   "paimos-gdpr-v1",
		"exported_at":     time.Now().UTC().Format(time.RFC3339),
		"subject_user_id": id,
		"user":            user,
		"sessions":         gdprRows(`SELECT id, expires_at FROM sessions WHERE user_id=?`, id),
		"api_keys":         gdprRows(`SELECT id, name, key_prefix, created_at, last_used_at FROM api_keys WHERE user_id=?`, id),
		"comments":         gdprRows(`SELECT id, issue_id, body, created_at FROM comments WHERE author_id=?`, id),
		"time_entries":     gdprRows(`SELECT id, issue_id, started_at, stopped_at, comment FROM time_entries WHERE user_id=?`, id),
		"attachments":      gdprRows(`SELECT id, issue_id, filename, content_type, size_bytes, created_at FROM attachments WHERE uploaded_by=?`, id),
		"documents":        gdprRows(`SELECT id, scope, customer_id, project_id, filename, mime_type, size_bytes, uploaded_at FROM documents WHERE uploaded_by=?`, id),
		"access_audit":     gdprRows(`SELECT id, project_id, user_id, actor_id, action, old_level, new_level, created_at FROM access_audit WHERE actor_id=? OR user_id=?`, id, id),
		"session_activity": gdprRows(`SELECT id, session_id, method, path, status_code, occurred_at FROM session_activity WHERE user_id=?`, id),
		"incidents":        gdprRows(`SELECT id, severity, title, detected_at, status FROM incident_log WHERE reported_by=?`, id),
		"recent_projects":  gdprRows(`SELECT user_id, project_id, visited_at FROM user_recent_projects WHERE user_id=?`, id),
		"project_members":  gdprRows(`SELECT user_id, project_id, access_level FROM project_members WHERE user_id=?`, id),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		`attachment; filename="user-`+strconv.FormatInt(id, 10)+`-gdpr.json"`)
	_ = json.NewEncoder(w).Encode(out)
}

// EraseSubject — POST /api/users/{id}/gdpr-erase
//
// Admin-only. Hard-anonymises the user record: PII columns are wiped,
// sessions and API keys are dropped, status flips to 'deleted'. Every
// FK reference (time entries, comments, audit rows) is preserved so
// historical project data stays consistent — this matches the GDPR
// "erasure with overriding legitimate interest" path.
func EraseSubject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	caller := auth.GetUser(r)
	if caller != nil && caller.ID == id {
		jsonError(w, "cannot erase your own account", http.StatusBadRequest)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "begin tx failed", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Replace PII with deterministic placeholders so audit rows stay
	// readable ("erased-user-42 deleted issue X") without leaking the
	// original identity.
	placeholder := "erased-user-" + strconv.FormatInt(id, 10)
	if _, err := tx.Exec(`
		UPDATE users SET
			username        = ?,
			password        = '',
			nickname        = '',
			first_name      = '',
			last_name       = '',
			email           = '',
			avatar_path     = '',
			totp_secret     = '',
			totp_enabled    = 0,
			status          = 'deleted'
		WHERE id = ?
	`, placeholder, id); err != nil {
		log.Printf("EraseSubject: update users: %v", err)
		jsonError(w, "erase failed", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		log.Printf("EraseSubject: delete sessions: %v", err)
	}
	if _, err := tx.Exec(`DELETE FROM api_keys WHERE user_id=?`, id); err != nil {
		log.Printf("EraseSubject: delete api_keys: %v", err)
	}
	if _, err := tx.Exec(`DELETE FROM password_reset_tokens WHERE user_id=?`, id); err != nil {
		log.Printf("EraseSubject: delete reset tokens: %v", err)
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}
	log.Printf("audit: gdpr_erase user_id=%d by=%d", id, callerID(caller))
	jsonOK(w, map[string]any{"erased": true, "subject_user_id": id})
}

// ── helpers ──────────────────────────────────────────────────────────

func scanGDPRUser(id int64) map[string]any {
	row := db.DB.QueryRow(`
		SELECT id, username, role, status, created_at, nickname, first_name,
		       last_name, email, locale, last_login_at
		FROM users WHERE id=?`, id)
	var (
		uid                                       int64
		username, role, status, createdAt, locale string
		nickname, firstName, lastName, email      string
		lastLogin                                 *string
	)
	if err := row.Scan(&uid, &username, &role, &status, &createdAt,
		&nickname, &firstName, &lastName, &email, &locale, &lastLogin); err != nil {
		return nil
	}
	return map[string]any{
		"id":            uid,
		"username":      username,
		"role":          role,
		"status":        status,
		"created_at":    createdAt,
		"nickname":      nickname,
		"first_name":    firstName,
		"last_name":     lastName,
		"email":         email,
		"locale":        locale,
		"last_login_at": lastLogin,
	}
}

// gdprRows runs a parameterised query and returns each row as a
// column-keyed map. Generic so we can dump every reference table without
// repeating struct-shape boilerplate per query.
func gdprRows(sqlText string, args ...any) []map[string]any {
	rows, err := db.DB.Query(sqlText, args...)
	if err != nil {
		log.Printf("gdpr-export: %v", err)
		return nil
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out
}

func callerID(u *models.User) int64 {
	if u == nil {
		return 0
	}
	return u.ID
}
