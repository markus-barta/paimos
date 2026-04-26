package handlers

import "database/sql"

func listAIActionCatalog(dbConn *sql.DB) []aiActionListItem {
	placements := loadPromptPlacementOverrides(dbConn)
	out := make([]aiActionListItem, 0, len(actionRegistry))
	for _, d := range actionRegistry {
		placement := d.Placement
		if placement == "" {
			placement = "text"
		}
		if override, ok := placements[d.Key]; ok && override != "" {
			placement = override
		}
		out = append(out, aiActionListItem{
			Key:         d.Key,
			Label:       d.Label,
			Surface:     d.Surface,
			Placement:   placement,
			SubKeys:     d.SubKeys,
			Implemented: d.Implemented,
		})
	}
	return out
}

func loadPromptPlacementOverrides(dbConn *sql.DB) map[string]string {
	out := map[string]string{}
	rows, err := dbConn.Query(`SELECT key, COALESCE(placement, '') FROM ai_prompts`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, p string
		if err := rows.Scan(&k, &p); err == nil {
			out[k] = p
		}
	}
	return out
}
