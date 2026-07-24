// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/inspr-at/paimos/backend/db"
)

func TestCreateIssueConcurrentAllocatesUniqueNumbers(t *testing.T) {
	ts := newTestServer(t)
	projectID := seedBatchProject(t, "Race Project", "RACE")

	const creates = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, creates)
	nums := make(chan int, creates)

	for i := 0; i < creates; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			body, _ := json.Marshal(map[string]any{
				"title": fmt.Sprintf("Concurrent ticket %02d", i),
				"type":  "ticket",
			})
			req, err := http.NewRequest(http.MethodPost, ts.srv.URL+fmt.Sprintf("/api/projects/%d/issues", projectID), bytes.NewReader(body))
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Cookie", ts.adminCookie)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				errs <- fmt.Errorf("status=%d", resp.StatusCode)
				return
			}
			var out struct {
				IssueNumber int `json:"issue_number"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				errs <- err
				return
			}
			nums <- out.IssueNumber
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(nums)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create failed: %v", err)
		}
	}

	seen := map[int]bool{}
	for n := range nums {
		if seen[n] {
			t.Fatalf("duplicate issue number returned: %d", n)
		}
		seen[n] = true
	}
	if len(seen) != creates {
		t.Fatalf("created numbers=%d want %d", len(seen), creates)
	}

	var total, distinct int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*), COUNT(DISTINCT issue_number)
		FROM issues
		WHERE project_id = ?
	`, projectID).Scan(&total, &distinct); err != nil {
		t.Fatalf("count issue numbers: %v", err)
	}
	if total != creates || distinct != creates {
		t.Fatalf("db count=%d distinct=%d want %d/%d", total, distinct, creates, creates)
	}
}
