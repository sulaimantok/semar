package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

func TestConsoleTokensAreSingleUseAndBound(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if _, err := st.EnsureBootstrapAdmin(ctx, "admin", "h"); err != nil {
		t.Fatal(err)
	}
	u, _ := st.UserByUsername(ctx, "admin")
	tok := store.ConsoleToken{TokenHash: []byte("t1"), UserID: u.ID, SessionHash: []byte("s1"),
		ProviderID: "pve-lab", ExternalID: "100", Kind: "vnc", ExpiresAt: time.Now().Add(30 * time.Second)}
	if err := st.CreateConsoleToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeConsoleToken(ctx, []byte("t1"), []byte("other-session")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token from another session: %v", err)
	}
	got, err := st.ConsumeConsoleToken(ctx, []byte("t1"), []byte("s1"))
	if err != nil || got.ExternalID != "100" || got.UserID != u.ID || got.Kind != "vnc" {
		t.Fatalf("consume = %+v, %v", got, err)
	}
	if _, err := st.ConsumeConsoleToken(ctx, []byte("t1"), []byte("s1")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second use: %v", err)
	}
	expired := tok
	expired.TokenHash, expired.ExpiresAt = []byte("t2"), time.Now().Add(-time.Second)
	if err := st.CreateConsoleToken(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeConsoleToken(ctx, []byte("t2"), []byte("s1")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
}
