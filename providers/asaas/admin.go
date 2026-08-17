package asaas

// This file is localpsp's own control surface: the HTTP API behind the
// `state`, `trigger` and `clock advance` CLI commands. It is fixed at
// /_localpsp/* regardless of the Asaas facade's basePath, and it is not
// part of mirroring the real Asaas API: no real PSP exposes routes like
// these. It exists purely so localpsp's own tooling can inspect and drive
// the emulator it's sitting on top of.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danellalc/localpsp/engine"
)

// adminRoutes mounts localpsp's own control surface. Kept separate from
// routes() (the Asaas-mirroring routes) so the two are never confused for
// one another when reading server.go.
func (s *Server) adminRoutes() {
	s.mux.HandleFunc("GET /_localpsp/state", s.handleAdminState)
	s.mux.HandleFunc("POST /_localpsp/clock/advance", s.handleClockAdvance)
	s.mux.HandleFunc("POST /_localpsp/trigger", s.handleTrigger)
	s.mux.HandleFunc("POST /_localpsp/chaos/duplicate-delivery", s.handleChaosDuplicateDelivery)
	s.mux.HandleFunc("POST /_localpsp/chaos/retry-storm", s.handleChaosRetryStorm)
	s.mux.HandleFunc("POST /_localpsp/chaos/queue-interrupt", s.handleChaosQueueInterrupt)
	s.mux.HandleFunc("POST /_localpsp/chaos/out-of-order", s.handleChaosOutOfOrder)
}

type adminCustomer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	TaxID     string    `json:"taxId"`
	CreatedAt time.Time `json:"createdAt"`
}

type adminCharge struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customerId"`
	BillingType string    `json:"billingType"`
	Amount      int64     `json:"amount"`
	Status      string    `json:"status"`
	DueDate     time.Time `json:"dueDate"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type adminSubscription struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customerId"`
	BillingType string    `json:"billingType"`
	Interval    string    `json:"interval"`
	Amount      int64     `json:"amount"`
	NextDueDate time.Time `json:"nextDueDate"`
	CreatedAt   time.Time `json:"createdAt"`
}

type adminWebhookState struct {
	ID                  string `json:"id"`
	URL                 string `json:"url"`
	Interrupted         bool   `json:"interrupted"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	Pending             int    `json:"pending"`
}

type adminDeliveryAttempt struct {
	EventID  string    `json:"eventId"`
	Endpoint string    `json:"endpoint"`
	At       time.Time `json:"at"`
	Status   int       `json:"status"`
	Success  bool      `json:"success"`
	Err      *string   `json:"error,omitempty"`
}

type adminStateResponse struct {
	Customers     []adminCustomer        `json:"customers"`
	Charges       []adminCharge          `json:"charges"`
	Subscriptions []adminSubscription    `json:"subscriptions"`
	Webhooks      []adminWebhookState    `json:"webhooks"`
	DeliveryLog   []adminDeliveryAttempt `json:"deliveryLog"`
}

func (s *Server) handleAdminState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	customers, err := s.engine.ListCustomers(ctx)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	charges, err := s.engine.ListCharges(ctx)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	subs, err := s.engine.ListSubscriptions(ctx)
	if err != nil {
		writeEngineError(w, err)
		return
	}

	resp := adminStateResponse{
		Customers:     make([]adminCustomer, 0, len(customers)),
		Charges:       make([]adminCharge, 0, len(charges)),
		Subscriptions: make([]adminSubscription, 0, len(subs)),
		Webhooks:      make([]adminWebhookState, 0),
		DeliveryLog:   make([]adminDeliveryAttempt, 0),
	}

	for _, c := range customers {
		resp.Customers = append(resp.Customers, adminCustomer{
			ID:        c.ID,
			Name:      c.Name,
			Email:     c.Email,
			TaxID:     c.TaxID,
			CreatedAt: c.CreatedAt,
		})
	}
	for _, c := range charges {
		resp.Charges = append(resp.Charges, adminCharge{
			ID:          c.ID,
			CustomerID:  c.CustomerID,
			BillingType: string(c.BillingType),
			Amount:      c.Amount,
			Status:      string(c.Status),
			DueDate:     c.DueDate,
			CreatedAt:   c.CreatedAt,
			UpdatedAt:   c.UpdatedAt,
		})
	}
	for _, sub := range subs {
		resp.Subscriptions = append(resp.Subscriptions, adminSubscription{
			ID:          sub.ID,
			CustomerID:  sub.CustomerID,
			BillingType: string(sub.BillingType),
			Interval:    string(sub.Interval),
			Amount:      sub.Amount,
			NextDueDate: sub.NextDueDate,
			CreatedAt:   sub.CreatedAt,
		})
	}
	for _, cfg := range s.webhooks.all() {
		state, _ := s.dispatch.State(cfg.endpointName)
		resp.Webhooks = append(resp.Webhooks, adminWebhookState{
			ID:                  cfg.ID,
			URL:                 cfg.URL,
			Interrupted:         state.Interrupted,
			ConsecutiveFailures: state.ConsecutiveFailures,
			Pending:             state.Pending,
		})
	}
	for _, a := range s.dispatch.DeliveryLog() {
		var errMsg *string
		if a.Err != nil {
			m := a.Err.Error()
			errMsg = &m
		}
		resp.DeliveryLog = append(resp.DeliveryLog, adminDeliveryAttempt{
			EventID:  a.EventID,
			Endpoint: a.Endpoint,
			At:       a.At,
			Status:   a.Status,
			Success:  a.Success,
			Err:      errMsg,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

type clockAdvanceRequest struct {
	Duration string `json:"duration"`
}

type adminTransition struct {
	ChargeID string    `json:"chargeId"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	At       time.Time `json:"at"`
}

