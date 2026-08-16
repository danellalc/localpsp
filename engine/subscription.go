package engine

import "time"

// Interval is how often a subscription's charge repeats.
type Interval string

// The intervals a subscription can have. Generic billing vocabulary, not
// specific to any one provider.
const (
	IntervalWeekly    Interval = "WEEKLY"
	IntervalMonthly   Interval = "MONTHLY"
	IntervalQuarterly Interval = "QUARTERLY"
	IntervalYearly    Interval = "YEARLY"
)

// Valid reports whether i is one of the known intervals.
func (i Interval) Valid() bool {
	switch i {
	case IntervalWeekly, IntervalMonthly, IntervalQuarterly, IntervalYearly:
		return true
	default:
		return false
	}
}

// Subscription is a minimal recurring charge grouping. Richer fields
// (amount, next due date, cycle count) belong to a later phase, once the
// facade actually needs them.
type Subscription struct {
	ID          string
	CustomerID  string
	BillingType BillingType
	Interval    Interval
	CreatedAt   time.Time
}
