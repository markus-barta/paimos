package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

func openChangesTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
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

func seedChangesProject(t *testing.T, key string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO projects(name, key) VALUES(?,?)`, key+" project", key)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedChangesIssue(t *testing.T, projectID int64, num int) int64 {
	t.Helper()
	res, err := db.DB.Exec(
		`INSERT INTO issues(project_id, issue_number, type, title, status) VALUES(?,?,?,?,?)`,
		projectID, num, "ticket", "Change target", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertChangesMutation(t *testing.T, issueID int64, mutationType string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`
		INSERT INTO mutation_log(
			request_id, mutation_type, subject_type, subject_id,
			inverse_op, before_state, after_state, before_hash, after_hash
		) VALUES('req-test', ?, 'issue', ?, '{}', '{}', '{}', 'before', 'after')`,
		mutationType, issueID)
	if err != nil {
		t.Fatalf("insert mutation: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestChangesStreamDisabledByDefault(t *testing.T) {
	t.Setenv("PAIMOS_LIVE_UPDATES_ENABLED", "")
	rec := httptest.NewRecorder()
	ChangesStream(rec, httptest.NewRequest(http.MethodGet, "/api/changes", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 when live updates are disabled", rec.Code)
	}
}

func TestReplayMutationChangesFiltersByProject(t *testing.T) {
	openChangesTestDB(t)
	p1 := seedChangesProject(t, "P1")
	p2 := seedChangesProject(t, "P2")
	i1 := seedChangesIssue(t, p1, 1)
	i2 := seedChangesIssue(t, p2, 1)
	insertChangesMutation(t, i1, "issue.update")
	insertChangesMutation(t, i2, "issue.update")

	sub := &mutationChangeSubscriber{projects: map[int64]bool{p1: true}}
	events, err := replayMutationChanges(context.Background(), 0, sub)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(events) != 1 || events[0].SubjectID != i1 || events[0].ProjectID == nil || *events[0].ProjectID != p1 {
		t.Fatalf("events=%+v, want only project %d issue %d", events, p1, i1)
	}
}

func TestMutationChangeEventPayloadIsMetadataOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	projectID := int64(7)
	writeMutationChangeEvent(rec, rec, mutationChangeEvent{
		ID:           42,
		MutationType: "issue.update",
		SubjectType:  "issue",
		SubjectID:    9,
		ProjectID:    &projectID,
		CreatedAt:    "2026-07-07 09:00:00",
	})
	body := rec.Body.String()
	for _, forbidden := range []string{"before_state", "after_state", "inverse_op", "Issue title body"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("SSE payload leaked %q: %s", forbidden, body)
		}
	}
	for _, want := range []string{"event: mutation", "id: 42", `"mutation_type":"issue.update"`, `"project_id":7`} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE payload missing %q: %s", want, body)
		}
	}
}

func TestChangeStreamSinceUsesLastEventID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/changes?since=10", nil)
	req.Header.Set("Last-Event-ID", "12")
	got, err := changeStreamSince(req)
	if err != nil {
		t.Fatalf("changeStreamSince: %v", err)
	}
	if got != 12 {
		t.Fatalf("since=%d, want Last-Event-ID to advance cursor to 12", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/changes?since=15", nil)
	req.Header.Set("Last-Event-ID", "12")
	got, err = changeStreamSince(req)
	if err != nil {
		t.Fatalf("changeStreamSince: %v", err)
	}
	if got != 15 {
		t.Fatalf("since=%d, want query cursor to remain at 15", got)
	}
}

func TestRecordMutationPublishesLiveChange(t *testing.T) {
	openChangesTestDB(t)
	p1 := seedChangesProject(t, "P1")
	i1 := seedChangesIssue(t, p1, 1)
	sub, ok := mutationChanges().Subscribe(100, true, nil)
	if !ok {
		t.Fatal("subscribe failed")
	}
	defer mutationChanges().Unsubscribe(sub)

	tx, err := db.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := recordMutation(context.Background(), tx, mutationRecordArgs{
		RequestID:    "req-live",
		MutationType: "issue.update",
		SubjectType:  "issue",
		SubjectID:    i1,
		InverseOp:    InverseOp{Method: http.MethodPatch, Path: "/api/issues/1"},
		BeforeState:  map[string]any{"title": "before"},
		AfterState:   map[string]any{"title": "after"},
	}); err != nil {
		t.Fatalf("recordMutation: %v", err)
	}

	select {
	case ev := <-sub.ch:
		if ev.MutationType != "issue.update" || ev.ProjectID == nil || *ev.ProjectID != p1 {
			t.Fatalf("event=%+v, want issue.update for project %d", ev, p1)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live mutation event")
	}
}

func TestLiveUpdatesMaxConnectionsDefaultAndEnv(t *testing.T) {
	old := os.Getenv("PAIMOS_LIVE_UPDATES_MAX_CONNECTIONS")
	t.Cleanup(func() { os.Setenv("PAIMOS_LIVE_UPDATES_MAX_CONNECTIONS", old) })
	os.Unsetenv("PAIMOS_LIVE_UPDATES_MAX_CONNECTIONS")
	if got := liveUpdatesMaxConnections(); got != 100 {
		t.Fatalf("default max connections = %d, want 100", got)
	}
	os.Setenv("PAIMOS_LIVE_UPDATES_MAX_CONNECTIONS", "12")
	if got := liveUpdatesMaxConnections(); got != 12 {
		t.Fatalf("env max connections = %d, want 12", got)
	}
}
