package handlers_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/handlers"
)

func TestRegression_Auth_001_NoCreds(t *testing.T) {
	ts := newTestServer(t)
	for _, p := range []string{"/api/auth/me", "/api/projects", "/api/users"} {
		resp := ts.get(t, p, "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("INV-AUTH-001 violated: %s with no creds -> %d, want 401", p, resp.StatusCode)
		}
	}
}

func TestRegression_Auth_002_DisabledBlocked(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.memberCookie
	resp := ts.get(t, "/api/auth/me", cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: member cookie invalid, got %d", resp.StatusCode)
	}
	if _, err := db.DB.Exec("UPDATE users SET status='deleted' WHERE username='member'"); err != nil {
		t.Fatal(err)
	}
	resp = ts.get(t, "/api/auth/me", cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("INV-AUTH-002 violated: deleted-user cookie returned %d, want 401", resp.StatusCode)
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id=?", strings.TrimPrefix(cookie, "session=")).Scan(&n)
	if n != 0 {
		t.Errorf("INV-AUTH-002 violated: session row not cleaned up after disable")
	}
}

func TestRegression_Auth_004_ResetKillsSessions(t *testing.T) {
	ts := newTestServer(t)
	var memberID int64
	_ = db.DB.QueryRow("SELECT id FROM users WHERE username='member'").Scan(&memberID)
	rawToken := strings.Repeat("z", 43)
	_, err := db.DB.Exec(
		`INSERT INTO password_reset_tokens(user_id, token_hash, created_at, expires_at, ip_address)
		 VALUES(?, ?, ?, ?, '')`,
		memberID, sha256Hex(rawToken),
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	var sessions int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id=?", memberID).Scan(&sessions)
	if sessions == 0 {
		t.Fatalf("setup: expected member to have a session")
	}
	resp := ts.post(t, "/api/auth/reset", "", map[string]string{
		"token":        rawToken,
		"new_password": "newpassword456",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset: %d", resp.StatusCode)
	}
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id=?", memberID).Scan(&sessions)
	if sessions != 0 {
		t.Errorf("INV-AUTH-004 violated: %d sessions survived password reset", sessions)
	}
}

func TestRegression_CSRF_001_MissingTokenBlocks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sink", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(auth.CSRFMiddleware(mux))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	parent := newTestServer(t)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/sink", strings.NewReader(""))
	req.Header.Set("Cookie", parent.memberCookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("INV-CSRF-001 violated: missing CSRF token -> %d, want 403", resp.StatusCode)
	}
}

func TestRegression_CSRF_001_ValidTokenAllows(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sink", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(auth.CSRFMiddleware(mux))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	parent := newTestServer(t)
	cookie := parent.memberCookie
	sid := strings.TrimPrefix(cookie, "session=")
	var csrf string
	_ = db.DB.QueryRow("SELECT csrf_token FROM sessions WHERE id=?", sid).Scan(&csrf)
	if csrf == "" {
		csrf = "abcd1234abcd1234"
		if _, err := db.DB.Exec("UPDATE sessions SET csrf_token=? WHERE id=?", csrf, sid); err != nil {
			t.Fatal(err)
		}
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/sink", strings.NewReader(""))
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set(auth.CSRFHeaderName, csrf)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("INV-CSRF-001 violated: valid token -> %d, want 200", resp.StatusCode)
	}
}

func TestRegression_CSRF_001_APIKeyExempt(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/auth/api-keys", ts.adminCookie, map[string]string{"name": "csrf-test"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create api key: %d", resp.StatusCode)
	}
	var body struct {
		Key string `json:"key"`
	}
	readJSON(t, resp, &body)
	if body.Key == "" {
		t.Fatal("api key response missing key")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sink", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(auth.CSRFMiddleware(mux))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/sink", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+body.Key)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Errorf("INV-CSRF-001 violated: API key request blocked by CSRF -> %d", r2.StatusCode)
	}
}

