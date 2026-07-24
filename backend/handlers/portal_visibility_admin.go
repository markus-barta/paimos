// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/inspr-at/paimos/backend/db"
)

// PAI-467: admin-only Customer Portal Visibility report.
//
// Two endpoints power the report:
//
//   GET  /api/admin/projects/{id}/portal-visibility
//        ?audit_offset=N&audit_limit=M
//     → JSON {visible_count, issues[], audit[], total_audit}
//
//   GET  /api/admin/projects/{id}/portal-visibility.csv?section=current|audit
//     → text/csv with either the current visibility set or the audit feed

type adminVisibilityIssue struct {
	ID            int64  `json:"id"`
	IssueKey      string `json:"issue_key"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	LastActor     string `json:"last_actor,omitempty"`
	LastAt        string `json:"last_at,omitempty"`
	LastEventType string `json:"last_event_type,omitempty"`
}

type adminVisibilityAuditRow struct {
	At        string `json:"at"`
	Actor     string `json:"actor,omitempty"`
	EventType string `json:"event_type"`
	IssueID   int64  `json:"issue_id"`
	IssueKey  string `json:"issue_key"`
	Title     string `json:"title"`
}

type adminVisibilityResponse struct {
	ProjectID    int64                     `json:"project_id"`
	VisibleCount int                       `json:"visible_count"`
	Issues       []adminVisibilityIssue    `json:"issues"`
	Audit        []adminVisibilityAuditRow `json:"audit"`
	TotalAudit   int                       `json:"total_audit"`
	AuditOffset  int                       `json:"audit_offset"`
	AuditLimit   int                       `json:"audit_limit"`
}

const defaultAuditLimit = 50

// GetAdminProjectPortalVisibility serves the JSON report.
func GetAdminProjectPortalVisibility(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	tagID, ok := customerPortalTagID()
	if !ok {
		jsonError(w, "CUSTOMERPORTAL tag missing", http.StatusInternalServerError)
		return
	}

	offset := portalParseIntParam(r.URL.Query().Get("audit_offset"), 0)
	if offset < 0 {
		offset = 0
	}
	limit := portalParseIntParam(r.URL.Query().Get("audit_limit"), defaultAuditLimit)
	if limit <= 0 || limit > 500 {
		limit = defaultAuditLimit
	}

	out := adminVisibilityResponse{
		ProjectID:   projectID,
		Issues:      []adminVisibilityIssue{},
		Audit:       []adminVisibilityAuditRow{},
		AuditOffset: offset,
		AuditLimit:  limit,
	}

	// Current visibility set — every visible issue with its most recent
	// CUSTOMERPORTAL attach event resolved in the same query so the UI
	// can render the "last toggled" hint without N+1 calls.
	visibleQ := `
		SELECT i.id,
		       COALESCE(p.key || '-' || i.issue_number, ''),
		       i.title, i.status,
		       (SELECT m.mutation_type FROM mutation_log m
		         WHERE m.subject_type='issue_tag' AND m.subject_id=i.id
		           AND m.mutation_type IN (
		             'portal.submit.auto_tag','issue.tag.migration_backfill',
		             'issue.tag.add','issue.tag.remove',
		             'issue.tag.bulk_add','issue.tag.bulk_remove'
		           )
		         ORDER BY m.id DESC LIMIT 1) AS last_event,
		       (SELECT m.created_at FROM mutation_log m
		         WHERE m.subject_type='issue_tag' AND m.subject_id=i.id
		           AND m.mutation_type IN (
		             'portal.submit.auto_tag','issue.tag.migration_backfill',
		             'issue.tag.add','issue.tag.remove',
		             'issue.tag.bulk_add','issue.tag.bulk_remove'
		           )
		         ORDER BY m.id DESC LIMIT 1) AS last_at,
		       (SELECT COALESCE(u.username,'') FROM mutation_log m
		         LEFT JOIN users u ON u.id = m.user_id
		         WHERE m.subject_type='issue_tag' AND m.subject_id=i.id
		           AND m.mutation_type IN (
		             'portal.submit.auto_tag','issue.tag.migration_backfill',
		             'issue.tag.add','issue.tag.remove',
		             'issue.tag.bulk_add','issue.tag.bulk_remove'
		           )
		         ORDER BY m.id DESC LIMIT 1) AS last_actor
		FROM issues i
		JOIN issue_tags it ON it.issue_id = i.id AND it.tag_id = ?
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE i.project_id = ? AND i.deleted_at IS NULL
		ORDER BY i.updated_at DESC`
	rows, err := db.DB.Query(visibleQ, tagID, projectID)
	if err != nil {
		jsonError(w, "visibility query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var iv adminVisibilityIssue
		var ev, at, actor *string
		if err := rows.Scan(&iv.ID, &iv.IssueKey, &iv.Title, &iv.Status, &ev, &at, &actor); err != nil {
			continue
		}
		if ev != nil {
			iv.LastEventType = portalVisibilityEventLabel(*ev)
		}
		if at != nil {
			iv.LastAt = *at
		}
		if actor != nil {
			iv.LastActor = *actor
		}
		out.Issues = append(out.Issues, iv)
	}
	out.VisibleCount = len(out.Issues)

	// Audit feed — every attach/detach/migration row touching an issue
	// in this project, paginated. We resolve the issue/project key in
	// the same query so the CSV export doesn't need a second pass.
	auditTotalQ := `
		SELECT COUNT(*)
		FROM mutation_log m
		JOIN issues i ON i.id = m.subject_id
		WHERE m.subject_type='issue_tag'
		  AND i.project_id = ?
		  AND m.mutation_type IN (
		    'portal.submit.auto_tag','issue.tag.migration_backfill',
		    'issue.tag.add','issue.tag.remove',
		    'issue.tag.bulk_add','issue.tag.bulk_remove'
		  )`
	if err := db.DB.QueryRow(auditTotalQ, projectID).Scan(&out.TotalAudit); err != nil {
		out.TotalAudit = 0
	}

	auditQ := `
		SELECT m.created_at, COALESCE(u.username,''), m.mutation_type,
		       i.id, COALESCE(p.key || '-' || i.issue_number,''), i.title
		FROM mutation_log m
		LEFT JOIN users u ON u.id = m.user_id
		JOIN issues i ON i.id = m.subject_id
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE m.subject_type='issue_tag'
		  AND i.project_id = ?
		  AND m.mutation_type IN (
		    'portal.submit.auto_tag','issue.tag.migration_backfill',
		    'issue.tag.add','issue.tag.remove',
		    'issue.tag.bulk_add','issue.tag.bulk_remove'
		  )
		ORDER BY m.id DESC
		LIMIT ? OFFSET ?`
	arows, err := db.DB.Query(auditQ, projectID, limit, offset)
	if err == nil {
		defer arows.Close()
		for arows.Next() {
			var a adminVisibilityAuditRow
			var mtype string
			if err := arows.Scan(&a.At, &a.Actor, &mtype, &a.IssueID, &a.IssueKey, &a.Title); err != nil {
				continue
			}
			a.EventType = portalVisibilityEventLabel(mtype)
			out.Audit = append(out.Audit, a)
		}
	}

	jsonOK(w, out)
}

// GetAdminProjectPortalVisibilityCSV streams the current set or the
// audit feed as text/csv. Section ∈ {current, audit}.
func GetAdminProjectPortalVisibilityCSV(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}
	tagID, ok := customerPortalTagID()
	if !ok {
		http.Error(w, "CUSTOMERPORTAL tag missing", http.StatusInternalServerError)
		return
	}
	section := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("section")))
	if section == "" {
		section = "current"
	}
	if section != "current" && section != "audit" {
		http.Error(w, "section must be current or audit", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="portal-visibility-%s-%d.csv"`, section, projectID))
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if section == "current" {
		_ = cw.Write([]string{"issue_key", "title", "status", "last_actor", "last_at", "last_event_type"})
		rows, err := db.DB.Query(`
			SELECT COALESCE(p.key || '-' || i.issue_number,''), i.title, i.status,
			       (SELECT COALESCE(u.username,'') FROM mutation_log m
			         LEFT JOIN users u ON u.id = m.user_id
			         WHERE m.subject_type='issue_tag' AND m.subject_id=i.id
			           AND m.mutation_type IN (
			             'portal.submit.auto_tag','issue.tag.migration_backfill',
			             'issue.tag.add','issue.tag.remove',
			             'issue.tag.bulk_add','issue.tag.bulk_remove'
			           )
			         ORDER BY m.id DESC LIMIT 1) AS last_actor,
			       (SELECT m.created_at FROM mutation_log m
			         WHERE m.subject_type='issue_tag' AND m.subject_id=i.id
			           AND m.mutation_type IN (
			             'portal.submit.auto_tag','issue.tag.migration_backfill',
			             'issue.tag.add','issue.tag.remove',
			             'issue.tag.bulk_add','issue.tag.bulk_remove'
			           )
			         ORDER BY m.id DESC LIMIT 1) AS last_at,
			       (SELECT m.mutation_type FROM mutation_log m
			         WHERE m.subject_type='issue_tag' AND m.subject_id=i.id
			           AND m.mutation_type IN (
			             'portal.submit.auto_tag','issue.tag.migration_backfill',
			             'issue.tag.add','issue.tag.remove',
			             'issue.tag.bulk_add','issue.tag.bulk_remove'
			           )
			         ORDER BY m.id DESC LIMIT 1) AS last_event
			FROM issues i
			JOIN issue_tags it ON it.issue_id = i.id AND it.tag_id = ?
			LEFT JOIN projects p ON p.id = i.project_id
			WHERE i.project_id = ? AND i.deleted_at IS NULL
			ORDER BY i.updated_at DESC`, tagID, projectID)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var key, title, status string
			var actor, at, ev *string
			if err := rows.Scan(&key, &title, &status, &actor, &at, &ev); err != nil {
				continue
			}
			actorS, atS, evS := "", "", ""
			if actor != nil {
				actorS = *actor
			}
			if at != nil {
				atS = *at
			}
			if ev != nil {
				evS = portalVisibilityEventLabel(*ev)
			}
			_ = cw.Write([]string{key, title, status, actorS, atS, evS})
		}
		return
	}

	// audit section
	_ = cw.Write([]string{"at", "actor", "event_type", "issue_key", "title"})
	rows, err := db.DB.Query(`
		SELECT m.created_at, COALESCE(u.username,''), m.mutation_type,
		       COALESCE(p.key || '-' || i.issue_number,''), i.title
		FROM mutation_log m
		LEFT JOIN users u ON u.id = m.user_id
		JOIN issues i ON i.id = m.subject_id
		LEFT JOIN projects p ON p.id = i.project_id
		WHERE m.subject_type='issue_tag'
		  AND i.project_id = ?
		  AND m.mutation_type IN (
		    'portal.submit.auto_tag','issue.tag.migration_backfill',
		    'issue.tag.add','issue.tag.remove',
		    'issue.tag.bulk_add','issue.tag.bulk_remove'
		  )
		ORDER BY m.id DESC`, projectID)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var at, actor, mtype, key, title string
		if err := rows.Scan(&at, &actor, &mtype, &key, &title); err != nil {
			continue
		}
		_ = cw.Write([]string{at, actor, portalVisibilityEventLabel(mtype), key, title})
	}
}

func portalParseIntParam(raw string, def int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}
