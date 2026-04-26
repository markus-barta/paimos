package handlers_test

import (
	"context"
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
