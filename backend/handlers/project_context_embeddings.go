package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

const (
	projectContextEmbeddingModel      = "local-hash-v1"
	projectContextEmbeddingDim        = 256
	projectContextEmbeddingDebounce   = 150 * time.Millisecond
	projectContextEmbeddingRetryDelay = 100 * time.Millisecond
)

var embeddingTokenPattern = regexp.MustCompile(`[A-Za-z0-9_./:-]+`)

type retrievalDoc struct {
	EntityType string
	EntityID   int64
	Title      string
	Content    string
	Hit        map[string]any
}

type projectContextEmbeddingIndexState struct {
	mu      sync.Mutex
	queued  map[int64]bool
	running map[int64]bool
	rerun   map[int64]bool
}

var (
	projectContextEmbeddingQueue      = make(chan int64, 128)
	projectContextEmbeddingWorkerOnce sync.Once
	projectContextEmbeddingState      = projectContextEmbeddingIndexState{
		queued:  map[int64]bool{},
		running: map[int64]bool{},
		rerun:   map[int64]bool{},
	}
)

func retrieveProjectContextVectorHits(projectID int64, q string, k int) ([]map[string]any, error) {
	docs, err := collectProjectRetrievalDocs(projectID)
	if err != nil {
		return nil, err
	}
	enqueueProjectContextEmbeddingIndex(projectID)
	docByKey := map[string]retrievalDoc{}
	for _, doc := range docs {
		docByKey[retrievalDocKey(doc.EntityType, doc.EntityID)] = doc
	}
	queryVec := embedTextDeterministic(q)
	rows, err := db.DB.Query(`
		SELECT entity_type, entity_id, vector
		FROM entity_embeddings
		WHERE project_id = ? AND model = ?
	`, projectID, projectContextEmbeddingModel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scored struct {
		hit   map[string]any
		score float64
	}
	scoredHits := []scored{}
	for rows.Next() {
		var entityType string
		var entityID int64
		var raw []byte
		if err := rows.Scan(&entityType, &entityID, &raw); err != nil {
			return nil, err
		}
		doc, ok := docByKey[retrievalDocKey(entityType, entityID)]
		if !ok {
			continue
		}
		vec, err := decodeEmbedding(raw)
		if err != nil {
			return nil, err
		}
		score := cosineSimilarity(queryVec, vec)
		if score <= 0.05 {
			continue
		}
		hit := map[string]any{}
		for key, value := range doc.Hit {
			hit[key] = value
		}
		hit["sources"] = []string{"vector"}
		hit["score"] = score
		hit["expanded_from"] = nil
		scoredHits = append(scoredHits, scored{hit: hit, score: score})
	}
	sort.SliceStable(scoredHits, func(i, j int) bool {
		if scoredHits[i].score == scoredHits[j].score {
			left := fmt.Sprintf("%v:%v", scoredHits[i].hit["entity_type"], scoredHits[i].hit["entity_id"])
			right := fmt.Sprintf("%v:%v", scoredHits[j].hit["entity_type"], scoredHits[j].hit["entity_id"])
			return left < right
		}
		return scoredHits[i].score > scoredHits[j].score
	})
	out := make([]map[string]any, 0, minInt(k, len(scoredHits)))
	for _, item := range scoredHits {
		if len(out) >= k {
			break
		}
		out = append(out, item.hit)
	}
	return out, rows.Err()
}

func enqueueProjectContextEmbeddingIndex(projectID int64) {
	if projectID <= 0 {
		return
	}
	projectContextEmbeddingWorkerOnce.Do(func() {
		go runProjectContextEmbeddingWorker()
	})
	projectContextEmbeddingState.mu.Lock()
	if projectContextEmbeddingState.queued[projectID] {
		projectContextEmbeddingState.mu.Unlock()
		return
	}
	if projectContextEmbeddingState.running[projectID] {
		projectContextEmbeddingState.rerun[projectID] = true
		projectContextEmbeddingState.mu.Unlock()
		return
	}
	projectContextEmbeddingState.queued[projectID] = true
	projectContextEmbeddingState.mu.Unlock()

	select {
	case projectContextEmbeddingQueue <- projectID:
	default:
		go func() { projectContextEmbeddingQueue <- projectID }()
	}
}

func runProjectContextEmbeddingWorker() {
	for projectID := range projectContextEmbeddingQueue {
		runProjectContextEmbeddingJob(projectID)
	}
}

func runProjectContextEmbeddingJob(projectID int64) {
	for {
		projectContextEmbeddingState.mu.Lock()
		projectContextEmbeddingState.queued[projectID] = false
		projectContextEmbeddingState.running[projectID] = true
		projectContextEmbeddingState.mu.Unlock()

		time.Sleep(projectContextEmbeddingDebounce)
		if err := indexProjectContextEmbeddingsWithRetry(projectID); err != nil {
			log.Printf("project context embedding index project=%d: %v", projectID, err)
		}

		projectContextEmbeddingState.mu.Lock()
		if projectContextEmbeddingState.rerun[projectID] {
			projectContextEmbeddingState.rerun[projectID] = false
			projectContextEmbeddingState.queued[projectID] = true
			projectContextEmbeddingState.mu.Unlock()
			continue
		}
		delete(projectContextEmbeddingState.queued, projectID)
		delete(projectContextEmbeddingState.running, projectID)
		delete(projectContextEmbeddingState.rerun, projectID)
		projectContextEmbeddingState.mu.Unlock()
		return
	}
}

func indexProjectContextEmbeddingsWithRetry(projectID int64) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(projectContextEmbeddingRetryDelay)
		}
		err := indexProjectContextEmbeddings(projectID)
		if err == nil {
			return nil
		}
		if !isSQLiteBusyError(err) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

