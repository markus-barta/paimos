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

package knowledge

// PAI-353 — write-path hooks. The convenience-endpoint write paths
// (CREATE / UPDATE / DELETE) were doing direct `db.Exec(...)` and
// therefore bypassed the canonical issue handler's side-effects:
//
//   1. issue_history snapshots (PAI-324 attribution columns).
//   2. mutation_log rows (PAI-209 undo / redo).
//   3. system tag re-evaluation, search-index updates, etc.
//
// Routing the writes through the parent `handlers` package's helpers
// would create a circular import (handlers → knowledge for routing,
// knowledge → handlers for the write helpers). Instead the parent
// package registers function-variable hooks at init() time and the
// handlers in this package delegate to those when they're set.
//
// The fallback path (when a hook is nil) is the original direct-SQL
// implementation — kept so unit tests of the knowledge sub-package
// can still exercise the schema invariants without bringing the full
// handlers package online. Production binaries always import
// `handlers`, so the hooks are always populated there.

import (
	"net/http"
)

// CreateEntryHook implements the canonical-issue path for INSERTs
// originating from a knowledge convenience endpoint. When non-nil,
// MakeCreateHandler delegates to it so the new row gets an
// issue_history snapshot + mutation_log entry just like a regular
// CreateIssue would.
//
// Returning errSlugTaken (the package-private sentinel) lets the
// dispatcher map UNIQUE constraint violations onto a 409 without
// leaking SQL detail strings.
var CreateEntryHook func(r *http.Request, projectID int64, mod Module, in Input) (Output, error)

// UpdateEntryHook is the equivalent for UPDATE. The currentSlug
// parameter is the URL-supplied slug used to locate the row; the
// new slug (if any) lives in `in.Slug`. ErrSlugTaken / ErrNotFound
// signal the same 409 / 404 mappings as CreateEntryHook.
var UpdateEntryHook func(r *http.Request, projectID int64, mod Module, currentSlug string, in Input) (Output, error)

// DeleteEntryHook is the equivalent for soft-DELETE. Returns the
// number of rows affected (0 → 404) so the dispatcher's HTTP shape
// matches the existing direct-SQL fallback exactly.
var DeleteEntryHook func(r *http.Request, projectID int64, mod Module, slug string) (int64, error)

// ErrSlugTaken is the exported sentinel hook implementations return
// when the partial UNIQUE INDEX on (type, slug, project_id) rejects
// an insert/update. Mirrors the package-private errSlugTaken so a
// hook in `handlers` can raise it without re-importing this file's
// internals.
var ErrSlugTaken = errSlugTaken

// httpCodedError is the contract a hook can implement to surface a
// non-default HTTP status (e.g. 429 / 503) without smuggling status
// codes through string parsing. PAI-349's propose-gate hook returns
// errors satisfying this interface so the dispatcher can map rate-
// limit / opt-out failures to the right shape.
type httpCodedError interface {
	error
	HTTPStatus() int
}