type clockAdvanceResponse struct {
	Now         time.Time         `json:"now"`
	Transitions []adminTransition `json:"transitions"`
}

// parseAdvanceDuration parses a Go duration string ("72h", "30m"), or a
// plain number of days ("3d"), the unit README.md's own example uses and
// time.ParseDuration itself has no notion of.
func parseAdvanceDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err == nil {
			return time.Duration(n * float64(24*time.Hour)), nil
		}
	}
	return 0, fmt.Errorf("%q is not a valid duration", s)
}

// handleClockAdvance moves the engine's virtual clock forward and fires a
// PAYMENT_OVERDUE webhook for every charge the advance flips to overdue,
// through the same fireStatusEvent helper the sandbox confirm handler
// uses.
func (s *Server) handleClockAdvance(w http.ResponseWriter, r *http.Request) {
	var req clockAdvanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	d, err := parseAdvanceDuration(req.Duration)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_duration",
			"duration must be a Go duration string like \"72h\" or \"30m\", or a number of days like \"3d\"")
		return
	}

	transitions, err := s.engine.AdvanceClock(r.Context(), d)
	if err != nil {
		writeEngineError(w, err)
		return
	}

	resp := clockAdvanceResponse{
		Now:         s.engine.Now(),
		Transitions: make([]adminTransition, 0, len(transitions)),
	}
	for _, t := range transitions {
		resp.Transitions = append(resp.Transitions, adminTransition{
			ChargeID: t.ChargeID,
			From:     string(t.From),
			To:       string(t.To),
			At:       t.At,
		})
	}
	writeJSON(w, http.StatusOK, resp)

	for _, t := range transitions {
		if t.To != engine.StatusOverdue {
			continue
		}
		s.fireStatusEvent(t.Charge, eventPaymentOverdue)
	}
}

// triggerEvent maps a CLI-facing event name to the engine transition and
// Asaas webhook event name it fires.
type triggerEvent struct {
	status engine.Status
	name   string
}

// triggerEvents lists exactly the event names localpsp's own docs use as
// `localpsp trigger` examples: payment.confirmed and payment.received.
var triggerEvents = map[string]triggerEvent{
	"payment.confirmed": {status: engine.StatusConfirmed, name: eventPaymentConfirmed},
	"payment.received":  {status: engine.StatusReceived, name: eventPaymentReceived},
}

type triggerRequest struct {
	Charge string `json:"charge"`
	Event  string `json:"event"`
}

type triggerResponse struct {
	Event  string          `json:"event"`
	Charge paymentResponse `json:"charge"`
}

// handleTrigger fires a lifecycle event at a charge on demand, the
// mechanism behind `localpsp trigger`. It shares fireStatusEvent with the
// sandbox confirm handler and handleClockAdvance instead of duplicating
// the envelope-building and dispatch logic.
func (s *Server) handleTrigger(w http.ResponseWriter, r *http.Request) {
	var req triggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(req.Charge) == "" {
		writeError(w, http.StatusBadRequest, "charge_required", "charge is required")
		return
	}

	spec, ok := triggerEvents[req.Event]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_event",
			fmt.Sprintf("event %q is not one localpsp trigger supports", req.Event))
		return
	}

	charge, err := s.engine.TransitionCharge(r.Context(), req.Charge, spec.status)
	if err != nil {
		writeEngineError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, triggerResponse{Event: spec.name, Charge: s.newPaymentResponse(charge)})

	s.fireStatusEvent(charge, spec.name)
}
