// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/markus-barta/paimos/backend/auth"
)

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// handleDBError checks for common DB errors and sends appropriate HTTP responses.
// Returns true if an error was handled (caller should return), false if no error.
func handleDBError(w http.ResponseWriter, err error, entity string) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		jsonError(w, entity+" already exists", http.StatusConflict)
		return true
	}
	log.Printf("%s DB error: %v", entity, err)
	jsonError(w, "internal error", http.StatusInternalServerError)
	return true
}

// projectIDFilter returns a SQL fragment and matching args that restrict
// a query to the set of projects the current user can view. The returned
// clause begins with " AND " (so it can be appended to a WHERE clause).
// column is the fully qualified column name of the project ID (e.g.
// "p.id", "i.project_id"). If allowOrphans is true, rows with NULL in
// that column also pass (used for issue lists that include orphan sprints).
//
// For admins, the returned clause is empty — no filtering needed.
// For other users with no accessible projects, the clause evaluates to
// an always-false predicate so the query returns zero rows.
func projectIDFilter(r *http.Request, column string, allowOrphans bool) (string, []any) {
	ids := auth.AccessibleProjectIDs(r)
	if ids == nil {
		return "", nil // admin — no filter
	}
	if len(ids) == 0 {
		if allowOrphans {
			return " AND " + column + " IS NULL", nil
		}
		return " AND 1=0", nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if allowOrphans {
		return " AND (" + column + " IS NULL OR " + column + " IN (" + placeholders + "))", args
	}
	return " AND " + column + " IN (" + placeholders + ")", args
}
