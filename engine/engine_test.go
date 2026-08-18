package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestEngine(t *testing.T, seed int64, start time.Time) *Engine {
	t.Helper()
	e, err := New(context.Background(), Options{Seed: seed, StartTime: start})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return e
}

func TestChargeGoesOverdueWhenClockPassesDueDate(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 42, start)

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Ana", Email: "ana@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	charge, err := e.CreateCharge(ctx, CreateChargeInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypePix,
		Amount:      1000,
		DueDate:     start.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateCharge() error = %v", err)
	}
	if charge.Status != StatusCreated {
		t.Fatalf("initial status = %s, want %s", charge.Status, StatusCreated)
	}

	transitions, err := e.AdvanceClock(ctx, 72*time.Hour)
	if err != nil {
		t.Fatalf("AdvanceClock() error = %v", err)
	}

	if len(transitions) != 1 {
		t.Fatalf("len(transitions) = %d, want 1: %+v", len(transitions), transitions)
	}
	got := transitions[0]
	if got.ChargeID != charge.ID || got.From != StatusCreated || got.To != StatusOverdue {
		t.Fatalf("transition = %+v, want overdue transition for %s", got, charge.ID)
	}
	if !got.At.Equal(start.Add(72 * time.Hour)) {
		t.Fatalf("transition.At = %v, want %v", got.At, start.Add(72*time.Hour))
	}
	// Charge must reflect the transition's own resulting state, callers
	// (like the facade firing a webhook) rely on this instead of a fresh
	// GetCharge, which could race a second, concurrent transition on the
	// same charge and report the wrong status.
	if got.Charge == nil {
		t.Fatal("transition.Charge = nil, want the resulting charge")
	}
	if got.Charge.Status != StatusOverdue {
		t.Errorf("transition.Charge.Status = %s, want %s", got.Charge.Status, StatusOverdue)
	}
	if got.Charge.ID != charge.ID {
		t.Errorf("transition.Charge.ID = %s, want %s", got.Charge.ID, charge.ID)
	}

	stored, err := e.GetCharge(ctx, charge.ID)
	if err != nil {
		t.Fatalf("GetCharge() error = %v", err)
	}
	if stored.Status != StatusOverdue {
		t.Fatalf("stored status = %s, want %s", stored.Status, StatusOverdue)
	}
}

func TestAdvanceClockLeavesConfirmedChargesAlone(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 7, start)

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Bea", Email: "bea@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	charge, err := e.CreateCharge(ctx, CreateChargeInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypeBoleto,
		Amount:      2500,
		DueDate:     start.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateCharge() error = %v", err)
	}

	if _, err := e.TransitionCharge(ctx, charge.ID, StatusConfirmed); err != nil {
		t.Fatalf("TransitionCharge() error = %v", err)
	}

	transitions, err := e.AdvanceClock(ctx, 72*time.Hour)
	if err != nil {
		t.Fatalf("AdvanceClock() error = %v", err)
	}
	if len(transitions) != 0 {
		t.Fatalf("transitions = %+v, want none for an already confirmed charge", transitions)
	}

	stored, err := e.GetCharge(ctx, charge.ID)
	if err != nil {
		t.Fatalf("GetCharge() error = %v", err)
	}
	if stored.Status != StatusConfirmed {
		t.Fatalf("status = %s, want %s", stored.Status, StatusConfirmed)
	}
}

// TestConcurrentTransitionAndAdvanceClockNeverLoseAConfirmation guards
// against a real race that was caught in review: TransitionCharge and
// AdvanceClock both did a read-check-write against the store with nothing
// serializing them, so a charge confirmed by one goroutine could be
// silently stamped back to overdue by an AdvanceClock that read the old
// status first. Confirmed is reachable from both created and overdue, so
// no matter which goroutine the engine's lock lets run first, the charge
// must end up confirmed, never overdue.
func TestConcurrentTransitionAndAdvanceClockNeverLoseAConfirmation(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t, 55, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Gil", Email: "gil@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	const iterations = 200
	for i := 0; i < iterations; i++ {
		charge, err := e.CreateCharge(ctx, CreateChargeInput{
			CustomerID:  cust.ID,
			BillingType: BillingTypePix,
			Amount:      1000,
			DueDate:     e.Now().Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateCharge() error = %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := e.TransitionCharge(ctx, charge.ID, StatusConfirmed); err != nil {
				t.Errorf("TransitionCharge() error = %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := e.AdvanceClock(ctx, 48*time.Hour); err != nil {
				t.Errorf("AdvanceClock() error = %v", err)
			}
		}()
		wg.Wait()

		stored, err := e.GetCharge(ctx, charge.ID)
		if err != nil {
			t.Fatalf("GetCharge() error = %v", err)
		}
		if stored.Status != StatusConfirmed {
			t.Fatalf("iteration %d: status = %s, want %s (a confirmation must never be lost to a concurrent AdvanceClock)",
				i, stored.Status, StatusConfirmed)
		}
	}
}

func TestCreateChargeRejectsUnknownCustomer(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 1, start)

	_, err := e.CreateCharge(ctx, CreateChargeInput{
		CustomerID:  "cus_does_not_exist",
		BillingType: BillingTypePix,
		Amount:      100,
		DueDate:     start.Add(24 * time.Hour),
	})
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("CreateCharge() error = %v, want ErrCustomerNotFound", err)
	}
}

