package asaas

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danellalc/localpsp/dispatch"
	"github.com/danellalc/localpsp/engine"
)

func TestCreatePayment(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer:    custID,
		BillingType: "PIX",
		Value:       19.90,
		DueDate:     "2026-02-01",
	})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}

	var pay paymentResponse
	resp.decode(t, &pay)

	if !strings.HasPrefix(pay.ID, "pay_") {
		t.Errorf("id = %q, want pay_ prefix", pay.ID)
	}
	if pay.Status != "PENDING" {
		t.Errorf("status = %q, want PENDING", pay.Status)
	}
	if pay.Value != 19.90 {
		t.Errorf("value = %v, want 19.90", pay.Value)
	}
	if pay.DueDate != "2026-02-01" {
		t.Errorf("dueDate = %q, want 2026-02-01", pay.DueDate)
	}
	if pay.BillingType != "PIX" {
		t.Errorf("billingType = %q, want PIX", pay.BillingType)
	}
	if pay.PaymentDate != nil {
		t.Errorf("paymentDate = %v, want nil for a pending payment", *pay.PaymentDate)
	}

	getResp := request(t, http.MethodGet, httpSrv.URL+testBasePath+"/payments/"+pay.ID, nil)
	if getResp.status != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", getResp.status, http.StatusOK, getResp.body)
	}
	var fetched paymentResponse
	getResp.decode(t, &fetched)
	if fetched.ID != pay.ID {
		t.Errorf("fetched id = %q, want %q", fetched.ID, pay.ID)
	}
}

func TestPaymentStatusTranslation(t *testing.T) {
	tests := []struct {
		status engine.Status
		want   string
	}{
		{engine.StatusCreated, "PENDING"},
		{engine.StatusConfirmed, "CONFIRMED"},
		{engine.StatusReceived, "RECEIVED"},
		{engine.StatusOverdue, "OVERDUE"},
		{engine.StatusRefunded, "REFUNDED"},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := asaasPaymentStatus(tt.status); got != tt.want {
				t.Errorf("asaasPaymentStatus(%s) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestPaymentMoneyRoundTrip(t *testing.T) {
	tests := []struct {
		reais     float64
		wantCents int64
	}{
		{19.90, 1990},
		{0.10, 10},
		{100.00, 10000},
		{0.01, 1},
		{9999.99, 999999},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			srv, httpSrv := newTestServer(t, 1, dispatch.Options{})
			custID := createTestCustomer(t, httpSrv)

			resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
				Customer:    custID,
				BillingType: "PIX",
				Value:       tt.reais,
				DueDate:     "2026-02-01",
			})
			if resp.status != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
			}
			var pay paymentResponse
			resp.decode(t, &pay)

			if pay.Value != tt.reais {
				t.Errorf("round-tripped JSON value = %v, want %v", pay.Value, tt.reais)
			}

			charge, err := srv.engine.GetCharge(t.Context(), pay.ID)
			if err != nil {
				t.Fatalf("GetCharge() error = %v", err)
			}
			if charge.Amount != tt.wantCents {
				t.Errorf("stored cents = %d, want %d", charge.Amount, tt.wantCents)
			}
		})
	}
}

func TestCreatePaymentValidation(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	tests := []struct {
		name       string
		req        paymentRequest
		wantStatus int
		wantCode   string
	}{
		{"missing customer", paymentRequest{BillingType: "PIX", Value: 10, DueDate: "2026-02-01"}, http.StatusBadRequest, "customer_required"},
		{"missing billingType", paymentRequest{Customer: custID, Value: 10, DueDate: "2026-02-01"}, http.StatusBadRequest, "billing_type_required"},
		{"missing dueDate", paymentRequest{Customer: custID, BillingType: "PIX", Value: 10}, http.StatusBadRequest, "due_date_required"},
		{"invalid dueDate", paymentRequest{Customer: custID, BillingType: "PIX", Value: 10, DueDate: "02/01/2026"}, http.StatusBadRequest, "invalid_due_date"},
		{"garbage billing type", paymentRequest{Customer: custID, BillingType: "DEBIT_CARD", Value: 10, DueDate: "2026-02-01"}, http.StatusBadRequest, "invalid_billing_type"},
		{"zero value", paymentRequest{Customer: custID, BillingType: "PIX", Value: 0, DueDate: "2026-02-01"}, http.StatusBadRequest, "invalid_value"},
		{"unknown customer", paymentRequest{Customer: "cus_does_not_exist", BillingType: "PIX", Value: 10, DueDate: "2026-02-01"}, http.StatusNotFound, "customer_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", tt.req)
			if resp.status != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.status, tt.wantStatus, resp.body)
			}
			var errResp errorResponse
			resp.decode(t, &errResp)
			if len(errResp.Errors) != 1 || errResp.Errors[0].Code != tt.wantCode {
				t.Errorf("errors = %+v, want a single %s error", errResp.Errors, tt.wantCode)
			}
		})
	}
}

// TestCreatePaymentAcceptsUndefinedBillingType guards against a real bug
// caught in review: UNDEFINED is a confirmed, real Asaas billingType (see
// FIDELITY.md), it lets the payer choose at checkout, it was wrongly
// rejected as invalid.
func TestCreatePaymentAcceptsUndefinedBillingType(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer:    custID,
		BillingType: "UNDEFINED",
		Value:       10,
		DueDate:     "2026-02-01",
	})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var pay paymentResponse
	resp.decode(t, &pay)
	if pay.BillingType != "UNDEFINED" {
		t.Errorf("billingType = %q, want UNDEFINED", pay.BillingType)
	}
}

func TestGetPaymentNotFound(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	resp := request(t, http.MethodGet, httpSrv.URL+testBasePath+"/payments/pay_does_not_exist", nil)
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusNotFound)
	}
}

func TestConfirmPayment(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	createResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer:    custID,
		BillingType: "PIX",
		Value:       50,
		DueDate:     "2026-02-01",
	})
	var created paymentResponse
	createResp.decode(t, &created)

	confirmResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+created.ID+"/confirm", nil)
	if confirmResp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", confirmResp.status, http.StatusOK, confirmResp.body)
	}
	var confirmed paymentResponse
	confirmResp.decode(t, &confirmed)
	if confirmed.Status != "CONFIRMED" {
		t.Errorf("status = %q, want CONFIRMED", confirmed.Status)
	}
	if confirmed.PaymentDate == nil {
		t.Errorf("paymentDate = nil, want a date once confirmed")
	}
}

func TestConfirmPaymentNotFound(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/pay_does_not_exist/confirm", nil)
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusNotFound)
	}
}

func TestConfirmPaymentTwiceFails(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)
	createResp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/payments", paymentRequest{
		Customer: custID, BillingType: "PIX", Value: 50, DueDate: "2026-02-01",
	})
	var created paymentResponse
	createResp.decode(t, &created)

	request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+created.ID+"/confirm", nil)
	second := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/sandbox/payment/"+created.ID+"/confirm", nil)
	if second.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", second.status, http.StatusBadRequest, second.body)
	}
}
