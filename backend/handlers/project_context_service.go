package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

const timeRFC3339 = time.RFC3339

const projectContextFTSBaseRank = 60.0

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

func retrieveProjectContextHits(projectID int64, q string, k int) ([]map[string]any, map[string]any, error) {
	if err := syncProjectContextSearchIndex(projectID); err != nil {
		return nil, nil, err
	}
	ftsQuery := buildContextFTSQuery(q)
	if ftsQuery == "" {
		return []map[string]any{}, map[string]any{
			"fusion": "rrf",
			"stages": map[string]int{},
		}, nil
	}
	issueHits, err := retrieveIssueLexicalHits(projectID, ftsQuery, k)
	if err != nil {
		return nil, nil, err
	}
	contextHits, err := retrieveProjectContextLexicalHits(projectID, ftsQuery, k)
	if err != nil {
		return nil, nil, err
	}
	vectorHits, err := retrieveProjectContextVectorHits(projectID, q, k)
	if err != nil {
		return nil, nil, err
	}
	hits := fuseProjectContextRRF(k, issueHits, contextHits, vectorHits)
	expanded := appendGraphExpandedHits(projectID, hits, k)
	if len(expanded) > 0 {
		hits = fuseProjectContextRRF(k, hits, expanded)
	}
	meta := map[string]any{
		"fusion": "rrf",
		"k":      k,
		"stages": map[string]int{
			"issue_lexical":   len(issueHits),
			"context_lexical": len(contextHits),
			"vector":          len(vectorHits),
			"graph_expansion": len(expanded),
			"final":           len(hits),
		},
	}
	return hits, meta, nil
}

func buildContextFTSQuery(q string) string {
	parts := strings.Fields(strings.TrimSpace(q))
	if len(parts) == 0 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, `"'`))
		if part == "" {
			continue
		}
		out = append(out, quoteFTSTerm(part)+"*")
	}
	return strings.Join(out, " ")
}

func quoteFTSTerm(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
}

