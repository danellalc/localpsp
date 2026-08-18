package engine

import (
	"fmt"
	"time"
)

// BillingType is how a charge is meant to be paid.
type BillingType string

// The billing types a charge can have. PIX and boleto are payment method
// names shared across every Brazilian provider, not Asaas-specific
// vocabulary; there is no more neutral term to use here.
// Undefined lets the payer pick a billing type later (at checkout, once
// localpsp simulates one); it's a real, confirmed value, not an invented
// one, see FIDELITY.md.
const (
	BillingTypePix        BillingType = "PIX"
	BillingTypeBoleto     BillingType = "BOLETO"
	BillingTypeCreditCard BillingType = "CREDIT_CARD"
	BillingTypeUndefined  BillingType = "UNDEFINED"
)

// Valid reports whether b is one of the known billing types.
func (b BillingType) Valid() bool {
	switch b {
	case BillingTypePix, BillingTypeBoleto, BillingTypeCreditCard, BillingTypeUndefined:
		return true
	default:
		return false
	}
}

// Status is a charge's position in its lifecycle.
type Status string

// The states a charge can be in. These are the project's own vocabulary,
// not any provider's exact wording.
const (
	StatusCreated   Status = "created"
	StatusConfirmed Status = "confirmed"
	StatusReceived  Status = "received"
	StatusOverdue   Status = "overdue"
	StatusRefunded  Status = "refunded"
)

// chargeTransitions lists, for each status, the statuses a charge may move
// to next. Anything not listed here is rejected.
//
// created is the only status that can become overdue: once a charge has
// been confirmed or received, a due date passing no longer means anything.
// overdue can still resolve to confirmed or received, mirroring a late
// payment arriving after the due date. refunded is terminal.
var chargeTransitions = map[Status][]Status{
	StatusCreated:   {StatusConfirmed, StatusReceived, StatusOverdue},
	StatusConfirmed: {StatusReceived, StatusRefunded},
	StatusReceived:  {StatusRefunded},
	StatusOverdue:   {StatusConfirmed, StatusReceived},
	StatusRefunded:  {},
}

// Charge represents a PIX, boleto or credit card charge.
type Charge struct {
	ID          string
	CustomerID  string
	BillingType BillingType
	Amount      int64
	DueDate     time.Time
	Status      Status
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CanTransition reports whether the charge may move to status to from its
// current status.
func (c *Charge) CanTransition(to Status) bool {
	for _, allowed := range chargeTransitions[c.Status] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Transition moves the charge to status to, stamping UpdatedAt with at.
// It returns a wrapped ErrInvalidTransition, leaving the charge unchanged,
// if the move is not allowed from the current status.
func (c *Charge) Transition(to Status, at time.Time) error {
	if !c.CanTransition(to) {
		return fmt.Errorf("charge %s: %w: %s to %s", c.ID, ErrInvalidTransition, c.Status, to)
	}
	c.Status = to
	c.UpdatedAt = at
	return nil
}