func TestCreateChargeRejectsInvalidBillingType(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 1, start)

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Cid", Email: "cid@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	_, err = e.CreateCharge(ctx, CreateChargeInput{
		CustomerID:  cust.ID,
		BillingType: BillingType("DEBIT_CARD"),
		Amount:      100,
		DueDate:     start.Add(24 * time.Hour),
	})
	if !errors.Is(err, ErrInvalidBillingType) {
		t.Fatalf("CreateCharge() error = %v, want ErrInvalidBillingType", err)
	}
}

func TestCreateChargeRejectsNonPositiveAmount(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := newTestEngine(t, 1, start)

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Dan", Email: "dan@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	_, err = e.CreateCharge(ctx, CreateChargeInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypePix,
		Amount:      0,
		DueDate:     start.Add(24 * time.Hour),
	})
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("CreateCharge() error = %v, want ErrInvalidAmount", err)
	}
}

func TestIDsAreDeterministicForASeed(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	e1 := newTestEngine(t, 99, start)
	e2 := newTestEngine(t, 99, start)

	c1, err := e1.CreateCustomer(ctx, CreateCustomerInput{Name: "Eva", Email: "eva@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	c2, err := e2.CreateCustomer(ctx, CreateCustomerInput{Name: "Eva", Email: "eva@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	if c1.ID != c2.ID {
		t.Fatalf("same seed produced different customer ids: %s vs %s", c1.ID, c2.ID)
	}
}

// TestNextIDAndNextTokenAreDeterministic guards the same promise
// TestIDsAreDeterministicForASeed makes for entity ids, but for the two
// methods a provider facade uses to mint its own ids (webhook ids, event
// ids, auto-generated auth tokens): they share the engine's seeded
// counter, so the same seed and call sequence must produce the same
// output here too.
func TestNextIDAndNextTokenAreDeterministic(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e1 := newTestEngine(t, 55, start)
	e2 := newTestEngine(t, 55, start)

	if id1, id2 := e1.NextID("evt_"), e2.NextID("evt_"); id1 != id2 {
		t.Fatalf("NextID() = %q vs %q, want identical for the same seed", id1, id2)
	}
	if tok1, tok2 := e1.NextToken(), e2.NextToken(); tok1 != tok2 {
		t.Fatalf("NextToken() = %q vs %q, want identical for the same seed", tok1, tok2)
	}

	if id, prefix := e1.NextID("wh_"), "wh_"; len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		t.Errorf("NextID(%q) = %q, want it to start with the given prefix", prefix, id)
	}
}

// scenarioSnapshot is the full outcome of runScenario, serialized for a
// byte for byte comparison across runs.
type scenarioSnapshot struct {
	Customer     *Customer
	Charge       *Charge
	Subscription *Subscription
	Transitions  []Transition
}

func runScenario(t *testing.T, seed int64, start time.Time) string {
	t.Helper()
	ctx := context.Background()

	e, err := New(ctx, Options{Seed: seed, StartTime: start})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if err := e.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	cust, err := e.CreateCustomer(ctx, CreateCustomerInput{Name: "Fatima", Email: "fatima@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	charge, err := e.CreateCharge(ctx, CreateChargeInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypeBoleto,
		Amount:      5000,
		DueDate:     start.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateCharge() error = %v", err)
	}

	sub, err := e.CreateSubscription(ctx, CreateSubscriptionInput{
		CustomerID:  cust.ID,
		BillingType: BillingTypeCreditCard,
		Interval:    IntervalMonthly,
	})
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}

	transitions, err := e.AdvanceClock(ctx, 72*time.Hour)
	if err != nil {
		t.Fatalf("AdvanceClock() error = %v", err)
	}

	finalCharge, err := e.GetCharge(ctx, charge.ID)
	if err != nil {
		t.Fatalf("GetCharge() error = %v", err)
	}

	snapshot := scenarioSnapshot{
		Customer:     cust,
		Charge:       finalCharge,
		Subscription: sub,
		Transitions:  transitions,
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(encoded)
}

// TestDeterminismAcross100Runs is the load-bearing test for the project's
// determinism claim. The same seed, run through the same scenario 100
// times, must produce byte-identical ids, timestamps and transitions
// every time.
func TestDeterminismAcross100Runs(t *testing.T) {
	const runs = 100
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	first := runScenario(t, 777, start)
	if first == "" {
		t.Fatal("first run produced an empty snapshot")
	}

	for i := 1; i < runs; i++ {
		got := runScenario(t, 777, start)
		if got != first {
			t.Fatalf("run %d diverged from run 0:\nrun 0: %s\nrun %d: %s", i, first, i, got)
		}
	}
}

func TestDeterminismDiffersAcrossSeeds(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := runScenario(t, 1, start)
	b := runScenario(t, 2, start)

	if a == b {
		t.Fatal("different seeds produced identical snapshots")
	}
}
