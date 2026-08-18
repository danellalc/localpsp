package asaas

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/danellalc/localpsp/dispatch"
	"github.com/danellalc/localpsp/engine"
)

// runWebhookScenario builds a full engine+dispatch+HTTP facade stack under
// seed, drives a fixed scenario against it over real HTTP (a webhook
// registration, two payments, a sandbox confirm and a clock advance past
// the second payment's due date), and returns every webhook body actually
// delivered, in arrival order, joined into one string. This is what backs
// AGENTS.md and ARCHITECTURE.md's determinism claim: the same seed
// produces byte-identical webhook logs, not just identical internal
// state, which is a narrower thing TestDeterminismAcross100Runs (in the
// engine package) already covers on its own.
func runWebhookScenario(t *testing.T, seed int64) string {
	t.Helper()

	var mu sync.Mutex
	var bodies [][]byte
	delivered := make(chan struct{}, 8)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		delivered <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer appServer.Close()

	eng, err := engine.New(context.Background(), engine.Options{
		Seed:      seed,
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("engine.New() error = %v", err)
	}
	defer func() {
		if err := eng.Close(); err != nil {
			t.Fatalf("engine Close() error = %v", err)
		}
	}()

	disp := dispatch.New(dispatch.Options{RetryInterval: 2 * time.Millisecond})
	defer func() {
		if err := disp.Close(); err != nil {
			t.Fatalf("dispatch Close() error = %v", err)
		}
	}()

	srv := NewServer(eng, disp, testBasePath, "")
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	// httptest.NewServer binds a random port each run, which would leak
	// into invoiceUrl and make every run look non-deterministic for a
	// reason that has nothing to do with the seed. Production doesn't
	// have this problem, publicURL there comes from --addr, fixed for
	// the life of the process, so pin it here too.
	srv.publicURL = "http://localhost:8420"

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/webhooks", webhookRequest{
		URL:    appServer.URL,
		Events: []string{"PAYMENT_CONFIRMED", "PAYMENT_OVERDUE"},
	})

	custResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/customers", customerRequest{
		Name:    "Fatima",
		CPFCNPJ: "12345678901",
	})
	var cust customerResponse
	custResp.decode(t, &cust)

	toConfirm := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: cust.ID, BillingType: "BOLETO", Value: 50, DueDate: "2026-01-03",
	})
	var confirmPay paymentResponse
	toConfirm.decode(t, &confirmPay)

	toOverdue := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: cust.ID, BillingType: "BOLETO", Value: 75, DueDate: "2026-01-02",
	})
	var overduePay paymentResponse
	toOverdue.decode(t, &overduePay)

	confirmResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+confirmPay.ID+"/confirm", nil)
	if confirmResp.status != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d: %s", confirmResp.status, http.StatusOK, confirmResp.body)
	}

	advanceResp := request(t, http.MethodPost, httpSrv.URL+"/_localpsp/clock/advance", map[string]string{"duration": "72h"})
	if advanceResp.status != http.StatusOK {
		t.Fatalf("clock advance status = %d, want %d: %s", advanceResp.status, http.StatusOK, advanceResp.body)
	}

	const wantDeliveries = 2
	for i := 0; i < wantDeliveries; i++ {
		select {
		case <-delivered:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d webhook deliveries arrived", i, wantDeliveries)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	return string(bytes.Join(bodies, []byte("\n")))
}

// TestWebhookLogsAreDeterministicAcross100Runs is the load-bearing test
// for the determinism claim as actually written in AGENTS.md and
// ARCHITECTURE.md: the same seed, run through the same scenario 100
// times, must deliver byte-identical webhook bodies, in the same order,
// every time, not just produce identical internal engine state.
func TestWebhookLogsAreDeterministicAcross100Runs(t *testing.T) {
	const runs = 100

	first := runWebhookScenario(t, 777)
	if first == "" {
		t.Fatal("first run delivered no webhook bodies")
	}

	for i := 1; i < runs; i++ {
		got := runWebhookScenario(t, 777)
		if got != first {
			t.Fatalf("run %d diverged from run 0:\nrun 0: %s\nrun %d: %s", i, first, i, got)
		}
	}
}
