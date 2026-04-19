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

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/go-chi/chi/v5"
)

// ── Mite API types ───────────────────────────────────────────────────────────

type miteTimeEntryWrapper struct {
	TimeEntry miteTimeEntry `json:"time_entry"`
}

type miteTimeEntry struct {
	ID          int     `json:"id"`
	Minutes     int     `json:"minutes"`
	DateAt      string  `json:"date_at"`
	Note        string  `json:"note"`
	UserID      int     `json:"user_id"`
	UserName    string  `json:"user_name"`
	ProjectID   int     `json:"project_id"`
	ProjectName string  `json:"project_name"`
	ServiceID   int     `json:"service_id"`
	ServiceName string  `json:"service_name"`
	Billable    bool    `json:"billable"`
	Revenue     float64 `json:"revenue"`
	HourlyRate  float64 `json:"hourly_rate"`
}

// ── Import request/result ────────────────────────────────────────────────────

type importMiteRequest struct {
	TargetProjectID int64  `json:"target_project_id"` // ignored (kept for backwards compat)
	FromDate        string `json:"from_date"`         // YYYY-MM-DD
	ToDate          string `json:"to_date"`           // YYYY-MM-DD
	MiteProjects    string `json:"mite_projects"`     // comma-separated filter
	DryRun          bool   `json:"dry_run"`           // preview only, no writes
}

type miteImportResult struct {
	Imported          int                    `json:"imported"`
	TotalMinutes      int                    `json:"total_minutes"`
	SkippedDuplicates []miteDupDetail        `json:"skipped_duplicates"`
	UnmatchedIssues   []miteUnmatchedIssue   `json:"unmatched_issues"`
	UnmatchedUsers    []miteUnmatchedUser    `json:"unmatched_users"`
	Matched           []miteMatchedDetail    `json:"matched"`
	Errors            []miteErrorDetail      `json:"errors"`
	ByProject         []miteProjectSummary   `json:"by_project"`
	DryRun            bool                   `json:"dry_run"`
}

type miteMatchedDetail struct {
	MiteID      int    `json:"mite_id"`
	JiraKey     string `json:"jira_key"`
	IssueKey string `json:"issue_key"`
	ProjectKey  string `json:"project_key"`
	Minutes     int    `json:"minutes"`
	User        string `json:"user"`
}

type miteProjectSummary struct {
	ProjectKey string `json:"project_key"`
	Imported   int    `json:"imported"`
	Minutes    int    `json:"minutes"`
	Unmatched  int    `json:"unmatched"`
	Duplicates int    `json:"duplicates"`
}

type miteDupDetail struct {
	MiteID  int    `json:"mite_id"`
	JiraKey string `json:"jira_key"`
}

type miteUnmatchedIssue struct {
	MiteID       int    `json:"mite_id"`
	Note         string `json:"note"`
	ExtractedKey string `json:"extracted_key"`
	Reason       string `json:"reason"`
}

type miteUnmatchedUser struct {
	MiteUserName string `json:"mite_user_name"`
	MiteUserID   int    `json:"mite_user_id"`
	Count        int    `json:"count"`
}

type miteErrorDetail struct {
	MiteID int    `json:"mite_id"`
	Reason string `json:"reason"`
}

// ── Mite import job store ────────────────────────────────────────────────────

type miteImportJob struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"` // "running", "complete", "error", "cancelled"
	Result       *miteImportResult `json:"result,omitempty"`
	Error        string            `json:"error,omitempty"`
	Started      time.Time         `json:"started"`
	Finished     *time.Time        `json:"finished,omitempty"`
	Total        int               `json:"total"`
	Processed    int               `json:"processed"`
	Phase        string            `json:"phase,omitempty"`
	PagesFetched int               `json:"pages_fetched"`
	MatchedCount int               `json:"matched_count"`
	SkippedCount int               `json:"skipped_count"`
	ErrorCount   int               `json:"error_count"`
	cancel       context.CancelFunc
}

var miteImportJobs = map[string]*miteImportJob{}

