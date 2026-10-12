package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

func keyring(t *testing.T, current string, versions ...string) *secret.Keyring {
	t.Helper()
	keks := map[string][]byte{}
	for i, v := range append([]string{current}, versions...) {
		keks[v] = bytes.Repeat([]byte{byte(i + 1)}, 32)
	}
	k, err := secret.NewKeyring(current, keks)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func provider(id, fingerprint string) model.Provider {
	return model.Provider{
		ID:          id,
		Type:        "openstack",
		Name:        "OpenStack " + id,
		Endpoints:   []string{"https://keystone.example:5000/v3"},
		RegionName:  "RegionOne",
		Fingerprint: fingerprint,
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dbURL := storetest.NewDatabase(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := store.Migrate(ctx, dbURL); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}
}

func TestConcurrentMigrate(t *testing.T) {
	dbURL := storetest.NewDatabase(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Migrate(context.Background(), dbURL)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate: %v", err)
		}
	}
}

func TestSitesAndProjects(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if _, err := st.CreateSite(ctx, model.Site{ID: "jkt", Name: "Jakarta IDC", Labels: map[string]string{"env": "prod"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSite(ctx, model.Site{ID: "jkt", Name: "dup"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate site: %v, want ErrConflict", err)
	}
	if _, err := st.CreateSite(ctx, model.Site{ID: "Bad ID", Name: "x"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid site id: %v, want ErrInvalid", err)
	}
	if _, err := st.CreateSite(ctx, model.Site{ID: "lab", Name: ""}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("empty site name: %v, want ErrInvalid", err)
	}
	sites, err := st.ListSites(ctx)
	if err != nil || len(sites) != 1 || sites[0].Labels["env"] != "prod" {
		t.Fatalf("ListSites = %+v, %v", sites, err)
	}

	if _, err := st.CreateProject(ctx, model.Project{ID: "payment", Name: "Payment team"}); err != nil {
		t.Fatal(err)
	}
	projects, err := st.ListProjects(ctx)
	if err != nil || len(projects) != 1 || projects[0].Name != "Payment team" {
		t.Fatalf("ListProjects = %+v, %v", projects, err)
	}
}

func TestProviderLifecycle(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if _, err := st.CreateSite(ctx, model.Site{ID: "jkt", Name: "Jakarta"}); err != nil {
		t.Fatal(err)
	}

	p := provider("os-jkt", "keystone.example:5000/v3#RegionOne")
	p.SiteID = "jkt"
	p.Capabilities = json.RawMessage(`{"floatingIp":true}`)
	p.MaxMicroversions = map[string]string{"compute": "2.96"}
	created, err := st.CreateProvider(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if created.SiteID != "jkt" || created.MaxMicroversions["compute"] != "2.96" || created.CreatedAt.IsZero() {
		t.Fatalf("created = %+v", created)
	}

	// The same cloud cannot be registered twice under another ID.
	if _, err := st.CreateProvider(ctx, provider("os-jkt-2", p.Fingerprint)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate fingerprint: %v, want ErrConflict", err)
	}
	bad := provider("os-x", "fp-x")
	bad.SiteID = "missing"
	if _, err := st.CreateProvider(ctx, bad); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("unknown site: %v, want ErrInvalidReference", err)
	}
	bad = provider("os-y", "fp-y")
	bad.Type = "vmware"
	if _, err := st.CreateProvider(ctx, bad); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown type: %v, want ErrInvalid", err)
	}
	bad = provider("os-z", "fp-z")
	bad.Endpoints = nil
	if _, err := st.CreateProvider(ctx, bad); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("no endpoints: %v, want ErrInvalid", err)
	}

	if err := st.DeleteProvider(ctx, "os-jkt"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetProvider(ctx, "os-jkt"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get deleted: %v, want ErrNotFound", err)
	}
	if err := st.DeleteProvider(ctx, "os-jkt"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete twice: %v, want ErrNotFound", err)
	}
	// After a soft delete the cloud may be registered again.
	if _, err := st.CreateProvider(ctx, provider("os-jkt-new", p.Fingerprint)); err != nil {
		t.Fatalf("re-register after delete: %v", err)
	}
	list, err := st.ListProviders(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "os-jkt-new" {
		t.Fatalf("ListProviders = %+v, %v", list, err)
	}
}

func TestProjectMappings(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	mustCreate(t, st, provider("pve-lab", "pve-cluster-lab"))
	for _, id := range []string{"payment", "search"} {
		if _, err := st.CreateProject(ctx, model.Project{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := st.CreateProjectMapping(ctx, model.ProjectMapping{ProjectID: "payment", ProviderID: "pve-lab", ExternalScope: "pool:payment"}); err != nil {
		t.Fatal(err)
	}
	// One external scope maps to at most one project.
	if _, err := st.CreateProjectMapping(ctx, model.ProjectMapping{ProjectID: "search", ProviderID: "pve-lab", ExternalScope: "pool:payment"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second mapping of the same scope: %v, want ErrConflict", err)
	}
	if got, err := st.ProjectForScope(ctx, "pve-lab", "pool:payment"); err != nil || got != "payment" {
		t.Fatalf("ProjectForScope = %q, %v", got, err)
	}
	if _, err := st.ProjectForScope(ctx, "pve-lab", "pool:other"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unmapped scope: %v, want ErrNotFound", err)
	}
}

func TestCredentialsAreSealedAndReplaceable(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	ks := keyring(t, "v1")
	mustCreate(t, st, provider("os-jkt", "fp"))

	c := model.ProviderCredential{ProviderID: "os-jkt", Purpose: model.PurposeDiscovery, Scope: "provider", Owner: "svc-semar"}
	first, err := st.PutCredential(ctx, ks, c, []byte(`{"secret":"first-SECRET-marker"}`))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.KEKVersion != "v1" {
		t.Fatalf("PutCredential = %+v", first)
	}
	assertNoPlaintext(t, st, "SECRET-marker")

	second, err := st.PutCredential(ctx, ks, c, []byte(`{"secret":"second"}`))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("replacement changed row ID: %s → %s", first.ID, second.ID)
	}
	got, plain, err := st.OpenCredential(ctx, ks, "os-jkt", model.PurposeDiscovery, "provider")
	if err != nil || string(plain) != `{"secret":"second"}` || got.Owner != "svc-semar" {
		t.Fatalf("OpenCredential = %+v, %q, %v", got, plain, err)
	}

	if _, _, err := st.OpenCredential(ctx, ks, "os-jkt", model.PurposeMutate, "provider"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other purpose: %v, want ErrNotFound", err)
	}
	if _, err := st.PutCredential(ctx, ks, model.ProviderCredential{ProviderID: "os-jkt", Purpose: "root", Scope: "provider"}, []byte("x")); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown purpose: %v, want ErrInvalid", err)
	}
	if _, err := st.PutCredential(ctx, ks, model.ProviderCredential{ProviderID: "missing", Purpose: model.PurposeDiscovery, Scope: "provider"}, []byte("x")); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("unknown provider: %v, want ErrInvalidReference", err)
	}
}

func TestConcurrentFirstPutCredential(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	ks := keyring(t, "v1")
	mustCreate(t, st, provider("os-jkt", "fp"))

	c := model.ProviderCredential{ProviderID: "os-jkt", Purpose: model.PurposeMutate, Scope: "project:abc"}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := st.PutCredential(ctx, ks, c, []byte(strconv.Itoa(i)))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent put: %v", err)
		}
	}
	// Whichever write won, it must open with its row's AAD.
	if _, _, err := st.OpenCredential(ctx, ks, "os-jkt", model.PurposeMutate, "project:abc"); err != nil {
		t.Fatalf("open after concurrent puts: %v", err)
	}
}

func TestRewrapCredentialsAfterKEKRotation(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	mustCreate(t, st, provider("os-jkt", "fp"))

	old := keyring(t, "v1")
	for _, scope := range []string{"project:a", "project:b", "project:c"} {
		c := model.ProviderCredential{ProviderID: "os-jkt", Purpose: model.PurposeMutate, Scope: scope}
		if _, err := st.PutCredential(ctx, old, c, []byte(scope)); err != nil {
			t.Fatal(err)
		}
	}

	// v1 keeps the old keyring's key bytes; v2 is the new current KEK.
	rotated, err := secret.NewKeyring("v2", map[string][]byte{
		"v2": bytes.Repeat([]byte{9}, 32),
		"v1": bytes.Repeat([]byte{1}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.RewrapCredentials(ctx, rotated)
	if err != nil || n != 3 {
		t.Fatalf("RewrapCredentials = %d, %v; want 3", n, err)
	}
	if n, _ := st.RewrapCredentials(ctx, rotated); n != 0 {
		t.Fatalf("second rewrap touched %d credentials, want 0", n)
	}

	onlyV2, err := secret.NewKeyring("v2", map[string][]byte{"v2": bytes.Repeat([]byte{9}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	c, plain, err := st.OpenCredential(ctx, onlyV2, "os-jkt", model.PurposeMutate, "project:b")
	if err != nil || string(plain) != "project:b" || c.KEKVersion != "v2" {
		t.Fatalf("open after dropping v1: %+v, %q, %v", c, plain, err)
	}
}

func mustCreate(t *testing.T, st *store.Store, p model.Provider) {
	t.Helper()
	if _, err := st.CreateProvider(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

// assertNoPlaintext checks that no credential column stores the marker.
func assertNoPlaintext(t *testing.T, st *store.Store, marker string) {
	t.Helper()
	n, err := st.CountCredentialsContaining(context.Background(), []byte(marker))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d credential rows contain plaintext %q", n, marker)
	}
}