func indexProjectContextEmbeddings(projectID int64) error {
	if db.DB == nil {
		return fmt.Errorf("database not open")
	}
	docs, err := collectProjectRetrievalDocs(projectID)
	if err != nil {
		return err
	}
	return syncProjectContextEmbeddings(projectID, docs)
}

func syncProjectContextEmbeddings(projectID int64, docs []retrievalDoc) error {
	type embeddingRow struct {
		entityType string
		entityID   int64
		dim        int
		vector     []byte
		hash       string
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	seen := map[string]struct{}{}
	embeddingRows := make([]embeddingRow, 0, len(docs))
	for _, doc := range docs {
		text := strings.TrimSpace(doc.Title + "\n" + doc.Content)
		if text == "" {
			continue
		}
		seen[retrievalDocKey(doc.EntityType, doc.EntityID)] = struct{}{}
		vec := embedTextDeterministic(text)
		raw, err := encodeEmbedding(vec)
		if err != nil {
			return err
		}
		hash := sha256.Sum256([]byte(text))
		embeddingRows = append(embeddingRows, embeddingRow{
			entityType: doc.EntityType,
			entityID:   doc.EntityID,
			dim:        len(vec),
			vector:     raw,
			hash:       fmt.Sprintf("%x", hash[:]),
		})
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT entity_type, entity_id
		FROM entity_embeddings
		WHERE project_id = ? AND model = ?
	`, projectID, projectContextEmbeddingModel)
	if err != nil {
		return err
	}
	stored := map[string]struct{}{}
	for rows.Next() {
		var entityType string
		var entityID int64
		if err := rows.Scan(&entityType, &entityID); err != nil {
			rows.Close()
			return err
		}
		stored[retrievalDocKey(entityType, entityID)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, row := range embeddingRows {
		if _, err := tx.Exec(`
			INSERT INTO entity_embeddings(project_id, entity_type, entity_id, model, dim, vector, source_hash, last_indexed_at)
			VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT(entity_type, entity_id, model) DO UPDATE SET
				project_id = excluded.project_id,
				dim = excluded.dim,
				vector = excluded.vector,
				source_hash = excluded.source_hash,
				last_indexed_at = excluded.last_indexed_at
		`, projectID, row.entityType, row.entityID, projectContextEmbeddingModel, row.dim, row.vector, row.hash, now); err != nil {
			return err
		}
	}
	for key := range stored {
		if _, ok := seen[key]; ok {
			continue
		}
		entityType, entityID, ok := parseRetrievalDocKey(key)
		if !ok {
			continue
		}
		if _, err := tx.Exec(`
			DELETE FROM entity_embeddings
			WHERE project_id = ? AND entity_type = ? AND entity_id = ? AND model = ?
		`, projectID, entityType, entityID, projectContextEmbeddingModel); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func collectProjectRetrievalDocs(projectID int64) ([]retrievalDoc, error) {
	issues, err := collectProjectIssueDocs(projectID)
	if err != nil {
		return nil, err
	}
	anchors, err := collectProjectAnchorDocs(projectID)
	if err != nil {
		return nil, err
	}
	symbols, err := collectProjectSymbolDocs(projectID)
	if err != nil {
		return nil, err
	}
	// PAI-358: collectProjectManifestDocs removed with the
	// project_manifests table. NFR/ADR retrieval now flows through
	// the regular issue path (knowledge entries are issues).
	out := make([]retrievalDoc, 0, len(issues)+len(anchors)+len(symbols))
	out = append(out, issues...)
	out = append(out, anchors...)
	out = append(out, symbols...)
	return out, nil
}

func collectProjectIssueDocs(projectID int64) ([]retrievalDoc, error) {
	rows, err := db.DB.Query(`
		SELECT i.id, i.title, COALESCE(i.description,''), COALESCE(i.acceptance_criteria,''), COALESCE(i.notes,''),
		       COALESCE(i.type,''), COALESCE(p.key,''), i.issue_number
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.project_id = ? AND i.deleted_at IS NULL
		ORDER BY i.id ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []retrievalDoc{}
	for rows.Next() {
		var id int64
		var title, description, acceptance, notes, issueType, projectKey string
		var issueNumber int
		if err := rows.Scan(&id, &title, &description, &acceptance, &notes, &issueType, &projectKey, &issueNumber); err != nil {
			return nil, err
		}
		issueKey := fmt.Sprintf("%s-%d", projectKey, issueNumber)
		out = append(out, retrievalDoc{
			EntityType: "issue",
			EntityID:   id,
			Title:      title,
			Content:    strings.Join([]string{issueKey, issueType, description, acceptance, notes}, "\n"),
			Hit: map[string]any{
				"entity_type":   "issue",
				"entity_id":     id,
				"title":         title,
				"snippet":       title,
				"issue_key":     issueKey,
				"expanded_from": nil,
			},
		})
	}
	return out, rows.Err()
}

func collectProjectAnchorDocs(projectID int64) ([]retrievalDoc, error) {
	rows, err := db.DB.Query(`
		SELECT a.id, a.issue_id, a.file_path, a.line, a.label,
		       COALESCE(pr.label,''), pr.url, COALESCE(p.key,''), i.issue_number
		FROM issue_anchors a
		JOIN project_repos pr ON pr.id = a.repo_id
		JOIN issues i ON i.id = a.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE a.project_id = ?
		ORDER BY a.id ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []retrievalDoc{}
	for rows.Next() {
		var anchorID, issueID int64
		var filePath, label, repoLabel, repoURL, projectKey string
		var line, issueNumber int
		if err := rows.Scan(&anchorID, &issueID, &filePath, &line, &label, &repoLabel, &repoURL, &projectKey, &issueNumber); err != nil {
			return nil, err
		}
		title := defaultString(strings.TrimSpace(label), filePath)
		issueKey := fmt.Sprintf("%s-%d", projectKey, issueNumber)
		out = append(out, retrievalDoc{
			EntityType: "anchor",
			EntityID:   anchorID,
			Title:      title,
			Content:    strings.Join([]string{filePath, issueKey, defaultString(repoLabel, deriveRepoLabel(repoURL))}, "\n"),
			Hit: map[string]any{
				"entity_type":   "anchor",
				"entity_id":     anchorID,
				"title":         title,
				"snippet":       fmt.Sprintf("%s:%d", filePath, line),
				"issue_id":      issueID,
				"issue_key":     issueKey,
				"file_path":     filePath,
				"line":          line,
				"repo_label":    defaultString(repoLabel, deriveRepoLabel(repoURL)),
				"repo_url":      repoURL,
				"expanded_from": nil,
			},
		})
	}
	return out, rows.Err()
}

// PAI-358: collectProjectManifestDocs deleted with the manifest blob.

func collectProjectSymbolDocs(projectID int64) ([]retrievalDoc, error) {
	rows, err := db.DB.Query(`
		SELECT a.repo_id, a.file_path, a.symbol_json
		FROM issue_anchors a
		WHERE a.project_id = ? AND a.symbol_json != ''
		ORDER BY a.id ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []retrievalDoc{}
	seen := map[int64]bool{}
	for rows.Next() {
		var repoID int64
		var filePath, raw string
		if err := rows.Scan(&repoID, &filePath, &raw); err != nil {
			return nil, err
		}
		sym, ok := decodeStoredAnchorSymbol(raw)
		if !ok {
			continue
		}
		symbolID := symbolIDForAnchor(repoID, filePath, *sym)
		if seen[symbolID] {
			continue
		}
		seen[symbolID] = true
		title := fmt.Sprintf("%s %s", defaultString(sym.Kind, "symbol"), sym.Name)
		out = append(out, retrievalDoc{
			EntityType: "symbol",
			EntityID:   symbolID,
			Title:      title,
			Content:    strings.Join([]string{sym.Name, sym.Kind, sym.Language, filePath}, "\n"),
			Hit: map[string]any{
				"entity_type":   "symbol",
				"entity_id":     symbolID,
				"title":         title,
				"snippet":       filePath,
				"name":          sym.Name,
				"kind":          sym.Kind,
				"language":      sym.Language,
				"file_path":     filePath,
				"start_line":    sym.StartLine,
				"end_line":      sym.EndLine,
				"repo_id":       repoID,
				"expanded_from": nil,
			},
		})
	}
	return out, rows.Err()
}

func embedTextDeterministic(text string) []float32 {
	vec := make([]float32, projectContextEmbeddingDim)
	tokens := embeddingTokenPattern.FindAllString(strings.ToLower(text), -1)
	for _, token := range tokens {
		sum := fnvHash(token)
		idx := int(sum % uint64(projectContextEmbeddingDim))
		sign := float32(1)
		if sum&(1<<63) != 0 {
			sign = -1
		}
		weight := float32(1.0 + math.Log1p(float64(len(token))))
		vec[idx] += sign * weight
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v * v)
	}
	if norm == 0 {
		return vec
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= scale
	}
	return vec
}

func fnvHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func encodeEmbedding(vec []float32) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(vec)*4))
	for _, v := range vec {
		if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func decodeEmbedding(raw []byte) ([]float32, error) {
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("invalid embedding blob length %d", len(raw))
	}
	out := make([]float32, len(raw)/4)
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i] * b[i])
	}
	return dot
}

func retrievalDocKey(entityType string, entityID int64) string {
	return fmt.Sprintf("%s:%d", entityType, entityID)
}

func parseRetrievalDocKey(key string) (string, int64, bool) {
	entityType, rawID, ok := strings.Cut(key, ":")
	if !ok || entityType == "" {
		return "", 0, false
	}
	var entityID int64
	if _, err := fmt.Sscanf(rawID, "%d", &entityID); err != nil || entityID <= 0 {
		return "", 0, false
	}
	return entityType, entityID, true
}
