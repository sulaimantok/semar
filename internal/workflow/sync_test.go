package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
	"github.com/sulaimantok/semar/internal/store"
)

// countingDriver wraps a driver, counts ListVMs calls and can override errors.
type countingDriver struct {
	*fake.Driver
	listVMs atomic.Int32
	vmsErr  error
}

func (d *countingDriver) ListVMs(ctx context.Context) ([]contract.VirtualMachine, error) {
	d.listVMs.Add(1)
	if d.vmsErr != nil {
		return nil, d.vmsErr
	}
	return d.Driver.ListVMs(ctx)
}

func newEnv(t *testing.T, drivers StaticDrivers) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(SyncProviderWorkflow)
	env.RegisterActivity(&Activities{
		Drivers: drivers,
		Now:     func() time.Time { return time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) },
	})
	return env
}

func TestSyncProviderWorkflowSummarisesInventory(t *testing.T) {
	d := fake.New(fake.Config{Hypervisors: 2, VMsPerHypervisor: 3})
	env := newEnv(t, StaticDrivers{"fake-1": d})

	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "fake-1"})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var got InventorySummary
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != "fake-1" || got.Hypervisors != 2 || got.VMs != 6 || got.Volumes != 7 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestSyncProviderWorkflowRetriesTransientErrors(t *testing.T) {
	d := fake.New(fake.Config{})
	d.SetFaults(fake.Faults{FailNext: 2})
	env := newEnv(t, StaticDrivers{"fake-1": d})

	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "fake-1"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("transient errors were not retried away: %v", err)
	}
}

func TestSyncProviderWorkflowDoesNotRetryPermanentErrors(t *testing.T) {
	d := &countingDriver{Driver: fake.New(fake.Config{}), vmsErr: fmt.Errorf("auth: %w", contract.ErrUnauthorized)}
	env := newEnv(t, StaticDrivers{"fake-1": d})

	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "fake-1"})
	err := env.GetWorkflowError()
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != errTypeProviderNonRetryable {
		t.Fatalf("workflow error = %v, want non-retryable provider error", err)
	}
	if n := d.listVMs.Load(); n != 1 {
		t.Fatalf("ListVMs called %d times, want 1 (no retries)", n)
	}
}

func TestSyncOfRemovedProviderIsSkipped(t *testing.T) {
	env := newEnv(t, StaticDrivers{})
	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "missing"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("removed provider failed the workflow: %v", err)
	}
	var got InventorySummary
	if err := env.GetWorkflowResult(&got); err != nil || !got.Skipped {
		t.Fatalf("summary = %+v, %v; want Skipped", got, err)
	}
}

func TestTaskQueueAndScheduleNames(t *testing.T) {
	if got := TaskQueueForSite("jkt"); got != "site-jkt" {
		t.Errorf("TaskQueueForSite = %q", got)
	}
	if got := SyncScheduleID("prov-1"); got != "sync-prov-1" {
		t.Errorf("SyncScheduleID = %q", got)
	}
}

// recordingInventory is an in-memory InventoryStore.
type recordingInventory struct {
	snapshots []store.Snapshot
	failures  []string
	applyErr  error
}

func (r *recordingInventory) ApplySnapshot(_ context.Context, snap store.Snapshot, orphanAfter int) (store.SyncResult, error) {
	if r.applyErr != nil {
		return store.SyncResult{}, r.applyErr
	}
	r.snapshots = append(r.snapshots, snap)
	return store.SyncResult{Created: len(snap.Resources)}, nil
}

func (r *recordingInventory) RecordSyncFailure(_ context.Context, providerID, reason string) (store.SyncHealth, error) {
	r.failures = append(r.failures, providerID+": "+reason)
	return store.SyncDegraded, nil
}

func newEnvWithInventory(t *testing.T, drivers StaticDrivers, inv InventoryStore) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(SyncProviderWorkflow)
	env.RegisterActivity(&Activities{
		Drivers:   drivers,
		Inventory: inv,
		Now:       func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) },
	})
	return env
}

