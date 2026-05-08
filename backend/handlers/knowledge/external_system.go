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

import (
	"errors"
	"net/url"
	"strings"
)

// externalSystemModule implements PAI-338's `external_system`
// knowledge type — pointers to systems outside paimos that the
// project depends on (Sentry, Grafana, Argo, GitHub repos, etc.).
// PAI-346 §"Cost — what we pay" calls out external_system as the
// motivating example for the `category_metadata` JSON column:
// fields like `url` and `secret_path` don't fit cleanly on the
// generic `issues` table.
//
// Validation here enforces the one structural invariant we care
// about for v1: when `metadata.url` is present, it must parse as
// an absolute URL. Other fields (secret_path, label, role) are
// validated by PAI-339's editor — server-side strictness here
// would only break PAI-344's content migrations.
type externalSystemModule struct{}

func (externalSystemModule) Type() string          { return "external_system" }
func (externalSystemModule) Label() string         { return "External system" }
func (externalSystemModule) DefaultStatus() string { return "backlog" }

func (externalSystemModule) ValidateInput(in Input) error {
	raw, ok := in.Metadata["url"]
	if !ok || raw == nil {
		return nil
	}
	str, ok := raw.(string)
	if !ok {
		return errors.New("metadata.url must be a string")
	}
	str = strings.TrimSpace(str)
	if str == "" {
		return nil
	}
	parsed, err := url.Parse(str)
	if err != nil {
		return errors.New("metadata.url must parse as a valid URL")
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("metadata.url must be absolute (scheme + host)")
	}
	return nil
}

func (externalSystemModule) MarshalMeta(meta map[string]any) (string, error) {
	return MarshalMetaDefault(meta)
}

func (externalSystemModule) UnmarshalMeta(raw string) (map[string]any, error) {
	return UnmarshalMetaDefault(raw)
}

var externalSystemModuleInstance Module = externalSystemModule{}
