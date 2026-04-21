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

package auth

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Response convention for all project-scoped middlewares:
//
//   - no view access    → 404 (don't leak existence to unauthorized users)
//   - view but not edit → 403 (user knows it exists, just can't modify)
//   - not authenticated → 401 (handled earlier by Middleware)
//
// The 404-for-no-view rule matches the portal convention and avoids giving
// an attacker a project-ID enumeration oracle via response-code differences.

const (
	noViewStatus = http.StatusNotFound
	noEditStatus = http.StatusForbidden
)

// projectIDFromURL extracts a project ID from the most common chi URL
// param names. Returns 0, false if none are present or parseable.
func projectIDFromURL(r *http.Request) (int64, bool) {
	for _, k := range []string{"id", "projectId", "project_id"} {
		if v := chi.URLParam(r, k); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil {
				return id, true
			}
		}
	}
	return 0, false
}

// RequireProjectView gates a project-scoped route on view (read) access.
// Assumes the matched URL contains a project ID in "id", "projectId", or
// "project_id". Returns 404 when the user cannot view the project.
func RequireProjectView(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pid, ok := projectIDFromURL(r)
		if !ok {
			http.Error(w, `{"error":"invalid project id"}`, http.StatusBadRequest)
			return
		}
		if !CanViewProject(r, pid) {
			http.Error(w, `{"error":"not found"}`, noViewStatus)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireProjectEdit gates a project-scoped route on edit (write) access.
// Returns 404 on no-view, 403 on view-only.
func RequireProjectEdit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pid, ok := projectIDFromURL(r)
		if !ok {
			http.Error(w, `{"error":"invalid project id"}`, http.StatusBadRequest)
			return
		}
		lvl := ProjectAccessLevel(r, pid)
		switch lvl {
		case AccessEditor:
			next.ServeHTTP(w, r)
		case AccessViewer:
			http.Error(w, `{"error":"forbidden"}`, noEditStatus)
		default:
			http.Error(w, `{"error":"not found"}`, noViewStatus)
		}
	})
}

// entityAccessMiddleware returns a middleware that resolves the owning
// project for an entity via lookupFn(entityID), then enforces view (if
// editRequired=false) or edit (if editRequired=true) access. Entities with
// a nil project (e.g. orphan sprint issues) are allowed through for any
// authenticated user — cross-project global objects aren't scoped. A
// missing row (or DB error) fails closed with 404 so unauthenticated
// enumeration can't tell existing IDs from bogus ones.
func entityAccessMiddleware(
	paramName string,
	lookupFn func(int64) (int64, bool, bool),
	editRequired bool,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := chi.URLParam(r, paramName)
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
				return
			}
			pid, found, orphan := lookupFn(id)
			if !found {
				http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
				return
			}
			if orphan {
				// Cross-project orphan (e.g. sprint issue). Handler is
				// responsible for orphan-specific rules.
				next.ServeHTTP(w, r)
				return
			}
			if editRequired {
				lvl := ProjectAccessLevel(r, pid)
				switch lvl {
				case AccessEditor:
					next.ServeHTTP(w, r)
				case AccessViewer:
					http.Error(w, `{"error":"forbidden"}`, noEditStatus)
				default:
					http.Error(w, `{"error":"not found"}`, noViewStatus)
				}
				return
			}
			if !CanViewProject(r, pid) {
				http.Error(w, `{"error":"not found"}`, noViewStatus)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireIssueAccess gates routes like /api/issues/{id}/* on view access to
// the issue's owning project. Orphan issues (NULL project_id — sprints)
// pass through. Accepts either a numeric issue id or an issue key (e.g.
// "PAI-83") — keys are resolved to their numeric id before the access check.
func RequireIssueAccess(next http.Handler) http.Handler {
	return resolveIssueRefThen(entityAccessMiddleware("id", ProjectIDForIssue, false)(next))
}

// RequireIssueEdit gates write-side issue routes on edit access. Like
// RequireIssueAccess, accepts numeric ids or issue keys.
func RequireIssueEdit(next http.Handler) http.Handler {
	return resolveIssueRefThen(entityAccessMiddleware("id", ProjectIDForIssue, true)(next))
}

// resolveIssueRefThen rewrites the "id" URL param from an issue key (e.g.
// "PAI-83") to its numeric form before delegating to the given handler.
// Malformed inputs (neither number nor key-shape) return 400; key-shaped
// inputs with no matching issue return 404.
func resolveIssueRefThen(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, malformed := rewriteIssueRefToID(r)
		if malformed {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAttachmentAccess gates /api/attachments/{id} on view access to the
// issue's owning project.
func RequireAttachmentAccess(next http.Handler) http.Handler {
	return entityAccessMiddleware("id", ProjectIDForAttachment, false)(next)
}

// RequireAttachmentEdit gates attachment-mutating routes on edit access.
func RequireAttachmentEdit(next http.Handler) http.Handler {
	return entityAccessMiddleware("id", ProjectIDForAttachment, true)(next)
}

// RequireTimeEntryAccess gates /api/time-entries/{id} on view access.
func RequireTimeEntryAccess(next http.Handler) http.Handler {
	return entityAccessMiddleware("id", ProjectIDForTimeEntry, false)(next)
}

// RequireTimeEntryEdit gates time-entry-mutating routes on edit access.
func RequireTimeEntryEdit(next http.Handler) http.Handler {
	return entityAccessMiddleware("id", ProjectIDForTimeEntry, true)(next)
}

// RequireCommentAccess gates /api/comments/{id} on view access.
func RequireCommentAccess(next http.Handler) http.Handler {
	return entityAccessMiddleware("id", ProjectIDForComment, false)(next)
}

// RequireCommentEdit gates comment-mutating routes on edit access. Note
// that delete-own-comment remains allowed by the handler independent of
// project access — this middleware only blocks outright no-view cases.
func RequireCommentEdit(next http.Handler) http.Handler {
	return entityAccessMiddleware("id", ProjectIDForComment, true)(next)
}
