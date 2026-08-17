package asaas

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/danellalc/localpsp/dispatch"
)

// chaosFixture stands up a server with a customer, a PIX charge and a
// webhook subscribed to every event a chaos scenario in this file might
// fire, and confirms the payment so a real PAYMENT_CONFIRMED event (the
// material chaos duplicate-delivery and chaos retry-storm resend) has
// already fired once.
func chaosFixture(t *testing.T, whURL string, dispatchOpts dispatch.Options) (*Server, *httptest.Server, string) {
	t.Helper()

	srv, httpSrv := newTestServer(t, 1, dispatchOpts)
	custID := createTestCustomer(t, httpSrv)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    whURL,
		Events: []string{"PAYMENT_CONFIRMED", "PAYMENT_RECEIVED"},
	})

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	confirmResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+pay.ID+"/confirm", nil)
	if confirmResp.status != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d: %s", confirmResp.status, http.StatusOK, confirmResp.body)
	}

	return srv, httpSrv, pay.ID
}

func TestChaosDuplicateDeliveryResendsSameEventID(t *testing.T) {
	type arrival struct {
		eventID string
	}
	arrivals := make(chan arrival, 4)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env eventEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		arrivals <- arrival{eventID: env.ID}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	_, httpSrv, payID := chaosFixture(t, appServer.URL, dispatch.Options{RetryInterval: 5 * time.Millisecond})

	var first arrival
	select {
	case first = <-arrivals:
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the original PAYMENT_CONFIRMED webhook")
	}
	if first.eventID == "" {
		t.Fatal("original delivery arrived with an empty event id")
	}

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/chaos/duplicate-delivery", map[string]string{"charge": payID})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var body chaosDuplicateDeliveryResponse
	resp.decode(t, &body)
	if body.EventID != first.eventID {
		t.Errorf("response eventId = %q, want %q (the original event, not a new one)", body.EventID, first.eventID)
	}
	if len(body.Endpoints) != 1 {
		t.Fatalf("response endpoints = %v, want exactly one", body.Endpoints)
	}

	select {
	case second := <-arrivals:
		if second.eventID != first.eventID {
			t.Errorf("resent event id = %q, want %q: duplicate delivery must reuse the same id", second.eventID, first.eventID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the duplicate delivery")
	}
}

func TestChaosRetryStormInjectsFailuresThenDeliversForReal(t *testing.T) {
	var requestCount int
	var mu sync.Mutex
	delivered := make(chan string, 1)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		var env eventEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		delivered <- env.ID
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	srv, httpSrv, payID := chaosFixture(t, appServer.URL, dispatch.Options{RetryInterval: 200 * time.Millisecond})

	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the original PAYMENT_CONFIRMED webhook")
	}

	mu.Lock()
	requestCount = 0
	mu.Unlock()

	const failures = 3
	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/chaos/retry-storm", map[string]any{
		"charge": payID, "failures": failures,
	})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var body chaosRetryStormResponse
	resp.decode(t, &body)
	if len(body.Endpoints) != 1 || body.Endpoints[0].Injected != failures {
		t.Fatalf("response endpoints = %+v, want one entry with %d injected", body.Endpoints, failures)
	}
	endpoint := body.Endpoints[0].Endpoint

	select {
	case eventID := <-delivered:
		if eventID != body.EventID {
			t.Errorf("delivered event id = %q, want %q", eventID, body.EventID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("real delivery after the injected failures never arrived")
	}

	mu.Lock()
	got := requestCount
	mu.Unlock()
	if got != 1 {
		t.Errorf("requestCount = %d, want exactly 1: the injected failures must never reach the real server", got)
	}

	// requestCount alone can't tell a real injection from InjectFailures
	// silently doing nothing and the resent event just succeeding on its
	// first real attempt, both look like exactly one real request. The
	// delivery log is what actually proves the failures happened.
	//
	// The <-delivered receive above only proves the HTTP handler ran, not
	// that dispatch has finished appending that attempt's outcome to its
	// own log yet, so poll for it instead of reading immediately.
	entriesFor := func() []dispatch.DeliveryAttempt {
		var out []dispatch.DeliveryAttempt
		for _, entry := range srv.dispatch.DeliveryLog() {
			if entry.Endpoint == endpoint {
				out = append(out, entry)
			}
		}
		return out
	}
	waitFor(t, 2*time.Second, func() bool {
		tail := entriesFor()
		return len(tail) > 0 && tail[len(tail)-1].Success
	})

	tail := entriesFor()
	if len(tail) < failures+1 {
		t.Fatalf("delivery log has %d entries for %s, want at least %d (failures + the real delivery)", len(tail), endpoint, failures+1)
	}
	streak := tail[len(tail)-failures-1 : len(tail)-1]
	for i, entry := range streak {
		if entry.Success {
			t.Errorf("synthetic entry %d: Success = true, want false", i)
		}
		if !errors.Is(entry.Err, dispatch.ErrSimulatedFailure) {
			t.Errorf("synthetic entry %d: Err = %v, want a synthetic failure", i, entry.Err)
		}
	}
	last := tail[len(tail)-1]
	if !last.Success || last.EventID != body.EventID {
		t.Errorf("last entry = %+v, want a successful delivery of %s", last, body.EventID)
	}
}

// TestChaosQueueInterruptActuallyInterruptsTheQueue confirms the queue
// really does end up interrupted, tripped by exactly
// dispatch.DefaultFailureThreshold consecutive failures, and that the
// consecutive run that tripped it is entirely synthetic: real requests
// (like the fixture's own original PAYMENT_CONFIRMED delivery) are
// allowed to have happened earlier, they just can't be part of the
// failure streak that caused the interruption.
func TestChaosQueueInterruptActuallyInterruptsTheQueue(t *testing.T) {
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	srv, httpSrv, payID := chaosFixture(t, appServer.URL, dispatch.Options{RetryInterval: 2 * time.Millisecond})

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/chaos/queue-interrupt", map[string]string{"charge": payID})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var body chaosQueueInterruptResponse
	resp.decode(t, &body)
	if body.InjectedFailures != dispatch.DefaultFailureThreshold {
		t.Errorf("injectedFailures = %d, want %d", body.InjectedFailures, dispatch.DefaultFailureThreshold)
	}
	if len(body.Endpoints) != 1 {
		t.Fatalf("response endpoints = %v, want exactly one", body.Endpoints)
	}
	endpoint := body.Endpoints[0]

	waitFor(t, 2*time.Second, func() bool {
		state, err := srv.dispatch.State(endpoint)
		return err == nil && state.Interrupted
	})

	state, err := srv.dispatch.State(endpoint)
	if err != nil {
		t.Fatalf("dispatch.State() error = %v", err)
	}
	if state.ConsecutiveFailures != dispatch.DefaultFailureThreshold {
		t.Errorf("ConsecutiveFailures = %d, want %d", state.ConsecutiveFailures, dispatch.DefaultFailureThreshold)
	}

	log := srv.dispatch.DeliveryLog()
	if len(log) < dispatch.DefaultFailureThreshold {
		t.Fatalf("DeliveryLog has %d entries, want at least %d", len(log), dispatch.DefaultFailureThreshold)
	}
	tail := log[len(log)-dispatch.DefaultFailureThreshold:]
	for i, entry := range tail {
		if entry.Success {
			t.Errorf("tail entry %d: Success = true, want false", i)
		}
		if !errors.Is(entry.Err, dispatch.ErrSimulatedFailure) {
			t.Errorf("tail entry %d: Err = %v, want a synthetic failure: the interruption must be tripped by injected failures, not a real outage", i, entry.Err)
		}
	}
}

func TestChaosOutOfOrderDeliversReceivedBeforeConfirmed(t *testing.T) {
	var mu sync.Mutex
	var order []string
	arrived := make(chan struct{}, 2)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env eventEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		mu.Lock()
		order = append(order, env.Event)
		mu.Unlock()
		arrived <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	_, httpSrv := newTestServer(t, 9, dispatch.Options{RetryInterval: 5 * time.Millisecond})
	custID := createTestCustomer(t, httpSrv)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    appServer.URL,
		Events: []string{"PAYMENT_CONFIRMED", "PAYMENT_RECEIVED"},
	})

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/chaos/out-of-order", map[string]string{"charge": pay.ID})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var body chaosOutOfOrderResponse
	resp.decode(t, &body)
	if body.Charge.Status != "RECEIVED" {
		t.Errorf("charge status = %q, want RECEIVED", body.Charge.Status)
	}
	if len(body.Events) != 2 || body.Events[0].Event != "PAYMENT_RECEIVED" || body.Events[1].Event != "PAYMENT_CONFIRMED" {
		t.Fatalf("response events = %+v, want [PAYMENT_RECEIVED, PAYMENT_CONFIRMED] in that order", body.Events)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 2 webhook deliveries arrived", i)
		}
	}

	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "PAYMENT_RECEIVED" || got[1] != "PAYMENT_CONFIRMED" {
		t.Errorf("delivery arrival order = %v, want [PAYMENT_RECEIVED PAYMENT_CONFIRMED]", got)
	}
}

