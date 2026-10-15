// Package requestid carries SEMAR's correlation ID through a context, from
// the HTTP request or workflow that started the work down to provider calls
// (roadmap §3.10).
package requestid

import (
	"context"
	"encoding/hex"
	"strings"
)

type key struct{}

// With returns ctx carrying id.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// From returns the ID in ctx, or "".
func From(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}

// OpenStack formats id as an X-OpenStack-Request-ID ("req-<uuid>"), which
// OpenStack services accept from clients and record in their logs and in
// os-instance-actions. IDs that are not 32 hex digits yield "".
func OpenStack(id string) string {
	h := strings.ReplaceAll(strings.ToLower(id), "-", "")
	if len(h) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(h); err != nil {
		return ""
	}
	return "req-" + h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
