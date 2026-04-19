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

package handlers_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
)

// bcryptHash wraps auth.HashPassword so tests can seed users with
// real password hashes compatible with auth.CheckPassword.
func bcryptHash(t *testing.T, password string) string {
	t.Helper()
	h, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return h
}

// sha256Hex mirrors the token-hashing scheme used by the handler so
// tests can mint synthetic tokens and insert them directly.
func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// rfcNow returns `time.Now().UTC()` offset by `offset` formatted as
// RFC3339 — which is the format the handler expects in the DB columns.
func rfcNow(offset time.Duration) string {
	return time.Now().UTC().Add(offset).Format(time.RFC3339)
}

// seedUserWithEmail inserts a real user with an email so the forgot-
// password lookup can find it. Returns the user id.
func seedUserWithEmail(t *testing.T, username, email, password string) int64 {
	t.Helper()
	// Use the live bcrypt helper so the stored hash is compatible with
	// auth.CheckPassword when we verify login at the end of the flow.
	hash := bcryptHash(t, password)
	res, err := db.DB.Exec(
		"INSERT INTO users(username, password, role, status, email) VALUES(?,?,?,?,?)",
		username, hash, "member", "active", email,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// latestResetTokenHash returns the token_hash of the most recently
// inserted password_reset_tokens row. The real token is only in the
// email body (dev mode: in the log), so tests grab it via the hash +
// raw value returned by the helper below.
func latestResetToken(t *testing.T, userID int64) (tokenID int64, tokenHash string) {
	t.Helper()
	err := db.DB.QueryRow(
		`SELECT id, token_hash FROM password_reset_tokens
		 WHERE user_id = ? ORDER BY id DESC LIMIT 1`,
		userID,
	).Scan(&tokenID, &tokenHash)
	if err != nil {
		t.Fatalf("latestResetToken: %v", err)
	}
	return
}

// readJSON drains the response body and unmarshals it into out.
func readJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("unmarshal %s: %v", string(body), err)
	}
}

// Test_ForgotPassword_EnumerationResistance: POST /auth/forgot returns
// the same 202 + message for a known email and an unknown email. No
// status code, body, or timing difference should leak existence.
func Test_ForgotPassword_EnumerationResistance(t *testing.T) {
	ts := newTestServer(t)
	seedUserWithEmail(t, "alice", "alice@example.com", "oldpassword123")

	// Known email
	resp := ts.post(t, "/api/auth/forgot", "", map[string]string{"email": "alice@example.com"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("known email: status %d, want 202", resp.StatusCode)
	}
	var knownBody struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	readJSON(t, resp, &knownBody)
	if knownBody.Status != "accepted" {
		t.Fatalf("known email: status %q, want accepted", knownBody.Status)
	}

	// Unknown email — must get the EXACT same response shape
	resp2 := ts.post(t, "/api/auth/forgot", "", map[string]string{"email": "nobody@example.com"})
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("unknown email: status %d, want 202", resp2.StatusCode)
	}
	var unknownBody struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	readJSON(t, resp2, &unknownBody)
	if unknownBody != knownBody {
		t.Fatalf("enumeration leak: known=%+v unknown=%+v", knownBody, unknownBody)
	}
}

// Test_ForgotPassword_HappyPath: forgot → token persisted → validate
// returns valid → reset succeeds → can log in with new password → old
// sessions are gone.
func Test_ForgotPassword_HappyPath(t *testing.T) {
	ts := newTestServer(t)
	userID := seedUserWithEmail(t, "bob", "bob@example.com", "oldpassword123")

	// Log bob in so we can assert his session gets nuked on reset.
	oldCookie := ts.login(t, "bob", "oldpassword123")
	if oldCookie == "" {
		t.Fatal("no session cookie from initial login")
	}

	// Fire forgot — in dev mode this logs the token to stdout, but we
	// skip the email entirely and read the token row directly from the
	// DB. Same effect as scraping the email body.
	resp := ts.post(t, "/api/auth/forgot", "", map[string]string{"email": "bob@example.com"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("forgot: %d", resp.StatusCode)
	}

	// We can't recover the raw token from the hash, so we mint a NEW
	// test-only token by calling the flow through its public seams.
	// Instead, exploit the fact that the handler writes the link via
	// log.Printf in dev mode — but tests can't easily capture that.
	// Cleanest approach: insert a known-value token directly and verify
	// the validate + reset endpoints, keeping the forgot handler test
	// purely focused on enumeration resistance + persistence.
	_, _ = latestResetToken(t, userID) // assert the insert happened

	// Independent happy path: insert a fresh token we know the plaintext
	// for, then exercise validate + reset through the public routes.
	rawToken := "test-token-abc123-" + strings.Repeat("x", 30) // > 32 bytes
	_, err := db.DB.Exec(
		`INSERT INTO password_reset_tokens(user_id, token_hash, created_at, expires_at, ip_address)
		 VALUES(?, ?, ?, ?, '')`,
		userID, sha256Hex(rawToken), rfcNow(0), rfcNow(60*time.Minute),
	)
	if err != nil {
		t.Fatalf("insert synthetic token: %v", err)
	}

	// Validate endpoint — should return valid:true
	validateResp := ts.get(t, "/api/auth/reset/validate?token="+rawToken, "")
	if validateResp.StatusCode != http.StatusOK {
		t.Fatalf("validate status %d", validateResp.StatusCode)
	}
	var validateBody struct {
		Valid  bool   `json:"valid"`
		Reason string `json:"reason"`
	}
	readJSON(t, validateResp, &validateBody)
	if !validateBody.Valid {
		t.Fatalf("validate said invalid: %q", validateBody.Reason)
	}

	// Reset endpoint
	resetResp := ts.post(t, "/api/auth/reset", "", map[string]string{
		"token":        rawToken,
		"new_password": "newpassword456",
	})
	if resetResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resetResp.Body)
		t.Fatalf("reset status %d: %s", resetResp.StatusCode, body)
	}

	// Old session should be dead.
	meResp := ts.get(t, "/api/auth/me", oldCookie)
	if meResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old session still alive after reset: status %d", meResp.StatusCode)
	}

	// New password should work.
	newCookie := ts.login(t, "bob", "newpassword456")
	if newCookie == "" {
		t.Fatal("login with new password failed")
	}
}

