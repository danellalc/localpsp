package asaas

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danellalc/localpsp/engine"
)

type paymentRequest struct {
	Customer    string  `json:"customer"`
	BillingType string  `json:"billingType"`
	Value       float64 `json:"value"`
	DueDate     string  `json:"dueDate"`
}

type paymentResponse struct {
	Object      string  `json:"object"`
	ID          string  `json:"id"`
	DateCreated string  `json:"dateCreated"`
	Customer    string  `json:"customer"`
	BillingType string  `json:"billingType"`
	Value       float64 `json:"value"`
	NetValue    float64 `json:"netValue"`
	Status      string  `json:"status"`
	DueDate     string  `json:"dueDate"`
	PaymentDate *string `json:"paymentDate"`
	InvoiceURL  *string `json:"invoiceUrl"`
	BankSlipURL *string `json:"bankSlipUrl"`
	PixQrCodeID *string `json:"pixQrCodeId"`
	Deleted     bool    `json:"deleted"`
	Anticipated bool    `json:"anticipated"`
}

func asaasPaymentStatus(status engine.Status) string {
	switch status {
	case engine.StatusCreated:
		return "PENDING"
	case engine.StatusConfirmed:
		return "CONFIRMED"
	case engine.StatusReceived:
		return "RECEIVED"
	case engine.StatusOverdue:
		return "OVERDUE"
	case engine.StatusRefunded:
		return "REFUNDED"
	default:
		return string(status)
	}
}

// paymentDateApplies reports whether a payment's response should include
// a paymentDate. StatusRefunded is deliberately excluded: UpdatedAt gets
// re-stamped on every transition, so it can't tell a refund's timestamp
// apart from the original payment's, and there's no refund route wired up
// yet to even reach this case. Once one exists, this needs a real "first
// paid at" timestamp in engine, not a guess from UpdatedAt.
func paymentDateApplies(status engine.Status) bool {
	switch status {
	case engine.StatusConfirmed, engine.StatusReceived:
		return true
	default:
		return false
	}
}

func (s *Server) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	var req paymentRequest
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
	if strings.TrimSpace(req.DueDate) == "" {
		writeError(w, http.StatusBadRequest, "due_date_required", "dueDate is required")
		return
	}
	dueDate, err := parseDate(req.DueDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_due_date", "dueDate must be in the format YYYY-MM-DD")
		return
	}

	charge, err := s.engine.CreateCharge(r.Context(), engine.CreateChargeInput{
		CustomerID:  req.Customer,
		BillingType: engine.BillingType(req.BillingType),
		Amount:      reaisToCents(req.Value),
		DueDate:     dueDate,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, newPaymentResponse(charge))
}

func (s *Server) handleGetPayment(w http.ResponseWriter, r *http.Request) {
	charge, err := s.engine.GetCharge(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newPaymentResponse(charge))
}

func (s *Server) handleConfirmPayment(w http.ResponseWriter, r *http.Request) {
	charge, err := s.engine.TransitionCharge(r.Context(), r.PathValue("id"), engine.StatusConfirmed)
	if err != nil {
		writeEngineError(w, err)
		return
	}

	resp := newPaymentResponse(charge)
	writeJSON(w, http.StatusOK, resp)

	s.fireEvent(eventPaymentConfirmed, resp)
}

func newPaymentResponse(c *engine.Charge) paymentResponse {
	value := centsToReais(c.Amount)
	resp := paymentResponse{
		Object:      "payment",
		ID:          c.ID,
		DateCreated: formatDateTime(c.CreatedAt),
		Customer:    c.CustomerID,
		BillingType: string(c.BillingType),
		Value:       value,
		NetValue:    value,
		Status:      asaasPaymentStatus(c.Status),
		DueDate:     formatDate(c.DueDate),
		Deleted:     false,
		Anticipated: false,
	}
	if paymentDateApplies(c.Status) {
		d := formatDate(c.UpdatedAt)
		resp.PaymentDate = &d
	}
	return resp
}
