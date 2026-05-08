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

// runbookModule implements PAI-338's `runbook` knowledge type —
// the procedural cousin of memory. Structurally identical to
// memory (slug + markdown body + free-form metadata) but
// semantically distinct: runbooks describe *how to do* something,
// memories describe *what is true*. The split exists so the
// Knowledge tab (PAI-339) can pivot the listing UI separately.
type runbookModule struct{}

func (runbookModule) Type() string          { return "runbook" }
func (runbookModule) Label() string         { return "Runbook" }
func (runbookModule) DefaultStatus() string { return "backlog" }

func (runbookModule) ValidateInput(in Input) error {
	return nil
}

func (runbookModule) MarshalMeta(meta map[string]any) (string, error) {
	return MarshalMetaDefault(meta)
}

func (runbookModule) UnmarshalMeta(raw string) (map[string]any, error) {
	return UnmarshalMetaDefault(raw)
}

var runbookModuleInstance Module = runbookModule{}
