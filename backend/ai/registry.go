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

// PAI-151. The provider registry. New providers (PAI-122) are added by
// dropping a sibling file in this package, implementing Provider, and
// adding one line to the providers map below. The HTTP handler asks
// the registry for the active provider by name; nothing else cares.

package ai

import "fmt"

// providers is the static registry. Compile-time so a typo in
// ai_settings.provider produces a clean error rather than a runtime
// nil-pointer.
var providers = map[string]Provider{}

// Register adds a provider to the registry. Called from each provider
// file's init(). Panics on duplicate registration — that's a developer
// error, not a runtime condition, so failing loud at boot is correct.
func Register(p Provider) {
	if _, exists := providers[p.Name()]; exists {
		panic("ai: duplicate provider registration: " + p.Name())
	}
	providers[p.Name()] = p
}

// Get returns the provider registered under the given name. Returns
// an error (not a nil provider) when the name is unknown so callers
// don't have to remember to nil-check.
func Get(name string) (Provider, error) {
	p, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("ai: unknown provider %q", name)
	}
	return p, nil
}

// Names returns the registered provider names in deterministic order.
// Not used in v1 (the settings UI hard-codes the slot for OpenRouter)
// but exposed so a future settings dropdown can reflect what was
// actually compiled in.
func Names() []string {
	out := make([]string, 0, len(providers))
	for name := range providers {
		out = append(out, name)
	}
	return out
}
