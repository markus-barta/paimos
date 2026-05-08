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

// guidelineModule implements PAI-338's `guideline` knowledge type
// — short normative rules ("Use 'prod' not 'live'") that don't
// need the full memory metadata. Functionally identical to
// memory at the storage layer; the type discriminator lets the
// Knowledge tab (PAI-339) and PAI-345's compiler treat them
// differently. Per PAI-337's framing, guidelines compile into
// non-negotiable rules cleanly because they're already
// declarative.
type guidelineModule struct{}

func (guidelineModule) Type() string          { return "guideline" }
func (guidelineModule) Label() string         { return "Guideline" }
func (guidelineModule) DefaultStatus() string { return "backlog" }

func (guidelineModule) ValidateInput(in Input) error {
	return nil
}

func (guidelineModule) MarshalMeta(meta map[string]any) (string, error) {
	return MarshalMetaDefault(meta)
}

func (guidelineModule) UnmarshalMeta(raw string) (map[string]any, error) {
	return UnmarshalMetaDefault(raw)
}

var guidelineModuleInstance Module = guidelineModule{}
