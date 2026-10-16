package registry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/store"
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

// openerFunc adapts a function to CredentialOpener.
type openerFunc func(providerID string) (model.ProviderCredential, []byte, error)

func (f openerFunc) OpenCredential(_ context.Context, _ secret.Store, providerID string, purpose model.CredentialPurpose, scope string) (model.ProviderCredential, []byte, error) {
	if purpose != model.PurposeDiscovery || scope != "provider" {
		return model.ProviderCredential{}, nil, store.ErrNotFound
	}
	return f(providerID)
}

func TestResolver(t *testing.T) {
	ctx := context.Background()
	rotated := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	secrets := map[string]string{
		"os":  `{"application_credential_id":"id","application_credential_secret":"s"}`,
		"pve": `{"token_id":"semar@pve!discovery","token_secret":"s"}`,
		"bad": `{"token":"x"}`,
		"raw": `not json`,
	}
	r := &Resolver{Secrets: noSecrets{}, Credentials: openerFunc(func(id string) (model.ProviderCredential, []byte, error) {
		s, ok := secrets[id]
		if !ok {
			return model.ProviderCredential{}, nil, store.ErrNotFound
		}
		return model.ProviderCredential{ID: "cred-" + id, RotatedAt: rotated}, []byte(s), nil
	})}
	osP := model.Provider{ID: "os", Type: contract.ProviderOpenStack, Endpoints: []string{"https://keystone.example.com/v3"}, RegionName: "RegionOne"}
	pveP := model.Provider{ID: "pve", Type: contract.ProviderProxmox, Endpoints: []string{"https://pve1.example.com:8006"}}

	d1, err := r.Driver(ctx, osP)
	if err != nil || d1.Type() != contract.ProviderOpenStack {
		t.Fatalf("openstack: %v, %v", d1, err)
	}
	if d2, _ := r.Driver(ctx, osP); d2 != d1 {
		t.Fatal("driver not reused: OpenStack tokens would not be cached across syncs")
	}
	rotated = rotated.Add(time.Hour)
	if d3, _ := r.Driver(ctx, osP); d3 == d1 {
		t.Fatal("driver reused after the credential rotated")
	}
	if d, err := r.Driver(ctx, pveP); err != nil || d.Type() != contract.ProviderProxmox {
		t.Fatalf("proxmox: %v, %v", d, err)
	}

	missing := model.Provider{ID: "none", Type: contract.ProviderProxmox, Endpoints: []string{"https://x.example.com"}}
	if _, err := r.Driver(ctx, missing); !errors.Is(err, ErrNoCredential) || !errors.Is(err, contract.ErrUnauthorized) {
		t.Fatalf("no credential: %v", err)
	}
	for _, id := range []string{"bad", "raw"} {
		p := model.Provider{ID: id, Type: contract.ProviderProxmox, Endpoints: []string{"https://x.example.com"}}
		if _, err := r.Driver(ctx, p); !errors.Is(err, contract.ErrUnauthorized) || errors.Is(err, ErrNoCredential) {
			t.Fatalf("%s credential: %v", id, err)
		}
	}
	pinned := pveP
	pinned.TLSFingerprint = "bogus"
	pinned.ID = "pve-pinned"
	secrets["pve-pinned"] = secrets["pve"]
	if _, err := r.Driver(ctx, pinned); err == nil {
		t.Fatal("malformed TLS pin accepted")
	}
	if _, err := (&Resolver{}).Driver(ctx, osP); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("without a keyring: %v", err)
	}
	if d, err := (&Resolver{}).Driver(ctx, model.Provider{ID: "f", Type: contract.ProviderFake}); err != nil || d.Type() != contract.ProviderFake {
		t.Fatalf("fake without keyring: %v", err)
	}
}

// noSecrets satisfies secret.Store; the opener above never calls it.
type noSecrets struct{}

func (noSecrets) Seal([]byte, []byte) (secret.Sealed, error) { return secret.Sealed{}, nil }
func (noSecrets) Open(secret.Sealed, []byte) ([]byte, error) { return nil, nil }
func (noSecrets) Rewrap(s secret.Sealed, _ []byte) (secret.Sealed, bool, error) {
	return s, false, nil
}
func (noSecrets) CurrentVersion() string { return "k1" }

func TestConsoleDriverPrefersMutateCredential(t *testing.T) {
	ctx := context.Background()
	var asked []string
	r := &Resolver{Secrets: noSecrets{}, Credentials: purposeOpener(func(purpose model.CredentialPurpose, scope string) (model.ProviderCredential, []byte, error) {
		asked = append(asked, string(purpose)+" "+scope)
		switch {
		case purpose == model.PurposeMutate && scope == "pool:payment":
			return model.ProviderCredential{ID: "mutate-payment"}, []byte(`{"token_id":"semar@pve!payment","token_secret":"s"}`), nil
		case purpose == model.PurposeDiscovery:
			return model.ProviderCredential{ID: "discovery"}, []byte(`{"token_id":"semar@pve!discovery","token_secret":"s"}`), nil
		}
		return model.ProviderCredential{}, nil, store.ErrNotFound
	})}
	p := model.Provider{ID: "pve", Type: contract.ProviderProxmox, Endpoints: []string{"https://pve1.example.com:8006"}}

	mutate, err := r.ConsoleDriver(ctx, p, "pool:payment")
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := r.ConsoleDriver(ctx, p, "pool:search")
	if err != nil {
		t.Fatal(err)
	}
	discovery, _ := r.Driver(ctx, p)
	if mutate == fallback || fallback != discovery {
		t.Fatal("console drivers: want the mutate credential for pool:payment and discovery otherwise")
	}
	want := "mutate pool:payment|mutate pool:search|discovery provider|discovery provider"
	if got := strings.Join(asked, "|"); got != want {
		t.Fatalf("credentials asked: %s, want %s", got, want)
	}
	if mutateScope(contract.ProviderOpenStack, "abc123") != "project:abc123" || mutateScope(contract.ProviderProxmox, "") != "" {
		t.Fatal("mutateScope")
	}
}

// purposeOpener adapts a function to CredentialOpener.
type purposeOpener func(purpose model.CredentialPurpose, scope string) (model.ProviderCredential, []byte, error)

func (f purposeOpener) OpenCredential(_ context.Context, _ secret.Store, _ string, purpose model.CredentialPurpose, scope string) (model.ProviderCredential, []byte, error) {
	return f(purpose, scope)
}