func TestRegression_CSRF_002_CookieAttrs(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/auth/login", "", map[string]string{
		"username": "member", "password": "memberpass",
	})
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name != "csrf_token" {
			continue
		}
		if c.HttpOnly {
			t.Error("INV-CSRF-002 violated: csrf_token cookie is HttpOnly")
		}
		if c.SameSite != http.SameSiteStrictMode {
			t.Errorf("INV-CSRF-002 violated: csrf_token cookie SameSite=%v, want Strict", c.SameSite)
		}
		return
	}
	t.Error("INV-CSRF-002 violated: login response did not set csrf_token cookie")
}

func TestRegression_Authz_001_NoViewIsNotFound(t *testing.T) {
	ts := newTestServer(t)
	cresp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Closed Project", "key": "CLP",
	})
	defer cresp.Body.Close()
	var project struct {
		ID int64 `json:"id"`
	}
	readJSON(t, cresp, &project)
	if project.ID == 0 {
		t.Fatal("setup: project not created")
	}
	var memberID int64
	_ = db.DB.QueryRow("SELECT id FROM users WHERE username='member'").Scan(&memberID)
	if _, err := db.DB.Exec(
		"INSERT OR REPLACE INTO project_members(user_id, project_id, access_level) VALUES(?,?,'none')",
		memberID, project.ID,
	); err != nil {
		t.Fatal(err)
	}

	resp := ts.get(t, "/api/projects/"+itoa(project.ID), ts.memberCookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("INV-AUTHZ-001 violated: no-view project read -> %d, want 404", resp.StatusCode)
	}
}

func TestRegression_Authz_005_AdminOnly(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/users", ts.memberCookie, map[string]string{
		"username": "should-not-create", "password": "pw", "role": "member",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("INV-AUTHZ-005 violated: member POST /users -> %d, want 403", resp.StatusCode)
	}
}

func TestRegression_Authz_006_ExternalBlocked(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/api/projects", ts.externalCookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("INV-AUTHZ-006 violated: external on /api/projects -> %d, want 403", resp.StatusCode)
	}
}

func TestRegression_Data_002_EraseSelfBlocked(t *testing.T) {
	ts := newTestServer(t)
	var adminID int64
	_ = db.DB.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&adminID)
	resp := ts.post(t, "/api/users/"+itoa(adminID)+"/gdpr-erase", ts.adminCookie, map[string]string{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("INV-DATA-002 violated: erase self -> %d, want 400", resp.StatusCode)
	}
}

