package contract

import (
	"context"
	"errors"
)

// Sentinel errors drivers wrap so callers can classify failures without
// knowing the provider.
var (
	ErrUnauthorized = errors.New("provider: unauthorized")
	ErrNotFound     = errors.New("provider: not found")
	ErrUnavailable  = errors.New("provider: unavailable")
	ErrRateLimited  = errors.New("provider: rate limited")
)

// IsRetryable reports whether err is transient and the call may be retried.
func IsRetryable(err error) bool {
	return errors.Is(err, ErrUnavailable) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, context.DeadlineExceeded)
}
