package asaas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danellalc/localpsp/dispatch"
)

func TestAdminStateReportsCountsAndEntities(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	subResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/subscriptions", subscriptionRequest{
		Customer: custID, BillingType: "PIX", Value: 10, NextDueDate: "2026-02-01", Cycle: "MONTHLY",
	})
	var sub subscriptionResponse
	subResp.decode(t, &sub)

	stateResp := request(t, http.MethodGet, httpSrv.URL+"/_localpsp/state", nil)
	if stateResp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", stateResp.status, http.StatusOK, stateResp.body)
	}
	var st adminStateResponse
	stateResp.decode(t, &st)

	if len(st.Customers) != 1 || st.Customers[0].ID != custID {
		t.Errorf("customers = %+v, want one entry for %s", st.Customers, custID)
	}
	if len(st.Charges) != 1 || st.Charges[0].ID != pay.ID {
		t.Errorf("charges = %+v, want one entry for %s", st.Charges, pay.ID)
	}
	if st.Charges[0].Status != "created" {
		t.Errorf("charge status = %q, want %q", st.Charges[0].Status, "created")
	}
	if len(st.Subscriptions) != 1 || st.Subscriptions[0].ID != sub.ID {
		t.Errorf("subscriptions = %+v, want one entry for %s", st.Subscriptions, sub.ID)
	}
}

func TestAdminStateReflectsWebhookQueueInterruption(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	_, httpSrv := newTestServer(t, 1, dispatch.Options{
		RetryInterval:    2 * time.Millisecond,
		FailureThreshold: 3,
	})
	custID := createTestCustomer(t, httpSrv)

	whResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    failing.URL,
		Events: []string{"PAYMENT_CONFIRMED"},
	})
	var wh webhookResponse
	whResp.decode(t, &wh)

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+pay.ID+"/confirm", nil)

	waitFor(t, 2*time.Second, func() bool {
		resp := request(t, http.MethodGet, httpSrv.URL+"/_localpsp/state", nil)
		var st adminStateResponse
		resp.decode(t, &st)
		for _, w := range st.Webhooks {
			if w.ID == wh.ID {
				return w.Interrupted
			}
		}
		return false
	})

	resp := request(t, http.MethodGet, httpSrv.URL+"/_localpsp/state", nil)
	var st adminStateResponse
	resp.decode(t, &st)

	var found *adminWebhookState
	for i := range st.Webhooks {
		if st.Webhooks[i].ID == wh.ID {
			found = &st.Webhooks[i]
		}
	}
	if found == nil {
		t.Fatalf("webhook %s not present in state, webhooks = %+v", wh.ID, st.Webhooks)
	}
	if !found.Interrupted {
		t.Errorf("interrupted = false, want true after %d consecutive failures", found.ConsecutiveFailures)
	}
	if found.ConsecutiveFailures != 3 {
		t.Errorf("consecutiveFailures = %d, want 3", found.ConsecutiveFailures)
	}
	if found.URL != failing.URL {
		t.Errorf("url = %q, want %q", found.URL, failing.URL)
	}

	foundLog := false
	for _, a := range st.DeliveryLog {
		if a.Endpoint == wh.ID {
			foundLog = true
			if a.Success {
				t.Errorf("delivery log entry %+v marked successful, want failure", a)
			}
		}
	}
	if !foundLog {
		t.Error("delivery log has no entries for the interrupted webhook")
	}
}

func TestClockAdvanceFiresPaymentOverdueWebhook(t *testing.T) {
	receivedCh := make(chan eventEnvelope, 1)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env eventEnvelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Errorf("decode webhook body error = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedCh <- env
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	_, httpSrv := newTestServer(t, 3, dispatch.Options{RetryInterval: 5 * time.Millisecond})
	custID := createTestCustomer(t, httpSrv)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    appServer.URL,
		Events: []string{"PAYMENT_OVERDUE"},
	})

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "BOLETO", Value: 25, DueDate: "2026-01-03",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	advanceResp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/clock/advance", map[string]string{"duration": "72h"})
	if advanceResp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", advanceResp.status, http.StatusOK, advanceResp.body)
	}
	var advanced clockAdvanceResponse
	advanceResp.decode(t, &advanced)
	if len(advanced.Transitions) != 1 {
		t.Fatalf("transitions = %+v, want exactly one", advanced.Transitions)
	}
	if advanced.Transitions[0].ChargeID != pay.ID || advanced.Transitions[0].To != "overdue" {
		t.Fatalf("transition = %+v, want %s to overdue", advanced.Transitions[0], pay.ID)
	}

	select {
	case env := <-receivedCh:
		if env.Event != "PAYMENT_OVERDUE" {
			t.Errorf("event = %q, want PAYMENT_OVERDUE", env.Event)
		}
		if env.Payment.ID != pay.ID {
			t.Errorf("payment id = %q, want %q", env.Payment.ID, pay.ID)
		}
		if env.Payment.Status != "OVERDUE" {
			t.Errorf("payment status = %q, want OVERDUE", env.Payment.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the PAYMENT_OVERDUE webhook")
	}
}

func TestClockAdvanceWithNothingDueReturnsNoTransitions(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/clock/advance", map[string]string{"duration": "1h"})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var advanced clockAdvanceResponse
	resp.decode(t, &advanced)
	if len(advanced.Transitions) != 0 {
		t.Errorf("transitions = %+v, want none", advanced.Transitions)
	}
}

// TestClockAdvanceAcceptsDaySuffix guards against a real gap caught in
// review: README.md's own example is "localpsp clock advance 3d", but
// time.ParseDuration has no day unit and used to reject it outright.
func TestClockAdvanceAcceptsDaySuffix(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "BOLETO", Value: 25, DueDate: "2026-01-02",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/clock/advance", map[string]string{"duration": "3d"})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var advanced clockAdvanceResponse
	resp.decode(t, &advanced)
	if len(advanced.Transitions) != 1 || advanced.Transitions[0].ChargeID != pay.ID {
		t.Fatalf("transitions = %+v, want one entry for %s", advanced.Transitions, pay.ID)
	}
	if advanced.Transitions[0].To != "overdue" {
		t.Errorf("to = %q, want overdue: 3d should advance the clock 72h, past the due date", advanced.Transitions[0].To)
	}
}

func TestClockAdvanceRejectsInvalidDuration(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/clock/advance", map[string]string{"duration": "3 days"})
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusBadRequest, resp.body)
	}
	var errResp errorResponse
	resp.decode(t, &errResp)
	if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "invalid_duration" {
		t.Errorf("errors = %+v, want a single invalid_duration error", errResp.Errors)
	}
}