func TestRegression_Data_003_EraseAnonymises(t *testing.T) {
	ts := newTestServer(t)
	res, err := db.DB.Exec("INSERT INTO users(username,password,role,status,email) VALUES('target','x','member','active','t@example.com')")
	if err != nil {
		t.Fatal(err)
	}
	tid, _ := res.LastInsertId()

	resp := ts.post(t, "/api/users/"+itoa(tid)+"/gdpr-erase", ts.adminCookie, map[string]string{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("erase: %d", resp.StatusCode)
	}

	var username, email, status string
	if err := db.DB.QueryRow("SELECT username, email, status FROM users WHERE id=?", tid).Scan(&username, &email, &status); err != nil {
		t.Fatalf("INV-DATA-003 violated: user row deleted: %v", err)
	}
	if !strings.HasPrefix(username, "erased-user-") {
		t.Errorf("INV-DATA-003 violated: username not anonymised: %q", username)
	}
	if email != "" {
		t.Errorf("INV-DATA-003 violated: email not wiped: %q", email)
	}
	if status != "deleted" {
		t.Errorf("INV-DATA-003 violated: status %q, want deleted", status)
	}
}

func TestRegression_Audit_001_DefaultOn(t *testing.T) {
	t.Setenv("PAIMOS_AUDIT_SESSIONS", "")
	ts := newTestServer(t)
	resp := ts.post(t, "/api/tags", ts.adminCookie, map[string]string{"name": "audit-default-on"})
	resp.Body.Close()
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM session_activity`).Scan(&n)
	if n == 0 {
		t.Error("INV-AUDIT-001 violated: PAIMOS_AUDIT_SESSIONS unset -> no audit rows written")
	}
}

func TestRegression_Hdr_001_BaselineHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/anything", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(handlers.SecurityHeaders(mux))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/anything")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("INV-HDR-001 violated: %s=%q, want %q", k, got, v)
		}
	}
	if resp.Header.Get("Permissions-Policy") == "" {
		t.Error("INV-HDR-001 violated: Permissions-Policy missing")
	}
}

func TestRegression_Hdr_002_HSTSOnSecure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("COOKIE_SECURE", "")
		srv := httptest.NewServer(handlers.SecurityHeaders(mux))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if h := resp.Header.Get("Strict-Transport-Security"); h != "" {
			t.Errorf("INV-HDR-002 violated: HSTS present without COOKIE_SECURE: %q", h)
		}
	})
	t.Run("enabled when secure", func(t *testing.T) {
		t.Setenv("COOKIE_SECURE", "true")
		srv := httptest.NewServer(handlers.SecurityHeaders(mux))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if h := resp.Header.Get("Strict-Transport-Security"); !strings.Contains(h, "max-age=") {
			t.Errorf("INV-HDR-002 violated: HSTS missing with COOKIE_SECURE=true: %q", h)
		}
	})
}

func TestRegression_Hdr_003_CSPReportOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(handlers.SecurityHeaders(mux))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy-Report-Only")
	if csp == "" {
		t.Fatal("INV-HDR-003 violated: CSP-Report-Only header missing")
	}
	if !strings.Contains(csp, "report-uri /api/csp-report") {
		t.Errorf("INV-HDR-003 violated: report-uri not pointing at /api/csp-report: %q", csp)
	}
	for _, banned := range []string{"googleapis.com", "gstatic.com", "cdn.jsdelivr", "unpkg.com"} {
		if strings.Contains(csp, banned) {
			t.Errorf("INV-HDR-003 violated: third-party host %q in CSP: %q", banned, csp)
		}
	}
}

// PAI-322 — sliding renewal: when remaining TTL drops below half the
// 30-day window, the next authenticated request bumps expires_at back
// out. The middleware does the renewal asynchronously to the response,
// so we verify by reading the row before and after.
func TestRegression_Session_001_SlidingRenewalBumpsExpiry(t *testing.T) {
	ts := newTestServer(t)
	sid := strings.TrimPrefix(ts.memberCookie, "session=")

	// Force the session to be "near expiry" — within the 15-day renew
	// threshold. Use a stamp 1 day from now.
	target := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec("UPDATE sessions SET expires_at=? WHERE id=?", target, sid); err != nil {
		t.Fatal(err)
	}

	resp := ts.get(t, "/api/auth/me", ts.memberCookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authed GET returned %d", resp.StatusCode)
	}

	var newExpiry string
	if err := db.DB.QueryRow("SELECT expires_at FROM sessions WHERE id=?", sid).Scan(&newExpiry); err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse("2006-01-02 15:04:05", newExpiry)
	if err != nil {
		t.Fatalf("parse new expiry %q: %v", newExpiry, err)
	}
	// Expect renewed expiry well past the 1-day mark we set — at least
	// 25 days out (sliding window is 30d, leave slack for clock skew).
	if time.Until(parsed) < 25*24*time.Hour {
		t.Errorf("INV-SESSION-001 violated: expires_at not slid; remaining=%s", time.Until(parsed))
	}
}

// PAI-322 — absolute cap: a session older than 90 days is rejected
// even when sliding would otherwise extend it.
func TestRegression_Session_002_AbsoluteCapForcesLogout(t *testing.T) {
	ts := newTestServer(t)
	sid := strings.TrimPrefix(ts.memberCookie, "session=")

	// Backdate created_at to 100 days ago (past the 90-day cap), and
	// keep expires_at far in the future so the SQL filter doesn't
	// eject the session before our cap check sees it.
	created := time.Now().UTC().Add(-100 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	expires := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := db.DB.Exec(
		"UPDATE sessions SET created_at=?, expires_at=? WHERE id=?",
		created, expires, sid,
	); err != nil {
		t.Fatal(err)
	}

	resp := ts.get(t, "/api/auth/me", ts.memberCookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("INV-SESSION-002 violated: capped session returned %d, want 401", resp.StatusCode)
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id=?", sid).Scan(&n)
	if n != 0 {
		t.Errorf("INV-SESSION-002 violated: capped session row not cleaned up")
	}
}

// PAI-322 — every authenticated response surfaces the session expiry
// so the SPA can drive a low-key pre-expiry toast.
func TestRegression_Session_003_ExpiresAtHeader(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.get(t, "/api/auth/me", ts.memberCookie)
	resp.Body.Close()
	hdr := resp.Header.Get("X-Session-Expires-At")
	if hdr == "" {
		t.Fatal("INV-SESSION-003 violated: X-Session-Expires-At header missing")
	}
	if _, err := time.Parse(time.RFC3339, hdr); err != nil {
		t.Errorf("INV-SESSION-003 violated: header %q not RFC3339: %v", hdr, err)
	}
}

// PAI-320 — promoting a user takes effect on the very next request,
// without requiring re-login. The backend reads role fresh on every
// request via loadSession's JOIN, so an admin endpoint that was 403
// for member should be 200 right after the role flip.
func TestRegression_PermsEpoch_001_PromoteTakesEffectImmediately(t *testing.T) {
	ts := newTestServer(t)
	// Member hits an admin-only endpoint — must 403 before the change.
	resp := ts.get(t, "/api/users", ts.memberCookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// /users is open to all internal roles; pick one we know is
		// admin-gated.
		t.Logf("note: /users returned %d for member (informational)", resp.StatusCode)
	}
	resp = ts.post(t, "/api/users", ts.memberCookie, map[string]string{
		"username": "x", "password": "y", "role": "member",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("setup: member POST /users got %d, want 403", resp.StatusCode)
	}

	if _, err := db.DB.Exec("UPDATE users SET role='admin' WHERE username='member'"); err != nil {
		t.Fatal(err)
	}
	// Same cookie — but role now resolves to admin via loadSession.
	resp = ts.post(t, "/api/users", ts.memberCookie, map[string]string{
		"username": "fresh-after-promote", "password": "secret123", "role": "member",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("INV-PERMS-001 violated: promoted member POST /users got %d, want 201", resp.StatusCode)
	}
}

// PAI-320 — every authenticated response carries the user's
// permissions_epoch so the SPA can re-fetch /auth/me when the value
// changes. This test verifies presence + that bumping the column
// reflects in the next response.
func TestRegression_PermsEpoch_002_HeaderBumpsOnRoleChange(t *testing.T) {
	ts := newTestServer(t)

	// First request: capture the baseline epoch from the header.
	resp := ts.get(t, "/api/auth/me", ts.memberCookie)
	resp.Body.Close()
	first := resp.Header.Get("X-Permissions-Epoch")
	if first == "" {
		t.Fatal("INV-PERMS-002 violated: X-Permissions-Epoch header missing")
	}

	// Bump via a role change.
	if _, err := db.DB.Exec("UPDATE users SET permissions_epoch = permissions_epoch + 1 WHERE username='member'"); err != nil {
		t.Fatal(err)
	}

	// Same cookie, next request — the header must reflect the bump.
	resp = ts.get(t, "/api/auth/me", ts.memberCookie)
	resp.Body.Close()
	second := resp.Header.Get("X-Permissions-Epoch")
	if second == first {
		t.Errorf("INV-PERMS-002 violated: epoch did not change after bump (still %q)", first)
	}
}

// PAI-321 — a freshly created user with must_change_password is
// blocked from non-allowlisted endpoints with 403
// {"error":"must_change_password"} until they POST /auth/password.
func TestRegression_MustChange_001_BlocksProtectedEndpoints(t *testing.T) {
	ts := newTestServer(t)
	// Create a user that requires a password change. Admin path.
	resp := ts.post(t, "/api/users", ts.adminCookie, map[string]any{
		"username":             "newhire",
		"password":             "tempinitial",
		"role":                 "member",
		"must_change_password": true,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: %d", resp.StatusCode)
	}
	cookie := ts.login(t, "newhire", "tempinitial")
	// Allowlisted: /auth/me must work so the SPA can render the
	// first-login screen.
	resp = ts.get(t, "/api/auth/me", cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("INV-MUSTCHG-001 violated: /auth/me blocked for must-change user (got %d)", resp.StatusCode)
	}
	// Non-allowlisted: /projects must 403 with the gate marker.
	resp = ts.get(t, "/api/projects", cookie)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("INV-MUSTCHG-001 violated: /projects returned %d, want 403", resp.StatusCode)
	}
	if !strings.Contains(string(body), "must_change_password") {
		t.Errorf("INV-MUSTCHG-001 violated: 403 body missing marker, got %q", string(body))
	}
}

// PAI-321 — changing the password clears the flag and unlocks every
// non-allowlisted endpoint on the next request.
func TestRegression_MustChange_002_PasswordChangeUnlocks(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/users", ts.adminCookie, map[string]any{
		"username":             "newhire2",
		"password":             "tempinitial",
		"role":                 "member",
		"must_change_password": true,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: %d", resp.StatusCode)
	}
	cookie := ts.login(t, "newhire2", "tempinitial")
	// Submit the password change. CSRF is enforced — fetch token first.
	sid := strings.TrimPrefix(cookie, "session=")
	var csrfTok string
	_ = db.DB.QueryRow("SELECT csrf_token FROM sessions WHERE id=?", sid).Scan(&csrfTok)
	body := bytes.NewBufferString(`{"current_password":"tempinitial","new_password":"realpermpw"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.srv.URL+"/api/auth/password", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	if csrfTok != "" {
		req.Header.Set("X-CSRF-Token", csrfTok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change password: %d", resp.StatusCode)
	}
	// Re-login (ChangePassword nukes other sessions; the cookie above
	// is the current session and survives, but reusing it is the
	// realistic flow).
	resp = ts.get(t, "/api/projects", cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("INV-MUSTCHG-002 violated: /projects still blocked after password change (got %d)", resp.StatusCode)
	}
}

// PAI-321 — admin opt-out: must_change_password=false on create
// produces a user who can hit protected endpoints immediately.
func TestRegression_MustChange_003_OptOutSkipsGate(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.post(t, "/api/users", ts.adminCookie, map[string]any{
		"username":             "service-acct",
		"password":             "perm-from-day-one",
		"role":                 "member",
		"must_change_password": false,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: %d", resp.StatusCode)
	}
	cookie := ts.login(t, "service-acct", "perm-from-day-one")
	resp = ts.get(t, "/api/projects", cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("INV-MUSTCHG-003 violated: opted-out user blocked anyway (got %d)", resp.StatusCode)
	}
}

// PAI-322 — changing one's own password invalidates every OTHER
// session for the user but keeps the current one alive. Mirrors how
// modern apps (GitHub / Google) handle password change.
func TestRegression_Session_004_PasswordChangeKillsOtherSessions(t *testing.T) {
	ts := newTestServer(t)
	var memberID int64
	if err := db.DB.QueryRow("SELECT id FROM users WHERE username='member'").Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	currentSID := strings.TrimPrefix(ts.memberCookie, "session=")

	// Plant a second, separate session for the same user — simulates a
	// second device / browser. Use a known id and a future expiry so
	// it's a "live" session in the same sense as the one issued by
	// login.
	otherSID := "ffffffffffffffffffffffffffffffff"
	now := time.Now().UTC()
	if _, err := db.DB.Exec(
		`INSERT INTO sessions(id, user_id, expires_at, csrf_token, via_dev_login, created_at)
		 VALUES (?, ?, ?, '', 0, ?)`,
		otherSID, memberID,
		now.Add(7*24*time.Hour).Format("2006-01-02 15:04:05"),
		now.Format("2006-01-02 15:04:05"),
	); err != nil {
		t.Fatal(err)
	}

	// Member changes their own password through the current session.
	// CSRF is enforced on /auth/password — fetch the token first.
	var csrfTok string
	if err := db.DB.QueryRow("SELECT csrf_token FROM sessions WHERE id=?", currentSID).Scan(&csrfTok); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"current_password":"memberpass","new_password":"newmemberpass"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.srv.URL+"/api/auth/password", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", ts.memberCookie)
	if csrfTok != "" {
		req.Header.Set("X-CSRF-Token", csrfTok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change password: status %d", resp.StatusCode)
	}

	// The current session must still be alive.
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id=?", currentSID).Scan(&n)
	if n != 1 {
		t.Errorf("INV-SESSION-004 violated: current session was deleted on own password change")
	}
	// The "other device" session must be gone.
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE id=?", otherSID).Scan(&n)
	if n != 0 {
		t.Errorf("INV-SESSION-004 violated: other-device session survived password change")
	}
}
