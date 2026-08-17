// Package engine owns the emulator's truth: entities, their lifecycle
// state machines, the virtual clock and seeded id generation. It knows
// nothing about any payment provider: no JSON tags, no HTTP, no
// provider-specific field or event names. Provider facades sit on top and
// translate.
package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/danellalc/localpsp/clock"
)

// Options configures a new Engine.
type Options struct {
	// Seed drives every generated id. The same seed, given the same
	// sequence of calls, always produces the same ids.
	Seed int64
	// StartTime is where the virtual clock begins.
	StartTime time.Time
	// DSN is the SQLite data source name. Empty defaults to ":memory:".
	DSN string
}

// Engine is the emulator's core: it owns entities, their state and the
// virtual clock they move against. Every exported method takes mu, so an
// Engine is safe to call from multiple goroutines: no two calls ever
// interleave their read-check-write sequence against the store or the
// seeded id counter.
type Engine struct {
	mu    sync.Mutex
	clock *clock.Clock
	store *store
	ids   *idGenerator
}

// New builds an Engine backed by a fresh SQLite database at opts.DSN.
func New(ctx context.Context, opts Options) (*Engine, error) {
	dsn := opts.DSN
	if dsn == "" {
		dsn = ":memory:"
	}

	st, err := openStore(ctx, dsn)
	if err != nil {
		return nil, err
	}

	return &Engine{
		clock: clock.New(opts.StartTime),
		store: st,
		ids:   newIDGenerator(opts.Seed),
	}, nil
}

// Close releases the engine's storage. It does not stop the clock, which
// has no resources of its own to release.
func (e *Engine) Close() error {
	return e.store.close()
}

// Now returns the engine's current virtual time.
func (e *Engine) Now() time.Time {
	return e.clock.Now()
}

// NextID returns a new id deterministically derived from the engine's
// seed and internal call counter, prefixed with prefix. Exposed so a
// provider facade can mint its own ids (like a webhook or an outbound
// event) from the same deterministic source as the engine's own entities,
// instead of an unseeded random source that would break reproducibility.
func (e *Engine) NextID(prefix string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ids.next(prefix)
}

// NextToken returns a new opaque, deterministically derived hex string,
// long enough to stand in for an auto-generated secret (like Asaas's
// auto-generated webhook auth token) while keeping a run fully
// reproducible from its seed.
func (e *Engine) NextToken() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ids.nextHex(40)
}

// CreateCustomerInput is the input to CreateCustomer.
type CreateCustomerInput struct {
	Name  string
	Email string
	TaxID string
}

