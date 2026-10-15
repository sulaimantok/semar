package workflow

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

// fullSyncBudget is the Phase 1 exit criterion for a 1,000-VM provider.
const fullSyncBudget = 60 * time.Second

// TestFullSyncOf1000VMsWithinBudget runs the sync activity against a
// database for a 1,000-VM fake provider: the first sync creates every
// resource, the second sees them all unchanged, and the third follows a
// deletion. Each must finish within fullSyncBudget.
func TestFullSyncOf1000VMsWithinBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	st := storetest.New(t)
	ctx := context.Background()
	p := model.Provider{ID: "fake-scale", Type: contract.ProviderFake, Name: "Scale", Endpoints: []string{"https://fake.example.com"}}
	var err error
	if p.Fingerprint, err = model.ProviderFingerprint(p.Type, p.Endpoints, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	d := fake.New(fake.Config{Seed: 1000, Hypervisors: 10, VMsPerHypervisor: 100})

	clock := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	acts := &Activities{
		Drivers:   StaticDrivers{"fake-scale": d},
		Inventory: st,
		Now:       func() time.Time { return clock },
	}
	var s testsuite.WorkflowTestSuite
	env := s.NewTestActivityEnvironment()
	env.RegisterActivity(acts)

	sync := func(name string) InventorySummary {
		t.Helper()
		start := time.Now()
		val, err := env.ExecuteActivity(acts.CollectInventory, SyncInput{ProviderID: "fake-scale"})
		took := time.Since(start)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var sum InventorySummary
		if err := val.Get(&sum); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d VMs in %s", name, sum.VMs, took)
		if took > fullSyncBudget {
			t.Errorf("%s took %s, budget %s", name, took, fullSyncBudget)
		}
		clock = clock.Add(time.Minute)
		return sum
	}

	first := sync("initial sync")
	if first.VMs != 1000 || first.Created < 1000 {
		t.Fatalf("initial sync = %+v, want 1000 VMs created", first)
	}
	var vms []contract.VirtualMachine
	if again := sync("unchanged sync"); again.Created != 0 || again.Updated != 0 {
		t.Fatalf("unchanged sync = %+v, want no changes", again)
	}

	vms, err = d.ListVMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !d.DeleteVM(vms[0].ExternalID) {
		t.Fatal("DeleteVM found nothing")
	}
	if after := sync("sync after delete"); after.VMs != 999 {
		t.Fatalf("sync after delete = %+v, want 999 VMs", after)
	}
}
