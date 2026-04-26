package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/db"
)

const (
	projectContextEmbeddingModel = "local-hash-v1"
	projectContextEmbeddingDim   = 256
)

var embeddingTokenPattern = regexp.MustCompile(`[A-Za-z0-9_./:-]+`)

type retrievalDoc struct {
	EntityType string
	EntityID   int64
	Title      string
	Content    string
	Hit        map[string]any
}

func retrieveProjectContextVectorHits(projectID int64, q string, k int) ([]map[string]any, error) {
	docs, err := collectProjectRetrievalDocs(projectID)
	if err != nil {
		return nil, err
	}
	if err := syncProjectContextEmbeddings(projectID, docs); err != nil {
		return nil, err
	}
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

func syncProjectContextEmbeddings(projectID int64, docs []retrievalDoc) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM entity_embeddings WHERE project_id = ? AND model = ?`, projectID, projectContextEmbeddingModel); err != nil {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for _, doc := range docs {
		text := strings.TrimSpace(doc.Title + "\n" + doc.Content)
		if text == "" {
			continue
		}
		vec := embedTextDeterministic(text)
		raw, err := encodeEmbedding(vec)
		if err != nil {
			return err
		}
		hash := sha256.Sum256([]byte(text))
		if _, err := tx.Exec(`
			INSERT INTO entity_embeddings(project_id, entity_type, entity_id, model, dim, vector, source_hash, last_indexed_at)
			VALUES(?,?,?,?,?,?,?,?)
		`, projectID, doc.EntityType, doc.EntityID, projectContextEmbeddingModel, len(vec), raw, fmt.Sprintf("%x", hash[:]), now); err != nil {
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
	manifestDocs, err := collectProjectManifestDocs(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]retrievalDoc, 0, len(issues)+len(anchors)+len(manifestDocs))
	out = append(out, issues...)
	out = append(out, anchors...)
	out = append(out, manifestDocs...)
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

func collectProjectManifestDocs(projectID int64) ([]retrievalDoc, error) {
	var raw string
	err := db.DB.QueryRow(`SELECT manifest_json FROM project_manifests WHERE project_id=?`, projectID).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		data = map[string]any{}
	}
	out := []retrievalDoc{{
		EntityType: "manifest",
		EntityID:   projectID,
		Title:      "Project manifest",
		Content:    flattenContextText(data),
		Hit: map[string]any{
			"entity_type":   "manifest",
			"entity_id":     projectID,
			"title":         "Project manifest",
			"snippet":       "Structured project context",
			"project_id":    projectID,
			"expanded_from": nil,
		},
	}}
	if list, ok := data["nfrs"].([]any); ok {
		for idx, item := range list {
			title := manifestEntryTitle("NFR", idx, item)
			out = append(out, retrievalDoc{
				EntityType: "nfr",
				EntityID:   int64(idx + 1),
				Title:      title,
				Content:    flattenContextText(item),
				Hit: map[string]any{
					"entity_type":   "nfr",
					"entity_id":     int64(idx + 1),
					"title":         title,
					"snippet":       title,
					"project_id":    projectID,
					"section_key":   fmt.Sprintf("nfr:%d", idx+1),
					"expanded_from": nil,
				},
			})
		}
	}
	if list, ok := data["adrs"].([]any); ok {
		for idx, item := range list {
			title := manifestEntryTitle("ADR", idx, item)
			out = append(out, retrievalDoc{
				EntityType: "adr",
				EntityID:   int64(idx + 1),
				Title:      title,
				Content:    flattenContextText(item),
				Hit: map[string]any{
					"entity_type":   "adr",
					"entity_id":     int64(idx + 1),
					"title":         title,
					"snippet":       title,
					"project_id":    projectID,
					"section_key":   fmt.Sprintf("adr:%d", idx+1),
					"expanded_from": nil,
				},
			})
		}
	}
	return out, nil
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
