package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markus-barta/paimos/backend/auth"
	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/models"
)

type projectRepoPayload struct {
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
	Label         string `json:"label"`
	SortOrder     int    `json:"sort_order"`
}

type anchorIngestRequest struct {
	RepoID        int64                          `json:"repo_id"`
	Repo          string                         `json:"repo"`
	GeneratedAt   string                         `json:"generated_at"`
	SchemaVersion string                         `json:"schema_version"`
	RepoRevision  string                         `json:"repo_revision"`
	Anchors       map[string][]anchorIngestEntry `json:"anchors"`
}

type anchorIngestEntry struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Label      string `json:"label"`
	Confidence string `json:"confidence"`
	Symbol     any    `json:"symbol"`
}

func ListProjectRepos(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	rows, err := db.DB.Query(`
		SELECT id, project_id, url, default_branch, label, sort_order, created_at, updated_at
		FROM project_repos
		WHERE project_id = ?
		ORDER BY sort_order ASC, id ASC
	`, projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []models.ProjectRepo{}
	for rows.Next() {
		var repo models.ProjectRepo
		if err := rows.Scan(&repo.ID, &repo.ProjectID, &repo.URL, &repo.DefaultBranch, &repo.Label, &repo.SortOrder, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		if repo.Label == "" {
			repo.Label = deriveRepoLabel(repo.URL)
		}
		out = append(out, repo)
	}
	jsonOK(w, out)
}

func CreateProjectRepo(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body projectRepoPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if msg := validateProjectRepoPayload(body); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	if body.SortOrder == 0 {
		_ = db.DB.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM project_repos WHERE project_id=?`, projectID).Scan(&body.SortOrder)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.DB.Exec(`
		INSERT INTO project_repos(project_id, url, default_branch, label, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)
	`, projectID, strings.TrimSpace(body.URL), defaultString(body.DefaultBranch, "main"), strings.TrimSpace(body.Label), body.SortOrder, now, now)
	if err != nil {
		jsonError(w, "insert failed", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	repo := getProjectRepoByID(id)
	if repo == nil {
		jsonError(w, "not found after insert", http.StatusInternalServerError)
		return
	}
	upsertEntityRelation(projectID, "project", projectID, "repo", id, "project_uses_repo", "declared", "")
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, repo)
}

func UpdateProjectRepo(w http.ResponseWriter, r *http.Request) {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repoId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid repo id", http.StatusBadRequest)
		return
	}
	var body projectRepoPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if msg := validateProjectRepoPayload(body); msg != "" {
		jsonError(w, msg, http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = db.DB.Exec(`
		UPDATE project_repos
		SET url=?, default_branch=?, label=?, sort_order=?, updated_at=?
		WHERE id=?
	`, strings.TrimSpace(body.URL), defaultString(body.DefaultBranch, "main"), strings.TrimSpace(body.Label), body.SortOrder, now, repoID)
	if err != nil {
		jsonError(w, "update failed", http.StatusInternalServerError)
		return
	}
	repo := getProjectRepoByID(repoID)
	if repo == nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, repo)
}

func DeleteProjectRepo(w http.ResponseWriter, r *http.Request) {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repoId"), 10, 64)
	if err != nil {
		jsonError(w, "invalid repo id", http.StatusBadRequest)
		return
	}
	var projectID int64
	_ = db.DB.QueryRow(`SELECT project_id FROM project_repos WHERE id=?`, repoID).Scan(&projectID)
	res, err := db.DB.Exec(`DELETE FROM project_repos WHERE id=?`, repoID)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	_, _ = db.DB.Exec(`DELETE FROM entity_relations WHERE (source_type='project' AND source_id=? AND target_type='repo' AND target_id=?)
		OR (source_type='repo' AND source_id=?)
		OR (target_type='repo' AND target_id=?)`, projectID, repoID, repoID, repoID)
	w.WriteHeader(http.StatusNoContent)
}

func IngestProjectAnchors(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body anchorIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.RepoID == 0 {
		jsonError(w, "repo_id required", http.StatusBadRequest)
		return
	}
	if len(body.Anchors) == 0 {
		jsonError(w, "anchors required", http.StatusBadRequest)
		return
	}
	if !projectRepoBelongsToProject(body.RepoID, projectID) {
		jsonError(w, "repo not found", http.StatusNotFound)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		jsonError(w, "begin failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	type row struct {
		IssueID     int64
		FilePath    string
		Line        int
		Label       string
		Confidence  string
		SymbolJSON  string
	}
	var inserts []row
	for issueKey, anchors := range body.Anchors {
		issueID, found := auth.ResolveIssueRef(issueKey)
		if !found {
			jsonError(w, fmt.Sprintf("unknown issue key %q", issueKey), http.StatusBadRequest)
			return
		}
		if !issueBelongsToProject(issueID, projectID) {
			jsonError(w, fmt.Sprintf("issue %q not in project", issueKey), http.StatusBadRequest)
			return
		}
		for _, a := range anchors {
			if strings.TrimSpace(a.File) == "" || a.Line <= 0 {
				jsonError(w, fmt.Sprintf("invalid anchor for %q", issueKey), http.StatusBadRequest)
				return
			}
			conf := defaultString(strings.TrimSpace(a.Confidence), "declared")
			if conf != "declared" && conf != "derived" && conf != "suggested" {
				jsonError(w, fmt.Sprintf("invalid confidence for %q", issueKey), http.StatusBadRequest)
				return
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
		jsonError(w, "load existing anchors failed", http.StatusInternalServerError)
		return
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
			jsonError(w, "delete anchor edges failed", http.StatusInternalServerError)
			return
		}
	}
	if _, err := tx.Exec(`DELETE FROM issue_anchors WHERE project_id=? AND repo_id=?`, projectID, body.RepoID); err != nil {
		jsonError(w, "delete existing anchors failed", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for _, it := range inserts {
		res, err := tx.Exec(`
			INSERT INTO issue_anchors(project_id, issue_id, repo_id, file_path, line, label, confidence, symbol_json, schema_version, repo_revision, generated_at, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		`, projectID, it.IssueID, body.RepoID, it.FilePath, it.Line, it.Label, it.Confidence, it.SymbolJSON, body.SchemaVersion, body.RepoRevision, body.GeneratedAt, now, now)
		if err != nil {
			jsonError(w, "insert anchors failed", http.StatusInternalServerError)
			return
		}
		anchorID, _ := res.LastInsertId()
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at)
			VALUES(?,?,?,?,?,?,?,?,?)
		`, projectID, "anchor", anchorID, "issue", it.IssueID, "anchored_to_issue", it.Confidence, "", now); err != nil {
			jsonError(w, "insert anchor issue edge failed", http.StatusInternalServerError)
			return
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at)
			VALUES(?,?,?,?,?,?,?,?,?)
		`, projectID, "anchor", anchorID, "repo", body.RepoID, "in_repo", it.Confidence, "", now); err != nil {
			jsonError(w, "insert anchor repo edge failed", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		jsonError(w, "commit failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{
		"repo_id": body.RepoID,
		"replaced": len(inserts),
		"generated_at": body.GeneratedAt,
		"schema_version": body.SchemaVersion,
		"repo_revision": body.RepoRevision,
	})
}

func ListIssueAnchors(w http.ResponseWriter, r *http.Request) {
	issueID, ok := resolveIssueIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid issue id", http.StatusBadRequest)
		return
	}
	projectID, found, orphan := auth.ProjectIDForIssue(issueID)
	if !found || orphan || !auth.CanViewProject(r, projectID) {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	rows, err := db.DB.Query(`
		SELECT a.id, a.project_id, a.issue_id, a.repo_id,
		       COALESCE(pr.label, ''), pr.url, pr.default_branch,
		       a.file_path, a.line, a.label, a.confidence, a.symbol_json,
		       a.schema_version, a.repo_revision, a.generated_at,
		       a.hidden, a.stale, a.updated_at
		FROM issue_anchors a
		JOIN project_repos pr ON pr.id = a.repo_id
		WHERE a.issue_id = ?
		ORDER BY pr.sort_order ASC, pr.id ASC, a.file_path ASC, a.line ASC
	`, issueID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []models.IssueAnchor{}
	for rows.Next() {
		var a models.IssueAnchor
		var hidden, stale int
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.IssueID, &a.RepoID, &a.RepoLabel, &a.RepoURL, &a.DefaultBranch, &a.FilePath, &a.Line, &a.Label, &a.Confidence, &a.SymbolJSON, &a.SchemaVersion, &a.RepoRevision, &a.GeneratedAt, &hidden, &stale, &a.UpdatedAt); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		if a.RepoLabel == "" {
			a.RepoLabel = deriveRepoLabel(a.RepoURL)
		}
		a.Hidden = hidden == 1
		a.Stale = stale == 1
		if link := buildRepoDeepLink(a.RepoURL, a.RepoRevision, a.DefaultBranch, a.FilePath, a.Line); link != "" {
			a.DeepLink = &link
		}
		out = append(out, a)
	}
	jsonOK(w, out)
}

func GetProjectManifest(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var raw string
	var updatedAt string
	var updatedBy sql.NullInt64
	err := db.DB.QueryRow(`
		SELECT manifest_json, updated_at, updated_by
		FROM project_manifests
		WHERE project_id=?
	`, projectID).Scan(&raw, &updatedAt, &updatedBy)
	if err == sql.ErrNoRows {
		jsonOK(w, models.ProjectManifest{ProjectID: projectID, Data: map[string]any{}})
		return
	}
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		data = map[string]any{}
	}
	var uid *int64
	if updatedBy.Valid {
		uid = &updatedBy.Int64
	}
	jsonOK(w, models.ProjectManifest{ProjectID: projectID, Data: data, UpdatedAt: updatedAt, UpdatedBy: uid})
}

func PutProjectManifest(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body struct {
		Data any `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	data := body.Data
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		jsonError(w, "manifest must be valid JSON", http.StatusBadRequest)
		return
	}
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	user := auth.GetUser(r)
	var uid any
	if user != nil {
		uid = user.ID
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = db.DB.Exec(`
		INSERT INTO project_manifests(project_id, manifest_json, updated_at, updated_by)
		VALUES(?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET
			manifest_json = excluded.manifest_json,
			updated_at = excluded.updated_at,
			updated_by = excluded.updated_by
	`, projectID, string(raw), now, uid)
	if err != nil {
		jsonError(w, "save failed", http.StatusInternalServerError)
		return
	}
	upsertManifestContextRelations(projectID, data)
	var dataDecoded any
	_ = json.Unmarshal(raw, &dataDecoded)
	var updatedBy *int64
	if user != nil {
		updatedBy = &user.ID
	}
	jsonOK(w, models.ProjectManifest{ProjectID: projectID, Data: dataDecoded, UpdatedAt: now, UpdatedBy: updatedBy})
}

func ListProjectEntityRelations(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	depth := 1
	if v := r.URL.Query().Get("depth"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5 {
			depth = n
		}
	}
	_ = depth // reserved for future BFS expansion; current v1 graph is edge-local.

	query := `
		SELECT id, project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at
		FROM entity_relations
		WHERE project_id = ?
	`
	args := []any{projectID}
	if root != "" {
		parts := strings.SplitN(root, ":", 2)
		if len(parts) != 2 {
			jsonError(w, "invalid root", http.StatusBadRequest)
			return
		}
		rootID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			jsonError(w, "invalid root id", http.StatusBadRequest)
			return
		}
		query += ` AND ((source_type=? AND source_id=?) OR (target_type=? AND target_id=?))`
		args = append(args, parts[0], rootID, parts[0], rootID)
	}
	query += ` ORDER BY id ASC LIMIT 500`
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []models.EntityRelation{}
	nodeSet := map[string]map[string]any{}
	for rows.Next() {
		var rel models.EntityRelation
		if err := rows.Scan(&rel.ID, &rel.ProjectID, &rel.SourceType, &rel.SourceID, &rel.TargetType, &rel.TargetID, &rel.EdgeType, &rel.Confidence, &rel.Metadata, &rel.CreatedAt); err != nil {
			jsonError(w, "scan failed", http.StatusInternalServerError)
			return
		}
		out = append(out, rel)
		nodeSet[nodeKey(rel.SourceType, rel.SourceID)] = resolveEntityNode(rel.SourceType, rel.SourceID)
		nodeSet[nodeKey(rel.TargetType, rel.TargetID)] = resolveEntityNode(rel.TargetType, rel.TargetID)
	}
	nodes := make([]map[string]any, 0, len(nodeSet))
	for _, n := range nodeSet {
		nodes = append(nodes, n)
	}
	jsonOK(w, map[string]any{"nodes": nodes, "edges": out})
}

func RetrieveProjectContext(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectIDFromRequest(r)
	if !ok {
		jsonError(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var body struct {
		Q string `json:"q"`
		K int    `json:"k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(body.Q)
	if len(q) < 2 {
		jsonOK(w, map[string]any{"hits": []any{}})
		return
	}
	if body.K <= 0 || body.K > 50 {
		body.K = 20
	}
	like := "%" + q + "%"
	hits := []map[string]any{}
	rows, err := db.DB.Query(`
		SELECT i.id, i.title, p.key, i.issue_number
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.project_id = ? AND i.deleted_at IS NULL
		  AND (i.title LIKE ? OR i.description LIKE ? OR i.acceptance_criteria LIKE ? OR i.notes LIKE ?)
		ORDER BY i.updated_at DESC
		LIMIT ?
	`, projectID, like, like, like, like, body.K)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var title, key string
			var num int
			if err := rows.Scan(&id, &title, &key, &num); err == nil {
				hits = append(hits, map[string]any{
					"entity_type": "issue",
					"entity_id": id,
					"title": title,
					"snippet": title,
					"score": nil,
					"sources": []string{"bm25"},
					"expanded_from": nil,
					"issue_key": fmt.Sprintf("%s-%d", key, num),
				})
			}
		}
	}
	anchorRows, err := db.DB.Query(`
		SELECT a.id, a.file_path, a.line, a.label, COALESCE(pr.label,''), pr.url
		FROM issue_anchors a
		JOIN project_repos pr ON pr.id = a.repo_id
		WHERE a.project_id = ? AND (a.file_path LIKE ? OR a.label LIKE ?)
		ORDER BY a.updated_at DESC
		LIMIT ?
	`, projectID, like, like, body.K)
	if err == nil {
		defer anchorRows.Close()
		for anchorRows.Next() {
			var id int64
			var filePath, label, repoLabel, repoURL string
			var line int
			if err := anchorRows.Scan(&id, &filePath, &line, &label, &repoLabel, &repoURL); err == nil {
				hits = append(hits, map[string]any{
					"entity_type": "anchor",
					"entity_id": id,
					"title": defaultString(label, filePath),
					"snippet": fmt.Sprintf("%s:%d", filePath, line),
					"score": nil,
					"sources": []string{"bm25"},
					"expanded_from": nil,
					"repo_label": defaultString(repoLabel, deriveRepoLabel(repoURL)),
				})
			}
		}
	}
	var manifestRaw string
	if err := db.DB.QueryRow(`SELECT manifest_json FROM project_manifests WHERE project_id=?`, projectID).Scan(&manifestRaw); err == nil && strings.Contains(strings.ToLower(manifestRaw), strings.ToLower(q)) {
		hits = append(hits, map[string]any{
			"entity_type": "manifest",
			"entity_id": projectID,
			"title": "Project manifest",
			"snippet": "Structured project context",
			"score": nil,
			"sources": []string{"bm25"},
			"expanded_from": nil,
		})
	}
	jsonOK(w, map[string]any{"hits": hits})
}

func projectIDFromRequest(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func resolveIssueIDFromRequest(r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, ok := auth.ResolveIssueRef(raw)
	return id, ok
}

func validateProjectRepoPayload(p projectRepoPayload) string {
	if strings.TrimSpace(p.URL) == "" {
		return "url required"
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(p.URL)); err != nil {
		return "invalid repo url"
	}
	return ""
}

func getProjectRepoByID(id int64) *models.ProjectRepo {
	var repo models.ProjectRepo
	err := db.DB.QueryRow(`
		SELECT id, project_id, url, default_branch, label, sort_order, created_at, updated_at
		FROM project_repos WHERE id=?
	`, id).Scan(&repo.ID, &repo.ProjectID, &repo.URL, &repo.DefaultBranch, &repo.Label, &repo.SortOrder, &repo.CreatedAt, &repo.UpdatedAt)
	if err != nil {
		return nil
	}
	if repo.Label == "" {
		repo.Label = deriveRepoLabel(repo.URL)
	}
	return &repo
}

func projectRepoBelongsToProject(repoID, projectID int64) bool {
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM project_repos WHERE id=? AND project_id=?`, repoID, projectID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func issueBelongsToProject(issueID, projectID int64) bool {
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM issues WHERE id=? AND project_id=?`, issueID, projectID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func defaultString(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}

func deriveRepoLabel(raw string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	trimmed = strings.TrimRight(trimmed, "/")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx < len(trimmed)-1 {
		return trimmed[idx+1:]
	}
	return trimmed
}

func buildRepoDeepLink(repoURL, revision, branch, path string, line int) string {
	base := strings.TrimSuffix(strings.TrimSpace(repoURL), ".git")
	base = strings.TrimRight(base, "/")
	if base == "" || path == "" || line <= 0 {
		return ""
	}
	ref := defaultString(revision, branch)
	if strings.Contains(base, "github.com") {
		return fmt.Sprintf("%s/blob/%s/%s#L%d", base, ref, strings.TrimLeft(path, "/"), line)
	}
	if strings.Contains(base, "gitlab") {
		return fmt.Sprintf("%s/-/blob/%s/%s#L%d", base, ref, strings.TrimLeft(path, "/"), line)
	}
	return fmt.Sprintf("%s/blob/%s/%s#L%d", base, ref, strings.TrimLeft(path, "/"), line)
}

func upsertEntityRelation(projectID int64, sourceType string, sourceID int64, targetType string, targetID int64, edgeType, confidence, metadata string) {
	_, _ = db.DB.Exec(`
		INSERT OR IGNORE INTO entity_relations(project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata)
		VALUES(?,?,?,?,?,?,?,?)
	`, projectID, sourceType, sourceID, targetType, targetID, edgeType, confidence, metadata)
}

func upsertManifestContextRelations(projectID int64, data any) {
	obj, ok := data.(map[string]any)
	if !ok {
		return
	}
	if repos, ok := obj["repos"].([]any); ok {
		for _, item := range repos {
			if m, ok := item.(map[string]any); ok {
				if idf, ok := m["id"].(float64); ok && idf > 0 {
					upsertEntityRelation(projectID, "project", projectID, "repo", int64(idf), "project_uses_repo", "declared", "")
				}
			}
		}
	}
}

func nodeKey(typ string, id int64) string { return fmt.Sprintf("%s:%d", typ, id) }

func resolveEntityNode(typ string, id int64) map[string]any {
	node := map[string]any{"entity_type": typ, "entity_id": id}
	switch typ {
	case "issue":
		var title, projectKey string
		var num int
		if err := db.DB.QueryRow(`SELECT i.title, p.key, i.issue_number FROM issues i JOIN projects p ON p.id = i.project_id WHERE i.id=?`, id).Scan(&title, &projectKey, &num); err == nil {
			node["title"] = title
			node["issue_key"] = fmt.Sprintf("%s-%d", projectKey, num)
		}
	case "repo":
		var label, url string
		if err := db.DB.QueryRow(`SELECT COALESCE(label,''), url FROM project_repos WHERE id=?`, id).Scan(&label, &url); err == nil {
			node["title"] = defaultString(label, deriveRepoLabel(url))
			node["url"] = url
		}
	case "anchor":
		var filePath string
		var line int
		if err := db.DB.QueryRow(`SELECT file_path, line FROM issue_anchors WHERE id=?`, id).Scan(&filePath, &line); err == nil {
			node["title"] = fmt.Sprintf("%s:%d", filePath, line)
		}
	case "project":
		var name string
		if err := db.DB.QueryRow(`SELECT name FROM projects WHERE id=?`, id).Scan(&name); err == nil {
			node["title"] = name
		}
	}
	return node
}
