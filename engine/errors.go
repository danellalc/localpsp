package engine

import "errors"

// Sentinel errors returned by the engine. Wrap these with fmt.Errorf and
// %w so callers can match with errors.Is.
var (
	ErrCustomerNotFound     = errors.New("customer not found")
	ErrChargeNotFound       = errors.New("charge not found")
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrInvalidTransition    = errors.New("invalid charge status transition")
	ErrInvalidBillingType   = errors.New("invalid billing type")
	ErrInvalidInterval      = errors.New("invalid subscription interval")
	ErrInvalidAmount        = errors.New("charge amount must be positive")
)
