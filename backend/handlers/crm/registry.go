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

package crm

import (
	"sort"
	"sync"
)

// In-process Provider registry. Populated at boot by each provider
// subpackage's init() — for the test harness, Register can also be
// called from tests.
//
// Singleton + RWMutex: List is read-heavy (every API request that
// touches the integrations endpoint hits it), Register fires once per
// provider at boot.

var (
	registryMu sync.RWMutex
	registry   = map[string]Provider{}
)

// Register adds a Provider to the in-process registry. Re-registering
// the same ID overwrites — useful for tests that swap in a fake; not
// meant to be called twice in production.
func Register(p Provider) {
	if p == nil || p.ID() == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.ID()] = p
}

// Get returns the Provider registered under id, or (nil, false) if no
// such provider is compiled in.
func Get(id string) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// List returns every registered Provider sorted by display name. Stable
// order matters for the admin UI so the cards don't reshuffle between
// renders.
func List() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Reset is a test-only helper that clears the registry. Production code
// must not call this — it's exposed only because Go has no way to
// scope test mutations of package-level state.
func Reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]Provider{}
}
