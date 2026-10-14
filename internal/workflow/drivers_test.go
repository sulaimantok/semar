package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

type providerMap map[string]model.Provider

func (m providerMap) GetProvider(_ context.Context, id string) (model.Provider, error) {
	p, ok := m[id]
	if !ok {
		return model.Provider{}, store.ErrNotFound
	}
	return p, nil
}

func TestRegisteredDrivers(t *testing.T) {
	r := RegisteredDrivers{Providers: providerMap{
		"fake-a": {ID: "fake-a", Type: contract.ProviderFake},
		"os-jkt": {ID: "os-jkt", Type: contract.ProviderOpenStack},
	}}
	ctx := context.Background()
	d1, err := r.Driver(ctx, "fake-a")
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := r.Driver(ctx, "fake-a")
	v1, _ := d1.ListVMs(ctx)
	v2, _ := d2.ListVMs(ctx)
	if len(v1) == 0 || v1[0].ExternalID != v2[0].ExternalID || v1[0].Name != v2[0].Name {
		t.Fatal("fake provider inventory is not stable across syncs")
	}
	if _, err := r.Driver(ctx, "os-jkt"); !errors.Is(err, ErrDriverNotImplemented) {
		t.Fatalf("openstack: %v, want ErrDriverNotImplemented", err)
	}
	if _, err := r.Driver(ctx, "missing"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("missing: %v, want ErrUnknownProvider", err)
	}
}
