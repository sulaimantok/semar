package requestid

import (
	"context"
	"testing"
)

func TestContextAndOpenStackFormat(t *testing.T) {
	ctx := With(context.Background(), "0123456789abcdef0123456789ABCDEF")
	if From(ctx) != "0123456789abcdef0123456789ABCDEF" || From(context.Background()) != "" {
		t.Fatal("round trip failed")
	}
	if got := OpenStack(From(ctx)); got != "req-01234567-89ab-cdef-0123-456789abcdef" {
		t.Fatalf("OpenStack = %q", got)
	}
	for _, bad := range []string{"", "client-chosen-id", "zz23456789abcdef0123456789abcdef"} {
		if got := OpenStack(bad); got != "" {
			t.Errorf("OpenStack(%q) = %q, want empty", bad, got)
		}
	}
}
