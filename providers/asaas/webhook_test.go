package asaas

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/danellalc/localpsp/dispatch"
)

func TestRegisterWebhook(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    "http://example.test/webhook",
		Events: []string{"PAYMENT_CONFIRMED"},
	})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var wh webhookResponse
	resp.decode(t, &wh)

	if wh.ID == "" {
		t.Error("id is empty")
	}
	if wh.AuthToken == "" {
		t.Error("authToken was not generated")
	}
	if wh.Interrupted {
		t.Error("interrupted = true, want false on creation")
	}
	if !wh.Enabled {
		t.Error("enabled = false, want true by default")
	}
}

func TestRegisterWebhookGeneratesUniqueAuthTokens(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	first := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{URL: "http://example.test/a"})
	second := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{URL: "http://example.test/b"})

	var whA, whB webhookResponse
	first.decode(t, &whA)
	second.decode(t, &whB)

	if whA.AuthToken == whB.AuthToken {
		t.Errorf("two webhooks got the same generated authToken: %q", whA.AuthToken)
	}
	if whA.ID == whB.ID {
		t.Errorf("two webhooks got the same id: %q", whA.ID)
	}
}

func TestRegisterWebhookRequiresURL(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{})
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}
}

// TestConcurrentWebhookUpdatesStayConsistent guards against a real race
// caught in review: the registry write and the dispatch.UpdateEndpoint
// write used to happen as two separate, unsynchronized steps, so two
// concurrent PUTs could leave the registry reporting one URL/authToken
// while dispatch kept delivering to a different, stale one, permanently.
// After a burst of concurrent updates, one final sequential update must
// still apply cleanly and actually take effect on real delivery, proving
// the two stores never drifted apart.
func TestConcurrentWebhookUpdatesStayConsistent(t *testing.T) {
	received := make(chan string, 1)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case received <- r.Header.Get("asaas-access-token"):
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(appServer.Close)

	_, httpSrv := newTestServer(t, 3, dispatch.Options{RetryInterval: 5 * time.Millisecond})
	custID := createTestCustomer(t, httpSrv)

	whResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:       appServer.URL,
		AuthToken: "initial-token",
		Events:    []string{"PAYMENT_CONFIRMED"},
	})
	var wh webhookResponse
	whResp.decode(t, &wh)

	const burst = 50
	var wg sync.WaitGroup
	wg.Add(burst)
	for i := 0; i < burst; i++ {
		i := i
		go func() {
			defer wg.Done()
			request(t, http.MethodPut, httpSrv.URL+testBasePath+"/webhooks/"+wh.ID, webhookUpdateRequest{
				AuthToken: strPtr(strconv.Itoa(i)),
			})
		}()
	}
	wg.Wait()

	finalResp := request(t, http.MethodPut, httpSrv.URL+testBasePath+"/webhooks/"+wh.ID, webhookUpdateRequest{
		AuthToken: strPtr("final-token"),
	})
	if finalResp.status != http.StatusOK {
		t.Fatalf("final update status = %d, want %d: %s", finalResp.status, http.StatusOK, finalResp.body)
	}
	var final webhookResponse
	finalResp.decode(t, &final)
	if final.AuthToken != "final-token" {
		t.Fatalf("registry reports authToken = %q, want %q", final.AuthToken, "final-token")
	}

	payResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 10, DueDate: "2026-02-01",
	})
	var pay paymentResponse
	payResp.decode(t, &pay)
	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+pay.ID+"/confirm", nil)

	select {
	case got := <-received:
		if got != "final-token" {
			t.Errorf("delivered asaas-access-token = %q, want %q: registry and dispatch disagree", got, "final-token")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app server never received the webhook after the final update")
	}
}

func TestUpdateWebhookNotFound(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	resp := request(t, http.MethodPut, httpSrv.URL+testBasePath+"/webhooks/wh_does_not_exist", webhookUpdateRequest{})
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusNotFound)
	}
}

func TestUpdateWebhookReactivatesQueue(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	srv, httpSrv := newTestServer(t, 1, dispatch.Options{
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
		cfg, ok := srv.webhooks.get(wh.ID)
		if !ok {
			return false
		}
		state, err := srv.dispatch.State(cfg.endpointName)
		return err == nil && state.Interrupted
	})

	updateResp := request(t, http.MethodPut, httpSrv.URL+testBasePath+"/webhooks/"+wh.ID, webhookUpdateRequest{
		Interrupted: boolPtr(false),
	})
	if updateResp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", updateResp.status, http.StatusOK, updateResp.body)
	}
	var updated webhookResponse
	updateResp.decode(t, &updated)
	if updated.Interrupted {
		t.Error("interrupted = true right after reactivation, want false")
	}
}

func TestUpdateWebhookChangesURLAndEvents(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})

	createResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    "http://example.test/old",
		Events: []string{"PAYMENT_CONFIRMED"},
	})
	var wh webhookResponse
	createResp.decode(t, &wh)

	newURL := "http://example.test/new"
	updateResp := request(t, http.MethodPut, httpSrv.URL+testBasePath+"/webhooks/"+wh.ID, webhookUpdateRequest{
		URL:    &newURL,
		Events: []string{"PAYMENT_CONFIRMED", "PAYMENT_RECEIVED"},
	})
	if updateResp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", updateResp.status, http.StatusOK, updateResp.body)
	}
	var updated webhookResponse
	updateResp.decode(t, &updated)
	if updated.URL != newURL {
		t.Errorf("url = %q, want %q", updated.URL, newURL)
	}
	if len(updated.Events) != 2 {
		t.Errorf("events = %v, want 2 entries", updated.Events)
	}
}
