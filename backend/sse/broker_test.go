// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package sse

import (
	"testing"
	"time"
)

// drain reads up to n events from the subscriber, with a short
// per-event timeout so tests fail fast on broken delivery rather than
// hanging.
func drain(t *testing.T, s *Subscriber, n int) []Event {
	t.Helper()
	out := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-time.After(100 * time.Millisecond):
			return out
		}
	}
	return out
}

func TestBroker_PublishProjectFansOut(t *testing.T) {
	b := NewBroker()
	s := b.Subscribe(1, "dev-A", 7)
	defer b.Close(s)

	b.PublishProject(7, Event{Type: "agent_changed", Name: "qa", Rev: "abcd1234"})

	got := drain(t, s, 1)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Type != "agent_changed" || got[0].Name != "qa" {
		t.Errorf("event = %+v", got[0])
	}
	if got[0].ProjectID != 7 {
		t.Errorf("project_id = %d, want 7", got[0].ProjectID)
	}
}

func TestBroker_PublishProjectFiltersOnProject(t *testing.T) {
	b := NewBroker()
	s7 := b.Subscribe(1, "dev-A", 7)
	s8 := b.Subscribe(1, "dev-A", 8)
	defer b.Close(s7)
	defer b.Close(s8)

	b.PublishProject(7, Event{Type: "agent_changed", Name: "qa"})

	if got := drain(t, s7, 1); len(got) != 1 {
		t.Errorf("project 7 subscriber should receive: %d", len(got))
	}
	if got := drain(t, s8, 1); len(got) != 0 {
		t.Errorf("project 8 subscriber should NOT receive: %v", got)
	}
}

func TestBroker_DisconnectClosesChannel(t *testing.T) {
	b := NewBroker()
	s := b.Subscribe(1, "dev-A", 7)

	b.Disconnect(1, "dev-A", 7)

	// Channel should now be closed; reading should return zero-value
	// + ok=false.
	select {
	case _, ok := <-s.Events():
		if ok {
			t.Error("expected closed channel after Disconnect")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Disconnect did not close subscriber channel")
	}

	if b.SubscriberCount() != 0 {
		t.Errorf("subscriber not deregistered: %d", b.SubscriberCount())
	}
}

func TestBroker_DisconnectScopedToUserDeviceProject(t *testing.T) {
	b := NewBroker()
	target := b.Subscribe(1, "dev-A", 7)
	other := b.Subscribe(2, "dev-A", 7)            // different user
	otherDev := b.Subscribe(1, "dev-B", 7)         // different device
	otherProject := b.Subscribe(1, "dev-A", 8)     // different project
	defer b.Close(other)
	defer b.Close(otherDev)
	defer b.Close(otherProject)

	b.Disconnect(1, "dev-A", 7)

	// target closed.
	select {
	case _, ok := <-target.Events():
		if ok {
			t.Error("target should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("target channel never closed")
	}
	// Others still alive (publish a sentinel event scoped to project 7
	// — only the same-project subscribers should see it; we'll use
	// project 8 for otherProject).
	b.PublishProject(7, Event{Type: "agent_changed", Name: "qa"})
	if got := drain(t, other, 1); len(got) != 1 {
		t.Error("other user should still receive")
	}
	if got := drain(t, otherDev, 1); len(got) != 1 {
		t.Error("other device should still receive")
	}
	b.PublishProject(8, Event{Type: "agent_changed", Name: "ops"})
	if got := drain(t, otherProject, 1); len(got) != 1 {
		t.Error("other project should still receive")
	}
}

func TestBroker_CloseIsIdempotent(t *testing.T) {
	b := NewBroker()
	s := b.Subscribe(1, "dev-A", 7)
	b.Close(s)
	b.Close(s) // Should not panic.
	if b.SubscriberCount() != 0 {
		t.Errorf("count = %d", b.SubscriberCount())
	}
}

func TestBroker_FullBufferDropsForSlowConsumer(t *testing.T) {
	b := NewBroker()
	s := b.Subscribe(1, "dev-A", 7)
	defer b.Close(s)

	// Spam more events than the buffer can hold without draining.
	for i := 0; i < subscriberBufferSize*2; i++ {
		b.PublishProject(7, Event{Type: "agent_changed", Name: "qa"})
	}

	// We should be able to read up to buffer-size events (the rest
	// were dropped on the non-blocking send).
	got := drain(t, s, subscriberBufferSize*2)
	if len(got) > subscriberBufferSize {
		t.Errorf("got %d events; buffer is %d — non-blocking send must have dropped some",
			len(got), subscriberBufferSize)
	}
	if len(got) == 0 {
		t.Error("expected at least some events delivered")
	}
}

func TestGlobalBroker_Singleton(t *testing.T) {
	a := GlobalBroker()
	b := GlobalBroker()
	if a != b {
		t.Error("GlobalBroker should return the same instance")
	}
}
