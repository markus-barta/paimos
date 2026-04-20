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

package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// okHandler returns 200 OK — use to detect whether a middleware let a
// request through.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// runProjectMW builds a /projects/{id}-routed request for user, attaches
// it to a chi router with mw wrapped around okHandler, and returns the
// response recorder.
func runProjectMW(mw func(http.Handler) http.Handler, user *models.User, projectID int64) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.With(mw).Get("/projects/{id}", okHandler().ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/projects/%d", projectID), nil)
	ctx := context.WithValue(req.Context(), auth.UserKey, user)
	ctx = auth.WithAccessCache(ctx)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRequireProjectView(t *testing.T) {
	setupAccessTestDB(t)
	projA := insertProject(t, "A")

	admin := &models.User{ID: insertUser(t, "admin", "admin", "active"), Role: "admin", Status: "active"}
	extUID := insertUser(t, "ext", "external", "active")
	external := &models.User{ID: extUID, Role: "external", Status: "active"}

	// external granted viewer on A
	db.DB.Exec("INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)", extUID, projA, "viewer")

	other := insertProject(t, "B") // external has no grant here

	if got := runProjectMW(auth.RequireProjectView, admin, projA).Code; got != http.StatusOK {
		t.Errorf("admin view A: got %d, want 200", got)
	}
	if got := runProjectMW(auth.RequireProjectView, external, projA).Code; got != http.StatusOK {
		t.Errorf("external view A: got %d, want 200", got)
	}
	if got := runProjectMW(auth.RequireProjectView, external, other).Code; got != http.StatusNotFound {
		t.Errorf("external view B (no grant): got %d, want 404", got)
	}
}

func TestRequireProjectEdit(t *testing.T) {
	setupAccessTestDB(t)
	projA := insertProject(t, "A")

	admin := &models.User{ID: insertUser(t, "admin", "admin", "active"), Role: "admin", Status: "active"}
	viewerUID := insertUser(t, "viewer", "external", "active")
	viewer := &models.User{ID: viewerUID, Role: "external", Status: "active"}
	editorUID := insertUser(t, "editor", "external", "active")
	editor := &models.User{ID: editorUID, Role: "external", Status: "active"}

	db.DB.Exec("INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)", viewerUID, projA, "viewer")
	db.DB.Exec("INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)", editorUID, projA, "editor")

	cases := []struct {
		name string
		user *models.User
		want int
	}{
		{"admin", admin, http.StatusOK},
		{"editor", editor, http.StatusOK},
		{"viewer", viewer, http.StatusForbidden}, // 403 — knows it exists, can't edit
		{"stranger", &models.User{ID: 9999, Role: "external", Status: "active"}, http.StatusNotFound},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := runProjectMW(auth.RequireProjectEdit, tc.user, projA).Code; got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequireIssueAccess_OrphanPassesThrough(t *testing.T) {
	setupAccessTestDB(t)

	// Insert an orphan (sprint) issue — project_id NULL
	res, err := db.DB.Exec(`INSERT INTO issues(project_id, issue_number, type, title) VALUES(NULL, 0, 'sprint', 'Sprint 1')`)
	if err != nil {
		t.Fatalf("insert sprint: %v", err)
	}
	sprintID, _ := res.LastInsertId()

	extUID := insertUser(t, "ext", "external", "active")
	external := &models.User{ID: extUID, Role: "external", Status: "active"}

	r := chi.NewRouter()
	r.With(auth.RequireIssueAccess).Get("/issues/{id}", okHandler().ServeHTTP)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/issues/%d", sprintID), nil)
	ctx := context.WithValue(req.Context(), auth.UserKey, external)
	ctx = auth.WithAccessCache(ctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("orphan sprint: got %d, want 200 (orphan pass-through)", rec.Code)
	}
}
