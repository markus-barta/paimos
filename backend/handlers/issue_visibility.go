// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
)

// PAI-463: GET /api/issues/{id}/portal-visibility
//
// Compact endpoint that backs the IssueDetailView visibility toggle's
// audit line. Returns the current visible state plus the most recent
// CUSTOMERPORTAL attach/detach event from mutation_log so the frontend
// can render "Last toggled by mba · 3 days ago" without round-tripping
// the entire activity feed.

type portalVisibilityLastEvent struct {
	Actor string `json:"actor,omitempty"`
	At    string `json:"at,omitempty"`
	Type  string `json:"type,omitempty"`
}

type portalVisibilityResponse struct {
	Visible   bool                       `json:"visible"`
	LastEvent *portalVisibilityLastEvent `json:"last_event"`
}

// GetIssuePortalVisibility resolves the current CUSTOMERPORTAL attachment
// state for an issue, plus the latest mutation_log entry that touched
// that attachment. The route sits behind auth.RequireIssueAccess so any
// user who can read the issue can see the audit line.
func GetIssuePortalVisibility(w http.ResponseWriter, r *http.Request) {
	issueID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	tagID, ok := customerPortalTagID()
	if !ok {
		jsonError(w, "customer-portal tag missing", http.StatusInternalServerError)
		return
	}

	var attached int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*) FROM issue_tags WHERE issue_id=? AND tag_id=?
	`, issueID, tagID).Scan(&attached); err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}

	out := portalVisibilityResponse{Visible: attached > 0}

	// Look for the most recent mutation that touched the
	// CUSTOMERPORTAL attachment for this issue. The mutation types we
	// care about all use subject_type='issue_tag' with subject_id=issue_id;
	// to avoid matching unrelated tag mutations on the same issue, we
	// filter to the set of types this epic emits.
	var (
		mtype, createdAt, actor sql.NullString
	)
	err = db.DB.QueryRow(`
		SELECT m.mutation_type, m.created_at, COALESCE(u.username, '')
		FROM mutation_log m
		LEFT JOIN users u ON u.id = m.user_id
		WHERE m.subject_type='issue_tag'
		  AND m.subject_id = ?
		  AND m.mutation_type IN (
		    'portal.submit.auto_tag',
		    'issue.tag.migration_backfill',
		    'issue.tag.portal_visibility_add',
		    'issue.tag.portal_visibility_remove',
		    'issue.tag.add',
		    'issue.tag.remove'
		  )
		ORDER BY m.id DESC
		LIMIT 1
	`, issueID).Scan(&mtype, &createdAt, &actor)
	if err == nil {
		out.LastEvent = &portalVisibilityLastEvent{
			Actor: actor.String,
			At:    createdAt.String,
			Type:  portalVisibilityEventLabel(mtype.String),
		}
	} else if err != sql.ErrNoRows {
		// Not fatal — the toggle still works; we just don't render the
		// "last toggled" line.
		out.LastEvent = nil
	}

	jsonOK(w, out)
}

// portalVisibilityEventLabel normalizes the raw mutation_type strings the
// epic emits into a small set of audience-facing event kinds the UI
// renders into a translated phrase. Keeps the frontend free of magic
// strings about backend internals.
func portalVisibilityEventLabel(mt string) string {
	switch mt {
	case "portal.submit.auto_tag":
		return "auto_tag"
	case "issue.tag.migration_backfill":
		return "migration_backfill"
	case "issue.tag.add", "issue.tag.portal_visibility_add":
		return "toggle_add"
	case "issue.tag.remove", "issue.tag.portal_visibility_remove":
		return "toggle_remove"
	default:
		return mt
	}
}
