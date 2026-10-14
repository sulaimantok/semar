package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter(l LoginLimits) (*loginLimiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	lim := newLoginLimiter(l)
	lim.now = clock.now
	return lim, clock
}

func TestLimiterPerIP(t *testing.T) {
	lim, clock := newTestLimiter(LoginLimits{PerIP: 3, IPWindow: time.Minute})
	for i := 0; i < 3; i++ {
		if !lim.allowAttempt("192.0.2.1", "u") {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if lim.allowAttempt("192.0.2.1", "u") {
		t.Fatal("4th attempt in the window allowed")
	}
	if !lim.allowAttempt("192.0.2.2", "u") {
		t.Fatal("another address was limited")
	}
	clock.advance(time.Minute)
	if !lim.allowAttempt("192.0.2.1", "u") {
		t.Fatal("attempt after the window refused")
	}
}

func TestLimiterLocksUsernameAfterFailures(t *testing.T) {
	lim, clock := newTestLimiter(LoginLimits{PerIP: 100, FailuresPerUser: 2, UserWindow: 10 * time.Minute})
	for i := 0; i < 2; i++ {
		if !lim.allowAttempt("192.0.2.1", "admin") {
			t.Fatal("attempt refused before lockout")
		}
		lim.recordFailure("admin")
	}
	// Locked, from any address; other users are unaffected.
	if lim.allowAttempt("198.51.100.7", "admin") {
		t.Fatal("locked username allowed")
	}
	if !lim.allowAttempt("192.0.2.1", "bob") {
		t.Fatal("other username locked")
	}
	clock.advance(10 * time.Minute)
	if !lim.allowAttempt("192.0.2.1", "admin") {
		t.Fatal("lock did not expire")
	}
}

func TestLimiterSuccessResetsFailures(t *testing.T) {
	lim, _ := newTestLimiter(LoginLimits{PerIP: 100, FailuresPerUser: 2})
	lim.recordFailure("admin")
	lim.recordSuccess("admin")
	lim.recordFailure("admin")
	if !lim.allowAttempt("192.0.2.1", "admin") {
		t.Fatal("failure count was not reset by a successful login")
	}
}

func TestHashSlotsBoundConcurrency(t *testing.T) {
	lim, _ := newTestLimiter(LoginLimits{ConcurrentHashes: 1})
	release, err := lim.acquireHashSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := lim.acquireHashSlot(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second slot: %v, want DeadlineExceeded", err)
	}
	release()
	if r, err := lim.acquireHashSlot(context.Background()); err != nil {
		t.Fatal(err)
	} else {
		r()
	}
}

func TestLimiterSweepBoundsMemory(t *testing.T) {
	lim, clock := newTestLimiter(LoginLimits{PerIP: 1, IPWindow: time.Second})
	for i := 0; i < maxTrackedKeys; i++ {
		lim.allowAttempt(string(rune(i)), "u")
	}
	clock.advance(time.Second)
	lim.allowAttempt("fresh", "u")
	if n := len(lim.ips); n > 1 {
		t.Fatalf("%d entries tracked after sweep, want 1", n)
	}
}
