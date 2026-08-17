package asaas

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danellalc/localpsp/engine"
)

type apiError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type errorResponse struct {
	Errors []apiError `json:"errors"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, errorResponse{Errors: []apiError{{Code: code, Description: description}}})
}

func writeEngineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrCustomerNotFound):
		writeError(w, http.StatusNotFound, "customer_not_found", "customer not found")
	case errors.Is(err, engine.ErrChargeNotFound):
		writeError(w, http.StatusNotFound, "payment_not_found", "payment not found")
	case errors.Is(err, engine.ErrSubscriptionNotFound):
		writeError(w, http.StatusNotFound, "subscription_not_found", "subscription not found")
	case errors.Is(err, engine.ErrInvalidBillingType):
		writeError(w, http.StatusBadRequest, "invalid_billing_type", "billingType is not a value this emulator supports")
	case errors.Is(err, engine.ErrInvalidInterval):
		writeError(w, http.StatusBadRequest, "invalid_cycle", "cycle is not a value this emulator supports")
	case errors.Is(err, engine.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "invalid_value", "value must be greater than zero")
	case errors.Is(err, engine.ErrInvalidTransition):
		writeError(w, http.StatusBadRequest, "invalid_payment_state", "payment cannot move to that status from its current status")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "unexpected error")
	}
}
