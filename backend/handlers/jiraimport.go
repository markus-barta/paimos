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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/storage"
	"github.com/go-chi/chi/v5"
)

// ── Import job store (in-memory) ─────────────────────────────────────────────

type importJob struct {
	ID          string        `json:"id"`
	Status      string        `json:"status"` // "running", "complete", "error"
	Result      *importResult `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
	Started     time.Time     `json:"started"`
	Finished    *time.Time    `json:"finished,omitempty"`
	Total       int           `json:"total"`
	Processed   int           `json:"processed"`
	CurrentKey  string        `json:"current_key,omitempty"`
	Phase       string        `json:"phase,omitempty"` // "fetching", "importing", "linking"
}

var (
	importJobs   = map[string]*importJob{}
	importJobsMu sync.Mutex
)

func newJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Jira API types ────────────────────────────────────────────────────────────

type jiraSearchResp struct {
	Issues        []jiraIssue `json:"issues"`
	Total         int         `json:"total"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

type jiraIssue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Fields jiraFields  `json:"fields"`
}

type jiraFields struct {
	Summary              string        `json:"summary"`
	Description          any           `json:"description"` // ADF or string
	IssueType            jiraNamedObj  `json:"issuetype"`
	Status               jiraNamedObj  `json:"status"`
	Priority             *jiraNamedObj `json:"priority"`
	Assignee             *jiraUser     `json:"assignee"`
	Labels               []string      `json:"labels"`
	TimeOriginalEstimate *int          `json:"timeoriginalestimate"` // seconds
	StoryPoints          *float64      `json:"story_points"`
	DueDate              *string       `json:"duedate"`
	Created              string        `json:"created"`
	Updated              string        `json:"updated"`
	FixVersions          []struct {
		Name string `json:"name"`
	} `json:"fixVersions"`
	Parent *struct {
		Key string `json:"key"`
	} `json:"parent"`
	Epic *struct {
		Key string `json:"key"`
	} `json:"epic"`
	Comment *struct {
		Comments []jiraComment `json:"comments"`
	} `json:"comment"`
	Attachment  []jiraAttachment `json:"attachment"`
	IssueLinks  []jiraIssueLink  `json:"issuelinks"`
	// Custom fields (paimos Jira)
	CustomStory  any      `json:"customfield_10801"` // Story / user story text (ADF)
	CustomAC     any      `json:"customfield_10100"` // Acceptance Criteria (ADF)
	CustomNotes  any      `json:"customfield_10400"` // Notes (ADF)
	CustomArLp   *float64 `json:"customfield_11705"` // AR LP (Actual Result Leistungspunkte)
	CustomRateLp *float64 `json:"customfield_11703"` // Rate LP (€ per Leistungspunkt)
}

type jiraIssueLink struct {
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
	InwardIssue *struct {
		Key string `json:"key"`
	} `json:"inwardIssue"`
	OutwardIssue *struct {
		Key string `json:"key"`
	} `json:"outwardIssue"`
}

type jiraAttachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	Content  string `json:"content"` // download URL
}

type jiraNamedObj struct{ Name string `json:"name"` }
type jiraUser     struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

type jiraComment struct {
	Author  jiraUser `json:"author"`
	Body    any      `json:"body"` // ADF or string
	Created string   `json:"created"`
}

// ── Import request ────────────────────────────────────────────────────────────

type importJiraRequest struct {
	ProjectKey      string            `json:"project_key"`
	TargetProjectID int64             `json:"target_project_id"`
	NewProjectName  string            `json:"new_project_name"`  // create new project if set
	TypeMap         map[string]string `json:"type_map"`
	StatusMap       map[string]string `json:"status_map"`
	PriorityMap     map[string]string `json:"priority_map"`
	Options         importOptions     `json:"options"`
}

type importOptions struct {
	Overwrite          bool     `json:"overwrite"`
	CollisionSuffix    string   `json:"collision_suffix"`
	SkipDone           bool     `json:"skip_done"`
	SkipTypes          []string `json:"skip_types"`
	ImportLabelsAsTags bool     `json:"import_labels_as_tags"`
	ImportComments     bool     `json:"import_comments"`
	ImportAttachments  bool     `json:"import_attachments"`
	CreateImportTag    bool     `json:"create_import_tag"`
}

