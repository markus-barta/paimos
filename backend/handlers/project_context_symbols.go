package handlers

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/markus-barta/paimos/backend/db"
)

type storedAnchorSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Language  string `json:"language"`
}

func symbolIDForAnchor(repoID int64, filePath string, sym storedAnchorSymbol) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d|%s|%s|%s|%d|%d|%s", repoID, strings.ToLower(filePath), sym.Name, sym.Kind, sym.StartLine, sym.EndLine, sym.Language)))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

func decodeStoredAnchorSymbol(raw string) (*storedAnchorSymbol, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var sym storedAnchorSymbol
	if err := json.Unmarshal([]byte(raw), &sym); err != nil {
		return nil, false
	}
	if strings.TrimSpace(sym.Name) == "" || sym.StartLine <= 0 || sym.EndLine <= 0 {
		return nil, false
	}
	return &sym, true
}

func symbolMetadataJSON(repoID int64, filePath string, sym storedAnchorSymbol) string {
	body := map[string]any{
		"name":       sym.Name,
		"kind":       sym.Kind,
		"start_line": sym.StartLine,
		"end_line":   sym.EndLine,
		"language":   sym.Language,
		"file_path":  filePath,
		"repo_id":    repoID,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ""
	}
	return string(raw)
}

func resolveStoredSymbolNode(symbolID int64) map[string]any {
	var raw string
	err := db.DB.QueryRow(`
		SELECT metadata
		FROM entity_relations
		WHERE edge_type='anchored_inside'
		  AND ((source_type='symbol' AND source_id=?) OR (target_type='symbol' AND target_id=?))
		  AND metadata != ''
		ORDER BY id ASC
		LIMIT 1
	`, symbolID, symbolID).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return map[string]any{"entity_type": "symbol", "entity_id": symbolID}
	}
	var meta map[string]any
	if json.Unmarshal([]byte(raw), &meta) != nil {
		return map[string]any{"entity_type": "symbol", "entity_id": symbolID}
	}
	node := map[string]any{"entity_type": "symbol", "entity_id": symbolID}
	if name, ok := meta["name"].(string); ok && strings.TrimSpace(name) != "" {
		node["title"] = name
	}
	for _, key := range []string{"name", "kind", "start_line", "end_line", "language", "file_path", "repo_id"} {
		if value, ok := meta[key]; ok {
			node[key] = value
		}
	}
	if node["title"] == nil {
		node["title"] = fmt.Sprintf("symbol:%d", symbolID)
	}
	return node
}