func TestChaosScenariosRejectUnknownCharge(t *testing.T) {
	tests := []struct {
		name string
		path string
		body any
	}{
		{"duplicate-delivery", "/_localpsp/chaos/duplicate-delivery", map[string]string{"charge": "pay_does_not_exist"}},
		{"retry-storm", "/_localpsp/chaos/retry-storm", map[string]any{"charge": "pay_does_not_exist", "failures": 3}},
		{"queue-interrupt", "/_localpsp/chaos/queue-interrupt", map[string]string{"charge": "pay_does_not_exist"}},
		{"out-of-order", "/_localpsp/chaos/out-of-order", map[string]string{"charge": "pay_does_not_exist"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, httpSrv := newTestServer(t, 1, dispatch.Options{})

			resp := request(t, http.MethodPost, httpSrv.URL+tt.path, tt.body)
			if resp.status != http.StatusNotFound {
				t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusNotFound, resp.body)
			}
			var errResp errorResponse
			resp.decode(t, &errResp)
			if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "payment_not_found" {
				t.Errorf("errors = %+v, want a single payment_not_found error", errResp.Errors)
			}
		})
	}
}

func TestChaosScenariosRejectChargeWithNoPriorEvent(t *testing.T) {
	tests := []struct {
		name string
		path string
		body func(charge string) any
	}{
		{"duplicate-delivery", "/_localpsp/chaos/duplicate-delivery", func(c string) any { return map[string]string{"charge": c} }},
		{"retry-storm", "/_localpsp/chaos/retry-storm", func(c string) any { return map[string]any{"charge": c, "failures": 3} }},
		{"queue-interrupt", "/_localpsp/chaos/queue-interrupt", func(c string) any { return map[string]string{"charge": c} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, httpSrv := newTestServer(t, 1, dispatch.Options{})
			custID := createTestCustomer(t, httpSrv)
			payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
				Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
			})
			var pay paymentResponse
			payResp.decode(t, &pay)

			resp := request(t, http.MethodPost, httpSrv.URL+tt.path, tt.body(pay.ID))
			if resp.status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusBadRequest, resp.body)
			}
			var errResp errorResponse
			resp.decode(t, &errResp)
			if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "no_event_fired" {
				t.Errorf("errors = %+v, want a single no_event_fired error", errResp.Errors)
			}
		})
	}
}

