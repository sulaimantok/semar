package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/model"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	a, err := New(model.Provider{ID: "fake-a", Type: contract.ProviderFake})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := New(model.Provider{ID: "fake-a", Type: contract.ProviderFake})
	c, _ := New(model.Provider{ID: "fake-b", Type: contract.ProviderFake})
	ia, _ := a.Validate(ctx)
	ib, _ := b.Validate(ctx)
	ic, _ := c.Validate(ctx)
	if ia.Fingerprint == "" || ia.Fingerprint != ib.Fingerprint || ia.Fingerprint == ic.Fingerprint {
		t.Fatalf("fingerprints a=%q b=%q c=%q: want stable per provider and distinct across providers", ia.Fingerprint, ib.Fingerprint, ic.Fingerprint)
	}
	for _, typ := range []contract.ProviderType{contract.ProviderOpenStack, contract.ProviderProxmox} {
		if _, err := New(model.Provider{ID: "x", Type: typ}); !errors.Is(err, ErrNotImplemented) {
			t.Fatalf("%s: %v, want ErrNotImplemented", typ, err)
		}
	}
}