// POST /api/import/mite — start async import (or dry-run preview)
func ImportFromMite(w http.ResponseWriter, r *http.Request) {
	var req importMiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	cfg, err := loadMiteConfig()
	if err != nil || cfg.BaseURL == "" || cfg.APIKey == "" {
		jsonError(w, "Mite not configured", http.StatusBadRequest)
		return
	}

	if req.FromDate == "" {
		req.FromDate = cfg.LoadDataSinceDate
	}
	// Default from_date: day after latest mite entry in DB
	if req.FromDate == "" {
		var latestDate string
		if err := db.DB.QueryRow("SELECT date(MAX(started_at), '+1 day') FROM time_entries WHERE mite_id IS NOT NULL").Scan(&latestDate); err != nil {
			log.Printf("scan error: %v", err)
		}
		if latestDate != "" {
			req.FromDate = latestDate
		}
	}
	if req.FromDate == "" {
		req.FromDate = fmt.Sprintf("%d-01-01", time.Now().Year())
	}

	actor := auth.GetUser(r)
	jobID := newJobID()
	ctx, cancel := context.WithCancel(context.Background())
	job := &miteImportJob{ID: jobID, Status: "running", Started: time.Now().UTC(), cancel: cancel}

	importJobsMu.Lock()
	miteImportJobs[jobID] = job
	importJobsMu.Unlock()

	go func() {
		result, err := runMiteImport(ctx, cfg, req, actor.ID, job)
		now := time.Now().UTC()
		importJobsMu.Lock()
		defer importJobsMu.Unlock()
		job.Finished = &now
		if ctx.Err() != nil {
			job.Status = "cancelled"
			job.Result = result // partial results
		} else if err != nil {
			job.Status = "error"
			job.Error = err.Error()
		} else {
			job.Status = "complete"
			job.Result = result
		}
	}()

	jsonOK(w, map[string]string{"job_id": jobID})
}

// GET /api/import/mite/jobs/{id} — poll job status
func GetMiteImportJobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	importJobsMu.Lock()
	job, ok := miteImportJobs[jobID]
	importJobsMu.Unlock()
	if !ok {
		jsonError(w, "job not found", http.StatusNotFound)
		return
	}
	jsonOK(w, job)
}

// POST /api/import/mite/jobs/{id}/cancel — cancel a running import
func CancelMiteImportJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	importJobsMu.Lock()
	job, ok := miteImportJobs[jobID]
	importJobsMu.Unlock()
	if !ok {
		jsonError(w, "job not found", http.StatusNotFound)
		return
	}
	if job.cancel != nil {
		job.cancel()
	}
	jsonOK(w, map[string]string{"status": "cancelling"})
}

