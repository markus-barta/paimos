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

package handlers

// PAI-349 — bot-authored memory drafts. The CLI `paimos memory propose`
// verb POSTs to the existing /api/projects/:id/memory endpoint with
// `status: "proposed"`. The status enum already includes 'proposed'
// (M96 added it up-front per PAI-346's "adding both up-front avoids a
// follow-up recreate").
//
// This file owns the two cross-cuts the verb needs that aren't on the
// general knowledge write path:
//
//   1. Per-(agent_name, session_id) rate limit. Best-effort anti-spam,
//      not a security boundary — the storage is an in-memory counter
//      with a sliding-window cleanup. v1 deliberately doesn't add a
//      table; the limit resets on process restart, which is fine for
//      the "agent floods proposals" failure mode and bad for nothing
//      else (operators still see every accepted proposal in the
//      Knowledge tab).
//
//   2. Operator opt-out via env (`PAIMOS_PROPOSE_DISABLED=1`). When
//      set, every proposal write returns 503 with an explicit message.
//      Per-project setting layers on later.
//
// The actual `proposed` status routing is on the existing knowledge
// write hook (handlers/knowledge_writes.go); the wiring in init() below
// taps that path with a pre-flight check before the existing hook runs.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/inspr-at/paimos/backend/handlers/knowledge"
)

// proposedStatus is the status discriminator a knowledge entry gets
// when an agent drafts it via `paimos memory propose`. Mirrors the
// CLI-side constant; centralised here so the rate-limit + opt-out
// logic can short-circuit on a single string compare.
const proposedStatus = "proposed"

// defaultProposeLimitPerSession is the per-(agent, session) cap when
// the operator hasn't overridden via env. 5 matches the ticket spec
// — generous enough that a reasonable agent never hits it and tight
// enough that a misconfigured loop fails fast.
const defaultProposeLimitPerSession = 5

// defaultProposeStaleDays is the auto-archive threshold the stale
// endpoint surfaces by default. 30 days matches the ticket spec; the
// operator overrides via PAIMOS_PROPOSE_STALE_DAYS or the ?days=N
// query parameter.
const defaultProposeStaleDays = 30

// proposeLimitWindow controls how far back the rate-limit counter
// looks. Per-session quota resets after the window — agents that
// re-export PAIMOS_SESSION_ID for a long-running watcher don't get
// permanently capped on day 1. Twenty-four hours mirrors the typical
// "long" agent session in PAIMOS practice.
const proposeLimitWindow = 24 * time.Hour

// proposeLimiterStore is the in-memory counter. The map is keyed by
// (agent_name, session_id) and stores the timestamps of the last
// proposalsuccessfully accepted by the rate-limiter. A sliding window
// keeps memory bounded — entries older than `proposeLimitWindow` are
// dropped on every check and the whole map is swept periodically by
// the housekeeping goroutine started in init() below.
type proposeLimiterStore struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

// proposeLimiter is the package-level singleton. Tests reset via
// resetProposeLimiterForTest so isolated unit runs don't bleed state.
var proposeLimiter = &proposeLimiterStore{entries: map[string][]time.Time{}}

// proposeKey turns the attribution pair into a single map key. We
// trim + lowercase the agent name (PAI-326's pattern is already
// lowercase but the header is operator-supplied; defence in depth).
// session_id is opaque — preserve case so distinct sessions stay
// distinct.
func proposeKey(agent, session string) string {
	a := strings.ToLower(strings.TrimSpace(agent))
	s := strings.TrimSpace(session)
	return a + "\x1f" + s
}

// limitFromEnv returns the configured per-session cap. Env override
// takes precedence; a non-positive value falls back to the default
// rather than disabling the limiter entirely (a 0 limit would be a
// configuration foot-gun).
func limitFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("PAIMOS_PROPOSE_LIMIT_PER_SESSION"))
	if raw == "" {
		return defaultProposeLimitPerSession
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultProposeLimitPerSession
	}
	return n
}

