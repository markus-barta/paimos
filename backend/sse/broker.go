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

// Package sse implements a tiny in-process Server-Sent Events broker
// for PAI-331's auto-watch sync.
//
// Design notes:
//
//   - Subscribers are identified by (user_id, device_id, project_id) so
//     the auto-watch toggle handler (PAI-331) can disconnect a single
//     device + project pair when the user flips the row OFF without
//     touching unrelated subscribers.
//
//   - Publishers (skill / agent CRUD handlers) call Publish with an
//     Event scoped to a single project_id. The broker fans out to every
//     active subscriber whose (user_id, project_id) matches AND whose
//     auto-watch row is currently enabled (the membership check is
//     server-side; the broker just does the routing).
//
//   - The envelope `Event` shape is intentionally extensible: PAI-341
//     will add `memory_changed`, `runbook_changed`, etc. through the
//     same Type field. Keeping the broker kind-agnostic means PAI-341
//     does not have to touch this package.
//
//   - The implementation prioritises clarity over throughput. Production
//     load is low (one event per agent edit, fewer than dozens of active
//     CLI watchers). If load profile changes, replace the per-subscriber
//     channel with a fan-out goroutine; the public API stays.
package sse

import (
	"sync"
	"sync/atomic"
)

// Event is the broker's envelope. JSON-encoded bodies travel through
// the SSE write path; Event itself is not the wire format (the writer
// formats `data: <json>` lines per the SSE spec).
type Event struct {
	// Type is the event kind name (e.g. "agent_changed",
	// "memory_changed"). Free-form so PAI-341 can register new kinds
	// without changing the broker.
	Type string `json:"type"`

	// Name is the artifact slug (agent name, memory slug, …).
	Name string `json:"name,omitempty"`

	// Rev is the short hash matching the canonical artifact's rev.
	// Subscribers compare against their on-disk header to decide if
	// a re-render is necessary.
	Rev string `json:"rev,omitempty"`

	// ProjectID is set by the broker before fanout; publishers
	// pass it via Publish's projectID arg, not on the event.
	ProjectID int64 `json:"project_id"`
}

// Subscriber is an active SSE client. Construct via Broker.Subscribe;
// always call Close() before letting the http.Handler return so the
// broker drops the registration cleanly.
type Subscriber struct {
	UserID    int64
	DeviceID  string
	ProjectID int64

	// ch is the per-subscriber fan-in channel. Sized so a brief
	// publisher burst doesn't drop events on a single subscriber. New
	// subscribers start with the buffer empty; if the buffer fills
	// (slow consumer), the broker drops the event for THIS subscriber
	// and logs once — preserves liveness for everyone else.
	ch chan Event

	// closed is set atomically by Close(). Guards send-on-closed-channel.
	closed atomic.Bool
}

// Events returns the receive side. Range over it; broker closes the
// channel from Disconnect/Close.
func (s *Subscriber) Events() <-chan Event { return s.ch }

// subscriberBufferSize is the per-subscriber channel depth. PAI-331's
// load model expects << 1 event/sec per subscriber. 32 is "enough head
// room that a network blip on the writer doesn't drop events".
const subscriberBufferSize = 32

// Broker fans out events to subscribers. One global instance is
// sufficient for a single-server deployment (PAIMOS today); multi-node
// deployments would need to route through Redis or the like.
type Broker struct {
	mu sync.RWMutex
	// subs is the set of active subscribers, keyed by a composite tuple
	// so Disconnect() can find a target row in O(1). Map values store
	// every subscriber matching the key — there can be more than one
	// (e.g. two browser tabs from the same device) but the
	// project-scoping handler only registers one connection per request.
	subs map[subKey]map[*Subscriber]struct{}
}

// subKey is the composite map key for the subs registry.
type subKey struct {
	UserID    int64
	DeviceID  string
	ProjectID int64
}

// NewBroker returns a fresh broker. Tests use this; production code
// uses GlobalBroker().
func NewBroker() *Broker {
	return &Broker{subs: map[subKey]map[*Subscriber]struct{}{}}
}

// global is the default broker shared between handlers and the
// publishers. Lazy-init so tests that import this package never see a
// stale subscriber registry.
var (
	globalOnce sync.Once
	global     *Broker
)

// GlobalBroker returns the process-wide broker. PAI-331 uses this from
// the auto-watch upsert handler (Disconnect) and the agent CRUD
// handlers (Publish).
func GlobalBroker() *Broker {
	globalOnce.Do(func() { global = NewBroker() })
	return global
}

// Subscribe registers a new connection. The caller MUST drain Events()
// promptly and call Close() before letting its http handler return.
func (b *Broker) Subscribe(userID int64, deviceID string, projectID int64) *Subscriber {
	s := &Subscriber{
		UserID:    userID,
		DeviceID:  deviceID,
		ProjectID: projectID,
		ch:        make(chan Event, subscriberBufferSize),
	}
	key := subKey{UserID: userID, DeviceID: deviceID, ProjectID: projectID}
	b.mu.Lock()
	if b.subs[key] == nil {
		b.subs[key] = map[*Subscriber]struct{}{}
	}
	b.subs[key][s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Close drops the subscriber from the broker registry and closes its
// channel. Safe to call multiple times.
func (b *Broker) Close(s *Subscriber) {
	if s == nil {
		return
	}
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	key := subKey{UserID: s.UserID, DeviceID: s.DeviceID, ProjectID: s.ProjectID}
	b.mu.Lock()
	if set, ok := b.subs[key]; ok {
		delete(set, s)
		if len(set) == 0 {
			delete(b.subs, key)
		}
	}
	b.mu.Unlock()
	close(s.ch)
}

// Disconnect closes every subscriber matching (userID, deviceID,
// projectID). PAI-331's auto-watch handler calls this after toggling
// the row OFF so the CLI watcher's stream sees a clean close.
func (b *Broker) Disconnect(userID int64, deviceID string, projectID int64) {
	key := subKey{UserID: userID, DeviceID: deviceID, ProjectID: projectID}
	b.mu.Lock()
	set, ok := b.subs[key]
	if ok {
		delete(b.subs, key)
	}
	b.mu.Unlock()
	for s := range set {
		if s.closed.CompareAndSwap(false, true) {
			close(s.ch)
		}
	}
}

// PublishProject fans an Event out to every subscriber whose
// projectID matches. Publishers don't need to know which user/device
// pairs are subscribed — they just hand the broker an event and the
// project id, and the broker filters.
func (b *Broker) PublishProject(projectID int64, ev Event) {
	ev.ProjectID = projectID
	b.mu.RLock()
	defer b.mu.RUnlock()
	for key, set := range b.subs {
		if key.ProjectID != projectID {
			continue
		}
		for s := range set {
			if s.closed.Load() {
				continue
			}
			// Non-blocking send: drop on full buffer rather than block
			// every other subscriber. The CLI watch loop expects at-
			// most-once delivery and falls back to the .rev poll when
			// it suspects a missed event.
			select {
			case s.ch <- ev:
			default:
			}
		}
	}
}

// SubscriberCount is a test helper. Returns the number of active
// subscribers across all keys.
func (b *Broker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, set := range b.subs {
		n += len(set)
	}
	return n
}
