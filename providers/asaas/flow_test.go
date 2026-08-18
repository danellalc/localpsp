package asaas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danellalc/localpsp/dispatch"
)

// TestExitCriterionFullFlow mirrors Fase 2's exit criterion: create a
// customer, create a PIX charge, register a webhook subscribed to
// PAYMENT_CONFIRMED, simulate the payment through the sandbox confirm
// endpoint, and see the webhook actually arrive with the right signature
// header and body, without ever touching engine or dispatch directly.
func TestExitCriterionFullFlow(t *testing.T) {
	type received struct {
		authToken string
		envelope  eventEnvelope
	}
	receivedCh := make(chan received, 1)

	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env eventEnvelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Errorf("decode webhook body error = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedCh <- received{authToken: r.Header.Get("asaas-access-token"), envelope: env}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	_, httpSrv := newTestServer(t, 42, dispatch.Options{RetryInterval: 5 * time.Millisecond})

	custResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/customers", customerRequest{
		Name:    "Loja Selar",
		CPFCNPJ: "12345678901",
		Email:   "loja@example.com",
	})
	var cust customerResponse
	custResp.decode(t, &cust)

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer:    cust.ID,
		BillingType: "PIX",
		Value:       150.00,
		DueDate:     "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)
	if pay.Status != "PENDING" {
		t.Fatalf("initial payment status = %q, want PENDING", pay.Status)
	}

	whResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    appServer.URL,
		Events: []string{"PAYMENT_CONFIRMED"},
	})
	var wh webhookResponse
	whResp.decode(t, &wh)

	confirmResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+pay.ID+"/confirm", nil)
	if confirmResp.status != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d: %s", confirmResp.status, http.StatusOK, confirmResp.body)
	}

	select {
	case got := <-receivedCh:
		if got.authToken != wh.AuthToken {
			t.Errorf("asaas-access-token header = %q, want %q", got.authToken, wh.AuthToken)
		}
		if got.envelope.Event != "PAYMENT_CONFIRMED" {
			t.Errorf("event = %q, want PAYMENT_CONFIRMED", got.envelope.Event)
		}
		if got.envelope.Payment.ID != pay.ID {
			t.Errorf("payment id = %q, want %q", got.envelope.Payment.ID, pay.ID)
		}
		if got.envelope.Payment.Status != "CONFIRMED" {
			t.Errorf("payment status = %q, want CONFIRMED", got.envelope.Payment.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the PAYMENT_CONFIRMED webhook")
	}
}

// TestEventAndWebhookIDsAreDeterministic guards against a real bug caught
// in review: event ids, webhook ids and auto-generated auth tokens were
// minted from an unseeded math/rand/v2 source, breaking the project's
// core promise that the same seed and call sequence produce byte-
// identical webhook payloads. Running the same flow twice with the same
// seed must mint the exact same evt_/wh_ ids and the exact same
// auto-generated authToken both times.
func TestEventAndWebhookIDsAreDeterministic(t *testing.T) {
	run := func(t *testing.T) (eventID, webhookID, authToken string) {
		t.Helper()

		receivedCh := make(chan eventEnvelope, 1)
		appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var env eventEnvelope
			_ = json.NewDecoder(r.Body).Decode(&env)
			receivedCh <- env
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(appServer.Close)

		_, httpSrv := newTestServer(t, 42, dispatch.Options{RetryInterval: 5 * time.Millisecond})
		custID := createTestCustomer(t, httpSrv)

		payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
			Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
		})
		var pay paymentResponse
		payResp.decode(t, &pay)

		// authToken deliberately omitted: the server must auto-generate
		// one, exactly like real Asaas does when the caller doesn't
		// supply it.
		whResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
			URL:    appServer.URL,
			Events: []string{"PAYMENT_CONFIRMED"},
		})
		var wh webhookResponse
		whResp.decode(t, &wh)

		request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+pay.ID+"/confirm", nil)

		select {
		case env := <-receivedCh:
			return env.ID, wh.ID, wh.AuthToken
		case <-time.After(2 * time.Second):
			t.Fatal("app server never received the webhook")
			return "", "", ""
		}
	}

	eventID1, webhookID1, authToken1 := run(t)
	eventID2, webhookID2, authToken2 := run(t)

	if eventID1 != eventID2 {
		t.Errorf("event id = %q on run 1, %q on run 2, want identical for the same seed", eventID1, eventID2)
	}
	if webhookID1 != webhookID2 {
		t.Errorf("webhook id = %q on run 1, %q on run 2, want identical for the same seed", webhookID1, webhookID2)
	}
	if authToken1 != authToken2 {
		t.Errorf("auto-generated authToken = %q on run 1, %q on run 2, want identical for the same seed", authToken1, authToken2)
	}
}

func TestWebhookNotSubscribedDoesNotReceiveEvent(t *testing.T) {
	receivedCh := make(chan struct{}, 1)
	subscribed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCh <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(subscribed.Close)

	otherCalled := make(chan struct{}, 1)
	notSubscribed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherCalled <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(notSubscribed.Close)

	_, httpSrv := newTestServer(t, 7, dispatch.Options{RetryInterval: 5 * time.Millisecond})
	custID := createTestCustomer(t, httpSrv)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    subscribed.URL,
		Events: []string{"PAYMENT_CONFIRMED"},
	})
	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    notSubscribed.URL,
		Events: []string{"PAYMENT_RECEIVED"},
	})

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+pay.ID+"/confirm", nil)

	select {
	case <-receivedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("subscribed webhook never received the event")
	}

	select {
	case <-otherCalled:
		t.Fatal("webhook not subscribed to PAYMENT_CONFIRMED received the event anyway")
	case <-time.After(100 * time.Millisecond):
	}
}