func TestSyncPersistsCompleteSnapshot(t *testing.T) {
	inv := &recordingInventory{}
	d := fake.New(fake.Config{Hypervisors: 2, VMsPerHypervisor: 3})
	env := newEnvWithInventory(t, StaticDrivers{"fake-1": d}, inv)
	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "fake-1"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if len(inv.snapshots) != 1 {
		t.Fatalf("snapshots applied = %d, want 1", len(inv.snapshots))
	}
	snap := inv.snapshots[0]
	kinds := map[store.ResourceKind]int{}
	for _, r := range snap.Resources {
		kinds[r.Kind]++
	}
	// 2 hypervisors, 6 VMs, 2 images (amd64+arm64), 2 networks, 6+1 volumes.
	want := map[store.ResourceKind]int{store.KindHypervisor: 2, store.KindVM: 6, store.KindImage: 2, store.KindNetwork: 2, store.KindVolume: 7}
	for k, n := range want {
		if kinds[k] != n {
			t.Errorf("%s resources = %d, want %d", k, kinds[k], n)
		}
	}
	if snap.ProviderID != "fake-1" || snap.ObservedAt.IsZero() {
		t.Fatalf("snapshot = %+v", snap)
	}
	var got InventorySummary
	if err := env.GetWorkflowResult(&got); err != nil || got.Created != 19 {
		t.Fatalf("summary = %+v, %v", got, err)
	}
}

func TestSyncRecordsFailureAndAppliesNothing(t *testing.T) {
	inv := &recordingInventory{}
	d := &countingDriver{Driver: fake.New(fake.Config{}), vmsErr: fmt.Errorf("auth: %w", contract.ErrUnauthorized)}
	env := newEnvWithInventory(t, StaticDrivers{"fake-1": d}, inv)
	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "fake-1"})
	if env.GetWorkflowError() == nil {
		t.Fatal("workflow succeeded with a failing provider")
	}
	if len(inv.snapshots) != 0 {
		t.Fatal("a partial snapshot was applied")
	}
	if len(inv.failures) != 1 || !strings.Contains(inv.failures[0], "list vms") {
		t.Fatalf("failures = %v", inv.failures)
	}
}

func TestSyncTreatsStaleSnapshotAsNoOp(t *testing.T) {
	inv := &recordingInventory{applyErr: store.ErrStaleSnapshot}
	env := newEnvWithInventory(t, StaticDrivers{"fake-1": fake.New(fake.Config{})}, inv)
	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "fake-1"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("stale snapshot failed the workflow: %v", err)
	}
	var got InventorySummary
	if err := env.GetWorkflowResult(&got); err != nil || !got.Stale {
		t.Fatalf("summary = %+v, %v; want Stale", got, err)
	}
}

func TestProviderRemovedDuringSyncIsSkipped(t *testing.T) {
	inv := &recordingInventory{applyErr: store.ErrNotFound}
	env := newEnvWithInventory(t, StaticDrivers{"fake-1": fake.New(fake.Config{})}, inv)
	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "fake-1"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error = %v, want skipped", err)
	}
	var got InventorySummary
	if err := env.GetWorkflowResult(&got); err != nil || !got.Skipped {
		t.Fatalf("summary = %+v, %v; want Skipped", got, err)
	}
}

func TestUnimplementedDriverIsNotRetried(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(SyncProviderWorkflow)
	env.RegisterActivity(&Activities{Drivers: RegisteredDrivers{Providers: providerMap{"os": {ID: "os", Type: contract.ProviderOpenStack}}}})
	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "os"})
	var appErr *temporal.ApplicationError
	if err := env.GetWorkflowError(); !errors.As(err, &appErr) || appErr.Type() != errTypeProviderNonRetryable {
		t.Fatalf("workflow error = %v, want non-retryable", err)
	}
}
