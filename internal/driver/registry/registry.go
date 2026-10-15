// Package registry builds the driver for a registered provider. Both the
// worker (inventory sync) and the API (connection checks) use it, so a
// provider is always reached the same way.
package registry

import (
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
	"github.com/sulaimantok/semar/internal/model"
)

// ErrNotImplemented is returned for provider types without a driver yet.
var ErrNotImplemented = errors.New("driver not implemented")

// New returns the driver for p. OpenStack and Proxmox drivers are added in
// Phase 1 and will receive the provider's discovery credential here; until
// then only "fake" providers have a driver.
func New(p model.Provider) (contract.CloudDriver, error) {
	switch p.Type {
	case contract.ProviderFake:
		// Deterministic per provider so repeated syncs see the same cloud.
		return fake.New(fake.Config{Seed: SeedFor(p.ID)}), nil
	default:
		return nil, fmt.Errorf("%w for provider type %q", ErrNotImplemented, p.Type)
	}
}

// SeedFor derives a fake provider's inventory seed from its ID.
func SeedFor(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64() | 1
}
