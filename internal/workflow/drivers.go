package workflow

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

// ErrDriverNotImplemented is returned for provider types without a driver yet.
var ErrDriverNotImplemented = errors.New("driver not implemented")

// ProviderLookup finds registered providers; *store.Store implements it.
type ProviderLookup interface {
	GetProvider(ctx context.Context, id string) (model.Provider, error)
}

// RegisteredDrivers resolves drivers for providers registered in the database.
// OpenStack and Proxmox drivers are added in Phase 1; until then only "fake"
// providers can be synced.
type RegisteredDrivers struct {
	Providers ProviderLookup
}

// Driver implements DriverResolver.
func (r RegisteredDrivers) Driver(ctx context.Context, providerID string) (contract.CloudDriver, error) {
	p, err := r.Providers.GetProvider(ctx, providerID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, providerID)
	}
	if err != nil {
		return nil, err
	}
	switch p.Type {
	case contract.ProviderFake:
		// Deterministic per provider so repeated syncs see the same cloud.
		return fake.New(fake.Config{Seed: seedFor(providerID)}), nil
	default:
		return nil, fmt.Errorf("%w for provider type %q", ErrDriverNotImplemented, p.Type)
	}
}

func seedFor(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64() | 1
}
