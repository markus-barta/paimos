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
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"

	_ "modernc.org/sqlite"
)

func setupAccessTestDB(t *testing.T) {
	t.Helper()
	// t.Setenv auto-restores on cleanup and t.TempDir auto-deletes, so each
	// test gets a fresh DB file that the next test can't contaminate.
	t.Setenv("DATA_DIR", t.TempDir())
	// PAIMOS_TEST_MODE also speeds up the migration run inside db.Open().
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			db.DB.Close()
			db.DB = nil
		}
	})
}

// mkReq builds an *http.Request with the given user attached via
// auth.UserKey — no session cookie required since we're bypassing the
// HTTP middleware in the test.
func mkReq(u *models.User) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if u != nil {
		ctx := context.WithValue(r.Context(), auth.UserKey, u)
		ctx = auth.WithAccessCache(ctx)
		r = r.WithContext(ctx)
	}
	return r
}

func insertProject(t *testing.T, name string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO projects(name, key) VALUES(?, ?)`, name, name+"KEY")
	if err != nil {
		t.Fatalf("insert project %s: %v", name, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertUser(t *testing.T, username, role, status string) int64 {
	t.Helper()
	res, err := db.DB.Exec(
		`INSERT INTO users(username, password, role, status) VALUES(?, 'x', ?, ?)`,
		username, role, status,
	)
	if err != nil {
		t.Fatalf("insert user %s: %v", username, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestCanViewProject(t *testing.T) {
	setupAccessTestDB(t)

	projA := insertProject(t, "A")
	projB := insertProject(t, "B")

	admin := &models.User{ID: insertUser(t, "admin", "admin", "active"), Role: "admin", Status: "active"}
	memberUID := insertUser(t, "mem", "member", "active")
	member := &models.User{ID: memberUID, Role: "member", Status: "active"}
	extUID := insertUser(t, "ext", "external", "active")
	external := &models.User{ID: extUID, Role: "external", Status: "active"}
	inactiveUID := insertUser(t, "gone", "member", "inactive")
	inactive := &models.User{ID: inactiveUID, Role: "member", Status: "inactive"}

	// Grant external access to project A only.
	if _, err := db.DB.Exec(
		"INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)",
		extUID, projA, "viewer",
	); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// Put a 'none' row on member for project B to simulate explicit denial.
	if _, err := db.DB.Exec(
		"INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)",
		memberUID, projB, "none",
	); err != nil {
		t.Fatalf("deny: %v", err)
	}

	cases := []struct {
		name    string
		user    *models.User
		project int64
		want    bool
	}{
		{"admin sees A", admin, projA, true},
		{"admin sees B", admin, projB, true},
		{"member sees A (default editor)", member, projA, true},
		{"member denied on B via explicit 'none'", member, projB, false},
		{"external sees A via grant", external, projA, true},
		{"external blocked from B (no grant)", external, projB, false},
		{"inactive member blocked", inactive, projA, false},
		{"nil user blocked", nil, projA, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := auth.CanViewProject(mkReq(tc.user), tc.project)
			if got != tc.want {
				t.Errorf("CanViewProject: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanEditProject(t *testing.T) {
	setupAccessTestDB(t)

	projA := insertProject(t, "A")
	admin := &models.User{ID: insertUser(t, "admin", "admin", "active"), Role: "admin", Status: "active"}
	extUID := insertUser(t, "ext", "external", "active")
	external := &models.User{ID: extUID, Role: "external", Status: "active"}
	extEditorUID := insertUser(t, "exted", "external", "active")
	externalEditor := &models.User{ID: extEditorUID, Role: "external", Status: "active"}

	db.DB.Exec("INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)", extUID, projA, "viewer")
	db.DB.Exec("INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)", extEditorUID, projA, "editor")

	if !auth.CanEditProject(mkReq(admin), projA) {
		t.Error("admin should edit any project")
	}
	if auth.CanEditProject(mkReq(external), projA) {
		t.Error("external viewer should not edit")
	}
	if !auth.CanEditProject(mkReq(externalEditor), projA) {
		t.Error("external editor should edit")
	}
}

func TestAccessibleProjectIDs(t *testing.T) {
	setupAccessTestDB(t)

	projA := insertProject(t, "A")
	projB := insertProject(t, "B")
	projC := insertProject(t, "C")

	admin := &models.User{ID: insertUser(t, "admin", "admin", "active"), Role: "admin", Status: "active"}
	memUID := insertUser(t, "mem", "member", "active")
	member := &models.User{ID: memUID, Role: "member", Status: "active"}
	extUID := insertUser(t, "ext", "external", "active")
	external := &models.User{ID: extUID, Role: "external", Status: "active"}

	// member denied on C
	db.DB.Exec("INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)", memUID, projC, "none")
	// external granted on A only
	db.DB.Exec("INSERT INTO project_members(user_id, project_id, access_level) VALUES(?,?,?)", extUID, projA, "viewer")

	if got := auth.AccessibleProjectIDs(mkReq(admin)); got != nil {
		t.Errorf("admin: got %v, want nil (all)", got)
	}

	mem := auth.AccessibleProjectIDs(mkReq(member))
	sort.Slice(mem, func(i, j int) bool { return mem[i] < mem[j] })
	wantMem := []int64{projA, projB}
	if !equal(mem, wantMem) {
		t.Errorf("member: got %v, want %v", mem, wantMem)
	}

	ext := auth.AccessibleProjectIDs(mkReq(external))
	if !equal(ext, []int64{projA}) {
		t.Errorf("external: got %v, want [%d]", ext, projA)
	}
}

func TestSeedAccessForProject(t *testing.T) {
	setupAccessTestDB(t)
	adminID := insertUser(t, "admin", "admin", "active")
	memID := insertUser(t, "mem", "member", "active")
	extID := insertUser(t, "ext", "external", "active")

	projA := insertProject(t, "A")
	auth.SeedAccessForProject(projA)

	for _, tc := range []struct {
		uid     int64
		wantLvl string
	}{
		{adminID, "editor"},
		{memID, "editor"},
	} {
		var lvl string
		err := db.DB.QueryRow(
			"SELECT access_level FROM project_members WHERE user_id=? AND project_id=?",
			tc.uid, projA,
		).Scan(&lvl)
		if err != nil || lvl != tc.wantLvl {
			t.Errorf("user %d: got level=%q err=%v, want %q", tc.uid, lvl, err, tc.wantLvl)
		}
	}

	// external should NOT be seeded
	var count int
	db.DB.QueryRow(
		"SELECT COUNT(*) FROM project_members WHERE user_id=? AND project_id=?",
		extID, projA,
	).Scan(&count)
	if count != 0 {
		t.Errorf("external: expected no seed row, got count=%d", count)
	}
}

func TestSeedAccessForUser(t *testing.T) {
	setupAccessTestDB(t)
	projA := insertProject(t, "A")
	projB := insertProject(t, "B")

	newMem := insertUser(t, "new", "member", "active")
	auth.SeedAccessForUser(newMem, "member")

	var count int
	db.DB.QueryRow(
		"SELECT COUNT(*) FROM project_members WHERE user_id=? AND access_level='editor'",
		newMem,
	).Scan(&count)
	if count != 2 {
		t.Errorf("new member: expected 2 editor rows (A,B), got %d", count)
	}

	// external: no auto-seed
	newExt := insertUser(t, "newe", "external", "active")
	auth.SeedAccessForUser(newExt, "external")
	db.DB.QueryRow("SELECT COUNT(*) FROM project_members WHERE user_id=?", newExt).Scan(&count)
	if count != 0 {
		t.Errorf("external: expected 0 seed rows, got %d", count)
	}

	_ = projA
	_ = projB
}

func TestAccessCacheMemoization(t *testing.T) {
	setupAccessTestDB(t)
	projA := insertProject(t, "A")
	uid := insertUser(t, "mem", "member", "active")
	user := &models.User{ID: uid, Role: "member", Status: "active"}
	req := mkReq(user)

	// Two back-to-back calls must both return true; this smoke-tests that
	// the cache is being populated without errors.
	if !auth.CanViewProject(req, projA) {
		t.Fatal("first view check failed")
	}
	if !auth.CanViewProject(req, projA) {
		t.Fatal("second view check failed (cache bug?)")
	}
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]int64{}, a...)
	bb := append([]int64{}, b...)
	sort.Slice(aa, func(i, j int) bool { return aa[i] < aa[j] })
	sort.Slice(bb, func(i, j int) bool { return bb[i] < bb[j] })
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}