// proposeDisabled reports whether the operator turned the verb off
// instance-wide. PAIMOS_PROPOSE_DISABLED=1 (or any non-empty truthy)
// returns 503 from the propose path.
func proposeDisabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("PAIMOS_PROPOSE_DISABLED")))
	switch v {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// errProposeDisabled / errProposeRateLimited are the sentinels the hook
// surfaces; the dispatcher in handlers/knowledge maps them to the
// matching HTTP responses (503 / 429).
var (
	errProposeDisabled    = errors.New("propose disabled")
	errProposeRateLimited = errors.New("propose rate-limited")
)

// checkAndRecordPropose checks the rate-limit window for the
// (agent, session) pair. When the count is under the cap, the call
// records the proposal and returns nil. When at/over the cap, it
// returns errProposeRateLimited without incrementing.
//
// `now` is parameterised so tests can pin time without depending on
// the wall clock. Production callers pass time.Now().
func (s *proposeLimiterStore) checkAndRecord(agent, session string, now time.Time, limit int) error {
	if strings.TrimSpace(session) == "" {
		// No session attribution → can't rate-limit. The CLI auto-
		// generates a session UUID per invocation when the user hasn't
		// set one, so this is rare in practice. Treat as "allow" so a
		// minimal-attribution flow still works.
		return nil
	}
	key := proposeKey(agent, session)
	cutoff := now.Add(-proposeLimitWindow)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Compact the window — drop timestamps older than the cutoff so
	// the count reflects the rolling 24h count, not lifetime.
	stamps := s.entries[key]
	live := stamps[:0]
	for _, t := range stamps {
		if t.After(cutoff) {
			live = append(live, t)
		}
	}
	if len(live) >= limit {
		s.entries[key] = live
		return errProposeRateLimited
	}
	live = append(live, now)
	s.entries[key] = live
	return nil
}

// sweep purges stale (agent, session) buckets so the map can't grow
// forever in a long-running process. Called by the package init's
// background goroutine; safe to call from anywhere.
func (s *proposeLimiterStore) sweep(now time.Time) {
	cutoff := now.Add(-proposeLimitWindow)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, stamps := range s.entries {
		live := stamps[:0]
		for _, t := range stamps {
			if t.After(cutoff) {
				live = append(live, t)
			}
		}
		if len(live) == 0 {
			delete(s.entries, k)
		} else {
			s.entries[k] = live
		}
	}
}

// resetProposeLimiterForTest empties the limiter state. Exported via
// the _test package only — keeps the tests independent of one another
// without exposing the internal map directly.
func resetProposeLimiterForTest() {
	proposeLimiter.mu.Lock()
	defer proposeLimiter.mu.Unlock()
	proposeLimiter.entries = map[string][]time.Time{}
}

// ResetProposeLimiterForTest is the exported sibling of the private
// helper above — _test packages outside this file (handlers_test) call
// it to keep their per-case quotas independent. Production code never
// calls this; the symbol exists to keep the `_test` package's API
// minimal.
func ResetProposeLimiterForTest() {
	resetProposeLimiterForTest()
}

// proposeWriteError is the typed error the CreateEntryHook wrapper
// returns when the gate fails. The dispatcher maps the embedded code
// onto an HTTP status while preserving the message string.
type proposeWriteError struct {
	code int
	msg  string
}

func (e *proposeWriteError) Error() string { return e.msg }

// HTTPStatus exposes the code so the dispatcher can branch without
// type-asserting through the chain.
func (e *proposeWriteError) HTTPStatus() int { return e.code }

// init wraps the existing CreateEntryHook so a propose flow gets
// rate-limit + opt-out enforcement before the canonical write path
// runs. The wrapping pattern keeps PAI-353's hook intact: when the
// status isn't 'proposed' or the gate passes, we delegate to the
// underlying hook with no behaviour change.
//
// Order matters — knowledge_writes.go's init() runs first (alphabetical
// file order: knowledge_writes < memory_propose), so its hook is the
// "previous" we wrap here.
func init() {
	previous := knowledge.CreateEntryHook
	knowledge.CreateEntryHook = func(r *http.Request, projectID int64, mod knowledge.Module, in knowledge.Input) (knowledge.Output, error) {
		// Only `memory` proposals go through the gate. Other knowledge
		// types don't have a "proposed" UX in v1.
		if mod.Type() == "memory" && strings.TrimSpace(in.Status) == proposedStatus {
			if proposeDisabled() {
				return knowledge.Output{}, &proposeWriteError{
					code: http.StatusServiceUnavailable,
					msg:  "memory propose disabled by operator (PAIMOS_PROPOSE_DISABLED)",
				}
			}
			agent, session := readAgentAttribution(r)
			a := ""
			s := ""
			if agent != nil {
				a = *agent
			}
			if session != nil {
				s = *session
			}
			if err := proposeLimiter.checkAndRecord(a, s, time.Now(), limitFromEnv()); err != nil {
				return knowledge.Output{}, &proposeWriteError{
					code: http.StatusTooManyRequests,
					msg:  fmt.Sprintf("propose rate-limited: %d proposals per session in %s window", limitFromEnv(), proposeLimitWindow),
				}
			}
		}
		// Delegate to the canonical hook (PAI-353).
		if previous != nil {
			return previous(r, projectID, mod, in)
		}
		// Fallback (sub-package tests run with a nil CreateEntryHook —
		// the knowledge package's direct-SQL path takes over).
		return knowledge.Output{}, errors.New("CreateEntryHook not registered")
	}

	// Background sweep — runs once per hour in production. Cheap; the
	// map rarely has more than a few hundred buckets even on a busy
	// instance because (agent, session) pairs are short-lived.
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for now := range t.C {
			proposeLimiter.sweep(now)
		}
	}()
}
