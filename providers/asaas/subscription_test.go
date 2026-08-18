package asaas

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danellalc/localpsp/dispatch"
)

func TestCreateSubscription(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/subscriptions", subscriptionRequest{
		Customer:    custID,
		BillingType: "CREDIT_CARD",
		Value:       99.90,
		NextDueDate: "2026-03-01",
		Cycle:       "MONTHLY",
	})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.status, http.StatusOK, resp.body)
	}
	var sub subscriptionResponse
	resp.decode(t, &sub)

	if !strings.HasPrefix(sub.ID, "sub_") {
		t.Errorf("id = %q, want sub_ prefix", sub.ID)
	}
	if sub.Cycle != "MONTHLY" {
		t.Errorf("cycle = %q, want MONTHLY", sub.Cycle)
	}
	if sub.NextDueDate != "2026-03-01" {
		t.Errorf("nextDueDate = %q, want 2026-03-01", sub.NextDueDate)
	}
	if sub.Value != 99.90 {
		t.Errorf("value = %v, want 99.90", sub.Value)
	}
	if sub.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", sub.Status)
	}

	getResp := request(t, http.MethodGet, httpSrv.URL+testBasePath+"/subscriptions/"+sub.ID, nil)
	if getResp.status != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", getResp.status, http.StatusOK, getResp.body)
	}
	var fetched subscriptionResponse
	getResp.decode(t, &fetched)
	if fetched.ID != sub.ID {
		t.Errorf("fetched id = %q, want %q", fetched.ID, sub.ID)
	}
}

func TestCreateSubscriptionValidation(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	custID := createTestCustomer(t, httpSrv)

	tests := []struct {
		name       string
		req        subscriptionRequest
		wantStatus int
		wantCode   string
	}{
		{"missing customer", subscriptionRequest{BillingType: "PIX", Value: 10, NextDueDate: "2026-03-01", Cycle: "MONTHLY"}, http.StatusBadRequest, "customer_required"},
		{"missing billingType", subscriptionRequest{Customer: custID, Value: 10, NextDueDate: "2026-03-01", Cycle: "MONTHLY"}, http.StatusBadRequest, "billing_type_required"},
		{"missing cycle", subscriptionRequest{Customer: custID, BillingType: "PIX", Value: 10, NextDueDate: "2026-03-01"}, http.StatusBadRequest, "cycle_required"},
		{"invalid cycle", subscriptionRequest{Customer: custID, BillingType: "PIX", Value: 10, NextDueDate: "2026-03-01", Cycle: "DAILY"}, http.StatusBadRequest, "invalid_cycle"},
		{"missing nextDueDate", subscriptionRequest{Customer: custID, BillingType: "PIX", Value: 10, Cycle: "MONTHLY"}, http.StatusBadRequest, "next_due_date_required"},
		{"invalid nextDueDate", subscriptionRequest{Customer: custID, BillingType: "PIX", Value: 10, NextDueDate: "not-a-date", Cycle: "MONTHLY"}, http.StatusBadRequest, "invalid_next_due_date"},
		{"zero value", subscriptionRequest{Customer: custID, BillingType: "PIX", Value: 0, NextDueDate: "2026-03-01", Cycle: "MONTHLY"}, http.StatusBadRequest, "invalid_value"},
		{"unknown customer", subscriptionRequest{Customer: "cus_does_not_exist", BillingType: "PIX", Value: 10, NextDueDate: "2026-03-01", Cycle: "MONTHLY"}, http.StatusNotFound, "customer_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := request(t, http.MethodPost, httpSrv.URL+testBasePath+"/subscriptions", tt.req)
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

func TestGetSubscriptionNotFound(t *testing.T) {
	_, httpSrv := newTestServer(t, 1, dispatch.Options{})
	resp := request(t, http.MethodGet, httpSrv.URL+testBasePath+"/subscriptions/sub_does_not_exist", nil)
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusNotFound)
	}
}
