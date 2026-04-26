package handlers_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

type authzCase struct {
	desc       string
	role       string
	method     string
	path       string
	wantStatus int
}

func TestAuthzFuzz_RoleMatrix(t *testing.T) {
	ts := newTestServer(t)

	cookies := map[string]string{
		"admin":     ts.adminCookie,
		"member":    ts.memberCookie,
		"external":  ts.externalCookie,
		"anonymous": "",
	}

	cases := []authzCase{
		{"anon /api/auth/me", "anonymous", "GET", "/api/auth/me", 401},
		{"anon /api/projects", "anonymous", "GET", "/api/projects", 401},
		{"anon /api/users", "anonymous", "GET", "/api/users", 401},
		{"anon /api/incidents", "anonymous", "GET", "/api/incidents", 401},
		{"anon /api/incidents/export", "anonymous", "GET", "/api/incidents/export", 401},
		{"anon /api/gdpr/retention", "anonymous", "GET", "/api/gdpr/retention", 401},
		{"anon /api/access-audit", "anonymous", "GET", "/api/access-audit", 401},
		{"anon /api/permissions/matrix", "anonymous", "GET", "/api/permissions/matrix", 401},

		{"external /api/projects", "external", "GET", "/api/projects", 403},
		{"external /api/users", "external", "GET", "/api/users", 403},
		{"external /api/issues", "external", "GET", "/api/issues", 403},
		{"external /api/tags", "external", "GET", "/api/tags", 403},
		{"external /api/search", "external", "GET", "/api/search", 403},
		{"external /api/incidents", "external", "GET", "/api/incidents", 403},
		{"external /api/incidents/export", "external", "GET", "/api/incidents/export", 403},

		{"member POST /api/users", "member", "POST", "/api/users", 403},
		{"member POST /api/projects", "member", "POST", "/api/projects", 403},
		{"member POST /api/tags", "member", "POST", "/api/tags", 403},
		{"member GET /api/incidents", "member", "GET", "/api/incidents", 403},
		{"member GET /api/incidents/export", "member", "GET", "/api/incidents/export", 403},
		{"member GET /api/access-audit", "member", "GET", "/api/access-audit", 403},
		{"member GET /api/issues/trash", "member", "GET", "/api/issues/trash", 403},
		{"member PUT /api/branding", "member", "PUT", "/api/branding", 403},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			cookie := cookies[tc.role]
			var resp *http.Response
			switch tc.method {
			case "GET":
				resp = ts.get(t, tc.path, cookie)
			case "POST":
				resp = ts.post(t, tc.path, cookie, map[string]string{"name": "x"})
			case "PUT":
				resp = ts.put(t, tc.path, cookie, map[string]string{"name": "x"})
			case "PATCH":
				resp = ts.patch(t, tc.path, cookie, map[string]string{"name": "x"})
			case "DELETE":
				resp = ts.del(t, tc.path, cookie)
			default:
				t.Fatalf("unsupported method %q", tc.method)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("%s %s as %s -> %d, want %d", tc.method, tc.path, tc.role, resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestAuthzFuzz_IDOR_HiddenIsNotFound(t *testing.T) {
	ts := newTestServer(t)

	cresp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Hidden Project", "key": "HID",
	})
	defer cresp.Body.Close()
	var project struct {
		ID int64 `json:"id"`
	}
	readJSON(t, cresp, &project)
	if project.ID == 0 {
		t.Fatal("setup: hidden project not created")
	}
	var memberID int64
	_ = db.DB.QueryRow("SELECT id FROM users WHERE username='member'").Scan(&memberID)
	if _, err := db.DB.Exec(
		"INSERT OR REPLACE INTO project_members(user_id, project_id, access_level) VALUES(?,?,'none')",
		memberID, project.ID,
	); err != nil {
		t.Fatal(err)
	}

	pid := strconv.FormatInt(project.ID, 10)
	pathsByPID := []string{
		"/api/projects/" + pid,
		"/api/projects/" + pid + "/issues",
		"/api/projects/" + pid + "/export/csv",
		"/api/projects/" + pid + "/reports/lieferbericht",
	}
	for _, p := range pathsByPID {
		resp := ts.get(t, p, ts.memberCookie)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("IDOR: GET %s as no-view member -> %d, want 404", p, resp.StatusCode)
		}
	}

	for _, id := range []int64{0, -1, 999999999, 2147483647} {
		s := strconv.FormatInt(id, 10)
		for _, p := range []string{
			"/api/projects/" + s,
			"/api/issues/" + s,
			"/api/attachments/" + s,
			"/api/comments/" + s,
			"/api/time-entries/" + s,
			"/api/users/" + s + "/gdpr-export",
		} {
			resp := ts.get(t, p, ts.adminCookie)
			resp.Body.Close()
			if resp.StatusCode == http.StatusInternalServerError {
				t.Errorf("IDOR: GET %s on synthetic id -> 500", p)
			}
		}
	}
}

func TestAuthzFuzz_IDOR_PendingAttachmentHijack(t *testing.T) {
	ts := newTestServer(t)

	var adminID, memberID int64
	_ = db.DB.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&adminID)
	_ = db.DB.QueryRow("SELECT id FROM users WHERE username='member'").Scan(&memberID)

	pres := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{"name": "Hijack", "key": "HIJ"})
	var project struct {
		ID int64 `json:"id"`
	}
	readJSON(t, pres, &project)
	pres.Body.Close()
	if _, err := db.DB.Exec(
		"INSERT OR REPLACE INTO project_members(user_id, project_id, access_level) VALUES(?,?,'editor')",
		memberID, project.ID,
	); err != nil {
		t.Fatal(err)
	}
	ires := ts.post(t, "/api/projects/"+strconv.FormatInt(project.ID, 10)+"/issues", ts.memberCookie, map[string]string{"title": "I", "type": "ticket"})
	var issue struct {
		ID int64 `json:"id"`
	}
	readJSON(t, ires, &issue)
	ires.Body.Close()

	res, err := db.DB.Exec(`
		INSERT INTO attachments(issue_id, object_key, filename, content_type, size_bytes, uploaded_by)
		VALUES(NULL, ?, 'a.txt', 'text/plain', 1, ?)
	`, "pending/test.txt", adminID)
	if err != nil {
		t.Fatal(err)
	}
	aid, _ := res.LastInsertId()

	resp := ts.patch(t, "/api/attachments/link", ts.memberCookie, map[string]any{
		"issue_id":       issue.ID,
		"attachment_ids": []int64{aid},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("link: %d", resp.StatusCode)
	}
	var body struct {
		Linked int `json:"linked"`
	}
	readJSON(t, resp, &body)
	if body.Linked != 0 {
		t.Errorf("PAI-112 / INV-AUTHZ-003 violated: member linked %d pending attachments uploaded by admin", body.Linked)
	}
}
