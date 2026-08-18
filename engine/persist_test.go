package engine

import (
	"context"
	"testing"
	"time"
)

// TestPersistsAcrossReopen guards the whole point of Options.DSN (and the
// CLI's --persist flag on top of it): a customer created against a
// file-backed database must still be there after closing that engine and
// opening a fresh one against the exact same file, the same way a real
// `localpsp serve --persist` restart would. This had no coverage before,
// :memory: (the default everywhere else in this package's tests) can
// never catch a bug here, its "file" never outlives the process.
func TestPersistsAcrossReopen(t *testing.T) {
	dsn := t.TempDir() + "/test.db"
	ctx := context.Background()

	e1, err := New(ctx, Options{Seed: 1, StartTime: time.Now(), DSN: dsn})
	if err != nil {
		t.Fatalf("New() first open error = %v", err)
	}
	cust, err := e1.CreateCustomer(ctx, CreateCustomerInput{Name: "Persist Test", TaxID: "111"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	if err := e1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	e2, err := New(ctx, Options{Seed: 1, StartTime: time.Now(), DSN: dsn})
	if err != nil {
		t.Fatalf("New() second open error = %v", err)
	}
	defer func() { _ = e2.Close() }()

	list, err := e2.ListCustomers(ctx)
	if err != nil {
		t.Fatalf("ListCustomers() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != cust.ID {
		t.Fatalf("customers after reopen = %+v, want exactly [%s]", list, cust.ID)
	}
}
