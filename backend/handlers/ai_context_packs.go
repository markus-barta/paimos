// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
)

const (
	aiContextPackIssue     = "issue"
	aiContextPackKnowledge = "knowledge"
	aiContextPackRetrieve  = "retrieve"
	aiContextPackRepo      = "repo"

	aiContextPackBudgetBytes = 12_000
	aiContextPackMaxHits     = 8
)

type aiContextPackChoice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type aiContextSource struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Count     int    `json:"count,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func listAIContextPackChoices(projectID int64) []aiContextPackChoice {
	out := []aiContextPackChoice{
		aiContextPackChoiceFor(aiContextPackIssue),
	}
	if projectID <= 0 {
		return out
	}
	out = append(out,
		aiContextPackChoiceFor(aiContextPackKnowledge),
		aiContextPackChoiceFor(aiContextPackRetrieve),
	)
	if projectHasAnchorContext(projectID) {
		out = append(out, aiContextPackChoiceFor(aiContextPackRepo))
	}
	return out
}

func aiContextPackChoiceFor(id string) aiContextPackChoice {
	switch id {
	case aiContextPackKnowledge:
		return aiContextPackChoice{ID: id, Label: "Project knowledge", Description: "Issue plus project memory, runbooks, and guidelines."}
	case aiContextPackRetrieve:
		return aiContextPackChoice{ID: id, Label: "Retrieved context", Description: "Issue plus mixed project-context retrieval."}
	case aiContextPackRepo:
		return aiContextPackChoice{ID: id, Label: "Repo-aware bundle", Description: "Issue plus retrieval and uploaded code anchors."}
	default:
		return aiContextPackChoice{ID: aiContextPackIssue, Label: "Issue only", Description: "Current issue and selected field."}
	}
}

func canonicalAIContextPack(raw string) (string, bool) {
	pack := strings.ToLower(strings.TrimSpace(raw))
	switch pack {
	case "", aiContextPackIssue, "issue_only":
		return aiContextPackIssue, true
	case aiContextPackKnowledge, "project_knowledge":
		return aiContextPackKnowledge, true
	case aiContextPackRetrieve, "retrieved", "retrieved_context":
		return aiContextPackRetrieve, true
	case aiContextPackRepo, "repo_bundle", "repo_aware", "full":
		return aiContextPackRepo, true
	default:
		return "", false
	}
}

func validateAIContextPack(pack string, projectID *int64) *userError {
	if pack == aiContextPackIssue {
		return nil
	}
	if projectID == nil || *projectID <= 0 {
		return &userError{status: 400, msg: "context pack requires a project-scoped AI action"}
	}
	if pack == aiContextPackRepo && !projectHasAnchorContext(*projectID) {
		return &userError{status: 400, msg: "context pack is not available for this project"}
	}
	return nil
}

func projectHasAnchorContext(projectID int64) bool {
	if projectID <= 0 || db.DB == nil {
		return false
	}
	var exists int
	_ = db.DB.QueryRow(`SELECT 1 FROM issue_anchors WHERE project_id=? LIMIT 1`, projectID).Scan(&exists)
	return exists == 1
}

func assembleAIContextPack(
	ctx context.Context,
	ax *aiActionContext,
	projectID *int64,
) error {
	if ax == nil || ax.Options.ContextPack == aiContextPackIssue {
		return nil
	}
	if projectID == nil || *projectID <= 0 {
		return &userError{status: 400, msg: "context pack requires a project-scoped AI action"}
	}
	builder := newAIContextPackBuilder(aiContextPackBudgetBytes)
	switch ax.Options.ContextPack {
	case aiContextPackKnowledge:
		if err := addKnowledgeContext(ctx, builder, *projectID); err != nil {
			return err
		}
	case aiContextPackRetrieve:
		if err := addRetrieveContext(builder, *projectID, aiContextQuery(ax), aiContextPackMaxHits); err != nil {
			return err
		}
	case aiContextPackRepo:
		if err := addRetrieveContext(builder, *projectID, aiContextQuery(ax), aiContextPackMaxHits); err != nil {
			return err
		}
		if err := addAnchorContext(ctx, builder, *projectID, ax.IssueID, aiContextPackMaxHits); err != nil {
			return err
		}
	default:
		return &userError{status: 400, msg: "context pack is not available"}
	}
	ax.Options.contextPackBody = builder.String()
	ax.Options.ContextSources = builder.sources
	ax.Options.ContextTruncated = builder.truncated
	return nil
}

type aiContextPackBuilder struct {
	remaining int
	truncated bool
	sources   []aiContextSource
	b         strings.Builder
}

func newAIContextPackBuilder(budget int) *aiContextPackBuilder {
	if budget <= 0 {
		budget = aiContextPackBudgetBytes
	}
	return &aiContextPackBuilder{remaining: budget}
}

func (b *aiContextPackBuilder) addSource(source aiContextSource) {
	b.sources = append(b.sources, source)
	if source.Truncated {
		b.truncated = true
	}
}

func (b *aiContextPackBuilder) addSection(title, content string) bool {
	content = strings.TrimSpace(content)
	if content == "" || b.remaining <= 0 {
		return false
	}
	section := fmt.Sprintf("## %s\n%s\n\n", title, content)
	if len(section) > b.remaining {
		section = truncateBytes(section, b.remaining)
		b.truncated = true
	}
	b.b.WriteString(section)
	b.remaining -= len(section)
	return true
}

func (b *aiContextPackBuilder) String() string {
	return strings.TrimSpace(b.b.String())
}

func addKnowledgeContext(ctx context.Context, b *aiContextPackBuilder, projectID int64) error {
	rows, err := db.DB.QueryContext(ctx, `
		SELECT type, COALESCE(slug,''), title, description
		  FROM issues
		 WHERE project_id = ?
		   AND type IN ('memory','runbook','guideline')
		   AND deleted_at IS NULL
		   AND status != 'cancelled'
	  ORDER BY type ASC, title ASC
		 LIMIT 24
	`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var out strings.Builder
	count := 0
	truncated := false
	for rows.Next() {
		var typ, slug, title, body string
		if err := rows.Scan(&typ, &slug, &title, &body); err != nil {
			return err
		}
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		if len(body) > 900 {
			body = truncateBytes(body, 900)
			truncated = true
		}
		fmt.Fprintf(&out, "- [%s/%s] %s\n%s\n", typ, slug, title, body)
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	added := b.addSection("Project knowledge", out.String())
	b.addSource(aiContextSource{Kind: "knowledge", Label: "Project knowledge", Count: count, Truncated: truncated || !added})
	return nil
}

func addRetrieveContext(b *aiContextPackBuilder, projectID int64, query string, k int) error {
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		b.addSource(aiContextSource{Kind: "retrieve", Label: "Retrieved context", Count: 0})
		return nil
	}
	hits, meta, err := retrieveProjectContextHits(projectID, query, k)
	if err != nil {
		return err
	}
	var out strings.Builder
	for _, hit := range hits {
		title := anyString(hit["title"])
		if title == "" {
			title = anyString(hit["issue_key"])
		}
		ref := firstNonEmptyOption(anyString(hit["issue_key"]), anyString(hit["file_path"]), anyString(hit["entity_type"]))
		fmt.Fprintf(&out, "- [%s] %s", anyString(hit["entity_type"]), title)
		if ref != "" {
			fmt.Fprintf(&out, " (%s)", ref)
		}
		if sources := anyToStringSlice(hit["sources"]); len(sources) > 0 {
			fmt.Fprintf(&out, " sources=%s", strings.Join(sources, ","))
		}
		snippet := strings.TrimSpace(anyString(hit["snippet"]))
		if snippet != "" {
			fmt.Fprintf(&out, "\n  %s", snippet)
		}
		out.WriteString("\n")
	}
	added := b.addSection("Retrieved context", out.String())
	b.addSource(aiContextSource{
		Kind:      "retrieve",
		Label:     "Retrieved context",
		Count:     len(hits),
		Truncated: contextMetaTruncated(meta) || !added,
	})
	return nil
}

func addAnchorContext(ctx context.Context, b *aiContextPackBuilder, projectID, issueID int64, limit int) error {
	where := `a.project_id = ?`
	args := []any{projectID}
	if issueID > 0 {
		where += ` AND a.issue_id = ?`
		args = append(args, issueID)
	}
	args = append(args, limit+1)
	rows, err := db.DB.QueryContext(ctx, `
		SELECT COALESCE(r.label,''), a.file_path, a.line, COALESCE(a.label,''), COALESCE(a.confidence,''), COALESCE(a.symbol_json,''), COALESCE(a.repo_revision,'')
		  FROM issue_anchors a
		  LEFT JOIN project_repos r ON r.id = a.repo_id
		 WHERE `+where+`
	  ORDER BY a.id ASC
		 LIMIT ?
	`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	var out strings.Builder
	count := 0
	truncated := false
	for rows.Next() {
		if count >= limit {
			truncated = true
			continue
		}
		var repo, filePath, label, confidence, rawSymbol, revision string
		var line int
		if err := rows.Scan(&repo, &filePath, &line, &label, &confidence, &rawSymbol, &revision); err != nil {
			return err
		}
		symbol := compactAnchorSymbol(rawSymbol)
		fmt.Fprintf(&out, "- %s:%d", filePath, line)
		if repo != "" {
			fmt.Fprintf(&out, " repo=%s", repo)
		}
		if label != "" {
			fmt.Fprintf(&out, " label=%s", label)
		}
		if symbol != "" {
			fmt.Fprintf(&out, " symbol=%s", symbol)
		}
		if confidence != "" {
			fmt.Fprintf(&out, " confidence=%s", confidence)
		}
		if revision != "" {
			fmt.Fprintf(&out, " revision=%s", revision)
		}
		out.WriteString("\n")
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	added := b.addSection("Repo anchors", out.String())
	b.addSource(aiContextSource{Kind: "anchors", Label: "Repo anchors", Count: count, Truncated: truncated || !added})
	return nil
}

func aiContextQuery(ax *aiActionContext) string {
	var parts []string
	for _, value := range []string{
		ax.IssueData.IssueKey,
		ax.IssueData.IssueTitle,
		ax.Text,
		ax.IssueData.Description,
		ax.IssueData.AcceptanceCriteria,
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return truncateBytes(strings.Join(parts, " "), 600)
}

func aiUserPromptWithContext(ax *aiActionContext, userPrompt string) string {
	contextBody := strings.TrimSpace(ax.Options.contextPackBody)
	if contextBody == "" {
		return userPrompt
	}
	var b strings.Builder
	b.WriteString(userPrompt)
	b.WriteString("\n\nAdditional selected project context (use only when relevant; do not quote blindly):\n")
	b.WriteString(contextBody)
	return b.String()
}

func compactAnchorSymbol(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var symbol struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal([]byte(raw), &symbol); err != nil {
		return ""
	}
	parts := []string{}
	if symbol.Kind != "" {
		parts = append(parts, symbol.Kind)
	}
	if symbol.Name != "" {
		parts = append(parts, symbol.Name)
	}
	if symbol.Language != "" {
		parts = append(parts, symbol.Language)
	}
	return strings.Join(parts, ":")
}

func contextMetaTruncated(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	if v, ok := meta["truncated"].(bool); ok {
		return v
	}
	return false
}

func anyString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

func truncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	var b strings.Builder
	b.Grow(max)
	for _, r := range s {
		if b.Len()+len(string(r)) > max {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String()) + "\n[truncated]"
}
