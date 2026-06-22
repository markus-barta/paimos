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
	"database/sql"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/db"
)

// PAI-343 — lesson-capture trigger detection.
//
// When a ticket is being moved to a terminal state (done / delivered /
// cancelled), the UI / CLI asks the server: "should I prompt for a
// memory entry?". The decision splits cleanly from the rendering
// layer so trigger logic is testable in isolation and reusable from
// the CLI's `--draft-memory` headless path.
//
// GET /api/issues/:id/lesson-capture-prompt
//   → 200 { "should_prompt": bool, "suggested_name"?: string,
//           "reason"?: string, "ticket_key"?: string }
//
// The endpoint never writes — it only reads tags / parent / comments
// off the issue. Caller is responsible for triggering it at the right
// moment (e.g. when the user opens the status dropdown to a terminal
// value, or when `paimos issue update --status done` runs).
//
// Trigger rules (any one is sufficient):
//   1. ticket has a tag matching the lesson-tag pattern
//      (incident / bug / BUG / postmortem)
//   2. ticket's parent (or any ancestor) is an epic whose title
//      contains [POST-LAUNCH] / Hardening / audit / incident
//   3. comments contain heuristic phrases
//      ("lesson", "learned", "next time")
//
// Each rule contributes a human-readable reason so the UI can show
// the user *why* the prompt fired. Reasons stack — multiple matches
// produce a "+"-joined message.

// lessonCaptureLessonTagPattern matches tag names that flag a ticket
// as worth capturing. Case-insensitive substring match keeps the rule
// resilient to minor casing variations (BUG / bug / Bug, incident /
// Incident). The list is intentionally small for v1; PAI-343's spec
// calls these "operator-configurable" — that's a follow-up once the
// SystemSettings surface gains a `lesson_capture_tags` field.
var lessonCaptureLessonTagPattern = regexp.MustCompile(`(?i)(incident|bug|postmortem|outage|regression)`)

// lessonCaptureEpicTitlePattern matches parent-epic titles that
// indicate a hardening / audit / incident cluster — the kinds of
// projects whose tickets are most likely to teach a lesson.
var lessonCaptureEpicTitlePattern = regexp.MustCompile(`(?i)(\[POST-LAUNCH\]|hardening|audit|incident|postmortem|outage)`)

// lessonCaptureCommentPhrases are the heuristic phrases that, if
// they appear in any comment body, raise the trigger. Lowercased once
// at init time so the per-ticket scan is a flat substring loop.
var lessonCaptureCommentPhrases = []string{
	"lesson",
	"learned",
	"next time",
	"should have",
	"in hindsight",
}

// LessonCapturePromptResponse is the JSON shape returned by the
// endpoint. Fields beyond should_prompt are only populated when the
// trigger fires — the empty case stays a one-key object.
type LessonCapturePromptResponse struct {
	ShouldPrompt  bool   `json:"should_prompt"`
	Reason        string `json:"reason,omitempty"`
	SuggestedName string `json:"suggested_name,omitempty"`
	TicketKey     string `json:"ticket_key,omitempty"`
}

// LessonCapturePrompt powers GET /api/issues/:id/lesson-capture-prompt.
// Returns whether the lesson-capture flow should be surfaced for this
// ticket, plus a human-readable reason and a suggested memory slug
// pre-populated from the ticket's tag class.
//
// The endpoint is read-only — it never writes to the DB. Cheap by
// construction (3 small queries), so the UI is free to call it on
// every status-dropdown open.
func LessonCapturePrompt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := evaluateLessonCapturePrompt(id)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, res)
}

// evaluateLessonCapturePrompt runs the three trigger rules over the
// ticket and returns the aggregated decision. Exposed to the package
// (lower-case) so the CLI-facing helper or future invocations from
// other handlers can reuse it without duplicating the SQL.
func evaluateLessonCapturePrompt(issueID int64) (LessonCapturePromptResponse, error) {
	out := LessonCapturePromptResponse{}

	// Pull the ticket itself first — title is required for the
	// suggested-name fallback. A non-existent / soft-deleted issue
	// returns ShouldPrompt=false with no reason; the caller doesn't
	// need to distinguish between "no" and "not found" here.
	var (
		title    string
		parentID sql.NullInt64
		issueKey string
	)
	// PAI-584 P2: parent comes from the `parent` edge, not i.parent_id.
	err := db.DB.QueryRow(`
		SELECT i.title,
		       (SELECT source_id FROM issue_relations
		         WHERE target_id = i.id AND type='parent') AS parent_id,
		       COALESCE(p.key,'') || '-' || CAST(i.issue_number AS TEXT) AS issue_key
		  FROM issues i
		  LEFT JOIN projects p ON p.id = i.project_id
		 WHERE i.id = ?
		   AND i.deleted_at IS NULL
	`, issueID).Scan(&title, &parentID, &issueKey)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.TicketKey = issueKey

	reasons := []string{}

	// Rule 1: tag match.
	if matchedTag, err := lessonCaptureMatchTags(issueID); err == nil && matchedTag != "" {
		reasons = append(reasons, "tag:"+matchedTag)
	}

	// Rule 2: ancestor epic title.
	if matchedEpic, err := lessonCaptureMatchAncestorEpic(parentID); err == nil && matchedEpic != "" {
		reasons = append(reasons, "epic:"+matchedEpic)
	}

	// Rule 3: comment phrases. Skipped silently on query error so a
	// transient DB issue can't suppress the other rules.
	if matchedPhrase, err := lessonCaptureMatchComments(issueID); err == nil && matchedPhrase != "" {
		reasons = append(reasons, "comment:"+matchedPhrase)
	}

	if len(reasons) == 0 {
		return out, nil
	}
	out.ShouldPrompt = true
	out.Reason = strings.Join(reasons, " + ")
	out.SuggestedName = SuggestMemorySlug("feedback", title)
	return out, nil
}

