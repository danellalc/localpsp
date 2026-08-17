package engine

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestListCustomersEmptyEngineReturnsEmptySlice(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	customers, err := e.ListCustomers(ctx)
	if err != nil {
		t.Fatalf("ListCustomers() error = %v", err)
	}
	if customers == nil {
		t.Fatal("ListCustomers() = nil, want an empty, non-nil slice")
	}
	if len(customers) != 0 {
		t.Fatalf("ListCustomers() = %+v, want empty", customers)
	}
}

func TestListChargesEmptyEngineReturnsEmptySlice(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	charges, err := e.ListCharges(ctx)
	if err != nil {
		t.Fatalf("ListCharges() error = %v", err)
	}
	if charges == nil {
		t.Fatal("ListCharges() = nil, want an empty, non-nil slice")
	}
	if len(charges) != 0 {
		t.Fatalf("ListCharges() = %+v, want empty", charges)
	}
}

func TestListSubscriptionsEmptyEngineReturnsEmptySlice(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	subs, err := e.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListSubscriptions() error = %v", err)
	}
	if subs == nil {
		t.Fatal("ListSubscriptions() = nil, want an empty, non-nil slice")
	}
	if len(subs) != 0 {
		t.Fatalf("ListSubscriptions() = %+v, want empty", subs)
	}
}

func TestListCustomersReturnsEveryoneInIDOrder(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 5, start)

	names := []string{"Ana", "Bruno", "Carla", "Duda", "Elias"}
	created := make(map[string]bool, len(names))
	for _, name := range names {
		cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: name, Email: name + "@example.com"})
		if err != nil {
			t.Fatalf("CreateCustomer(%s) error = %v", name, err)
		}
		created[cust.ID] = true
	}

	got, err := e.ListCustomers(ctx)
	if err != nil {
		t.Fatalf("ListCustomers() error = %v", err)
	}
	if len(got) != len(names) {
		t.Fatalf("len(ListCustomers()) = %d, want %d", len(got), len(names))
	}

	ids := make([]string, len(got))
	for i, c := range got {
		ids[i] = c.ID
		if !created[c.ID] {
			t.Errorf("ListCustomers() returned unexpected id %q", c.ID)
		}
		delete(created, c.ID)
	}
	if len(created) != 0 {
		t.Errorf("ListCustomers() missing ids: %v", created)
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("ListCustomers() ids = %v, want sorted ascending", ids)
	}
}

func TestListChargesReturnsEveryoneInIDOrder(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 9, start)

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Fatima", Email: "fatima@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	const chargeCount = 5
	created := make(map[string]bool, chargeCount)
	for i := 0; i < chargeCount; i++ {
		charge, err := e.CreateCharge(ctx, CreateChargeInput{
			CustomerID:  cust.ID,
			BillingType: BillingTypePix,
			Amount:      int64(1000 + i),
			DueDate:     start.Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateCharge() error = %v", err)
		}
		created[charge.ID] = true
	}

	got, err := e.ListCharges(ctx)
	if err != nil {
		t.Fatalf("ListCharges() error = %v", err)
	}
	if len(got) != chargeCount {
		t.Fatalf("len(ListCharges()) = %d, want %d", len(got), chargeCount)
	}

	ids := make([]string, len(got))
	for i, c := range got {
		ids[i] = c.ID
		if !created[c.ID] {
			t.Errorf("ListCharges() returned unexpected id %q", c.ID)
		}
		delete(created, c.ID)
	}
	if len(created) != 0 {
		t.Errorf("ListCharges() missing ids: %v", created)
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("ListCharges() ids = %v, want sorted ascending", ids)
	}
}

func TestListSubscriptionsReturnsEveryoneInIDOrder(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 13, start)

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Gil", Email: "gil@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	const subCount = 4
	created := make(map[string]bool, subCount)
	for i := 0; i < subCount; i++ {
		sub, err := e.CreateSubscription(ctx, CreateSubscriptionInput{
			CustomerID:  cust.ID,
			BillingType: BillingTypeCreditCard,
			Interval:    IntervalMonthly,
			Amount:      int64(2000 + i),
			NextDueDate: start.Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateSubscription() error = %v", err)
		}
		created[sub.ID] = true
	}

	got, err := e.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListSubscriptions() error = %v", err)
	}
	if len(got) != subCount {
		t.Fatalf("len(ListSubscriptions()) = %d, want %d", len(got), subCount)
	}

	ids := make([]string, len(got))
	for i, s := range got {
		ids[i] = s.ID
		if !created[s.ID] {
			t.Errorf("ListSubscriptions() returned unexpected id %q", s.ID)
		}
		delete(created, s.ID)
	}
	if len(created) != 0 {
		t.Errorf("ListSubscriptions() missing ids: %v", created)
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("ListSubscriptions() ids = %v, want sorted ascending", ids)
	}
}
