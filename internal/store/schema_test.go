package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

func TestSchemaVersionAndWait(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	applied, required, err := st.SchemaVersion(ctx)
	if err != nil || required < 4 || applied != required {
		t.Fatalf("migrated database: applied %d, required %d, %v", applied, required, err)
	}
	if err := st.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.WaitForSchema(ctx, time.Millisecond, nil); err != nil {
		t.Fatal(err)
	}

	// A database that was never migrated is behind; waiting ends with ctx.
	url := storetest.NewDatabase(t)
	empty, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(empty.Close)
	if err := empty.CheckSchema(ctx); !errors.Is(err, store.ErrSchemaBehind) {
		t.Fatalf("empty database: %v", err)
	}
	waits := 0
	wctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := empty.WaitForSchema(wctx, 5*time.Millisecond, func(a, r int64) { waits++ }); !errors.Is(err, context.DeadlineExceeded) || waits == 0 {
		t.Fatalf("wait on empty database: %v after %d checks", err, waits)
	}

	// Migrating while a waiter is blocked releases it.
	done := make(chan error, 1)
	go func() { done <- empty.WaitForSchema(ctx, 10*time.Millisecond, nil) }()
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter not released after migration")
	}
}
