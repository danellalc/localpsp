package asaas

// This file is localpsp's own chaos engineering surface: deliberately
// broken webhook delivery scenarios, mounted at /_localpsp/chaos/*
// alongside the rest of localpsp's own control surface (admin.go). These
// scenarios are localpsp's own testing tool, not something mirroring a
// real Asaas behavior: no real PSP exposes routes like these.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/danellalc/localpsp/dispatch"
	"github.com/danellalc/localpsp/engine"
)

type chaosChargeRequest struct {
	Charge string `json:"charge"`
}

// resolveChaosTarget looks up chargeID's last fired event, the shared
// starting point for duplicate-delivery, retry-storm and queue-interrupt.
// It writes a clean error response and reports ok=false for an empty
// charge id, an unknown charge, or a charge nothing has ever fired for.
func (s *Server) resolveChaosTarget(w http.ResponseWriter, r *http.Request, chargeID string) (lastEvent, bool) {
	if strings.TrimSpace(chargeID) == "" {
		writeError(w, http.StatusBadRequest, "charge_required", "charge is required")
		return lastEvent{}, false
	}
	if _, err := s.engine.GetCharge(r.Context(), chargeID); err != nil {
		writeEngineError(w, err)
		return lastEvent{}, false
	}
	ev, ok := s.lastEvents.get(chargeID)
	if !ok {
		writeError(w, http.StatusBadRequest, "no_event_fired",
			fmt.Sprintf("no webhook event has ever fired for payment %s, nothing to resend", chargeID))
		return lastEvent{}, false
	}
	return ev, true
}

type chaosDuplicateDeliveryResponse struct {
	EventID   string   `json:"eventId"`
	Endpoints []string `json:"endpoints"`
}

// handleChaosDuplicateDelivery resends a charge's last fired event with
// the exact same event id, to the exact same endpoint(s) it went to the
// first time. Same event id on purpose: this is an idempotency test, not
// a new event.
func (s *Server) handleChaosDuplicateDelivery(w http.ResponseWriter, r *http.Request) {
	var req chaosChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}

	ev, ok := s.resolveChaosTarget(w, r, req.Charge)
	if !ok {
		return
	}

	for _, name := range ev.Endpoints {
		_ = s.dispatch.Enqueue(name, ev.Event)
	}

	writeJSON(w, http.StatusOK, chaosDuplicateDeliveryResponse{EventID: ev.Event.ID, Endpoints: ev.Endpoints})
}

type chaosRetryStormRequest struct {
	Charge   string `json:"charge"`
	Failures int    `json:"failures"`
}

type chaosEndpointFailures struct {
	Endpoint string `json:"endpoint"`
	Injected int    `json:"injected"`
}

type chaosRetryStormResponse struct {
	EventID   string                  `json:"eventId"`
	Endpoints []chaosEndpointFailures `json:"endpoints"`
}

// handleChaosRetryStorm injects the requested number of synthetic
// delivery failures on every endpoint a charge's last fired event went
// to, then re-enqueues that same event so it plays through the injected
// failures for real before a genuine delivery attempt.
func (s *Server) handleChaosRetryStorm(w http.ResponseWriter, r *http.Request) {
	var req chaosRetryStormRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if req.Failures <= 0 {
		writeError(w, http.StatusBadRequest, "failures_required", "failures must be a positive integer")
		return
	}

	ev, ok := s.resolveChaosTarget(w, r, req.Charge)
	if !ok {
		return
	}

	endpoints := make([]chaosEndpointFailures, 0, len(ev.Endpoints))
	for _, name := range ev.Endpoints {
		if err := s.dispatch.InjectFailures(name, req.Failures); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "could not inject synthetic failures")
			return
		}
		endpoints = append(endpoints, chaosEndpointFailures{Endpoint: name, Injected: req.Failures})
	}
	for _, name := range ev.Endpoints {
		_ = s.dispatch.Enqueue(name, ev.Event)
	}

	writeJSON(w, http.StatusOK, chaosRetryStormResponse{EventID: ev.Event.ID, Endpoints: endpoints})
}