// DELETE /api/import/mite/entries — delete all mite-imported time entries (for re-import with correct users)
func DeleteMiteEntries(w http.ResponseWriter, r *http.Request) {
	result, err := db.DB.Exec("DELETE FROM time_entries WHERE mite_id IS NOT NULL")
	if err != nil {
		jsonError(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	count, _ := result.RowsAffected()
	jsonOK(w, map[string]any{"deleted": count})
}

// GET /api/import/mite/resume-date — returns the day after the latest mite entry
func GetMiteResumeDate(w http.ResponseWriter, r *http.Request) {
	var latestDate *string
	db.DB.QueryRow("SELECT date(MAX(started_at), '+1 day') FROM time_entries WHERE mite_id IS NOT NULL").Scan(&latestDate)
	if latestDate == nil {
		jsonOK(w, map[string]any{"resume_date": nil})
		return
	}
	jsonOK(w, map[string]any{"resume_date": *latestDate})
}

// ── Jira key extraction ──────────────────────────────────────────────────────

var jiraKeyRe = regexp.MustCompile(`([A-Z]{2,10}\d{0,4}-\d+)`)

func extractJiraKeys(note string) []string {
	return jiraKeyRe.FindAllString(note, -1)
}

// ── Mite API fetch ───────────────────────────────────────────────────────────

func miteGET(cfg *miteConfig, path string) ([]byte, int, error) {
	url := strings.TrimRight(cfg.BaseURL, "/") + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-MiteApiKey", cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

const maxMitePages = 200 // safety limit — mite returns ~100 entries per page

func fetchMiteEntries(ctx context.Context, cfg *miteConfig, fromDate, toDate, miteProjects string, job *miteImportJob) ([]miteTimeEntry, error) {
	var all []miteTimeEntry
	seen := map[int]bool{} // dedup by mite entry ID to detect loops
	page := 1
	for {
		if ctx.Err() != nil {
			return all, nil
		}
		if page > maxMitePages {
			return all, fmt.Errorf("safety limit: stopped after %d pages (%d entries)", maxMitePages, len(all))
		}

		path := fmt.Sprintf("/time_entries.json?from=%s&direction=asc&page=%d", fromDate, page)
		if toDate != "" {
			path += "&to=" + toDate
		}
		if miteProjects != "" {
			path += "&project_id=" + miteProjects
		}

		body, status, err := miteGET(cfg, path)
		if err != nil {
			return nil, fmt.Errorf("mite fetch page %d: %w", page, err)
		}
		if status != 200 {
			return nil, fmt.Errorf("mite returned %d: %s", status, truncate(string(body), 200))
		}

		var entries []miteTimeEntryWrapper
		if err := json.Unmarshal(body, &entries); err != nil {
			return nil, fmt.Errorf("parse mite response page %d: %w", page, err)
		}
		if len(entries) == 0 {
			break
		}

		// Check for duplicate entries (API returning same page = infinite loop)
		newCount := 0
		for _, e := range entries {
			if !seen[e.TimeEntry.ID] {
				seen[e.TimeEntry.ID] = true
				all = append(all, e.TimeEntry)
				newCount++
			}
		}
		if newCount == 0 {
			break // all entries on this page were duplicates — stop
		}

		if job != nil {
			importJobsMu.Lock()
			job.Total = len(all)
			job.PagesFetched = page
			importJobsMu.Unlock()
		}
		page++

		// Rate-limit API calls (200ms between pages)
		time.Sleep(200 * time.Millisecond)
	}
	return all, nil
}

// ── Import engine ────────────────────────────────────────────────────────────

func runMiteImport(ctx context.Context, cfg *miteConfig, req importMiteRequest, actorID int64, job *miteImportJob) (*miteImportResult, error) {
	res := &miteImportResult{
		Matched:           []miteMatchedDetail{},
		SkippedDuplicates: []miteDupDetail{},
		UnmatchedIssues:   []miteUnmatchedIssue{},
		UnmatchedUsers:    []miteUnmatchedUser{},
		Errors:            []miteErrorDetail{},
		ByProject:         []miteProjectSummary{},
		DryRun:            req.DryRun,
	}

	// Phase 1: Fetch
	if job != nil {
		importJobsMu.Lock()
		job.Phase = "fetching"
		importJobsMu.Unlock()
	}

	entries, err := fetchMiteEntries(ctx, cfg, req.FromDate, req.ToDate, req.MiteProjects, job)
	if err != nil {
		return nil, err
	}

	if job != nil {
		importJobsMu.Lock()
		job.Total = len(entries)
		job.Phase = "matching"
		importJobsMu.Unlock()
	}

	// Build global lookup: jira_id → {issue_id, issue_key, project_key}
	type issueRef struct {
		ID         int64
		Key        string
		ProjectKey string
	}
	jiraToIssue := map[string]issueRef{}
	rows, err := db.DB.Query(
		"SELECT i.id, i.issue_number, i.jira_id, p.key FROM issues i JOIN projects p ON p.id = i.project_id WHERE i.jira_id IS NOT NULL AND i.jira_id != ''",
	)
	if err != nil {
		return nil, fmt.Errorf("load issues: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var num int
		var jiraID, pKey string
		if err := rows.Scan(&id, &num, &jiraID, &pKey); err != nil {
			continue
		}
		jiraToIssue[jiraID] = issueRef{ID: id, Key: fmt.Sprintf("%s-%d", pKey, num), ProjectKey: pKey}
	}

	// Build user lookup: multiple keys → {user_id, rate}
	// Keys: username, first+last, email — all lowercased
	type userRef struct {
		ID   int64
		Rate *float64
	}
	userMap := map[string]userRef{}
	urows, err := db.DB.Query("SELECT id, username, first_name, last_name, email, internal_rate_hourly FROM users")
	if err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	defer urows.Close()
	for urows.Next() {
		var id int64
		var username, firstName, lastName, email string
		var rate *float64
		if err := urows.Scan(&id, &username, &firstName, &lastName, &email, &rate); err != nil {
			continue
		}
		ref := userRef{ID: id, Rate: rate}
		userMap[strings.ToLower(username)] = ref
		if email != "" {
			userMap[strings.ToLower(email)] = ref
		}
		if firstName != "" && lastName != "" {
			userMap[strings.ToLower(firstName+" "+lastName)] = ref
		}
	}

	// Fetch mite users to get note field (often contains PAIMOS username)
	miteUserMap := map[int]string{} // mite user_id → PAIMOS username from note
	miteUsersBody, miteUsersStatus, err := miteGET(cfg, "/users.json")
	if err == nil && miteUsersStatus == 200 {
		var miteUsers []struct {
			User struct {
				ID    int    `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
				Note  string `json:"note"`
			} `json:"user"`
		}
		if json.Unmarshal(miteUsersBody, &miteUsers) == nil {
			for _, mu := range miteUsers {
				// Try note field first (often the PAIMOS short username)
				note := strings.TrimSpace(mu.User.Note)
				if note != "" {
					if _, ok := userMap[strings.ToLower(note)]; ok {
						miteUserMap[mu.User.ID] = note
						continue
					}
				}
				// Try email
				if mu.User.Email != "" {
					if _, ok := userMap[strings.ToLower(mu.User.Email)]; ok {
						miteUserMap[mu.User.ID] = mu.User.Email
						continue
					}
				}
				// Try full name
				if _, ok := userMap[strings.ToLower(mu.User.Name)]; ok {
					miteUserMap[mu.User.ID] = mu.User.Name
				}
			}
		}
	}

	// Actor fallback
	var actorRate *float64
	if err := db.DB.QueryRow("SELECT internal_rate_hourly FROM users WHERE id=?", actorID).Scan(&actorRate); err != nil {
		log.Printf("scan error: %v", err)
	}

	// Build existing mite_id set for dedup (global)
	existingMiteIDs := map[int]bool{}
	mrows, err := db.DB.Query("SELECT mite_id FROM time_entries WHERE mite_id IS NOT NULL")
	if err != nil {
		return nil, fmt.Errorf("load existing mite ids: %w", err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var mid int
		if err := mrows.Scan(&mid); err != nil {
			continue
		}
		existingMiteIDs[mid] = true
	}

	// Track unmatched users and per-project stats
	unmatchedUserCounts := map[string]*miteUnmatchedUser{}
	projectStats := map[string]*miteProjectSummary{}

	// Phase 2: Import (or dry-run)
	if job != nil {
		importJobsMu.Lock()
		job.Phase = "importing"
		importJobsMu.Unlock()
	}

	// Collect issue IDs that received new entries (for deferred tag eval)
	affectedIssueIDs := map[int64]bool{}

	// Batch inserts in a transaction for performance
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for i, entry := range entries {
		if ctx.Err() != nil {
			break // cancelled — keep partial results
		}

		if job != nil {
			importJobsMu.Lock()
			job.Processed = i + 1
			importJobsMu.Unlock()
		}

		// Dedup
		if existingMiteIDs[entry.ID] {
			jiraKey := ""
			if keys := extractJiraKeys(entry.Note); len(keys) > 0 {
				jiraKey = keys[0]
			}
			res.SkippedDuplicates = append(res.SkippedDuplicates, miteDupDetail{
				MiteID: entry.ID, JiraKey: jiraKey,
			})
			if job != nil {
				importJobsMu.Lock()
				job.SkippedCount++
				importJobsMu.Unlock()
			}
			continue
		}

		// Extract Jira key(s)
		keys := extractJiraKeys(entry.Note)
		if len(keys) == 0 {
			res.UnmatchedIssues = append(res.UnmatchedIssues, miteUnmatchedIssue{
				MiteID: entry.ID, Note: truncate(entry.Note, 120),
				ExtractedKey: "", Reason: "no Jira key in note",
			})
			continue
		}
		jiraKey := keys[0]
		// Log warning if multiple keys found
		if len(keys) > 1 {
			res.UnmatchedIssues = append(res.UnmatchedIssues, miteUnmatchedIssue{
				MiteID: entry.ID, Note: truncate(entry.Note, 120),
				ExtractedKey: strings.Join(keys, ", "),
				Reason: fmt.Sprintf("multiple keys found, using first: %s", jiraKey),
			})
		}

		// Match to PAIMOS issue
		ref, found := jiraToIssue[jiraKey]
		if !found {
			res.UnmatchedIssues = append(res.UnmatchedIssues, miteUnmatchedIssue{
				MiteID: entry.ID, Note: truncate(entry.Note, 120),
				ExtractedKey: jiraKey, Reason: fmt.Sprintf("%s not found in any PAIMOS project", jiraKey),
			})
			continue
		}

		// Match user: mite user mapping (note/email/name) → PAIMOS user, then fallback to actor
		userID := actorID
		userRate := actorRate
		userName := entry.UserName
		matched := false
		// 1. Try mite user → PAIMOS mapping (from /users.json note/email/name)
		if mappedKey, ok := miteUserMap[entry.UserID]; ok {
			if u, ok := userMap[strings.ToLower(mappedKey)]; ok {
				userID = u.ID
				userRate = u.Rate
				matched = true
			}
		}
		// 2. Try direct name match as fallback
		if !matched {
			if u, ok := userMap[strings.ToLower(entry.UserName)]; ok {
				userID = u.ID
				userRate = u.Rate
				matched = true
			}
		}
		if !matched {
			key := fmt.Sprintf("%d:%s", entry.UserID, entry.UserName)
			if uu, ok := unmatchedUserCounts[key]; ok {
				uu.Count++
			} else {
				unmatchedUserCounts[key] = &miteUnmatchedUser{
					MiteUserName: entry.UserName,
					MiteUserID:   entry.UserID,
					Count:        1,
				}
			}
		}

		// Create time entry (or just count for dry-run)
		if !req.DryRun {
			override := float64(entry.Minutes) / 60.0
			startedAt := entry.DateAt + " 00:00:00"

			_, err := tx.Exec(`
				INSERT INTO time_entries(issue_id, user_id, started_at, stopped_at, override, comment, internal_rate_hourly, mite_id)
				VALUES(?,?,?,?,?,?,?,?)
			`, ref.ID, userID, startedAt, startedAt, override, entry.Note, userRate, entry.ID)
			if err != nil {
				res.Errors = append(res.Errors, miteErrorDetail{MiteID: entry.ID, Reason: err.Error()})
				if job != nil {
					importJobsMu.Lock()
					job.ErrorCount++
					importJobsMu.Unlock()
				}
				continue
			}
			affectedIssueIDs[ref.ID] = true
		}

		res.Imported++
		res.TotalMinutes += entry.Minutes
		res.Matched = append(res.Matched, miteMatchedDetail{
			MiteID: entry.ID, JiraKey: jiraKey, IssueKey: ref.Key,
			ProjectKey: ref.ProjectKey, Minutes: entry.Minutes, User: userName,
		})

		// Update per-project stats
		if ps, ok := projectStats[ref.ProjectKey]; ok {
			ps.Imported++
			ps.Minutes += entry.Minutes
		} else {
			projectStats[ref.ProjectKey] = &miteProjectSummary{
				ProjectKey: ref.ProjectKey, Imported: 1, Minutes: entry.Minutes,
			}
		}

		if job != nil {
			importJobsMu.Lock()
			job.MatchedCount++
			importJobsMu.Unlock()
		}
	}

	// Commit transaction (unless dry-run)
	if !req.DryRun {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit transaction: %w", err)
		}

		// Deferred tag evaluation — once per unique issue, not per entry
		for issueID := range affectedIssueIDs {
			EvaluateSystemTags(issueID)
		}
	}

	// Collect unmatched users
	for _, u := range unmatchedUserCounts {
		res.UnmatchedUsers = append(res.UnmatchedUsers, *u)
	}

	// Collect per-project summary
	for _, ps := range projectStats {
		res.ByProject = append(res.ByProject, *ps)
	}

	return res, nil
}
