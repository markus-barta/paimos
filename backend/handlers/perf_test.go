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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

// TestPerf_ListIssues measures the response time for listing issues at various
// scales. Run with:
//
//	go test ./handlers/... -run TestPerf_ListIssues -v
//
// The test creates 1000 issues with tags, sprints, comments, time entries, and
// parent-child relationships, then measures GET /api/projects/:id/issues.
func TestPerf_ListIssues(t *testing.T) {
	ts := newTestServer(t)

	// Create project
	pResp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Perf Test Project", "key": "PERF",
	})
	assertStatus(t, pResp, http.StatusCreated)
	projectID := responseID(t, pResp)

	// Create a few tags
	tagIDs := make([]int64, 5)
	for i := range tagIDs {
		resp := ts.post(t, "/api/tags", ts.adminCookie, map[string]string{
			"name": fmt.Sprintf("perf-tag-%d", i), "color": "blue",
		})
		assertStatus(t, resp, http.StatusCreated)
		tagIDs[i] = responseID(t, resp)
	}

	// Create 2 sprints
	sprintIDs := make([]int64, 2)
	for i := range sprintIDs {
		resp := ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]any{
			"title": fmt.Sprintf("Sprint %d", i+1), "type": "sprint",
			"start_date": "2026-01-01", "end_date": "2026-01-14",
		})
		assertStatus(t, resp, http.StatusCreated)
		sprintIDs[i] = responseID(t, resp)
	}

	// Bulk-insert 1000 issues with realistic data via direct DB for speed.
	// (Using the API for 1000 issues would be too slow for a test.)
	t.Log("Seeding 1000 issues with tags, sprints, comments, time entries...")
	seedStart := time.Now()

	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	// Create 50 epics, 200 tickets (parented to epics), 750 tasks (parented to tickets)
	epicIDs := make([]int64, 50)
	ticketIDs := make([]int64, 200)
	allIDs := make([]int64, 0, 1000)
	issueNum := 1

	// Helper to get next issue number and insert
	insertIssue := func(iType, title string, parentID *int64, assigneeID int64) int64 {
		num := issueNum
		issueNum++
		res, err := tx.Exec(`INSERT INTO issues(
			project_id, issue_number, type, parent_id, title,
			description, acceptance_criteria, notes,
			status, priority, cost_unit, release, assignee_id,
			estimate_hours, ar_hours, created_by
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			projectID, num, iType, parentID, title,
			fmt.Sprintf("Description for %s — lorem ipsum dolor sit amet", title),
			"- [ ] AC item 1\n- [ ] AC item 2\n- [ ] AC item 3",
			"Some notes here",
			[]string{"new", "in-progress", "done"}[num%3],
			[]string{"low", "medium", "high"}[num%3],
			[]string{"", "internal", "external"}[num%3],
			[]string{"", "v1.0", "v2.0"}[num%3],
			assigneeID,
			float64(num%20), float64(num%15),
			1, // admin
		)
		if err != nil {
			t.Fatalf("insert issue %d: %v", num, err)
		}
		id, _ := res.LastInsertId()
		allIDs = append(allIDs, id)
		return id
	}

	// Get user IDs for assignment rotation
	var adminID, memberID int64
	db.DB.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&adminID)
	db.DB.QueryRow("SELECT id FROM users WHERE username='member'").Scan(&memberID)
	assignees := []int64{adminID, memberID}

	// 50 epics
	for i := 0; i < 50; i++ {
		epicIDs[i] = insertIssue("epic", fmt.Sprintf("Epic %d", i+1), nil, assignees[i%2])
	}
	// 200 tickets under epics
	for i := 0; i < 200; i++ {
		pid := epicIDs[i%50]
		ticketIDs[i] = insertIssue("ticket", fmt.Sprintf("Ticket %d", i+1), &pid, assignees[i%2])
	}
	// 750 tasks under tickets
	for i := 0; i < 750; i++ {
		pid := ticketIDs[i%200]
		insertIssue("task", fmt.Sprintf("Task %d", i+1), &pid, assignees[i%2])
	}

	// Assign tags (3 tags per issue on average)
	for i, id := range allIDs {
		for j := 0; j < 3; j++ {
			tagID := tagIDs[(i+j)%len(tagIDs)]
			tx.Exec("INSERT OR IGNORE INTO issue_tags(issue_id, tag_id) VALUES(?,?)", id, tagID)
		}
	}

	// Assign sprints (every 3rd issue gets a sprint)
	for i, id := range allIDs {
		if i%3 == 0 {
			sid := sprintIDs[i%len(sprintIDs)]
			tx.Exec("INSERT OR IGNORE INTO issue_relations(source_id, target_id, type) VALUES(?,?,?)", sid, id, "sprint")
		}
	}

	// Add comments (2 per every 5th issue)
	for i, id := range allIDs {
		if i%5 == 0 {
			for c := 0; c < 2; c++ {
				tx.Exec("INSERT INTO comments(issue_id, user_id, body) VALUES(?,?,?)",
					id, adminID, fmt.Sprintf("Comment %d on issue %d", c+1, id))
			}
		}
	}

	// Add time entries (1 per every 4th issue)
	for i, id := range allIDs {
		if i%4 == 0 {
			tx.Exec(`INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at) VALUES(?,?,?,?)`,
				id, adminID, "2026-01-10T09:00:00Z", "2026-01-10T10:30:00Z")
		}
	}

	// Add issue history (1 entry per every 3rd issue)
	for i, id := range allIDs {
		if i%3 == 0 {
			tx.Exec(`INSERT INTO issue_history(issue_id, changed_by, changed_at, snapshot) VALUES(?,?,?,?)`,
				id, adminID, "2026-01-10T10:00:00Z", `{"title":"old"}`)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	t.Logf("Seed completed in %v", time.Since(seedStart).Round(time.Millisecond))

	// Verify issue count
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM issues WHERE project_id=?", projectID).Scan(&count)
	t.Logf("Total issues in project: %d", count)

	// ── Benchmark at different scales ────────────────────────────────────

	scales := []struct {
		name     string
		query    string
		maxMS    int64 // target max response time in milliseconds
	}{
		{"100 issues (filtered)", fmt.Sprintf("/api/projects/%d/issues?type=epic", projectID), 100},
		{"400 issues (mixed)", fmt.Sprintf("/api/projects/%d/issues?type=epic,ticket", projectID), 200},
		{"1000 issues (all)", fmt.Sprintf("/api/projects/%d/issues", projectID), 500},
		{"1000 issues (with status filter)", fmt.Sprintf("/api/projects/%d/issues?status=backlog", projectID), 200},
		{"1000 issues (with tag filter)", fmt.Sprintf("/api/projects/%d/issues?tags=%d", projectID, tagIDs[0]), 300},
	}

	for _, sc := range scales {
		t.Run(sc.name, func(t *testing.T) {
			// Warm up
			resp := ts.get(t, sc.query, ts.adminCookie)
			resp.Body.Close()

			// Measure 5 runs
			var totalDuration time.Duration
			var issueCount int
			runs := 5

			for i := 0; i < runs; i++ {
				start := time.Now()
				resp := ts.get(t, sc.query, ts.adminCookie)
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				elapsed := time.Since(start)
				totalDuration += elapsed

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status %d: %s", resp.StatusCode, body)
				}

				if i == 0 {
					var issues []json.RawMessage
					json.Unmarshal(body, &issues)
					issueCount = len(issues)

					// Verify response size is reasonable
					t.Logf("Response: %d issues, %d bytes", issueCount, len(body))
				}
			}

			avg := totalDuration / time.Duration(runs)
			t.Logf("Average response time: %v (%d issues, %d runs)", avg.Round(100*time.Microsecond), issueCount, runs)

			if avg.Milliseconds() > sc.maxMS {
				t.Errorf("SLOW: %v average exceeds %dms target", avg.Round(time.Millisecond), sc.maxMS)
			}
		})
	}

	// ── Response size benchmark ──────────────────────────────────────────

	t.Run("response_size", func(t *testing.T) {
		resp := ts.get(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		sizeMB := float64(len(body)) / 1024 / 1024
		t.Logf("Full response: %.2f MB for %d issues", sizeMB, count)

		// Response should be under 5 MB for 1000 issues
		if sizeMB > 5.0 {
			t.Errorf("Response too large: %.2f MB (target: < 5 MB)", sizeMB)
		}
	})

	// ── Cross-project list benchmark ─────────────────────────────────────

	t.Run("cross_project_list", func(t *testing.T) {
		var totalDuration time.Duration
		runs := 5

		// Warm up
		resp := ts.get(t, "/api/issues", ts.adminCookie)
		resp.Body.Close()

		for i := 0; i < runs; i++ {
			start := time.Now()
			resp := ts.get(t, "/api/issues", ts.adminCookie)
			resp.Body.Close()
			totalDuration += time.Since(start)
		}

		avg := totalDuration / time.Duration(runs)
		t.Logf("Cross-project list average: %v (%d runs)", avg.Round(100*time.Microsecond), runs)

		if avg.Milliseconds() > 500 {
			t.Errorf("SLOW: %v average exceeds 500ms target", avg.Round(time.Millisecond))
		}
	})

	// ── Print summary ────────────────────────────────────────────────────

	t.Log(strings.Repeat("─", 60))
	t.Log("Performance test complete. Targets are calibrated for CI.")
	t.Log("On slow hardware, multiply thresholds by the HW slowdown factor.")
}
