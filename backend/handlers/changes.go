// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/inspr-at/paimos/backend/auth"
	"github.com/inspr-at/paimos/backend/db"
)

const changesHeartbeatInterval = 30 * time.Second

type mutationChangeEvent struct {
	ID           int64  `json:"id"`
	MutationType string `json:"mutation_type"`
	SubjectType  string `json:"subject_type"`
	SubjectID    int64  `json:"subject_id"`
	ProjectID    *int64 `json:"project_id"`
	UserID       *int64 `json:"user_id"`
	CreatedAt    string `json:"created_at"`
}

type mutationChangeSubscriber struct {
	ch          chan mutationChangeEvent
	allProjects bool
	projects    map[int64]bool
}

type mutationChangeBroker struct {
	mu   sync.RWMutex
	subs map[*mutationChangeSubscriber]struct{}
}

func newMutationChangeBroker() *mutationChangeBroker {
	return &mutationChangeBroker{subs: map[*mutationChangeSubscriber]struct{}{}}
}

var globalMutationChangeBroker = newMutationChangeBroker()

func mutationChanges() *mutationChangeBroker { return globalMutationChangeBroker }

func (b *mutationChangeBroker) Subscribe(max int, allProjects bool, projectIDs []int64) (*mutationChangeSubscriber, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if max > 0 && len(b.subs) >= max {
		return nil, false
	}
	projects := map[int64]bool{}
	for _, id := range projectIDs {
		projects[id] = true
	}
	sub := &mutationChangeSubscriber{
		ch:          make(chan mutationChangeEvent, 32),
		allProjects: allProjects,
		projects:    projects,
	}
	b.subs[sub] = struct{}{}
	return sub, true
}

func (b *mutationChangeBroker) Unsubscribe(sub *mutationChangeSubscriber) {
	if sub == nil {
		return
	}
	b.mu.Lock()
	if _, ok := b.subs[sub]; ok {
		delete(b.subs, sub)
		close(sub.ch)
	}
	b.mu.Unlock()
}

func (b *mutationChangeBroker) Publish(ev mutationChangeEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for sub := range b.subs {
		if !sub.canSee(ev) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
		}
	}
}

func (s *mutationChangeSubscriber) canSee(ev mutationChangeEvent) bool {
	if s.allProjects {
		return true
	}
	if ev.ProjectID == nil {
		return false
	}
	return s.projects[*ev.ProjectID]
}

func liveUpdatesEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PAIMOS_LIVE_UPDATES_ENABLED"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func liveUpdatesMaxConnections() int {
	raw := strings.TrimSpace(os.Getenv("PAIMOS_LIVE_UPDATES_MAX_CONNECTIONS"))
	if raw == "" {
		return 100
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 100
	}
	return n
}

// ChangesStream serves GET /api/changes?since=<mutation_log id>. Payloads are
// metadata only; before_state/after_state/inverse_op never leave this endpoint.
func ChangesStream(w http.ResponseWriter, r *http.Request) {
	if !liveUpdatesEnabled() {
		jsonError(w, "live updates disabled", http.StatusNotFound)
		return
	}
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	since, err := changeStreamSince(r)
	if err != nil {
		jsonError(w, "invalid since", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	projectIDs := auth.AccessibleProjectIDs(r)
	allProjects := projectIDs == nil
	sub, ok := mutationChanges().Subscribe(liveUpdatesMaxConnections(), allProjects, projectIDs)
	if !ok {
		jsonError(w, "too many live update streams", http.StatusServiceUnavailable)
		return
	}
	defer mutationChanges().Unsubscribe(sub)

	replay, err := replayMutationChanges(r.Context(), since, sub)
	if err != nil {
		jsonError(w, "replay failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprintf(w, ":connected\n\n")
	flusher.Flush()

	for _, ev := range replay {
		writeMutationChangeEvent(w, flusher, ev)
	}

	heartbeat := time.NewTicker(changesHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.ch:
			if !ok {
				return
			}
			writeMutationChangeEvent(w, flusher, ev)
		case <-heartbeat.C:
			fmt.Fprintf(w, ":heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func changeStreamSince(r *http.Request) (int64, error) {
	since := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return 0, err
		}
		since = n
	}
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && n > since {
			since = n
		}
	}
	return since, nil
}

func writeMutationChangeEvent(w http.ResponseWriter, flusher http.Flusher, ev mutationChangeEvent) {
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: mutation\nid: %d\ndata: %s\n\n", ev.ID, payload)
	flusher.Flush()
}

func replayMutationChanges(ctx context.Context, since int64, sub *mutationChangeSubscriber) ([]mutationChangeEvent, error) {
	rows, err := db.DB.QueryContext(ctx, mutationChangeSelectSQL()+` WHERE m.id > ? ORDER BY m.id ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mutationChangeEvent{}
	for rows.Next() {
		ev, err := scanMutationChange(rows)
		if err != nil {
			return nil, err
		}
		if sub.canSee(ev) {
			out = append(out, ev)
		}
	}
	return out, rows.Err()
}

type mutationChangeRow interface {
	Scan(...any) error
}

func scanMutationChange(row mutationChangeRow) (mutationChangeEvent, error) {
	var ev mutationChangeEvent
	var projectID, userID sql.NullInt64
	err := row.Scan(&ev.ID, &ev.MutationType, &ev.SubjectType, &ev.SubjectID, &projectID, &userID, &ev.CreatedAt)
	if projectID.Valid {
		ev.ProjectID = &projectID.Int64
	}
	if userID.Valid {
		ev.UserID = &userID.Int64
	}
	return ev, err
}

type mutationChangeQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadMutationChange(ctx context.Context, q mutationChangeQueryer, id int64) (mutationChangeEvent, error) {
	return scanMutationChange(q.QueryRowContext(ctx, mutationChangeSelectSQL()+` WHERE m.id = ?`, id))
}

func mutationChangeSelectSQL() string {
	return `
		SELECT
			m.id,
			m.mutation_type,
			m.subject_type,
			m.subject_id,
			COALESCE(
				CASE WHEN m.subject_type='project' THEN m.subject_id END,
				CASE WHEN m.subject_type IN ('issue','issue_tag') THEN (
					SELECT i.project_id FROM issues i WHERE i.id = m.subject_id
				) END,
				CASE WHEN m.subject_type='comment' THEN (
					SELECT i.project_id FROM comments c JOIN issues i ON i.id = c.issue_id WHERE c.id = m.subject_id
				) END,
				CASE WHEN m.subject_type='time_entry' THEN (
					SELECT i.project_id FROM time_entries te JOIN issues i ON i.id = te.issue_id WHERE te.id = m.subject_id
				) END,
				CASE WHEN m.subject_type='project_report' THEN (
					SELECT prs.project_id FROM project_report_snapshots prs WHERE prs.id = m.subject_id
				) END
			) AS project_id,
			m.user_id,
			m.created_at
		FROM mutation_log m`
}
