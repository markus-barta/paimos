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

// PAI-341 — `memory` Resource. Wraps knowledgeResource with the URL
// alias + cache subdirectory specific to memory entries (PAI-338's
// declarative knowledge type). The shared rendering / drift logic lives
// in knowledge_resource.go.

package sync

// NewMemoryResource returns the Resource implementation for the
// `memory` knowledge kind. Registry consumers use it via
// `paimos sync` and the convenience `--kind=memory` flag.
func NewMemoryResource() Resource {
	return &knowledgeResource{
		kind:        "memory",
		urlSegment:  "memory",
		cacheSubdir: "memory",
	}
}
