package handlers

import (
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
	out, err := listProjectReposData(projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
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
	deleteAnchorEntityRelationsByRepo(repoID)
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
	result, userErr := replaceProjectAnchors(projectID, body)
	if userErr != nil {
		jsonError(w, userErr.msg, userErr.status)
		return
	}
	jsonOK(w, result)
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
	manifest, err := loadProjectManifest(projectID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, manifest)
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
	user := auth.GetUser(r)
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	var updatedBy *int64
	if user != nil {
		updatedBy = &user.ID
	}
	manifest, err := saveProjectManifest(projectID, data, updatedBy, now)
	if err != nil {
		jsonError(w, "save failed", http.StatusInternalServerError)
		return
	}
	jsonOK(w, manifest)
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
	out, err := fetchEntityGraph(projectID, root, depth)
	if err != nil {
		if err == errInvalidRoot {
			jsonError(w, "invalid root", http.StatusBadRequest)
			return
		}
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	nodeSet := map[string]map[string]any{}
	for _, rel := range out {
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
	hits, err := retrieveProjectContextHits(projectID, q, body.K)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
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

func deleteEntityRelation(projectID int64, sourceType string, sourceID int64, targetType string, targetID int64, edgeType string) {
	_, _ = db.DB.Exec(`
		DELETE FROM entity_relations
		WHERE project_id=? AND source_type=? AND source_id=? AND target_type=? AND target_id=? AND edge_type=?
	`, projectID, sourceType, sourceID, targetType, targetID, edgeType)
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

var errInvalidRoot = fmt.Errorf("invalid root")

func fetchEntityGraph(projectID int64, root string, depth int) ([]models.EntityRelation, error) {
	if root == "" {
		return queryEntityRelations(`
			SELECT id, project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at
			FROM entity_relations
			WHERE project_id = ?
			ORDER BY id ASC
			LIMIT 500
		`, projectID)
	}
	parts := strings.SplitN(root, ":", 2)
	if len(parts) != 2 {
		return nil, errInvalidRoot
	}
	rootID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || rootID <= 0 {
		return nil, errInvalidRoot
	}
	type queueItem struct {
		typ   string
		id    int64
		depth int
	}
	queue := []queueItem{{typ: parts[0], id: rootID, depth: 0}}
	visitedNodes := map[string]bool{nodeKey(parts[0], rootID): true}
	visitedEdges := map[int64]bool{}
	out := make([]models.EntityRelation, 0, 32)
	for len(queue) > 0 && len(out) < 500 {
		item := queue[0]
		queue = queue[1:]
		rows, err := queryEntityRelations(`
			SELECT id, project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at
			FROM entity_relations
			WHERE project_id = ? AND ((source_type=? AND source_id=?) OR (target_type=? AND target_id=?))
			ORDER BY id ASC
			LIMIT 200
		`, projectID, item.typ, item.id, item.typ, item.id)
		if err != nil {
			return nil, err
		}
		for _, rel := range rows {
			if visitedEdges[rel.ID] {
				continue
			}
			visitedEdges[rel.ID] = true
			out = append(out, rel)
			if item.depth >= depth-1 {
				continue
			}
			srcKey := nodeKey(rel.SourceType, rel.SourceID)
			if !visitedNodes[srcKey] {
				visitedNodes[srcKey] = true
				queue = append(queue, queueItem{typ: rel.SourceType, id: rel.SourceID, depth: item.depth + 1})
			}
			tgtKey := nodeKey(rel.TargetType, rel.TargetID)
			if !visitedNodes[tgtKey] {
				visitedNodes[tgtKey] = true
				queue = append(queue, queueItem{typ: rel.TargetType, id: rel.TargetID, depth: item.depth + 1})
			}
		}
	}
	return out, nil
}

func queryEntityRelations(query string, args ...any) ([]models.EntityRelation, error) {
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.EntityRelation{}
	for rows.Next() {
		var rel models.EntityRelation
		if err := rows.Scan(&rel.ID, &rel.ProjectID, &rel.SourceType, &rel.SourceID, &rel.TargetType, &rel.TargetID, &rel.EdgeType, &rel.Confidence, &rel.Metadata, &rel.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, nil
}

func expandContextNeighbors(projectID int64, entityType string, entityID int64) []map[string]any {
	rows, err := queryEntityRelations(`
		SELECT id, project_id, source_type, source_id, target_type, target_id, edge_type, confidence, metadata, created_at
		FROM entity_relations
		WHERE project_id = ? AND ((source_type=? AND source_id=?) OR (target_type=? AND target_id=?))
		ORDER BY id ASC
		LIMIT 12
	`, projectID, entityType, entityID, entityType, entityID)
	if err != nil {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, rel := range rows {
		neighborType := rel.TargetType
		neighborID := rel.TargetID
		if rel.SourceType != entityType || rel.SourceID != entityID {
			neighborType = rel.SourceType
			neighborID = rel.SourceID
		}
		node := resolveEntityNode(neighborType, neighborID)
		title, _ := node["title"].(string)
		out = append(out, map[string]any{
			"entity_type":   neighborType,
			"entity_id":     neighborID,
			"title":         defaultString(title, fmt.Sprintf("%s:%d", neighborType, neighborID)),
			"snippet":       rel.EdgeType,
			"score":         nil,
			"sources":       []string{"graph"},
			"expanded_from": nil,
			"edge_type":     rel.EdgeType,
		})
	}
	return out
}

func upsertIssueEntityRelation(sourceID, targetID int64, edgeType string) {
	var projectID int64
	if err := db.DB.QueryRow(`
		SELECT project_id
		FROM issues
		WHERE id=? AND project_id = (SELECT project_id FROM issues WHERE id=?)
	`, sourceID, targetID).Scan(&projectID); err != nil || projectID == 0 {
		return
	}
	upsertEntityRelation(projectID, "issue", sourceID, "issue", targetID, edgeType, "declared", "")
}

func deleteIssueEntityRelation(sourceID, targetID int64, edgeType string) {
	var projectID int64
	if err := db.DB.QueryRow(`
		SELECT project_id
		FROM issues
		WHERE id=? AND project_id = (SELECT project_id FROM issues WHERE id=?)
	`, sourceID, targetID).Scan(&projectID); err != nil || projectID == 0 {
		return
	}
	deleteEntityRelation(projectID, "issue", sourceID, "issue", targetID, edgeType)
}

func deleteAnchorEntityRelationsByRepo(repoID int64) {
	rows, err := db.DB.Query(`SELECT id FROM issue_anchors WHERE repo_id=?`, repoID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var anchorID int64
		if rows.Scan(&anchorID) == nil {
			_, _ = db.DB.Exec(`DELETE FROM entity_relations WHERE source_type='anchor' AND source_id=?`, anchorID)
			_, _ = db.DB.Exec(`DELETE FROM entity_relations WHERE target_type='anchor' AND target_id=?`, anchorID)
		}
	}
}

func deleteAnchorEntityRelationsByIssueIDs(issueIDs []int64) {
	if len(issueIDs) == 0 {
		return
	}
	ph := makePlaceholders(len(issueIDs))
	args := make([]any, 0, len(issueIDs))
	for _, id := range issueIDs {
		args = append(args, id)
	}
	rows, err := db.DB.Query(`SELECT id FROM issue_anchors WHERE issue_id IN (`+ph+`)`, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var anchorID int64
		if rows.Scan(&anchorID) == nil {
			_, _ = db.DB.Exec(`DELETE FROM entity_relations WHERE source_type='anchor' AND source_id=?`, anchorID)
			_, _ = db.DB.Exec(`DELETE FROM entity_relations WHERE target_type='anchor' AND target_id=?`, anchorID)
		}
	}
}

func makePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