type importResult struct {
	Imported        int             `json:"imported"`
	Updated         int             `json:"updated"`
	Skipped         int             `json:"skipped"`
	SkippedDetails  []skippedDetail `json:"skipped_details"`
	Errors          []importError   `json:"errors"`
	TargetProjectID int64           `json:"target_project_id"`
	ImportTag       string          `json:"import_tag"`
}

type skippedDetail struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

type importError struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// GET /api/import/jira/projects — list accessible Jira projects (all pages)
func ListJiraProjects(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadJiraConfig()
	if err != nil || cfg.Host == "" || cfg.Token == "" {
		jsonError(w, "Jira not configured", http.StatusBadRequest)
		return
	}

	type jiraProject struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}

	var all []jiraProject
	startAt := 0
	for {
		path := fmt.Sprintf("/rest/api/3/project/search?maxResults=100&orderBy=name&startAt=%d", startAt)
		body, status, err := jiraGET(cfg, path)
		if err != nil || status != 200 {
			jsonError(w, fmt.Sprintf("Jira error %d: %s", status, truncate(string(body), 120)), http.StatusBadGateway)
			return
		}

		var resp struct {
			Values  []jiraProject `json:"values"`
			IsLast  bool          `json:"isLast"`
			Total   int           `json:"total"`
			StartAt int           `json:"startAt"`
		}
		if err := json.Unmarshal(body, &resp); err != nil || len(resp.Values) == 0 {
			// fallback: plain array (older Jira API versions)
			var arr []jiraProject
			json.Unmarshal(body, &arr)
			all = append(all, arr...)
			break
		}
		all = append(all, resp.Values...)
		startAt += len(resp.Values)
		if resp.IsLast || startAt >= resp.Total || len(resp.Values) == 0 {
			break
		}
	}

	if all == nil {
		all = []jiraProject{}
	}
	jsonOK(w, all)
}

// POST /api/import/jira — start async import, return job ID immediately
func ImportFromJira(w http.ResponseWriter, r *http.Request) {
	var req importJiraRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ProjectKey == "" {
		jsonError(w, "project_key required", http.StatusBadRequest)
		return
	}
	if req.TargetProjectID == 0 && req.NewProjectName == "" {
		jsonError(w, "target_project_id or new_project_name required", http.StatusBadRequest)
		return
	}

	cfg, err := loadJiraConfig()
	if err != nil || cfg.Host == "" || cfg.Token == "" {
		jsonError(w, "Jira not configured", http.StatusBadRequest)
		return
	}

	// Create new project if requested (synchronous — fast)
	if req.NewProjectName != "" && req.TargetProjectID == 0 {
		key := deriveProjectKey(req.NewProjectName)
		res, err := db.DB.Exec(
			`INSERT INTO projects(name, key, description, status) VALUES(?,?,?,?)`,
			req.NewProjectName, key, fmt.Sprintf("Imported from Jira project %s", req.ProjectKey), "active",
		)
		if err != nil {
			jsonError(w, "failed to create project: "+err.Error(), http.StatusInternalServerError)
			return
		}
		req.TargetProjectID, _ = res.LastInsertId()
	}

	actor := auth.GetUser(r)
	jobID := newJobID()
	job := &importJob{ID: jobID, Status: "running", Started: time.Now().UTC()}

	importJobsMu.Lock()
	importJobs[jobID] = job
	importJobsMu.Unlock()

	// Run import in background goroutine
	go func() {
		result, err := runJiraImport(cfg, req, actor.ID, job)
		now := time.Now().UTC()
		importJobsMu.Lock()
		defer importJobsMu.Unlock()
		job.Finished = &now
		if err != nil {
			job.Status = "error"
			job.Error = err.Error()
		} else {
			job.Status = "complete"
			job.Result = result
		}
	}()

	jsonOK(w, map[string]string{"job_id": jobID})
}

// GET /api/import/jira/jobs/{id} — poll job status
func GetImportJobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	importJobsMu.Lock()
	job, ok := importJobs[jobID]
	importJobsMu.Unlock()
	if !ok {
		jsonError(w, "job not found", http.StatusNotFound)
		return
	}
	jsonOK(w, job)
}