// lessonCaptureMatchTags returns the first tag name on the issue
// that matches the lesson-tag pattern, or "" if none does. The
// pattern allows substring matches (e.g. tag "production-incident"
// hits on "incident").
func lessonCaptureMatchTags(issueID int64) (string, error) {
	rows, err := db.DB.Query(`
		SELECT t.name
		  FROM issue_tags it
		  JOIN tags t ON t.id = it.tag_id
		 WHERE it.issue_id = ?
	`, issueID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		if lessonCaptureLessonTagPattern.MatchString(name) {
			return name, nil
		}
	}
	return "", rows.Err()
}

// lessonCaptureMatchAncestorEpic walks up the parent chain (max 5
// hops to stay cheap and avoid pathological cycles) looking for an
// epic whose title matches the lesson-epic pattern. Returns the
// matched title, or "" if none of the ancestors qualify.
func lessonCaptureMatchAncestorEpic(parentID sql.NullInt64) (string, error) {
	if !parentID.Valid {
		return "", nil
	}
	current := parentID.Int64
	for hops := 0; hops < 5 && current > 0; hops++ {
		var (
			title   string
			pType   string
			nextPar sql.NullInt64
		)
		err := db.DB.QueryRow(`
			SELECT i.title, i.type,
			       (SELECT source_id FROM issue_relations
			         WHERE target_id = i.id AND type='parent') AS parent_id
			  FROM issues i
			 WHERE i.id = ?
			   AND i.deleted_at IS NULL
		`, current).Scan(&title, &pType, &nextPar)
		if err == sql.ErrNoRows {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if lessonCaptureEpicTitlePattern.MatchString(title) {
			return title, nil
		}
		_ = pType // we don't restrict to type='epic' — any ancestor with a matching title triggers
		if !nextPar.Valid {
			return "", nil
		}
		current = nextPar.Int64
	}
	return "", nil
}

// lessonCaptureMatchComments scans the ticket's comments for any of
// the heuristic phrases. Returns the first phrase that fires (so the
// reason string stays compact) or "" when no comment matches.
func lessonCaptureMatchComments(issueID int64) (string, error) {
	rows, err := db.DB.Query(`SELECT body FROM comments WHERE issue_id = ?`, issueID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			continue
		}
		low := strings.ToLower(body)
		for _, p := range lessonCaptureCommentPhrases {
			if strings.Contains(low, p) {
				return p, nil
			}
		}
	}
	return "", rows.Err()
}

// SuggestMemorySlug builds a memory slug of the form
// "<type>_<first-six-words-of-rule>". The output is the suggested
// name the prompt pre-populates so the user sees a sensible default
// before tweaking. Whitespace collapses to a single underscore, all
// non-alphanumeric runs get squeezed out — the partial UNIQUE INDEX
// on (type, slug, project_id) accepts ASCII identifiers only.
//
// The first-six-words rule keeps slugs short enough to type and long
// enough to survive de-duplication; the type prefix matches the
// existing convention seeded by knowledge entries (feedback_*,
// project_*, reference_*).
func SuggestMemorySlug(memType, rule string) string {
	mt := strings.ToLower(strings.TrimSpace(memType))
	if mt == "" {
		mt = "feedback"
	}
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return mt + "_lesson"
	}
	// Tokenize on whitespace + punctuation, keep the first 6 tokens.
	fields := strings.FieldsFunc(rule, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	if len(fields) > 6 {
		fields = fields[:6]
	}
	for i, f := range fields {
		fields[i] = strings.ToLower(f)
	}
	tail := strings.Join(fields, "_")
	if tail == "" {
		tail = "lesson"
	}
	return mt + "_" + tail
}
