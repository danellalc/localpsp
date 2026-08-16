package asaas

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danellalc/localpsp/engine"
)

type subscriptionRequest struct {
	Customer    string  `json:"customer"`
	BillingType string  `json:"billingType"`
	Value       float64 `json:"value"`
	NextDueDate string  `json:"nextDueDate"`
	Cycle       string  `json:"cycle"`
}

type subscriptionResponse struct {
	Object          string  `json:"object"`
	ID              string  `json:"id"`
	DateCreated     string  `json:"dateCreated"`
	Customer        string  `json:"customer"`
	BillingType     string  `json:"billingType"`
	Value           float64 `json:"value"`
	NextDueDate     string  `json:"nextDueDate"`
	Cycle           string  `json:"cycle"`
	Status          string  `json:"status"`
	Deleted         bool    `json:"deleted"`
	CheckoutSession *string `json:"checkoutSession"`
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var req subscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(req.Customer) == "" {
		writeError(w, http.StatusBadRequest, "customer_required", "customer is required")
		return
	}
	if strings.TrimSpace(req.BillingType) == "" {
		writeError(w, http.StatusBadRequest, "billing_type_required", "billingType is required")
		return
	}
	if strings.TrimSpace(req.Cycle) == "" {
		writeError(w, http.StatusBadRequest, "cycle_required", "cycle is required")
		return
	}
	if strings.TrimSpace(req.NextDueDate) == "" {
		writeError(w, http.StatusBadRequest, "next_due_date_required", "nextDueDate is required")
		return
	}
	if req.Value <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_value", "value must be greater than zero")
		return
	}
	nextDueDate, err := parseDate(req.NextDueDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_next_due_date", "nextDueDate must be in the format YYYY-MM-DD")
		return
	}

	sub, err := s.engine.CreateSubscription(r.Context(), engine.CreateSubscriptionInput{
		CustomerID:  req.Customer,
		BillingType: engine.BillingType(req.BillingType),
		Interval:    engine.Interval(req.Cycle),
		Amount:      reaisToCents(req.Value),
		NextDueDate: nextDueDate,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, newSubscriptionResponse(sub))
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	sub, err := s.engine.GetSubscription(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newSubscriptionResponse(sub))
}

func newSubscriptionResponse(sub *engine.Subscription) subscriptionResponse {
	return subscriptionResponse{
		Object:      "subscription",
		ID:          sub.ID,
		DateCreated: formatDateTime(sub.CreatedAt),
		Customer:    sub.CustomerID,
		BillingType: string(sub.BillingType),
		Value:       centsToReais(sub.Amount),
		NextDueDate: formatDate(sub.NextDueDate),
		Cycle:       string(sub.Interval),
		Status:      "ACTIVE",
		Deleted:     false,
	}
}
