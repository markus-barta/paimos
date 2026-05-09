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

// PAI-341 — `related_project` Resource.

package sync

// NewRelatedProjectResource returns the Resource implementation for
// the `related_project` knowledge kind.
func NewRelatedProjectResource() Resource {
	return &knowledgeResource{
		kind:        "related_project",
		urlAlias:    "related-projects",
		cacheSubdir: "related-projects",
	}
}
