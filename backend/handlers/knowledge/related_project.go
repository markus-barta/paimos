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

// relatedProjectModule implements PAI-338's `related_project`
// knowledge type — cross-instance project pointers
// ("our customer's billing project lives at acme.example.com /
// BILL-471"). PAI-337's spec marks `instance_url` as required
// because paimos may serve multiple instances and a bare project
// key would be ambiguous.
//
// Validation: when metadata.instance_url is present, it must be
// an absolute URL. Other fields (project_key, role) are
// validated by the editor — keeping the server lax avoids
// breaking PAI-344 content migrations.
type relatedProjectModule struct{}

func (relatedProjectModule) Type() string          { return "related_project" }
func (relatedProjectModule) Label() string         { return "Related project" }
func (relatedProjectModule) DefaultStatus() string { return "backlog" }

func (relatedProjectModule) ValidateInput(in Input) error {
	raw, ok := in.Metadata["instance_url"]
	if !ok || raw == nil {
		return nil
	}
	str, ok := raw.(string)
	if !ok {
		return errors.New("metadata.instance_url must be a string")
	}
	str = strings.TrimSpace(str)
	if str == "" {
		return nil
	}
	parsed, err := url.Parse(str)
	if err != nil {
		return errors.New("metadata.instance_url must parse as a valid URL")
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("metadata.instance_url must be absolute (scheme + host)")
	}
	return nil
}

func (relatedProjectModule) MarshalMeta(meta map[string]any) (string, error) {
	return MarshalMetaDefault(meta)
}

func (relatedProjectModule) UnmarshalMeta(raw string) (map[string]any, error) {
	return UnmarshalMetaDefault(raw)
}

var relatedProjectModuleInstance Module = relatedProjectModule{}
