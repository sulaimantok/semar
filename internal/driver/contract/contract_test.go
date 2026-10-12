package contract

import (
	"context"
	"fmt"
	"testing"
)

func TestNormalizeArch(t *testing.T) {
	cases := map[string]Arch{
		"x86_64":   ArchAMD64,
		"AMD64":    ArchAMD64,
		" aarch64": ArchARM64,
		"arm64":    ArchARM64,
		"ppc64le":  ArchUnknown,
		"":         ArchUnknown,
	}
	for in, want := range cases {
		if got := NormalizeArch(in); got != want {
			t.Errorf("NormalizeArch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("list servers: %w", ErrUnavailable), true},
		{fmt.Errorf("list servers: %w", ErrRateLimited), true},
		{context.DeadlineExceeded, true},
		{fmt.Errorf("auth: %w", ErrUnauthorized), false},
		{ErrNotFound, false},
		{context.Canceled, false},
	}
	for _, c := range cases {
		if got := IsRetryable(c.err); got != c.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
