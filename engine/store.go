package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// timeLayout is a fixed width, UTC only timestamp format. Unlike
// time.RFC3339Nano, it never trims trailing zeros, so timestamps stored as
// text still compare correctly with plain string ordering (used by
// listChargesDue).
const timeLayout = "2006-01-02T15:04:05.000000000Z"

const schema = `
CREATE TABLE IF NOT EXISTS customers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL,
	tax_id TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS charges (
	id TEXT PRIMARY KEY,
	customer_id TEXT NOT NULL,
	billing_type TEXT NOT NULL,
	amount INTEGER NOT NULL,
	due_date TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
	id TEXT PRIMARY KEY,
	customer_id TEXT NOT NULL,
	billing_type TEXT NOT NULL,
	interval TEXT NOT NULL,
	amount INTEGER NOT NULL,
	next_due_date TEXT NOT NULL,
	created_at TEXT NOT NULL
);
`

// store is a thin SQLite backed persistence layer. It knows how to save
// and load the engine's entities; it has no lifecycle or id generation
// logic of its own.
type store struct {
	db *sql.DB
}

func openStore(ctx context.Context, dsn string) (*store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// A single connection keeps every caller talking to the same
	// in-memory database. Without this, database/sql may open a second
	// connection to ":memory:" that sees an empty, separate database.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) close() error {
	return s.db.Close()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(timeLayout, s)
}

func (s *store) insertCustomer(ctx context.Context, c *Customer) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO customers (id, name, email, tax_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Email, c.TaxID, formatTime(c.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert customer %s: %w", c.ID, err)
	}
	return nil
}

func (s *store) getCustomer(ctx context.Context, id string) (*Customer, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, tax_id, created_at FROM customers WHERE id = ?`, id,
	)
	c := &Customer{}
	var createdAt string
	if err := row.Scan(&c.ID, &c.Name, &c.Email, &c.TaxID, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("customer %s: %w", id, ErrCustomerNotFound)
		}
		return nil, fmt.Errorf("get customer %s: %w", id, err)
	}
	var err error
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse customer %s created at: %w", c.ID, err)
	}
	return c, nil
}

func (s *store) insertCharge(ctx context.Context, c *Charge) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO charges (id, customer_id, billing_type, amount, due_date, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.CustomerID, string(c.BillingType), c.Amount,
		formatTime(c.DueDate), string(c.Status), formatTime(c.CreatedAt), formatTime(c.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert charge %s: %w", c.ID, err)
	}
	return nil
}

func (s *store) getCharge(ctx context.Context, id string) (*Charge, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, billing_type, amount, due_date, status, created_at, updated_at
		 FROM charges WHERE id = ?`, id,
	)
	c, err := scanCharge(row)
	if err != nil {
		if errors.Is(err, ErrChargeNotFound) {
			return nil, fmt.Errorf("charge %s: %w", id, ErrChargeNotFound)
		}
		return nil, err
	}
	return c, nil
}

// execer is satisfied by both *sql.DB and *sql.Tx, so a single update
// helper works whether it runs standalone or as part of a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func updateChargeStatusExec(ctx context.Context, ex execer, id string, status Status, updatedAt time.Time) error {
	res, err := ex.ExecContext(ctx,
		`UPDATE charges SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), formatTime(updatedAt), id,
	)
	if err != nil {
		return fmt.Errorf("update charge %s: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update charge %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("charge %s: %w", id, ErrChargeNotFound)
	}
	return nil
}

func (s *store) updateChargeStatus(ctx context.Context, id string, status Status, updatedAt time.Time) error {
	return updateChargeStatusExec(ctx, s.db, id, status, updatedAt)
}

// chargeStatusUpdate is one entry in a batch passed to updateChargeStatuses.
type chargeStatusUpdate struct {
	id        string
	status    Status
	updatedAt time.Time
}

// updateChargeStatuses applies every update in a single transaction: either
// all of them land or none do. AdvanceClock relies on this so a mid batch
// failure never leaves some charges flipped to overdue in the database
// while the caller is told, via a nil result, that nothing happened.
func (s *store) updateChargeStatuses(ctx context.Context, updates []chargeStatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin charge status update transaction: %w", err)
	}

	for _, u := range updates {
		if err := updateChargeStatusExec(ctx, tx, u.id, u.status, u.updatedAt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit charge status updates: %w", err)
	}
	return nil
}

// listChargesDue returns every charge in the given status whose due date is
// at or before asOf, ordered by id so the result is stable across runs.
func (s *store) listChargesDue(ctx context.Context, status Status, asOf time.Time) ([]*Charge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, customer_id, billing_type, amount, due_date, status, created_at, updated_at
		 FROM charges WHERE status = ? AND due_date <= ? ORDER BY id ASC`,
		string(status), formatTime(asOf),
	)
	if err != nil {
		return nil, fmt.Errorf("list due charges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var charges []*Charge
	for rows.Next() {
		c, err := scanCharge(rows)
		if err != nil {
			return nil, err
		}
		charges = append(charges, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due charges: %w", err)
	}
	return charges, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCharge(row rowScanner) (*Charge, error) {
	c := &Charge{}
	var billingType, status, dueDate, createdAt, updatedAt string

	err := row.Scan(&c.ID, &c.CustomerID, &billingType, &c.Amount, &dueDate, &status, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrChargeNotFound)
		}
		return nil, fmt.Errorf("scan charge: %w", err)
	}

	c.BillingType = BillingType(billingType)
	c.Status = Status(status)
	if c.DueDate, err = parseTime(dueDate); err != nil {
		return nil, fmt.Errorf("parse charge %s due date: %w", c.ID, err)
	}
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse charge %s created at: %w", c.ID, err)
	}
	if c.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parse charge %s updated at: %w", c.ID, err)
	}
	return c, nil
}

func (s *store) insertSubscription(ctx context.Context, sub *Subscription) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subscriptions (id, customer_id, billing_type, interval, amount, next_due_date, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.CustomerID, string(sub.BillingType), string(sub.Interval),
		sub.Amount, formatTime(sub.NextDueDate), formatTime(sub.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert subscription %s: %w", sub.ID, err)
	}
	return nil
}

func (s *store) getSubscription(ctx context.Context, id string) (*Subscription, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, customer_id, billing_type, interval, amount, next_due_date, created_at
		 FROM subscriptions WHERE id = ?`, id,
	)
	sub := &Subscription{}
	var billingType, interval, nextDueDate, createdAt string
	err := row.Scan(&sub.ID, &sub.CustomerID, &billingType, &interval, &sub.Amount, &nextDueDate, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("subscription %s: %w", id, ErrSubscriptionNotFound)
		}
		return nil, fmt.Errorf("get subscription %s: %w", id, err)
	}
	sub.BillingType = BillingType(billingType)
	sub.Interval = Interval(interval)
	if sub.NextDueDate, err = parseTime(nextDueDate); err != nil {
		return nil, fmt.Errorf("parse subscription %s next due date: %w", sub.ID, err)
	}
	if sub.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse subscription %s created at: %w", sub.ID, err)
	}
	return sub, nil
}
