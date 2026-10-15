package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

// ErrDriverNotImplemented is returned for provider types without a driver yet.
var ErrDriverNotImplemented = registry.ErrNotImplemented

// ProviderLookup finds registered providers; *store.Store implements it.
type ProviderLookup interface {
	GetProvider(ctx context.Context, id string) (model.Provider, error)
}

// RegisteredDrivers resolves drivers for providers registered in the
// database. The provider is read on every call, so endpoint changes apply
// from the next sync.
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
	return registry.New(p)
}
