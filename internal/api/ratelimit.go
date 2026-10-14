package api

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

// LoginLimits bounds login attempts. Zero fields take the defaults.
type LoginLimits struct {
	// PerIP is the number of attempts one client address may make per IPWindow.
	PerIP    int
	IPWindow time.Duration
	// FailuresPerUser locks a username for UserWindow after this many
	// failed attempts; a successful login resets the count.
	FailuresPerUser int
	UserWindow      time.Duration
	// ConcurrentHashes bounds simultaneous password verifications. Each
	// argon2id verification allocates ~64 MiB, so an unbounded number of
	// concurrent logins could exhaust memory.
	ConcurrentHashes int
}

func (l LoginLimits) withDefaults() LoginLimits {
	if l.PerIP <= 0 {
		l.PerIP = 10
	}
	if l.IPWindow <= 0 {
		l.IPWindow = time.Minute
	}
	if l.FailuresPerUser <= 0 {
		l.FailuresPerUser = 5
	}
	if l.UserWindow <= 0 {
		l.UserWindow = 15 * time.Minute
	}
	if l.ConcurrentHashes <= 0 {
		l.ConcurrentHashes = 4
	}
	return l
}

// maxTrackedKeys bounds limiter memory; expired entries are swept when the
// map grows past it.
const maxTrackedKeys = 10_000

type rateWindow struct {
	start time.Time
	count int
}

// loginLimiter is an in-memory, per-process limiter. With several API
// replicas each replica enforces its own limits, which still bounds the
// total attempt rate.
type loginLimiter struct {
	limits LoginLimits
	now    func() time.Time
	slots  chan struct{}

	mu       sync.Mutex
	ips      map[string]*rateWindow
	failures map[string]*rateWindow
}

func newLoginLimiter(l LoginLimits) *loginLimiter {
	l = l.withDefaults()
	return &loginLimiter{
		limits:   l,
		now:      time.Now,
		slots:    make(chan struct{}, l.ConcurrentHashes),
		ips:      make(map[string]*rateWindow),
		failures: make(map[string]*rateWindow),
	}
}

// allowAttempt counts one attempt from ip for username and reports whether
// it may proceed to password verification.
func (l *loginLimiter) allowAttempt(ip, username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if f := current(l.failures, username, now, l.limits.UserWindow); f != nil && f.count >= l.limits.FailuresPerUser {
		return false
	}
	w := current(l.ips, ip, now, l.limits.IPWindow)
	if w == nil {
		w = &rateWindow{start: now}
		l.ips[ip] = w
		sweep(l.ips, now, l.limits.IPWindow)
	}
	if w.count >= l.limits.PerIP {
		return false
	}
	w.count++
	return true
}

// recordFailure counts a failed login for username.
func (l *loginLimiter) recordFailure(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	f := current(l.failures, username, now, l.limits.UserWindow)
	if f == nil {
		f = &rateWindow{start: now}
		l.failures[username] = f
		sweep(l.failures, now, l.limits.UserWindow)
	}
	f.count++
}

// recordSuccess clears username's failure count.
func (l *loginLimiter) recordSuccess(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, username)
}

// acquireHashSlot waits for a password-verification slot.
func (l *loginLimiter) acquireHashSlot(ctx context.Context) (release func(), err error) {
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// current returns the unexpired window for key, dropping an expired one.
func current(m map[string]*rateWindow, key string, now time.Time, window time.Duration) *rateWindow {
	w, ok := m[key]
	if !ok {
		return nil
	}
	if now.Sub(w.start) >= window {
		delete(m, key)
		return nil
	}
	return w
}

func sweep(m map[string]*rateWindow, now time.Time, window time.Duration) {
	if len(m) <= maxTrackedKeys {
		return
	}
	for k, w := range m {
		if now.Sub(w.start) >= window {
			delete(m, k)
		}
	}
}

type clientIPKey struct{}

// withClientIP records the TCP peer address for rate limiting. Forwarding
// headers are deliberately ignored: they are client-controlled unless a
// trusted proxy is configured, which SEMAR does not support yet.
func withClientIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
	})
}

func clientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}
