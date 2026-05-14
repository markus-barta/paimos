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

// PAI-341 — `external_system` Resource. URL alias differs from kind
// because the convenience endpoints kebab-case multi-word aliases
// (external-systems) while the SQL discriminator stays snake_case.

package sync

// NewExternalSystemResource returns the Resource implementation for
// the `external_system` knowledge kind.
func NewExternalSystemResource() Resource {
	return &knowledgeResource{
		kind:        "external_system",
		urlSegment:  "external-system",
		cacheSubdir: "external-systems",
	}
}
