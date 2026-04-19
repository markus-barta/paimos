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
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/markus-barta/paimos/backend/db"
)

func TestLieferberichtPDF_BasicRender(t *testing.T) {
	ts := newTestServer(t)

	// Create a project
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "PDF Test Project",
		"key":  "PDF1",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	// Create an epic
	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":  "Epic One",
		"type":   "epic",
		"status": "backlog",
	})
	assertStatus(t, resp, http.StatusCreated)
	epicID := responseID(t, resp)

	// Create tickets under the epic with varying data
	for i := 1; i <= 3; i++ {
		status := "backlog"
		if i == 1 {
			status = "done"
		} else if i == 2 {
			status = "in-progress"
		}
		resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
			"title":       fmt.Sprintf("Ticket %d with a longer summary that tests wrapping behavior", i),
			"description": fmt.Sprintf("Description for ticket %d — this is a detailed description with Ümlauts and special chars", i),
			"type":        "ticket",
			"status":      status,
			"parent_id":   epicID,
		})
		assertStatus(t, resp, http.StatusCreated)
	}

	// Create a ticket where description matches summary (should be skipped)
	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":       "Duplicate description test",
		"description": "Duplicate description test",
		"type":        "ticket",
		"status":      "backlog",
		"parent_id":   epicID,
	})
	assertStatus(t, resp, http.StatusCreated)

	// Set some estimate values directly in DB for richer output
	db.DB.Exec("UPDATE issues SET estimate_lp=5, estimate_hours=40 WHERE title LIKE 'Ticket 1%'")
	db.DB.Exec("UPDATE issues SET estimate_lp=3, estimate_hours=24 WHERE title LIKE 'Ticket 2%'")

	// Request the PDF
	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/reports/lieferbericht/pdf?scope=all", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// Basic checks
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type: got %q, want application/pdf", ct)
	}
	if len(body) < 1000 {
		t.Errorf("PDF too small: %d bytes", len(body))
	}
	// PDF magic bytes
	if string(body[:5]) != "%PDF-" {
		t.Errorf("not a valid PDF: starts with %q", string(body[:5]))
	}
}

func TestLieferberichtPDF_ManyIssues(t *testing.T) {
	ts := newTestServer(t)

	// Create project
	resp := ts.post(t, "/api/projects", ts.adminCookie, map[string]string{
		"name": "Large PDF Project",
		"key":  "LPD",
	})
	assertStatus(t, resp, http.StatusCreated)
	projectID := responseID(t, resp)

	// Create epic
	resp = ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
		"title":  "Large Epic",
		"type":   "epic",
		"status": "backlog",
	})
	assertStatus(t, resp, http.StatusCreated)
	epicID := responseID(t, resp)

	// Create 60 tickets to test multi-page rendering
	for i := 1; i <= 60; i++ {
		ts.post(t, fmt.Sprintf("/api/projects/%d/issues", projectID), ts.adminCookie, map[string]interface{}{
			"title":       fmt.Sprintf("Issue %d — testing pagination with a reasonably long summary text", i),
			"description": fmt.Sprintf("Detailed description for issue %d that should wrap nicely in the PDF output", i),
			"type":        "ticket",
			"status":      "done",
			"parent_id":   epicID,
		})
	}

	resp = ts.get(t, fmt.Sprintf("/api/projects/%d/reports/lieferbericht/pdf?scope=all", projectID), ts.adminCookie)
	assertStatus(t, resp, http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if len(body) < 2000 {
		t.Errorf("PDF for 60 issues too small: %d bytes", len(body))
	}
	if string(body[:5]) != "%PDF-" {
		t.Errorf("not a valid PDF")
	}
}
