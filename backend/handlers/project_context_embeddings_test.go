package handlers

import (
	"testing"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

func TestSyncProjectContextEmbeddingsUpsertsAndDeletesStaleRows(t *testing.T) {
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

	projectRes, err := db.DB.Exec(`INSERT INTO projects(name, key) VALUES(?, ?)`, "Embedding Project", "EMB")
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	projectID, _ := projectRes.LastInsertId()
	issueRes, err := db.DB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, description, status)
		VALUES(?, ?, ?, ?, ?, ?)
	`, projectID, 1, "ticket", "Original title", "semantic indexing body", "backlog")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueID, _ := issueRes.LastInsertId()

	docs, err := collectProjectRetrievalDocs(db.DB, projectID)
	if err != nil {
		t.Fatalf("collect docs: %v", err)
	}
	if err := syncProjectContextEmbeddings(db.DB, projectID, docs); err != nil {
		t.Fatalf("sync embeddings: %v", err)
	}
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM entity_embeddings WHERE project_id=? AND model=?`, projectID, projectContextEmbeddingModel).Scan(&count); err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if count != 1 {
		t.Fatalf("embedding count = %d, want 1", count)
	}
	var firstHash string
	if err := db.DB.QueryRow(`
		SELECT source_hash
		FROM entity_embeddings
		WHERE project_id=? AND entity_type='issue' AND entity_id=? AND model=?
	`, projectID, issueID, projectContextEmbeddingModel).Scan(&firstHash); err != nil {
		t.Fatalf("first hash: %v", err)
	}

	if _, err := db.DB.Exec(`UPDATE issues SET title=? WHERE id=?`, "Updated title", issueID); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	docs, err = collectProjectRetrievalDocs(db.DB, projectID)
	if err != nil {
		t.Fatalf("collect updated docs: %v", err)
	}
	if err := syncProjectContextEmbeddings(db.DB, projectID, docs); err != nil {
		t.Fatalf("sync updated embeddings: %v", err)
	}
	var updatedHash string
	if err := db.DB.QueryRow(`
		SELECT source_hash
		FROM entity_embeddings
		WHERE project_id=? AND entity_type='issue' AND entity_id=? AND model=?
	`, projectID, issueID, projectContextEmbeddingModel).Scan(&updatedHash); err != nil {
		t.Fatalf("updated hash: %v", err)
	}
	if updatedHash == firstHash {
		t.Fatalf("source_hash did not change after document update")
	}

	if _, err := db.DB.Exec(`UPDATE issues SET deleted_at='2026-05-12 12:00:00' WHERE id=?`, issueID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	docs, err = collectProjectRetrievalDocs(db.DB, projectID)
	if err != nil {
		t.Fatalf("collect after delete: %v", err)
	}
	if err := syncProjectContextEmbeddings(db.DB, projectID, docs); err != nil {
		t.Fatalf("sync after delete: %v", err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM entity_embeddings WHERE project_id=? AND model=?`, projectID, projectContextEmbeddingModel).Scan(&count); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("embedding count after stale cleanup = %d, want 0", count)
	}
}

func TestIndexProjectContextEmbeddingsStaysBoundToScheduledDatabase(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("open scheduled database: %v", err)
	}
	scheduledDB := db.DB
	t.Cleanup(func() {
		_ = scheduledDB.Close()
		if db.DB == scheduledDB {
			db.DB = nil
		}
	})

	projectRes, err := scheduledDB.Exec(`INSERT INTO projects(name, key) VALUES(?, ?)`, "Scheduled", "OLD")
	if err != nil {
		t.Fatalf("seed scheduled project: %v", err)
	}
	projectID, _ := projectRes.LastInsertId()
	if _, err := scheduledDB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status)
		VALUES(?, 1, 'ticket', 'Scheduled database issue', 'backlog')
	`, projectID); err != nil {
		t.Fatalf("seed scheduled issue: %v", err)
	}

	// Simulate the handlers test harness moving on to a fresh database while
	// an embedding job scheduled by the previous test is still pending.
	t.Setenv("DATA_DIR", t.TempDir())
	if err := db.Open(); err != nil {
		t.Fatalf("open replacement database: %v", err)
	}
	replacementDB := db.DB
	t.Cleanup(func() {
		_ = replacementDB.Close()
		if db.DB == replacementDB {
			db.DB = nil
		}
	})
	if _, err := replacementDB.Exec(`INSERT INTO projects(name, key) VALUES(?, ?)`, "Replacement", "NEW"); err != nil {
		t.Fatalf("seed replacement project: %v", err)
	}
	if _, err := replacementDB.Exec(`
		INSERT INTO issues(project_id, issue_number, type, title, status)
		VALUES(1, 1, 'ticket', 'Replacement database issue', 'backlog')
	`); err != nil {
		t.Fatalf("seed replacement issue: %v", err)
	}

	if err := indexProjectContextEmbeddings(scheduledDB, projectID); err != nil {
		t.Fatalf("index scheduled database: %v", err)
	}
	var scheduledCount, replacementCount int
	if err := scheduledDB.QueryRow(`SELECT COUNT(*) FROM entity_embeddings`).Scan(&scheduledCount); err != nil {
		t.Fatalf("count scheduled embeddings: %v", err)
	}
	if err := replacementDB.QueryRow(`SELECT COUNT(*) FROM entity_embeddings`).Scan(&replacementCount); err != nil {
		t.Fatalf("count replacement embeddings: %v", err)
	}
	if scheduledCount != 1 {
		t.Fatalf("scheduled embedding count = %d, want 1", scheduledCount)
	}
	if replacementCount != 0 {
		t.Fatalf("replacement embedding count = %d, want 0", replacementCount)
	}
}

func TestProjectContextEmbeddingJobSurvivesDatabaseTeardown(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PAIMOS_TEST_MODE", "1")
	if err := db.Open(); err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	conn := db.DB
	t.Cleanup(func() {
		_ = conn.Close()
		if db.DB == conn {
			db.DB = nil
		}
	})
	projectRes, err := conn.Exec(`INSERT INTO projects(name, key) VALUES(?, ?)`, "Closing", "CLS")
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	projectID, _ := projectRes.LastInsertId()
	job := projectContextEmbeddingJob{db: conn, projectID: projectID}

	done := make(chan struct{})
	go func() {
		runProjectContextEmbeddingJob(job)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		projectContextEmbeddingState.mu.Lock()
		running := projectContextEmbeddingState.running[job]
		projectContextEmbeddingState.mu.Unlock()
		if running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("embedding job did not start")
		}
		time.Sleep(time.Millisecond)
	}

	db.DB = nil
	if err := conn.Close(); err != nil {
		t.Fatalf("close scheduled database: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("embedding job did not stop after database teardown")
	}
}
