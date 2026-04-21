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
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

// insertProjectWithKey bypasses the "KEY" suffix convention used by
// insertProject so we can seed projects whose keys match real PAIMOS
// instances (e.g. "PAI", "PMO26").
func insertProjectWithKey(t *testing.T, name, key string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO projects(name, key) VALUES(?, ?)`, name, key)
	if err != nil {
		t.Fatalf("insert project %s/%s: %v", name, key, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertIssue(t *testing.T, projectID int64, issueNumber int, extras string) int64 {
	t.Helper()
	q := `INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`
	res, err := db.DB.Exec(q, projectID, issueNumber, "ticket", "t", "backlog")
	if err != nil {
		t.Fatalf("insert issue %d/%d: %v", projectID, issueNumber, err)
	}
	id, _ := res.LastInsertId()
	if extras == "soft-delete" {
		_, err := db.DB.Exec(
			`UPDATE issues SET deleted_at=datetime('now') WHERE id=?`, id,
		)
		if err != nil {
			t.Fatalf("soft-delete issue %d: %v", id, err)
		}
	}
	return id
}

func TestIsIssueKey(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"PAI-1", true},
		{"PAI-83", true},
		{"PMO26-639", true},
		{"A-1", true},
		{"", false},
		{"PAI", false},
		{"pai-1", false},            // lowercase rejected
		{"PAI-", false},             // no number
		{"-1", false},               // no project
		{"PAI--1", false},           // negative-looking
		{"PAI 1", false},            // space
		{"PAI_1", false},            // underscore
		{"VERYLONGKEYNAME-1", true}, // 15 extra chars after first letter — fits
		{"SIXTEENCHARSLONGK-1", false},
		{"123", false}, // numeric is not a key
	}
	for _, tc := range cases {
		if got := auth.IsIssueKey(tc.in); got != tc.want {
			t.Errorf("IsIssueKey(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestResolveIssueRef(t *testing.T) {
	setupAccessTestDB(t)

	paiProj := insertProjectWithKey(t, "PAI", "PAI")
	pmoProj := insertProjectWithKey(t, "PMO", "PMO26")

	pai1 := insertIssue(t, paiProj, 1, "")
	pai83 := insertIssue(t, paiProj, 83, "")
	pmo639 := insertIssue(t, pmoProj, 639, "")
	paiDeleted := insertIssue(t, paiProj, 99, "soft-delete")

	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{strconv.FormatInt(pai1, 10), pai1, true}, // numeric passes through
		{"PAI-1", pai1, true},                     // key resolves
		{"PAI-83", pai83, true},
		{"PMO26-639", pmo639, true},  // multi-digit project key
		{"PAI-99", paiDeleted, true}, // soft-deleted resolves (handler enforces visibility)
		{"PAI-9999", 0, false},       // not-found key
		{"ZZZ-1", 0, false},          // project doesn't exist
		{"pai-83", 0, false},         // lowercase rejected
		{"foo", 0, false},            // garbage
		{"", 0, false},               // empty
		{"0", 0, false},              // zero is not valid
		{"-1", 0, false},             // negative parses but rejected
	}
	for _, tc := range cases {
		got, ok := auth.ResolveIssueRef(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ResolveIssueRef(%q) = (%d, %v), want (%d, %v)",
				tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// echoIDHandler returns the resolved chi "id" URL param verbatim so tests
// can assert the rewrite happened in-band before the handler ran.
func echoIDHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chi.URLParam(r, "id"))
	})
}

func TestRequireIssueAccess_KeyResolution(t *testing.T) {
	setupAccessTestDB(t)

	paiProj := insertProjectWithKey(t, "PAI", "PAI")
	pai83 := insertIssue(t, paiProj, 83, "")
	pai83Str := strconv.FormatInt(pai83, 10)

	admin := &models.User{
		ID:     insertUser(t, "admin", "admin", "active"),
		Role:   "admin",
		Status: "active",
	}

	cases := []struct {
		name     string
		path     string
		wantCode int
		wantBody string // only checked when 200
	}{
		{"numeric", "/issues/" + pai83Str, http.StatusOK, pai83Str},
		{"key", "/issues/PAI-83", http.StatusOK, pai83Str},
		{"key-not-found", "/issues/PAI-9999", http.StatusNotFound, ""},
		{"unknown-project-key", "/issues/ZZZ-1", http.StatusNotFound, ""},
		{"garbage", "/issues/foo", http.StatusBadRequest, ""},
		{"lowercase-key", "/issues/pai-83", http.StatusBadRequest, ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.With(auth.RequireIssueAccess).Get("/issues/{id}", echoIDHandler().ServeHTTP)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			ctx := context.WithValue(req.Context(), auth.UserKey, admin)
			ctx = auth.WithAccessCache(ctx)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("%s: code=%d, want %d (body=%q)",
					tc.name, rec.Code, tc.wantCode, rec.Body.String())
				return
			}
			if tc.wantCode == http.StatusOK && rec.Body.String() != tc.wantBody {
				t.Errorf("%s: body=%q, want %q", tc.name, rec.Body.String(), tc.wantBody)
			}
		})
	}
}
