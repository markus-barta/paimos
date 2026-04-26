package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

const timeRFC3339 = time.RFC3339

func listProjectReposData(projectID int64) ([]models.ProjectRepo, error) {
	rows, err := db.DB.Query(`
		SELECT id, project_id, url, default_branch, label, sort_order, created_at, updated_at
		FROM project_repos
		WHERE project_id = ?
		ORDER BY sort_order ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ProjectRepo{}
	for rows.Next() {
		var repo models.ProjectRepo
		if err := rows.Scan(&repo.ID, &repo.ProjectID, &repo.URL, &repo.DefaultBranch, &repo.Label, &repo.SortOrder, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			return nil, err
		}
		if repo.Label == "" {
			repo.Label = deriveRepoLabel(repo.URL)
		}
		out = append(out, repo)
	}
	return out, nil
}

func saveProjectManifest(projectID int64, data any, updatedBy *int64, now string) (models.ProjectManifest, error) {
	raw, err := normalizeManifestJSON(data)
	if err != nil {
		return models.ProjectManifest{}, err
	}
	var uid any
	if updatedBy != nil {
		uid = *updatedBy
	}
	if _, err := db.DB.Exec(`
		INSERT INTO project_manifests(project_id, manifest_json, updated_at, updated_by)
		VALUES(?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET
			manifest_json = excluded.manifest_json,
			updated_at = excluded.updated_at,
			updated_by = excluded.updated_by
	`, projectID, string(raw), now, uid); err != nil {
		return models.ProjectManifest{}, err
	}
	upsertManifestContextRelations(projectID, data)
	var decoded any
	_ = json.Unmarshal(raw, &decoded)
	return models.ProjectManifest{ProjectID: projectID, Data: decoded, UpdatedAt: now, UpdatedBy: updatedBy}, nil
}

func normalizeManifestJSON(data any) ([]byte, error) {
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	return raw, nil
}

func retrieveProjectContextHits(projectID int64, q string, k int) ([]map[string]any, error) {
	like := "%" + q + "%"
	issueHits := []map[string]any{}
	rows, err := db.DB.Query(`
		SELECT i.id, i.title, p.key, i.issue_number
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.project_id = ? AND i.deleted_at IS NULL
		  AND (i.title LIKE ? OR i.description LIKE ? OR i.acceptance_criteria LIKE ? OR i.notes LIKE ?)
		ORDER BY i.updated_at DESC
		LIMIT ?
	`, projectID, like, like, like, like, k)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var title, key string
			var num int
			if err := rows.Scan(&id, &title, &key, &num); err == nil {
				issueHits = append(issueHits, map[string]any{
					"entity_type":   "issue",
					"entity_id":     id,
					"title":         title,
					"snippet":       title,
					"score":         nil,
					"sources":       []string{"bm25"},
					"expanded_from": nil,
					"issue_key":     fmt.Sprintf("%s-%d", key, num),
				})
			}
		}
	}
	hits := make([]map[string]any, 0, k)
	seen := map[string]bool{}
	addHit := func(hit map[string]any) {
		key := fmt.Sprintf("%v:%v", hit["entity_type"], hit["entity_id"])
		if seen[key] || len(hits) >= k {
			return
		}
		seen[key] = true
		hits = append(hits, hit)
	}
	for _, hit := range issueHits {
		addHit(hit)
	}
	anchorRows, err := db.DB.Query(`
		SELECT a.id, a.file_path, a.line, a.label, COALESCE(pr.label,''), pr.url
		FROM issue_anchors a
		JOIN project_repos pr ON pr.id = a.repo_id
		WHERE a.project_id = ? AND (a.file_path LIKE ? OR a.label LIKE ?)
		ORDER BY a.updated_at DESC
		LIMIT ?
	`, projectID, like, like, k)
	if err == nil {
		defer anchorRows.Close()
		for anchorRows.Next() {
			var id int64
			var filePath, label, repoLabel, repoURL string
			var line int
			if err := anchorRows.Scan(&id, &filePath, &line, &label, &repoLabel, &repoURL); err == nil {
				addHit(map[string]any{
					"entity_type":   "anchor",
					"entity_id":     id,
					"title":         defaultString(label, filePath),
					"snippet":       fmt.Sprintf("%s:%d", filePath, line),
					"score":         nil,
					"sources":       []string{"bm25"},
					"expanded_from": nil,
					"repo_label":    defaultString(repoLabel, deriveRepoLabel(repoURL)),
				})
			}
		}
	}
	var manifestRaw string
	if err := db.DB.QueryRow(`SELECT manifest_json FROM project_manifests WHERE project_id=?`, projectID).Scan(&manifestRaw); err == nil && strings.Contains(strings.ToLower(manifestRaw), strings.ToLower(q)) {
		addHit(map[string]any{
			"entity_type":   "manifest",
			"entity_id":     projectID,
			"title":         "Project manifest",
			"snippet":       "Structured project context",
			"score":         nil,
			"sources":       []string{"bm25"},
			"expanded_from": nil,
		})
	}
	for _, hit := range issueHits {
		issueID, _ := hit["entity_id"].(int64)
		for _, expanded := range expandContextNeighbors(projectID, "issue", issueID) {
			expanded["expanded_from"] = map[string]any{
				"entity_type": "issue",
				"entity_id":   issueID,
				"title":       hit["title"],
			}
			addHit(expanded)
		}
	}
	return hits, nil
}

func replaceProjectAnchors(projectID int64, body anchorIngestRequest) (map[string]any, *userError) {
	if body.RepoID == 0 {
		return nil, &userError{msg: "repo_id required", status: 400}
	}
	if len(body.Anchors) == 0 {
		return nil, &userError{msg: "anchors required", status: 400}
	}
	if !projectRepoBelongsToProject(body.RepoID, projectID) {
		return nil, &userError{msg: "repo not found", status: 404}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, &userError{msg: "begin failed", status: 500}
	}
	defer tx.Rollback()
	type row struct {
		IssueID    int64
		FilePath   string
		Line       int
		Label      string
		Confidence string
		SymbolJSON string
	}
	var inserts []row
	for issueKey, anchors := range body.Anchors {
		issueID, found := auth.ResolveIssueRef(issueKey)
		if !found {
			return nil, &userError{msg: fmt.Sprintf("unknown issue key %q", issueKey), status: 400}
		}
		if !issueBelongsToProject(issueID, projectID) {
			return nil, &userError{msg: fmt.Sprintf("issue %q not in project", issueKey), status: 400}
		}
		for _, a := range anchors {
			if strings.TrimSpace(a.File) == "" || a.Line <= 0 {
				return nil, &userError{msg: fmt.Sprintf("invalid anchor for %q", issueKey), status: 400}
			}
			conf := defaultString(strings.TrimSpace(a.Confidence), "declared")
			if conf != "declared" && conf != "derived" && conf != "suggested" {
				return nil, &userError{msg: fmt.Sprintf("invalid confidence for %q", issueKey), status: 400}
			}
			symbolJSON := ""
			if a.Symbol != nil {
				b, _ := json.Marshal(a.Symbol)
				symbolJSON = string(b)
			}
			inserts = append(inserts, row{
				IssueID: issueID, FilePath: strings.TrimSpace(a.File), Line: a.Line,
				Label: strings.TrimSpace(a.Label), Confidence: conf, SymbolJSON: symbolJSON,
			})
		}
	}
	var oldAnchorIDs []int64
	oldRows, err := tx.Query(`SELECT id FROM issue_anchors WHERE project_id=? AND repo_id=?`, projectID, body.RepoID)
	if err != nil {
		return nil, &userError{msg: "load existing anchors failed", status: 500}
	}
	for oldRows.Next() {
		var id int64
		if err := oldRows.Scan(&id); err == nil {
			oldAnchorIDs = append(oldAnchorIDs, id)
		}
	}
	oldRows.Close()
	for _, id := range oldAnchorIDs {
		if _, err := tx.Exec(`DELETE FROM entity_relations WHERE source_type='anchor' AND source_id=?`, id); err != nil {
			return nil, &userError{msg: "delete anchor edges failed", status: 500}
		}
	}
	if _, err := tx.Exec(`DELETE FROM issue_anchors WHERE project_id=? AND repo_id=?`, projectID, body.RepoID); err != nil {
		return nil, &userError{msg: "delete existing anchors failed", status: 500}
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for _, it := range inserts {
		res, err := tx.Exec(`
			INSERT INTO issue_anchors(project_id, issue_id, repo_id, file_path, line, label, confidence, symbol_json, schema_version, repo_revision, generated_at, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		`, projectID, it.IssueID, body.RepoID, it.FilePath, it.Line, it.Label, it.Confidence, it.SymbolJSON, body.SchemaVersion, body.RepoRevision, body.GeneratedAt, now, now)
		if err != nil {
			return nil, &userError{msg: "insert anchors failed", status: 500}
		}
		anchorID, _ := res.LastInsertId()
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at)
			VALUES(?,?,?,?,?,?,?,?,?)
		`, projectID, "anchor", anchorID, "issue", it.IssueID, "anchored_to_issue", it.Confidence, "", now); err != nil {
			return nil, &userError{msg: "insert anchor issue edge failed", status: 500}
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at)
			VALUES(?,?,?,?,?,?,?,?,?)
		`, projectID, "anchor", anchorID, "repo", body.RepoID, "in_repo", it.Confidence, "", now); err != nil {
			return nil, &userError{msg: "insert anchor repo edge failed", status: 500}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, &userError{msg: "commit failed", status: 500}
	}
	return map[string]any{
		"repo_id":        body.RepoID,
		"replaced":       len(inserts),
		"generated_at":   body.GeneratedAt,
		"schema_version": body.SchemaVersion,
		"repo_revision":  body.RepoRevision,
	}, nil
}

func loadProjectManifest(projectID int64) (models.ProjectManifest, error) {
	var raw string
	var updatedAt string
	var updatedBy sql.NullInt64
	err := db.DB.QueryRow(`
		SELECT manifest_json, updated_at, updated_by
		FROM project_manifests
		WHERE project_id=?
	`, projectID).Scan(&raw, &updatedAt, &updatedBy)
	if err == sql.ErrNoRows {
		return models.ProjectManifest{ProjectID: projectID, Data: map[string]any{}}, nil
	}
	if err != nil {
		return models.ProjectManifest{}, err
	}
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		data = map[string]any{}
	}
	var uid *int64
	if updatedBy.Valid {
		uid = &updatedBy.Int64
	}
	return models.ProjectManifest{ProjectID: projectID, Data: data, UpdatedAt: updatedAt, UpdatedBy: uid}, nil
}
