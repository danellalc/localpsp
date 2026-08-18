package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIntervalValid(t *testing.T) {
	tests := []struct {
		name string
		i    Interval
		want bool
	}{
		{"weekly", IntervalWeekly, true},
		{"biweekly", IntervalBiweekly, true},
		{"monthly", IntervalMonthly, true},
		{"bimonthly", IntervalBimonthly, true},
		{"quarterly", IntervalQuarterly, true},
		{"semiannually", IntervalSemiannually, true},
		{"yearly", IntervalYearly, true},
		{"unknown", Interval("DAILY"), false},
		{"empty", Interval(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.i.Valid(); got != tt.want {
				t.Fatalf("Interval(%q).Valid() = %v, want %v", tt.i, got, tt.want)
			}
		})
	}
}

func TestGetSubscription(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Bea", Email: "bea@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	sub, err := e.CreateSubscription(ctx, CreateSubscriptionInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypePix,
		Interval:    IntervalMonthly,
		Amount:      2500,
		NextDueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}

	got, err := e.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if got.ID != sub.ID || got.CustomerID != cust.ID || got.Interval != IntervalMonthly || got.Amount != 2500 {
		t.Errorf("GetSubscription() = %+v, want a match for %+v", got, sub)
	}
}

func TestGetSubscriptionNotFound(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	_, err := e.GetSubscription(ctx, "sub_does_not_exist")
	if !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("GetSubscription() error = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestCreateSubscriptionRejectsUnknownCustomer(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	_, err := e.CreateSubscription(ctx, CreateSubscriptionInput{
		CustomerID:  "cus_does_not_exist",
		BillingType: BillingTypePix,
		Interval:    IntervalMonthly,
		Amount:      1000,
		NextDueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("CreateSubscription() error = %v, want ErrCustomerNotFound", err)
	}
}

func TestCreateSubscriptionRejectsInvalidBillingType(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Cid", Email: "cid@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	_, err = e.CreateSubscription(ctx, CreateSubscriptionInput{
		CustomerID:  cust.ID,
		BillingType: BillingType("DEBIT_CARD"),
		Interval:    IntervalMonthly,
		Amount:      1000,
		NextDueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrInvalidBillingType) {
		t.Fatalf("CreateSubscription() error = %v, want ErrInvalidBillingType", err)
	}
}

// TestCreateSubscriptionRejectsNonPositiveAmount guards against a real
// gap caught in review: CreateCharge validates Amount > 0, but its
// sibling CreateSubscription didn't, so calling engine's public API
// directly (as a future provider adapter would) could silently persist a
// subscription with a zero or negative amount.
func TestCreateSubscriptionRejectsNonPositiveAmount(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Eva", Email: "eva@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	_, err = e.CreateSubscription(ctx, CreateSubscriptionInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypePix,
		Interval:    IntervalMonthly,
		Amount:      0,
		NextDueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("CreateSubscription() error = %v, want ErrInvalidAmount", err)
	}
}

func TestCreateSubscriptionRejectsInvalidInterval(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Dan", Email: "dan@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	_, err = e.CreateSubscription(ctx, CreateSubscriptionInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypePix,
		Interval:    Interval("DAILY"),
		Amount:      1000,
		NextDueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("CreateSubscription() error = %v, want ErrInvalidInterval", err)
	}
}