// CreateCustomer registers a new customer and assigns it a seeded id.
func (e *Engine) CreateCustomer(ctx context.Context, in CreateCustomerInput) (*Customer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	c := &Customer{
		ID:        e.ids.next("cus_"),
		Name:      in.Name,
		Email:     in.Email,
		TaxID:     in.TaxID,
		CreatedAt: e.clock.Now(),
	}
	if err := e.store.insertCustomer(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// GetCustomer looks up a customer by id.
func (e *Engine) GetCustomer(ctx context.Context, id string) (*Customer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.store.getCustomer(ctx, id)
}

// ListCustomers returns every customer, ordered by id.
func (e *Engine) ListCustomers(ctx context.Context) ([]*Customer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.store.listCustomers(ctx)
}

// CreateChargeInput is the input to CreateCharge.
type CreateChargeInput struct {
	CustomerID  string
	BillingType BillingType
	Amount      int64
	DueDate     time.Time
}

// CreateCharge creates a charge for an existing customer, in StatusCreated.
func (e *Engine) CreateCharge(ctx context.Context, in CreateChargeInput) (*Charge, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !in.BillingType.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidBillingType, in.BillingType)
	}
	if in.Amount <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidAmount, in.Amount)
	}
	if _, err := e.store.getCustomer(ctx, in.CustomerID); err != nil {
		return nil, err
	}

	now := e.clock.Now()
	c := &Charge{
		ID:          e.ids.next("pay_"),
		CustomerID:  in.CustomerID,
		BillingType: in.BillingType,
		Amount:      in.Amount,
		DueDate:     in.DueDate.UTC(),
		Status:      StatusCreated,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := e.store.insertCharge(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// GetCharge looks up a charge by id.
func (e *Engine) GetCharge(ctx context.Context, id string) (*Charge, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.store.getCharge(ctx, id)
}

// ListCharges returns every charge, ordered by id.
func (e *Engine) ListCharges(ctx context.Context) ([]*Charge, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.store.listCharges(ctx)
}

// TransitionCharge moves a charge to a new status, stamped with the
// engine's current virtual time. It rejects moves the state machine does
// not allow.
func (e *Engine) TransitionCharge(ctx context.Context, id string, to Status) (*Charge, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	c, err := e.store.getCharge(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.Transition(to, e.clock.Now()); err != nil {
		return nil, err
	}
	if err := e.store.updateChargeStatus(ctx, c.ID, c.Status, c.UpdatedAt); err != nil {
		return nil, err
	}
	return c, nil
}

// CreateSubscriptionInput is the input to CreateSubscription.
type CreateSubscriptionInput struct {
	CustomerID  string
	BillingType BillingType
	Interval    Interval
	Amount      int64
	NextDueDate time.Time
}

// CreateSubscription creates a recurring charge grouping for an existing
// customer.
func (e *Engine) CreateSubscription(ctx context.Context, in CreateSubscriptionInput) (*Subscription, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !in.BillingType.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidBillingType, in.BillingType)
	}
	if !in.Interval.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidInterval, in.Interval)
	}
	if _, err := e.store.getCustomer(ctx, in.CustomerID); err != nil {
		return nil, err
	}

	sub := &Subscription{
		ID:          e.ids.next("sub_"),
		CustomerID:  in.CustomerID,
		BillingType: in.BillingType,
		Interval:    in.Interval,
		Amount:      in.Amount,
		NextDueDate: in.NextDueDate.UTC(),
		CreatedAt:   e.clock.Now(),
	}
	if err := e.store.insertSubscription(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// GetSubscription looks up a subscription by id.
func (e *Engine) GetSubscription(ctx context.Context, id string) (*Subscription, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.store.getSubscription(ctx, id)
}

// ListSubscriptions returns every subscription, ordered by id.
func (e *Engine) ListSubscriptions(ctx context.Context) ([]*Subscription, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.store.listSubscriptions(ctx)
}

// Transition records a single charge status change caused by AdvanceClock.
// Charge is the charge exactly as it stood right after this transition,
// callers that need the resulting state (like firing a webhook) should
// use it instead of a fresh GetCharge: fetching again after AdvanceClock
// returns risks racing a second, concurrent transition on the same
// charge and reporting the wrong status.
type Transition struct {
	ChargeID string
	From     Status
	To       Status
	At       time.Time
	Charge   *Charge
}

// AdvanceClock moves the virtual clock forward by d and applies whatever
// transitions that causes: any charge still in StatusCreated whose due
// date has now passed becomes StatusOverdue. It returns every transition
// that happened, ordered by charge id, for a later phase (webhook
// dispatch) to consume.
//
// The affected charges are persisted in a single transaction: either every
// due charge flips to overdue and the full transition list comes back, or
// (on any storage error) none of them do and the caller gets nil, err. A
// partial batch is never possible, so a returned error always means the
// database still matches what AdvanceClock reports.
func (e *Engine) AdvanceClock(ctx context.Context, d time.Duration) ([]Transition, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.clock.Advance(d)

	due, err := e.store.listChargesDue(ctx, StatusCreated, now)
	if err != nil {
		return nil, err
	}

	transitions := make([]Transition, 0, len(due))
	updates := make([]chargeStatusUpdate, 0, len(due))
	for _, c := range due {
		from := c.Status
		if err := c.Transition(StatusOverdue, now); err != nil {
			return nil, err
		}
		updates = append(updates, chargeStatusUpdate{id: c.ID, status: c.Status, updatedAt: c.UpdatedAt})
		transitions = append(transitions, Transition{
			ChargeID: c.ID,
			From:     from,
			To:       c.Status,
			At:       now,
			Charge:   c,
		})
	}

	if err := e.store.updateChargeStatuses(ctx, updates); err != nil {
		return nil, err
	}
	return transitions, nil
}
