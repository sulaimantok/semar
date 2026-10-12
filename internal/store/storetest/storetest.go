// Package storetest provides throwaway PostgreSQL databases for tests.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sulaimantok/semar/internal/store"
)

// EnvAdminURL names the variable holding a PostgreSQL URL whose user may
// create databases, e.g. postgres://postgres:postgres@localhost:5432/postgres.
const EnvAdminURL = "SEMAR_TEST_DATABASE_URL"

// New creates an empty, migrated database for one test and drops it when the
// test ends. Without EnvAdminURL the test is skipped locally and fails in CI,
// so database tests can never be silently skipped there.
func New(t testing.TB) *store.Store {
	t.Helper()
	dbURL := NewDatabase(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, dbURL); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

// NewDatabase creates an empty database (no migrations) and returns its URL.
func NewDatabase(t testing.TB) string {
	t.Helper()
	admin := os.Getenv(EnvAdminURL)
	if admin == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s must be set in CI", EnvAdminURL)
		}
		t.Skipf("%s not set; skipping database test", EnvAdminURL)
	}

	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	name := "semar_test_" + hex.EncodeToString(b[:])

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect to %s: %v", EnvAdminURL, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			t.Errorf("drop %s: %v", name, err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
