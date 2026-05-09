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

// PAI-341 — `runbook` Resource. The URL alias is pluralised
// ("runbooks") on the server so the resource keeps the singular Kind
// in line with the SQL discriminator; the alias is the convenience-
// endpoint path. Rendering / drift logic is shared via
// knowledgeResource.

package sync

// NewRunbookResource returns the Resource implementation for the
// `runbook` knowledge kind. Cache directory matches the URL alias
// ("runbooks") for human discoverability.
func NewRunbookResource() Resource {
	return &knowledgeResource{
		kind:        "runbook",
		urlAlias:    "runbooks",
		cacheSubdir: "runbooks",
	}
}
