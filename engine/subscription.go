package engine

import "time"

// Interval is how often a subscription's charge repeats.
type Interval string

// The intervals a subscription can have. Generic billing vocabulary, not
// specific to any one provider, matches the full set of recurring cycles
// confirmed against Asaas's own subscription API (see FIDELITY.md).
const (
	IntervalWeekly       Interval = "WEEKLY"
	IntervalBiweekly     Interval = "BIWEEKLY"
	IntervalMonthly      Interval = "MONTHLY"
	IntervalBimonthly    Interval = "BIMONTHLY"
	IntervalQuarterly    Interval = "QUARTERLY"
	IntervalSemiannually Interval = "SEMIANNUALLY"
	IntervalYearly       Interval = "YEARLY"
)

// Valid reports whether i is one of the known intervals.
func (i Interval) Valid() bool {
	switch i {
	case IntervalWeekly, IntervalBiweekly, IntervalMonthly, IntervalBimonthly,
		IntervalQuarterly, IntervalSemiannually, IntervalYearly:
		return true
	default:
		return false
	}
}

// Subscription is a recurring charge grouping.
type Subscription struct {
	ID          string
	CustomerID  string
	BillingType BillingType
	Interval    Interval
	Amount      int64
	NextDueDate time.Time
	CreatedAt   time.Time
}
