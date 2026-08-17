package asaas

// This file tracks, per charge, the most recent webhook event this
// facade actually enqueued: the exact dispatch.Event and which
// endpoint(s) it went to. chaos duplicate-delivery and chaos retry-storm
// both resend this same event, by id, on purpose: that is what makes them
// idempotency and retry tests instead of a new event arriving.

import (
	"sync"

	"github.com/danellalc/localpsp/dispatch"
)

// lastEvent is the most recent event fired for a charge: the event itself
// and which endpoint(s) it was enqueued to.
type lastEvent struct {
	Event     dispatch.Event
	Endpoints []string
}

type lastEventStore struct {
	mu       sync.Mutex
	byCharge map[string]lastEvent
}

func newLastEventStore() *lastEventStore {
	return &lastEventStore{byCharge: make(map[string]lastEvent)}
}

func (s *lastEventStore) set(chargeID string, ev lastEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byCharge[chargeID] = ev
}

func (s *lastEventStore) get(chargeID string) (lastEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev, ok := s.byCharge[chargeID]
	return ev, ok
}