func retrieveIssueLexicalHits(projectID int64, ftsQuery string, k int) ([]map[string]any, error) {
	rows, err := db.DB.Query(`
		SELECT i.id, i.title, p.key, i.issue_number,
		       snippet(search_index, 2, '[', ']', ' … ', 18),
		       bm25(search_index)
		FROM search_index si
		JOIN issues i ON i.id = CAST(si.entity_id AS INTEGER)
		JOIN projects p ON p.id = i.project_id
		WHERE si.entity_type = 'issue'
		  AND i.project_id = ?
		  AND i.deleted_at IS NULL
		  AND search_index MATCH ?
		ORDER BY bm25(search_index), i.updated_at DESC
		LIMIT ?
	`, projectID, ftsQuery, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := make([]map[string]any, 0, k)
	for rows.Next() {
		var id int64
		var title, key, snippet string
		var num int
		var score float64
		if err := rows.Scan(&id, &title, &key, &num, &snippet, &score); err != nil {
			return nil, err
		}
		if strings.TrimSpace(snippet) == "" {
			snippet = title
		}
		hits = append(hits, map[string]any{
			"entity_type":   "issue",
			"entity_id":     id,
			"title":         title,
			"snippet":       snippet,
			"score":         score,
			"sources":       []string{"bm25"},
			"expanded_from": nil,
			"issue_key":     fmt.Sprintf("%s-%d", key, num),
		})
	}
	return hits, nil
}

func retrieveProjectContextLexicalHits(projectID int64, ftsQuery string, k int) ([]map[string]any, error) {
	rows, err := db.DB.Query(`
		SELECT entity_type, entity_key, title,
		       snippet(project_context_index, 4, '[', ']', ' … ', 18),
		       bm25(project_context_index), metadata_json
		FROM project_context_index
		WHERE project_id = ?
		  AND project_context_index MATCH ?
		ORDER BY bm25(project_context_index), rowid ASC
		LIMIT ?
	`, projectID, ftsQuery, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := make([]map[string]any, 0, k)
	for rows.Next() {
		var entityType, entityKey, title, snippet, rawMeta string
		var score float64
		if err := rows.Scan(&entityType, &entityKey, &title, &snippet, &score, &rawMeta); err != nil {
			return nil, err
		}
		meta := map[string]any{}
		if strings.TrimSpace(rawMeta) != "" {
			_ = json.Unmarshal([]byte(rawMeta), &meta)
		}
		hit := map[string]any{
			"entity_type":   entityType,
			"entity_id":     contextSearchEntityID(entityType, entityKey, meta),
			"title":         title,
			"snippet":       defaultString(strings.TrimSpace(snippet), title),
			"score":         score,
			"sources":       []string{"bm25"},
			"expanded_from": nil,
		}
		for key, value := range meta {
			hit[key] = value
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func contextSearchEntityID(entityType, entityKey string, meta map[string]any) any {
	switch entityType {
	case "anchor":
		if id, ok := meta["anchor_id"]; ok {
			return id
		}
	case "manifest", "adr", "nfr":
		if id, ok := meta["project_id"]; ok {
			return id
		}
	}
	parts := strings.Split(entityKey, ":")
	if len(parts) >= 2 {
		if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			return id
		}
	}
	return entityKey
}

func fuseProjectContextRRF(k int, groups ...[]map[string]any) []map[string]any {
	type fused struct {
		hit   map[string]any
		score float64
		order int
	}
	merged := map[string]*fused{}
	mergeSources := func(dst map[string]any, src []string) {
		seen := map[string]bool{}
		for _, existing := range anyToStringSlice(dst["sources"]) {
			seen[existing] = true
		}
		for _, item := range src {
			if !seen[item] {
				dst["sources"] = append(anyToStringSlice(dst["sources"]), item)
				seen[item] = true
			}
		}
	}
	order := 0
	for _, group := range groups {
		for rank, hit := range group {
			key := fmt.Sprintf("%v:%v", hit["entity_type"], hit["entity_id"])
			contribution := 1.0 / (projectContextFTSBaseRank + float64(rank+1))
			if existing, ok := merged[key]; ok {
				existing.score += contribution
				mergeSources(existing.hit, anyToStringSlice(hit["sources"]))
				if existing.hit["expanded_from"] == nil && hit["expanded_from"] != nil {
					existing.hit["expanded_from"] = hit["expanded_from"]
				}
				continue
			}
			copied := map[string]any{}
			for k, v := range hit {
				copied[k] = v
			}
			copied["sources"] = anyToStringSlice(hit["sources"])
			merged[key] = &fused{hit: copied, score: contribution, order: order}
			order++
		}
	}
	list := make([]*fused, 0, len(merged))
	for _, item := range merged {
		item.hit["score"] = item.score
		item.hit["fusion"] = "rrf"
		list = append(list, item)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score == list[j].score {
			return list[i].order < list[j].order
		}
		return list[i].score > list[j].score
	})
	out := make([]map[string]any, 0, minInt(k, len(list)))
	for _, item := range list {
		if len(out) >= k {
			break
		}
		out = append(out, item.hit)
	}
	return out
}

func appendGraphExpandedHits(projectID int64, hits []map[string]any, k int) []map[string]any {
	out := []map[string]any{}
	seenRoots := map[int64]bool{}
	for _, hit := range hits {
		if hit["entity_type"] != "issue" {
			continue
		}
		issueID, ok := anyToInt64(hit["entity_id"])
		if !ok || seenRoots[issueID] {
			continue
		}
		seenRoots[issueID] = true
		for _, expanded := range expandContextNeighbors(projectID, "issue", issueID) {
			expanded["sources"] = []string{"graph"}
			expanded["expanded_from"] = map[string]any{
				"entity_type": "issue",
				"entity_id":   issueID,
				"title":       hit["title"],
			}
			out = append(out, expanded)
			if len(out) >= k {
				return out
			}
		}
	}
	return out
}

func anyToInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	case json.Number:
		id, err := n.Int64()
		return id, err == nil
	}
	return 0, false
}

func anyToStringSlice(v any) []string {
	switch vv := v.(type) {
	case []string:
		return append([]string(nil), vv...)
	case []any:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case nil:
		return []string{}
	default:
		return []string{}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func syncProjectContextSearchIndex(projectID int64) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM project_context_index WHERE project_id=?`, projectID); err != nil {
		return err
	}
	if err := insertProjectAnchorSearchDocs(tx, projectID); err != nil {
		return err
	}
	if err := insertProjectManifestSearchDocs(tx, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

func insertProjectAnchorSearchDocs(tx *sql.Tx, projectID int64) error {
	rows, err := tx.Query(`
		SELECT a.id, a.issue_id, a.repo_id, a.file_path, a.line, a.label, a.symbol_json,
		       COALESCE(pr.label,''), pr.url, COALESCE(p.key,''), i.issue_number
		FROM issue_anchors a
		JOIN project_repos pr ON pr.id = a.repo_id
		JOIN issues i ON i.id = a.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE a.project_id = ?
		ORDER BY a.id ASC
	`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var anchorID, issueID, repoID int64
		var filePath, label, symbolJSON, repoLabel, repoURL, projectKey string
		var line, issueNumber int
		if err := rows.Scan(&anchorID, &issueID, &repoID, &filePath, &line, &label, &symbolJSON, &repoLabel, &repoURL, &projectKey, &issueNumber); err != nil {
			return err
		}
		title := defaultString(strings.TrimSpace(label), filePath)
		meta := map[string]any{
			"anchor_id":   anchorID,
			"issue_id":    issueID,
			"issue_key":   fmt.Sprintf("%s-%d", projectKey, issueNumber),
			"file_path":   filePath,
			"line":        line,
			"repo_label":  defaultString(repoLabel, deriveRepoLabel(repoURL)),
			"repo_url":    repoURL,
			"repo_id":     repoID,
			"section_key": fmt.Sprintf("anchor:%d", anchorID),
		}
		content := strings.Join([]string{
			title,
			filePath,
			fmt.Sprintf("%s-%d", projectKey, issueNumber),
			defaultString(repoLabel, deriveRepoLabel(repoURL)),
		}, " ")
		if err := insertProjectContextDoc(tx, projectID, "anchor", fmt.Sprintf("anchor:%d", anchorID), title, content, meta); err != nil {
			return err
		}
		if sym, ok := decodeStoredAnchorSymbol(symbolJSON); ok {
			symbolID := symbolIDForAnchor(repoID, filePath, *sym)
			symbolTitle := fmt.Sprintf("%s %s", defaultString(sym.Kind, "symbol"), sym.Name)
			symbolContent := strings.Join([]string{
				sym.Name,
				sym.Kind,
				sym.Language,
				filePath,
				fmt.Sprintf("%s-%d", projectKey, issueNumber),
			}, " ")
			symbolMeta := map[string]any{
				"project_id":  projectID,
				"repo_id":     repoID,
				"file_path":   filePath,
				"symbol_name": sym.Name,
				"kind":        sym.Kind,
				"language":    sym.Language,
				"start_line":  sym.StartLine,
				"end_line":    sym.EndLine,
				"section_key": fmt.Sprintf("symbol:%d", symbolID),
			}
			if err := insertProjectContextDoc(tx, projectID, "symbol", fmt.Sprintf("symbol:%d", symbolID), symbolTitle, symbolContent, symbolMeta); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

func insertProjectManifestSearchDocs(tx *sql.Tx, projectID int64) error {
	var raw string
	if err := tx.QueryRow(`SELECT manifest_json FROM project_manifests WHERE project_id=?`, projectID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		data = map[string]any{}
	}
	if err := insertProjectContextDoc(tx, projectID, "manifest", fmt.Sprintf("manifest:%d", projectID), "Project manifest", flattenContextText(data), map[string]any{
		"project_id":  projectID,
		"section_key": "manifest",
	}); err != nil {
		return err
	}
	if list, ok := data["nfrs"].([]any); ok {
		for idx, item := range list {
			title := manifestEntryTitle("NFR", idx, item)
			if err := insertProjectContextDoc(tx, projectID, "nfr", fmt.Sprintf("nfr:%d:%d", projectID, idx+1), title, flattenContextText(item), map[string]any{
				"project_id":  projectID,
				"section_key": fmt.Sprintf("nfr:%d", idx+1),
			}); err != nil {
				return err
			}
		}
	}
	if list, ok := data["adrs"].([]any); ok {
		for idx, item := range list {
			title := manifestEntryTitle("ADR", idx, item)
			if err := insertProjectContextDoc(tx, projectID, "adr", fmt.Sprintf("adr:%d:%d", projectID, idx+1), title, flattenContextText(item), map[string]any{
				"project_id":  projectID,
				"section_key": fmt.Sprintf("adr:%d", idx+1),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertProjectContextDoc(tx *sql.Tx, projectID int64, entityType, entityKey, title, content string, metadata map[string]any) error {
	rawMeta := "{}"
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			rawMeta = string(b)
		}
	}
	_, err := tx.Exec(`
		INSERT INTO project_context_index(project_id, entity_type, entity_key, title, content, metadata_json)
		VALUES(?,?,?,?,?,?)
	`, projectID, entityType, entityKey, title, content, rawMeta)
	return err
}

func flattenContextText(v any) string {
	var parts []string
	var walk func(any)
	walk = func(node any) {
		switch current := node.(type) {
		case map[string]any:
			keys := make([]string, 0, len(current))
			for key := range current {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				parts = append(parts, key)
				walk(current[key])
			}
		case []any:
			for _, item := range current {
				walk(item)
			}
		case string:
			if s := strings.TrimSpace(current); s != "" {
				parts = append(parts, s)
			}
		case fmt.Stringer:
			if s := strings.TrimSpace(current.String()); s != "" {
				parts = append(parts, s)
			}
		case nil:
			return
		default:
			if s := strings.TrimSpace(fmt.Sprint(current)); s != "" {
				parts = append(parts, s)
			}
		}
	}
	walk(v)
	return strings.Join(parts, " ")
}

func manifestEntryTitle(prefix string, idx int, item any) string {
	if obj, ok := item.(map[string]any); ok {
		for _, key := range []string{"title", "name", "id", "key"} {
			if s, ok := obj[key].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return fmt.Sprintf("%s %d", prefix, idx+1)
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
	if _, err := tx.Exec(`DELETE FROM entity_relations WHERE source_type='symbol' AND target_type='repo' AND target_id=?`, body.RepoID); err != nil {
		return nil, &userError{msg: "delete symbol repo edges failed", status: 500}
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
		if sym, ok := decodeStoredAnchorSymbol(it.SymbolJSON); ok {
			symbolID := symbolIDForAnchor(body.RepoID, it.FilePath, *sym)
			metadata := symbolMetadataJSON(body.RepoID, it.FilePath, *sym)
			if _, err := tx.Exec(`
				INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at)
				VALUES(?,?,?,?,?,?,?,?,?)
			`, projectID, "anchor", anchorID, "symbol", symbolID, "anchored_inside", "derived", metadata, now); err != nil {
				return nil, &userError{msg: "insert anchor symbol edge failed", status: 500}
			}
			if _, err := tx.Exec(`
				INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at)
				VALUES(?,?,?,?,?,?,?,?,?)
			`, projectID, "symbol", symbolID, "repo", body.RepoID, "in_repo", "derived", metadata, now); err != nil {
				return nil, &userError{msg: "insert symbol repo edge failed", status: 500}
			}
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
