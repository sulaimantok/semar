package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
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

func TestSyncProviderWorkflowUnknownProvider(t *testing.T) {
	env := newEnv(t, StaticDrivers{})
	env.ExecuteWorkflow(SyncProviderWorkflow, SyncInput{ProviderID: "missing"})
	var appErr *temporal.ApplicationError
	if err := env.GetWorkflowError(); !errors.As(err, &appErr) || appErr.Type() != errTypeProviderNonRetryable {
		t.Fatalf("workflow error = %v, want non-retryable", err)
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
