package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGetCustomer(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Ana", Email: "ana@example.com", TaxID: "12345678901"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	got, err := e.GetCustomer(ctx, cust.ID)
	if err != nil {
		t.Fatalf("GetCustomer() error = %v", err)
	}
	if got.ID != cust.ID || got.Name != "Ana" || got.Email != "ana@example.com" || got.TaxID != "12345678901" {
		t.Errorf("GetCustomer() = %+v, want a match for %+v", got, cust)
	}
}

func TestGetCustomerNotFound(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	_, err := e.GetCustomer(ctx, "cus_does_not_exist")
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("GetCustomer() error = %v, want ErrCustomerNotFound", err)
	}
}
