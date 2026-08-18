package engine

import (
	"errors"
	"testing"
	"time"
)

func TestChargeTransition(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		from    Status
		to      Status
		wantErr bool
	}{
		{"created to confirmed", StatusCreated, StatusConfirmed, false},
		{"created to received", StatusCreated, StatusReceived, false},
		{"created to overdue", StatusCreated, StatusOverdue, false},
		{"created to refunded", StatusCreated, StatusRefunded, true},
		{"confirmed to received", StatusConfirmed, StatusReceived, false},
		{"confirmed to refunded", StatusConfirmed, StatusRefunded, false},
		{"confirmed to overdue", StatusConfirmed, StatusOverdue, true},
		{"confirmed to created", StatusConfirmed, StatusCreated, true},
		{"received to refunded", StatusReceived, StatusRefunded, false},
		{"received to confirmed", StatusReceived, StatusConfirmed, true},
		{"received to overdue", StatusReceived, StatusOverdue, true},
		{"overdue to confirmed", StatusOverdue, StatusConfirmed, false},
		{"overdue to received", StatusOverdue, StatusReceived, false},
		{"overdue to refunded", StatusOverdue, StatusRefunded, true},
		{"refunded to confirmed", StatusRefunded, StatusConfirmed, true},
		{"refunded to received", StatusRefunded, StatusReceived, true},
		{"same status is not a transition", StatusCreated, StatusCreated, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Charge{ID: "pay_test", Status: tt.from}

			err := c.Transition(tt.to, at)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Transition(%s -> %s) = nil, want error", tt.from, tt.to)
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("Transition(%s -> %s) error = %v, want wrapping ErrInvalidTransition", tt.from, tt.to, err)
				}
				if c.Status != tt.from {
					t.Fatalf("status changed on rejected transition: got %s, want %s", c.Status, tt.from)
				}
				return
			}

			if err != nil {
				t.Fatalf("Transition(%s -> %s) error = %v, want nil", tt.from, tt.to, err)
			}
			if c.Status != tt.to {
				t.Fatalf("status = %s, want %s", c.Status, tt.to)
			}
			if !c.UpdatedAt.Equal(at) {
				t.Fatalf("UpdatedAt = %v, want %v", c.UpdatedAt, at)
			}
		})
	}
}

func TestBillingTypeValid(t *testing.T) {
	tests := []struct {
		name string
		bt   BillingType
		want bool
	}{
		{"pix", BillingTypePix, true},
		{"boleto", BillingTypeBoleto, true},
		{"credit card", BillingTypeCreditCard, true},
		{"undefined", BillingTypeUndefined, true},
		{"unknown", BillingType("DEBIT_CARD"), false},
		{"empty", BillingType(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.bt.Valid(); got != tt.want {
				t.Fatalf("BillingType(%q).Valid() = %v, want %v", tt.bt, got, tt.want)
			}
		})
	}
}