// Test_ResetPassword_SingleUse: once a token is consumed, hitting /reset
// again with the same token must fail.
func Test_ResetPassword_SingleUse(t *testing.T) {
	ts := newTestServer(t)
	userID := seedUserWithEmail(t, "carol", "carol@example.com", "oldpassword123")

	rawToken := "single-use-token-" + strings.Repeat("y", 30)
	_, err := db.DB.Exec(
		`INSERT INTO password_reset_tokens(user_id, token_hash, created_at, expires_at, ip_address)
		 VALUES(?, ?, ?, ?, '')`,
		userID, sha256Hex(rawToken), rfcNow(0), rfcNow(60*time.Minute),
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}

	// First reset — ok
	first := ts.post(t, "/api/auth/reset", "", map[string]string{
		"token":        rawToken,
		"new_password": "firstnewpassword",
	})
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first reset: status %d", first.StatusCode)
	}

	// Second reset with the same token — must fail
	second := ts.post(t, "/api/auth/reset", "", map[string]string{
		"token":        rawToken,
		"new_password": "secondnewpassword",
	})
	if second.StatusCode == http.StatusOK {
		t.Fatal("second reset with same token succeeded — single-use broken")
	}
}

// Test_ResetPassword_Expired: a token whose expires_at is in the past
// must be rejected by both /validate and /reset.
func Test_ResetPassword_Expired(t *testing.T) {
	ts := newTestServer(t)
	userID := seedUserWithEmail(t, "dave", "dave@example.com", "oldpassword123")

	rawToken := "expired-token-" + strings.Repeat("z", 30)
	_, err := db.DB.Exec(
		`INSERT INTO password_reset_tokens(user_id, token_hash, created_at, expires_at, ip_address)
		 VALUES(?, ?, ?, ?, '')`,
		userID, sha256Hex(rawToken), rfcNow(-2*time.Hour), rfcNow(-time.Hour),
	)
	if err != nil {
		t.Fatalf("insert expired token: %v", err)
	}

	// /validate should report invalid with reason "expired"
	validateResp := ts.get(t, "/api/auth/reset/validate?token="+rawToken, "")
	var vb struct {
		Valid  bool   `json:"valid"`
		Reason string `json:"reason"`
	}
	readJSON(t, validateResp, &vb)
	if vb.Valid || vb.Reason != "expired" {
		t.Fatalf("expired token validated: valid=%v reason=%q", vb.Valid, vb.Reason)
	}

	// /reset should 400
	resetResp := ts.post(t, "/api/auth/reset", "", map[string]string{
		"token":        rawToken,
		"new_password": "newpassword789",
	})
	if resetResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expired token reset: status %d, want 400", resetResp.StatusCode)
	}
}

// Test_ResetPassword_MinLength: the backend refuses a <8-char new
// password even when the token is valid.
func Test_ResetPassword_MinLength(t *testing.T) {
	ts := newTestServer(t)
	userID := seedUserWithEmail(t, "eve", "eve@example.com", "oldpassword123")

	rawToken := "short-pw-" + strings.Repeat("q", 30)
	_, err := db.DB.Exec(
		`INSERT INTO password_reset_tokens(user_id, token_hash, created_at, expires_at, ip_address)
		 VALUES(?, ?, ?, ?, '')`,
		userID, sha256Hex(rawToken), rfcNow(0), rfcNow(60*time.Minute),
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}

	resp := ts.post(t, "/api/auth/reset", "", map[string]string{
		"token":        rawToken,
		"new_password": "short", // 5 chars
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password: status %d, want 400", resp.StatusCode)
	}
}