func TestChaosRetryStormRejectsNonPositiveFailures(t *testing.T) {
	tests := []struct {
		name     string
		failures int
	}{
		{"zero", 0},
		{"negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, httpSrv, payID := chaosFixture(t, "http://127.0.0.1:1/unreachable", dispatch.Options{})

			resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/chaos/retry-storm", map[string]any{
				"charge": payID, "failures": tt.failures,
			})
			if resp.status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusBadRequest, resp.body)
			}
			var errResp errorResponse
			resp.decode(t, &errResp)
			if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "failures_required" {
				t.Errorf("errors = %+v, want a single failures_required error", errResp.Errors)
			}
		})
	}
}

func TestChaosScenariosRejectInvalidJSON(t *testing.T) {
	paths := []string{
		"/_localpsp/chaos/duplicate-delivery",
		"/_localpsp/chaos/retry-storm",
		"/_localpsp/chaos/queue-interrupt",
		"/_localpsp/chaos/out-of-order",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			_, httpSrv := newTestServer(t, 1, dispatch.Options{})

			// A JSON string decodes fine as JSON but not into any of these
			// handlers' request structs, tripping the same decode-error
			// branch a genuinely malformed body would.
			resp := request(t, http.MethodPost, httpSrv.URL+path, "not a request object")
			if resp.status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusBadRequest, resp.body)
			}
			var errResp errorResponse
			resp.decode(t, &errResp)
			if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "invalid_json" {
				t.Errorf("errors = %+v, want a single invalid_json error", errResp.Errors)
			}
		})
	}
}

func TestChaosOutOfOrderRejectsChargeNotInCreatedStatus(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)
	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+pay.ID+"/confirm", nil)

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/chaos/out-of-order", map[string]string{"charge": pay.ID})
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusBadRequest, resp.body)
	}
	var errResp errorResponse
	resp.decode(t, &errResp)
	if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "invalid_payment_state" {
		t.Errorf("errors = %+v, want a single invalid_payment_state error", errResp.Errors)
	}
}