func TestTriggerFiresPaymentConfirmed(t *testing.T) {
	receivedCh := make(chan eventEnvelope, 1)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env eventEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		receivedCh <- env
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	_, httpSrv := newTestServer(t, 4, dispatch.Options{RetryInterval: 5 * time.Millisecond})
	custID := createTestCustomer(t, httpSrv)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    appServer.URL,
		Events: []string{"PAYMENT_CONFIRMED"},
	})

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	triggerResp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/trigger", map[string]string{
		"charge": pay.ID, "event": "payment.confirmed",
	})
	if triggerResp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", triggerResp.status, http.StatusOK, triggerResp.body)
	}
	var triggered triggerResponse
	triggerResp.decode(t, &triggered)
	if triggered.Event != "PAYMENT_CONFIRMED" {
		t.Errorf("event = %q, want PAYMENT_CONFIRMED", triggered.Event)
	}
	if triggered.Charge.Status != "CONFIRMED" {
		t.Errorf("charge status = %q, want CONFIRMED", triggered.Charge.Status)
	}

	select {
	case env := <-receivedCh:
		if env.Event != "PAYMENT_CONFIRMED" {
			t.Errorf("event = %q, want PAYMENT_CONFIRMED", env.Event)
		}
		if env.Payment.ID != pay.ID {
			t.Errorf("payment id = %q, want %q", env.Payment.ID, pay.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the PAYMENT_CONFIRMED webhook")
	}
}

func TestTriggerFiresPaymentReceived(t *testing.T) {
	receivedCh := make(chan eventEnvelope, 1)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env eventEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		receivedCh <- env
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	_, httpSrv := newTestServer(t, 5, dispatch.Options{RetryInterval: 5 * time.Millisecond})
	custID := createTestCustomer(t, httpSrv)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    appServer.URL,
		Events: []string{"PAYMENT_RECEIVED"},
	})

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	triggerResp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/trigger", map[string]string{
		"charge": pay.ID, "event": "payment.received",
	})
	if triggerResp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", triggerResp.status, http.StatusOK, triggerResp.body)
	}
	var triggered triggerResponse
	triggerResp.decode(t, &triggered)
	if triggered.Charge.Status != "RECEIVED" {
		t.Errorf("charge status = %q, want RECEIVED", triggered.Charge.Status)
	}

	select {
	case env := <-receivedCh:
		if env.Event != "PAYMENT_RECEIVED" {
			t.Errorf("event = %q, want PAYMENT_RECEIVED", env.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the PAYMENT_RECEIVED webhook")
	}
}

func TestTriggerRejectsUnknownEvent(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)
	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/trigger", map[string]string{
		"charge": pay.ID, "event": "payment.made_up",
	})
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusBadRequest, resp.body)
	}
	var errResp errorResponse
	resp.decode(t, &errResp)
	if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "unknown_event" {
		t.Errorf("errors = %+v, want a single unknown_event error", errResp.Errors)
	}
}

func TestTriggerSurfacesInvalidTransitionAsBadRequest(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)
	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	first := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/trigger", map[string]string{
		"charge": pay.ID, "event": "payment.confirmed",
	})
	if first.status != http.StatusOK {
		t.Fatalf("first trigger status = %d, want %d: %s", first.status, http.StatusOK, first.body)
	}

	second := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/trigger", map[string]string{
		"charge": pay.ID, "event": "payment.confirmed",
	})
	if second.status != http.StatusBadRequest {
		t.Fatalf("second trigger status = %d, want %d: %s", second.status, http.StatusBadRequest, second.body)
	}
	var errResp errorResponse
	second.decode(t, &errResp)
	if len(errResp.Errors) != 1 || errResp.Errors[0].Code != "invalid_payment_state" {
		t.Errorf("errors = %+v, want a single invalid_payment_state error", errResp.Errors)
	}
}

func TestTriggerRequiresCharge(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/trigger", map[string]string{
		"event": "payment.confirmed",
	})
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusBadRequest, resp.body)
	}
}

func TestTriggerNotFoundCharge(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/trigger", map[string]string{
		"charge": "pay_does_not_exist", "event": "payment.confirmed",
	})
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusNotFound, resp.body)
	}
}
