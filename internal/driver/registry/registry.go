// Package registry builds the driver for a registered provider. Both the
// worker (inventory sync) and the API (connection checks) use it, so a
// provider is always reached the same way.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
	"github.com/sulaimantok/semar/internal/driver/httpx"
	"github.com/sulaimantok/semar/internal/driver/openstack"
	"github.com/sulaimantok/semar/internal/driver/proxmox"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/store"
)

// ErrNotImplemented is returned for provider types without a driver.
var ErrNotImplemented = errors.New("driver not implemented")

// ErrNoCredential is returned when a provider has no discovery credential,
// or this process cannot decrypt credentials. It also matches
// contract.ErrUnauthorized.
var ErrNoCredential = errors.New("no usable discovery credential")

// CredentialOpener decrypts stored provider credentials; *store.Store
// implements it.
type CredentialOpener interface {
	OpenCredential(ctx context.Context, ks secret.Store, providerID string, purpose model.CredentialPurpose, scope string) (model.ProviderCredential, []byte, error)
}

var _ CredentialOpener = (*store.Store)(nil)

// Resolver builds drivers from providers and their discovery credential
// (ADR-0005). The secret is opened in this process only and is never put
// into a workflow payload. Drivers are cached per provider and rebuilt when
// the provider or its credential changes, so OpenStack tokens are reused
// across syncs.
type Resolver struct {
	Credentials CredentialOpener
	Secrets     secret.Store
	// Timeout bounds one HTTP request to a provider. Defaults to 30s.
	Timeout time.Duration

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	key    string
	driver contract.CloudDriver
}

// Driver returns the driver for p.
func (r *Resolver) Driver(ctx context.Context, p model.Provider) (contract.CloudDriver, error) {
	if p.Type == contract.ProviderFake {
		return New(p)
	}
	if p.Type != contract.ProviderOpenStack && p.Type != contract.ProviderProxmox {
		return nil, fmt.Errorf("%w for provider type %q", ErrNotImplemented, p.Type)
	}
	if r.Credentials == nil || r.Secrets == nil {
		return nil, fmt.Errorf("%w: credential decryption is not configured (SEMAR_KEK): %w", ErrNoCredential, contract.ErrUnauthorized)
	}
	cred, plaintext, err := r.Credentials.OpenCredential(ctx, r.Secrets, p.ID, model.PurposeDiscovery, "provider")
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: none stored for provider %s: %w", ErrNoCredential, p.ID, contract.ErrUnauthorized)
	}
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)

	key := p.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + cred.ID + "|" + cred.RotatedAt.UTC().Format(time.RFC3339Nano)
	r.mu.Lock()
	if c, ok := r.cache[p.ID]; ok && c.key == key {
		r.mu.Unlock()
		return c.driver, nil
	}
	r.mu.Unlock()

	var sec map[string]string
	if err := json.Unmarshal(plaintext, &sec); err != nil {
		return nil, fmt.Errorf("%w: discovery credential is not a JSON object", contract.ErrUnauthorized)
	}
	d, err := r.build(p, sec)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]cached{}
	}
	r.cache[p.ID] = cached{key: key, driver: d}
	r.mu.Unlock()
	return d, nil
}

func (r *Resolver) build(p model.Provider, sec map[string]string) (contract.CloudDriver, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client, err := httpx.Client(timeout, p.TLSFingerprint)
	if err != nil {
		return nil, err
	}
	switch p.Type {
	case contract.ProviderOpenStack:
		cfg, err := openstack.ConfigFromSecret(openstack.Config{AuthURL: p.Endpoints[0], Region: p.RegionName, HTTPClient: client}, sec)
		if err != nil {
			return nil, err
		}
		return openstack.New(cfg)
	default:
		cfg, err := proxmox.ConfigFromSecret(proxmox.Config{Endpoints: append([]string(nil), p.Endpoints...), HTTPClient: client}, sec)
		if err != nil {
			return nil, err
		}
		return proxmox.New(cfg)
	}
}

// Forget drops a provider's cached driver (after removal).
func (r *Resolver) Forget(providerID string) {
	r.mu.Lock()
	delete(r.cache, providerID)
	r.mu.Unlock()
}

// New returns the driver for a provider that needs no credential: only
// "fake" providers. Real providers go through a Resolver.
func New(p model.Provider) (contract.CloudDriver, error) {
	switch p.Type {
	case contract.ProviderFake:
		// Deterministic per provider so repeated syncs see the same cloud.
		return fake.New(fake.Config{Seed: SeedFor(p.ID)}), nil
	default:
		return nil, fmt.Errorf("%w for provider type %q without credentials", ErrNotImplemented, p.Type)
	}
}

// SeedFor derives a fake provider's inventory seed from its ID.
func SeedFor(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64() | 1
}