type chaosQueueInterruptResponse struct {
	EventID          string   `json:"eventId"`
	Endpoints        []string `json:"endpoints"`
	InjectedFailures int      `json:"injectedFailures"`
}

// handleChaosQueueInterrupt injects exactly dispatch.DefaultFailureThreshold
// synthetic failures, the real threshold that trips a real interruption,
// on every endpoint a charge's last fired event went to, then re-enqueues
// that event so the queue actually ends up interrupted, not just attempted.
func (s *Server) handleChaosQueueInterrupt(w http.ResponseWriter, r *http.Request) {
	var req chaosChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}

	ev, ok := s.resolveChaosTarget(w, r, req.Charge)
	if !ok {
		return
	}

	for _, name := range ev.Endpoints {
		if err := s.dispatch.InjectFailures(name, dispatch.DefaultFailureThreshold); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "could not inject synthetic failures")
			return
		}
	}
	for _, name := range ev.Endpoints {
		_ = s.dispatch.Enqueue(name, ev.Event)
	}

	writeJSON(w, http.StatusOK, chaosQueueInterruptResponse{
		EventID:          ev.Event.ID,
		Endpoints:        ev.Endpoints,
		InjectedFailures: dispatch.DefaultFailureThreshold,
	})
}

type chaosFiredEvent struct {
	Event     string   `json:"event"`
	EventID   string   `json:"eventId"`
	Endpoints []string `json:"endpoints"`
}

type chaosOutOfOrderResponse struct {
	Charge paymentResponse   `json:"charge"`
	Events []chaosFiredEvent `json:"events"`
}

// handleChaosOutOfOrder moves a charge created -> confirmed -> received
// through two real engine transitions, then enqueues the RECEIVED event
// before the CONFIRMED one, so a subscribed endpoint's FIFO delivery
// queue actually delivers PAYMENT_RECEIVED before PAYMENT_CONFIRMED.
// Events in the response are in that same delivery order.
func (s *Server) handleChaosOutOfOrder(w http.ResponseWriter, r *http.Request) {
	var req chaosChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(req.Charge) == "" {
		writeError(w, http.StatusBadRequest, "charge_required", "charge is required")
		return
	}

	confirmed, err := s.engine.TransitionCharge(r.Context(), req.Charge, engine.StatusConfirmed)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	confirmedEvent := s.buildStatusEvent(confirmed, eventPaymentConfirmed)

	received, err := s.engine.TransitionCharge(r.Context(), req.Charge, engine.StatusReceived)
	if err != nil {
		// The first transition already happened for real (it's committed
		// in engine, not something this handler can undo), so its webhook
		// must not just vanish because the second transition, needed to
		// reverse delivery order, failed, e.g. a concurrent transition
		// already moved this same charge on. Deliver it in the only
		// order left available: immediately, not reversed against an
		// event that never fired.
		s.enqueueEvent(confirmed.ID, eventPaymentConfirmed, confirmedEvent)
		writeEngineError(w, err)
		return
	}
	receivedEvent := s.buildStatusEvent(received, eventPaymentReceived)

	// Enqueue before writing the response, and read back the endpoints
	// enqueueEvent actually used (not a separate, earlier subscribers()
	// lookup): a concurrent webhook registration/update between "compute
	// the response" and "enqueue for real" would otherwise make the
	// reported endpoints disagree with who actually got the event, and
	// lastEvents (what chaos duplicate-delivery/retry-storm replay) must
	// be populated before a caller can see this response and chain
	// another chaos call against the same charge.
	receivedEndpoints := s.enqueueEvent(received.ID, eventPaymentReceived, receivedEvent)
	confirmedEndpoints := s.enqueueEvent(received.ID, eventPaymentConfirmed, confirmedEvent)

	writeJSON(w, http.StatusOK, chaosOutOfOrderResponse{
		Charge: s.newPaymentResponse(received),
		Events: []chaosFiredEvent{
			{Event: eventPaymentReceived, EventID: receivedEvent.ID, Endpoints: receivedEndpoints},
			{Event: eventPaymentConfirmed, EventID: confirmedEvent.ID, Endpoints: confirmedEndpoints},
		},
	})
}