// deriveProjectKey turns a project name into a short uppercase key (max 8 chars).
func deriveProjectKey(name string) string {
	words := strings.Fields(strings.ToUpper(name))
	if len(words) == 0 {
		return "IMP"
	}
	if len(words) == 1 {
		k := words[0]
		if len(k) > 8 {
			k = k[:8]
		}
		return k
	}
	key := ""
	for _, w := range words {
		if len(w) > 0 {
			key += string(w[0])
		}
		if len(key) >= 6 {
			break
		}
	}
	return key
}

// ── Core import logic ─────────────────────────────────────────────────────────

func runJiraImport(cfg *jiraConfig, req importJiraRequest, actorID int64, job *importJob) (*importResult, error) {
	res := &importResult{Errors: []importError{}, SkippedDetails: []skippedDetail{}, TargetProjectID: req.TargetProjectID}

	// Optional import tag: JI + timestamp e.g. JI26030515h52m59s.
	// Off by default —. Only created when the user ticks
	// "Create import tag" in the Jira Import UI.
	var importTagID int64
	if req.Options.CreateImportTag {
		importTag := "JI" + time.Now().UTC().Format("060102T15h04m05s")
		tagRes, err := db.DB.Exec(
			`INSERT INTO tags(name, color, description) VALUES(?,?,?)`,
			importTag, "indigo", fmt.Sprintf("Auto-tag for Jira import of %s", req.ProjectKey),
		)
		if err == nil {
			importTagID, _ = tagRes.LastInsertId()
			res.ImportTag = importTag
		}
	}

	if job != nil {
		importJobsMu.Lock()
		job.Phase = "fetching"
		importJobsMu.Unlock()
	}

	// Fetch all issues (paginated) using /rest/api/3/search/jql with cursor-based nextPageToken.
	// The legacy /rest/api/3/search endpoint has been removed by Atlassian.
	var allIssues []jiraIssue
	seenKeys := map[string]bool{}
	fields := "summary,description,issuetype,status,priority,assignee,labels,parent,comment,attachment,issuelinks,timeoriginalestimate,story_points,fixVersions,duedate,created,updated,customfield_10801,customfield_10100,customfield_10400,customfield_11705,customfield_11703"
	jql := url.QueryEscape(fmt.Sprintf("project=%s ORDER BY created ASC", req.ProjectKey))
	nextPageToken := ""
	for {
		path := fmt.Sprintf("/rest/api/3/search/jql?jql=%s&maxResults=100&fields=%s", jql, fields)
		if nextPageToken != "" {
			path += "&nextPageToken=" + url.QueryEscape(nextPageToken)
		}
		body, status, err := jiraGET(cfg, path)
		if err != nil {
			return nil, fmt.Errorf("jira fetch: %w", err)
		}
		if status != 200 {
			return nil, fmt.Errorf("jira returned %d: %s", status, truncate(string(body), 200))
		}
		var resp jiraSearchResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parse jira response: %w", err)
		}
		newCount := 0
		for _, iss := range resp.Issues {
			if !seenKeys[iss.Key] {
				seenKeys[iss.Key] = true
				allIssues = append(allIssues, iss)
				newCount++
			}
		}
		// Stop conditions: no results, dedup caught infinite loop, no more pages, or safety cap
		if len(resp.Issues) == 0 || newCount == 0 || resp.NextPageToken == "" || len(allIssues) >= 5000 {
			break
		}
		nextPageToken = resp.NextPageToken
	}

	// Build email → PAIMOS user ID map
	userEmailMap := map[string]int64{}
	rows, _ := db.DB.Query("SELECT id, username FROM users")
	if rows != nil {
		defer rows.Close()
		// We can't reliably match by email since PAIMOS doesn't store email — match by username / displayName
		// Store by id for admin fallback
		rows.Close()
	}
	// email→id from users (best effort — if Jira email matches PAIMOS username)
	emailRows, _ := db.DB.Query("SELECT id, username FROM users")
	if emailRows != nil {
		for emailRows.Next() {
			var id int64; var uname string
			emailRows.Scan(&id, &uname)
			userEmailMap[uname] = id
		}
		emailRows.Close()
	}

	// Pass 1: create / update issues, collect jiraKey → issueID map for parent linking
	jiraKeyToIssueID := map[string]int64{}

	// Update job progress: fetching complete, now importing
	if job != nil {
		importJobsMu.Lock()
		job.Total = len(allIssues)
		job.Phase = "importing"
		importJobsMu.Unlock()
	}

	// Build skip-types set for O(1) lookup
	skipTypesSet := map[string]bool{}
	for _, t := range req.Options.SkipTypes {
		skipTypesSet[t] = true
	}

	for _, ji := range allIssues {
		// Skip disabled Jira issue types
		if skipTypesSet[ji.Fields.IssueType.Name] {
			res.Skipped++
			res.SkippedDetails = append(res.SkippedDetails, skippedDetail{Key: ji.Key, Reason: fmt.Sprintf("type %q excluded", ji.Fields.IssueType.Name)})
			continue
		}

		bpType := mapVal(req.TypeMap, ji.Fields.IssueType.Name, "ticket")
		bpStatus := normaliseImportStatus(mapVal(req.StatusMap, ji.Fields.Status.Name, ""))
		bpPriority := "medium"
		if ji.Fields.Priority != nil {
			bpPriority = mapVal(req.PriorityMap, ji.Fields.Priority.Name, "medium")
		}

		// skip_done — uses canonical status values
		if req.Options.SkipDone && (bpStatus == "done" || bpStatus == "delivered" || bpStatus == "cancelled") {
			res.Skipped++
			res.SkippedDetails = append(res.SkippedDetails, skippedDetail{Key: ji.Key, Reason: fmt.Sprintf("status %q (skip done)", bpStatus)})
			continue
		}

		// Assignee
		var assigneeID *int64
		if ji.Fields.Assignee != nil {
			// try email match, then displayName match
			if id, ok := userEmailMap[ji.Fields.Assignee.EmailAddress]; ok {
				assigneeID = &id
			} else if id, ok := userEmailMap[ji.Fields.Assignee.DisplayName]; ok {
				assigneeID = &id
			}
		}

		// Description: standard field first, fall back to custom Story field (customfield_10801)
		description := adfToText(ji.Fields.Description)
		if description == "" {
			description = adfToText(ji.Fields.CustomStory)
		}
		// Acceptance criteria from custom field (customfield_10100)
		acceptanceCriteria := adfToText(ji.Fields.CustomAC)
		// Notes from custom field (customfield_10400)
		notes := adfToText(ji.Fields.CustomNotes)

		title := ji.Fields.Summary
		if title == "" {
			title = ji.Key
		}

		// Map Jira fields → PAIMOS fields
		var estimateHours *float64
		if ji.Fields.TimeOriginalEstimate != nil && *ji.Fields.TimeOriginalEstimate > 0 {
			h := float64(*ji.Fields.TimeOriginalEstimate) / 3600.0
			estimateHours = &h
		}
		var estimateLp *float64
		if ji.Fields.StoryPoints != nil && *ji.Fields.StoryPoints > 0 {
			estimateLp = ji.Fields.StoryPoints
		}
		var arLp *float64
		if ji.Fields.CustomArLp != nil && *ji.Fields.CustomArLp > 0 {
			arLp = ji.Fields.CustomArLp
		}
		var rateLp *float64
		if ji.Fields.CustomRateLp != nil && *ji.Fields.CustomRateLp > 0 {
			rateLp = ji.Fields.CustomRateLp
		}
		var jiraVersion string
		if len(ji.Fields.FixVersions) > 0 {
			jiraVersion = ji.Fields.FixVersions[0].Name
		}
		var endDate string
		if ji.Fields.DueDate != nil && *ji.Fields.DueDate != "" {
			endDate = *ji.Fields.DueDate
		}
		// Use Jira timestamps (truncate to YYYY-MM-DD HH:MM:SS)
		createdAt := time.Now().UTC().Format("2006-01-02 15:04:05")
		updatedAt := createdAt
		if ji.Fields.Created != "" {
			if t, err := time.Parse("2006-01-02T15:04:05.000-0700", ji.Fields.Created); err == nil {
				createdAt = t.UTC().Format("2006-01-02 15:04:05")
			}
		}
		if ji.Fields.Updated != "" {
			if t, err := time.Parse("2006-01-02T15:04:05.000-0700", ji.Fields.Updated); err == nil {
				updatedAt = t.UTC().Format("2006-01-02 15:04:05")
			}
		}
		// Build jira_text with key metadata
		jiraText := fmt.Sprintf("%s | type=%s status=%s", ji.Key, ji.Fields.IssueType.Name, ji.Fields.Status.Name)
		if ji.Fields.Priority != nil {
			jiraText += " priority=" + ji.Fields.Priority.Name
		}
		if jiraVersion != "" {
			jiraText += " version=" + jiraVersion
		}

		// Check collision by jira_id (clean, indexed)
		var existingID int64
		collisionReason := ""
		err := db.DB.QueryRow(
			"SELECT id FROM issues WHERE project_id=? AND jira_id=? LIMIT 1",
			req.TargetProjectID, ji.Key,
		).Scan(&existingID)
		hasCollision := err == nil && existingID > 0
		if hasCollision {
			collisionReason = "duplicate jira_id"
		}

		// Fallback: check by title only for legacy imports (issues with no jira_id).
		// Never match issues that already have a different jira_id — that causes
		// false collisions when multiple Jira issues share a title (e.g. "CR", "DR").
		if !hasCollision {
			db.DB.QueryRow(
				"SELECT id FROM issues WHERE project_id=? AND title=? AND jira_id IS NULL LIMIT 1",
				req.TargetProjectID, title,
			).Scan(&existingID)
			hasCollision = existingID > 0
			if hasCollision {
				collisionReason = "duplicate title (legacy)"
			}
		}

		if hasCollision && !req.Options.Overwrite {
			res.Skipped++
			res.SkippedDetails = append(res.SkippedDetails, skippedDetail{Key: ji.Key, Reason: fmt.Sprintf("%s — already exists, use overwrite to update", collisionReason)})
			jiraKeyToIssueID[ji.Key] = existingID
			continue
		}

		var issueID int64
		if hasCollision && req.Options.Overwrite && existingID > 0 {
			// Update existing
			_, err := db.DB.Exec(`
				UPDATE issues SET title=?, description=?, acceptance_criteria=?, notes=?, type=?, status=?, priority=?, assignee_id=?,
				jira_id=?, jira_text=?, jira_version=?, estimate_hours=?, estimate_lp=?,
				ar_lp=?, rate_lp=?,
				end_date=?, release=?, updated_at=? WHERE id=?
			`, title, description, acceptanceCriteria, notes, bpType, bpStatus, bpPriority, assigneeID,
				ji.Key, jiraText, jiraVersion, estimateHours, estimateLp,
				arLp, rateLp,
				endDate, jiraVersion, updatedAt, existingID)
			if err != nil {
				res.Errors = append(res.Errors, importError{Key: ji.Key, Reason: err.Error()})
				continue
			}
			issueID = existingID
			res.Updated++
		} else {
			// Assign next issue_number atomically (same pattern as CreateIssue)
			var nextNum int
			if err := db.DB.QueryRow(
				"SELECT COALESCE(MAX(issue_number),0)+1 FROM issues WHERE project_id=?",
				req.TargetProjectID,
			).Scan(&nextNum); err != nil {
				res.Errors = append(res.Errors, importError{Key: ji.Key, Reason: "numbering: " + err.Error()})
				continue
			}

			// Insert new — full field parity with normal CREATE
			sqlRes, err := db.DB.Exec(`
				INSERT INTO issues(project_id, issue_number, title, description, acceptance_criteria, notes, type, status, priority,
					assignee_id, jira_id, jira_text, jira_version,
					estimate_hours, estimate_lp, ar_lp, rate_lp, end_date, release,
					created_by, created_at, updated_at)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, req.TargetProjectID, nextNum, title, description, acceptanceCriteria, notes, bpType, bpStatus, bpPriority, assigneeID,
				ji.Key, jiraText, jiraVersion,
				estimateHours, estimateLp, arLp, rateLp, endDate, jiraVersion,
				actorID, createdAt, updatedAt)
			if err != nil {
				res.Errors = append(res.Errors, importError{Key: ji.Key, Reason: err.Error()})
				continue
			}
			issueID, _ = sqlRes.LastInsertId()
			res.Imported++
		}

		jiraKeyToIssueID[ji.Key] = issueID

		// Update progress
		if job != nil {
			importJobsMu.Lock()
			job.Processed++
			job.CurrentKey = ji.Key
			importJobsMu.Unlock()
		}

		// Attach import tag to every issue
		if importTagID > 0 {
			if _, err := db.DB.Exec(
				"INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)",
				issueID, importTagID,
			); err != nil {
				log.Printf("JiraImport: attach import tag issue=%d: %v", issueID, err)
			}
		}

		// Labels → tags
		if req.Options.ImportLabelsAsTags && len(ji.Fields.Labels) > 0 {
			for _, label := range ji.Fields.Labels {
				if label == "" {
					continue
				}
				var tagID int64
				err := db.DB.QueryRow("SELECT id FROM tags WHERE name=?", label).Scan(&tagID)
				if err != nil {
					// create tag
					r2, err := db.DB.Exec(
						"INSERT INTO tags(name,color,description) VALUES(?,?,?)",
						label, "gray", fmt.Sprintf("Imported from Jira"),
					)
					if err != nil {
						log.Printf("JiraImport: create tag %q: %v", label, err)
						continue
					}
					tagID, _ = r2.LastInsertId()
				}
				if tagID > 0 {
					if _, err := db.DB.Exec(
						"INSERT OR IGNORE INTO issue_tags(issue_id,tag_id) VALUES(?,?)",
						issueID, tagID,
					); err != nil {
						log.Printf("JiraImport: attach tag issue=%d tag=%d: %v", issueID, tagID, err)
					}
				}
			}
		}

		// Comments
		if req.Options.ImportComments && ji.Fields.Comment != nil {
			for _, jc := range ji.Fields.Comment.Comments {
				var commentAuthorID *int64
				if id, ok := userEmailMap[jc.Author.EmailAddress]; ok {
					commentAuthorID = &id
				} else if id, ok := userEmailMap[jc.Author.DisplayName]; ok {
					commentAuthorID = &id
				} else {
					commentAuthorID = &actorID // fallback to importing user
				}
				created := jc.Created
				if len(created) > 19 {
					created = created[:19]
				}
				body := fmt.Sprintf("[%s %s]\n%s", created, jc.Author.DisplayName, adfToText(jc.Body))
				if _, err := db.DB.Exec(
					"INSERT INTO comments(issue_id,author_id,body,created_at) VALUES(?,?,?,?)",
					issueID, commentAuthorID, body, created,
				); err != nil {
					log.Printf("JiraImport: insert comment issue=%d: %v", issueID, err)
				}
			}
		}

		// Attachments — download from Jira, upload to MinIO
		if req.Options.ImportAttachments && storage.Enabled() && len(ji.Fields.Attachment) > 0 {
			for _, att := range ji.Fields.Attachment {
				if att.Content == "" || att.Size > 10*1024*1024 { // skip >10MB
					continue
				}
				// Check if already imported (by filename + issue)
				var exists int
				db.DB.QueryRow("SELECT 1 FROM attachments WHERE issue_id=? AND filename=?", issueID, att.Filename).Scan(&exists)
				if exists > 0 {
					continue
				}
				// Download from Jira (direct URL, not via jiraGET)
				attReq, err := http.NewRequest(http.MethodGet, att.Content, nil)
				if err != nil {
					continue
				}
				attReq.SetBasicAuth(cfg.Email, cfg.Token)
				attResp, err := (&http.Client{Timeout: 60 * time.Second}).Do(attReq)
				if err != nil || attResp.StatusCode != 200 {
					if attResp != nil {
						attResp.Body.Close()
					}
					continue
				}
				attData, err := io.ReadAll(attResp.Body)
				attResp.Body.Close()
				if err != nil {
					continue
				}
				// Upload to MinIO
				objectKey := fmt.Sprintf("issues/%d/%s", issueID, att.Filename)
				ct := att.MimeType
				if ct == "" {
					ct = "application/octet-stream"
				}
				if err := storage.Put(context.Background(), objectKey, ct, bytes.NewReader(attData), int64(len(attData))); err != nil {
					continue
				}
				// Create DB record
				if _, err := db.DB.Exec(
					"INSERT INTO attachments(issue_id, filename, content_type, size, object_key, uploaded_by) VALUES(?,?,?,?,?,?)",
					issueID, att.Filename, ct, len(attData), objectKey, actorID,
				); err != nil {
					log.Printf("JiraImport: insert attachment issue=%d file=%s: %v", issueID, att.Filename, err)
				}
			}
		}
	}

	// Pass 2: resolve parent links
	for _, ji := range allIssues {
		issueID, ok := jiraKeyToIssueID[ji.Key]
		if !ok {
			continue
		}
		parentKey := ""
		if ji.Fields.Parent != nil {
			parentKey = ji.Fields.Parent.Key
		} else if ji.Fields.Epic != nil {
			parentKey = ji.Fields.Epic.Key
		}
		if parentKey == "" {
			continue
		}
		parentID, ok := jiraKeyToIssueID[parentKey]
		if !ok {
			continue
		}
		if _, err := db.DB.Exec("UPDATE issues SET parent_id=? WHERE id=?", parentID, issueID); err != nil {
			log.Printf("JiraImport: set parent id=%d parent=%d: %v", issueID, parentID, err)
		}
	}

	// Pass 3: resolve issue links (Relates, Blocks, etc.) → PAIMOS issue_relations
	for _, ji := range allIssues {
		issueID, ok := jiraKeyToIssueID[ji.Key]
		if !ok {
			continue
		}
		for _, link := range ji.Fields.IssueLinks {
			// Map Jira link type to PAIMOS relation type
			relationType := ""
			switch link.Type.Name {
			case "Relates":
				relationType = "impacts"
			case "Blocks":
				relationType = "depends_on"
			default:
				continue // skip unknown link types
			}
			// Get the linked issue's PAIMOS ID
			linkedKey := ""
			if link.OutwardIssue != nil {
				linkedKey = link.OutwardIssue.Key
			} else if link.InwardIssue != nil {
				linkedKey = link.InwardIssue.Key
			}
			if linkedKey == "" {
				continue
			}
			linkedID, ok := jiraKeyToIssueID[linkedKey]
			if !ok {
				continue // linked issue not in this import
			}
			// Avoid self-links and duplicates
			if issueID == linkedID {
				continue
			}
			if _, err := db.DB.Exec(
				"INSERT OR IGNORE INTO issue_relations(source_id, target_id, type) VALUES(?,?,?)",
				issueID, linkedID, relationType,
			); err != nil {
				log.Printf("JiraImport: insert relation id=%d linked=%d type=%s: %v", issueID, linkedID, relationType, err)
			}
		}
	}

	return res, nil
}

// GET /api/import/jira/debug?project_key=PSC26&limit=3 — fetch raw Jira issues for diagnosis
func DebugJiraFetch(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadJiraConfig()
	if err != nil || cfg.Host == "" || cfg.Token == "" {
		jsonError(w, "Jira not configured", http.StatusBadRequest)
		return
	}
	projectKey := r.URL.Query().Get("project_key")
	if projectKey == "" {
		jsonError(w, "project_key required", http.StatusBadRequest)
		return
	}
	limit := 3
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	fields := "summary,description,issuetype,status,priority"
	if r.URL.Query().Get("all_fields") == "true" {
		fields = "*all"
	}
	issueKey := r.URL.Query().Get("issue_key")
	var jql string
	if issueKey != "" {
		jql = url.QueryEscape(fmt.Sprintf("key=%s", issueKey))
	} else {
		jql = url.QueryEscape(fmt.Sprintf("project=%s ORDER BY created ASC", projectKey))
	}

	allFieldsMode := r.URL.Query().Get("all_fields") == "true"

	// Paginate to get ALL issues (same logic as import)
	type diagIssue struct {
		Key             string         `json:"key"`
		Summary         string         `json:"summary"`
		RawDescription  any            `json:"raw_description"`
		AdfToTextResult string         `json:"adf_to_text_result"`
		DescType        string         `json:"desc_type"`
		IssueType       string         `json:"issue_type"`
		Status          string         `json:"status"`
		Priority        string         `json:"priority"`
		RawFields       map[string]any `json:"raw_fields,omitempty"`
	}
	var diag []diagIssue
	seenKeys := map[string]bool{}
	debugNextToken := ""
	for {
		pageSize := 100
		if limit > 0 && limit-len(diag) < pageSize {
			pageSize = limit - len(diag)
		}
		if pageSize <= 0 {
			break
		}
		path := fmt.Sprintf("/rest/api/3/search/jql?jql=%s&maxResults=%d&fields=%s", jql, pageSize, fields)
		if debugNextToken != "" {
			path += "&nextPageToken=" + url.QueryEscape(debugNextToken)
		}
		body, status, err := jiraGET(cfg, path)
		if err != nil {
			jsonError(w, "jira fetch: "+err.Error(), http.StatusBadGateway)
			return
		}
		if status != 200 {
			jsonError(w, fmt.Sprintf("jira %d: %s", status, truncate(string(body), 200)), http.StatusBadGateway)
			return
		}
		var raw map[string]any
		json.Unmarshal(body, &raw)
		issues, _ := raw["issues"].([]any)
		rawNextToken, _ := raw["nextPageToken"].(string)
		for _, iss := range issues {
			im, _ := iss.(map[string]any)
			key, _ := im["key"].(string)
			f, _ := im["fields"].(map[string]any)
			summary, _ := f["summary"].(string)
			desc := f["description"]
			issType := ""
			if it, ok := f["issuetype"].(map[string]any); ok {
				issType, _ = it["name"].(string)
			}
			issStatus := ""
			if st, ok := f["status"].(map[string]any); ok {
				issStatus, _ = st["name"].(string)
			}
			issPriority := ""
			if pr, ok := f["priority"].(map[string]any); ok {
				issPriority, _ = pr["name"].(string)
			}
			if seenKeys[key] {
				continue
			}
			seenKeys[key] = true
			d := diagIssue{
				Key:             key,
				Summary:         summary,
				RawDescription:  desc,
				AdfToTextResult: adfToText(desc),
				DescType:        fmt.Sprintf("%T", desc),
				IssueType:       issType,
				Status:          issStatus,
				Priority:        issPriority,
			}
			if allFieldsMode {
				d.RawFields = f
			}
			diag = append(diag, d)
		}
		if len(issues) == 0 || rawNextToken == "" || len(diag) >= 5000 {
			break
		}
		debugNextToken = rawNextToken
	}
	if diag == nil {
		diag = []diagIssue{}
	}

	jsonOK(w, map[string]any{
		"total":  len(diag),
		"issues": diag,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func jiraGET(cfg *jiraConfig, path string) ([]byte, int, error) {
	u := strings.TrimRight(cfg.Host, "/") + path
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(cfg.Email, cfg.Token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func mapVal(m map[string]string, key, fallback string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return fallback
}

// adfToText extracts plain text from an Atlassian Document Format object or
// returns the string directly if the field is already a plain string.
func adfToText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// ADF: recursively collect text nodes
	if m, ok := v.(map[string]any); ok {
		return adfNodeText(m)
	}
	return fmt.Sprintf("%v", v)
}

func adfNodeText(node map[string]any) string {
	var sb strings.Builder
	nodeType, _ := node["type"].(string)

	switch nodeType {
	case "text":
		if text, ok := node["text"].(string); ok {
			sb.WriteString(text)
		}
		return sb.String()
	case "hardBreak":
		return "\n"
	case "mention":
		if text, ok := node["attrs"].(map[string]any)["text"].(string); ok {
			sb.WriteString(text)
		}
		return sb.String()
	case "inlineCard":
		if attrs, ok := node["attrs"].(map[string]any); ok {
			if u, ok := attrs["url"].(string); ok {
				sb.WriteString(u)
			}
		}
		return sb.String()
	}

	// Recurse into content children
	if content, ok := node["content"].([]any); ok {
		for i, child := range content {
			if cm, ok := child.(map[string]any); ok {
				sb.WriteString(adfNodeText(cm))
				// Add separator between list items
				if nodeType == "bulletList" || nodeType == "orderedList" {
					_ = i // list items handled via listItem prefix
				}
			}
		}
	}

	// Add formatting after block-level nodes
	switch nodeType {
	case "paragraph", "heading":
		sb.WriteString("\n")
	case "listItem":
		sb.WriteString("\n")
	case "bulletList", "orderedList":
		// extra newline after list blocks
	case "codeBlock":
		sb.WriteString("\n")
	case "blockquote":
		sb.WriteString("\n")
	case "rule":
		sb.WriteString("---\n")
	}

	// Prefix list items with bullet/number marker
	if nodeType == "bulletList" {
		text := sb.String()
		sb.Reset()
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			sb.WriteString("- " + line + "\n")
		}
	}

	return sb.String()
}
