package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

// TaskQueueForSite returns the task queue that runs provider calls for a site
// (roadmap §3.5): a remote site is served by a semar-worker listening on it.
func TaskQueueForSite(siteID string) string { return "site-" + siteID }

// SyncInput identifies the provider to synchronise. It carries IDs only,
// never credentials (roadmap §3.4).
type SyncInput struct {
	ProviderID string
}

// InventorySummary is the result of one sync run.
type InventorySummary struct {
	ProviderID  string
	Hypervisors int
	VMs         int
	Images      int
	Networks    int
	Volumes     int
	ObservedAt  time.Time
}

// DriverResolver returns the driver for a provider.
type DriverResolver interface {
	Driver(providerID string) (contract.CloudDriver, error)
}

// StaticDrivers is a DriverResolver backed by a fixed map.
type StaticDrivers map[string]contract.CloudDriver

// ErrUnknownProvider is returned for providers that are not registered.
var ErrUnknownProvider = errors.New("unknown provider")

// Driver implements DriverResolver.
func (s StaticDrivers) Driver(providerID string) (contract.CloudDriver, error) {
	d, ok := s[providerID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, providerID)
	}
	return d, nil
}

// Error types reported to Temporal; non-retryable errors stop the retry policy.
const (
	errTypeProviderRetryable    = "ProviderRetryable"
	errTypeProviderNonRetryable = "ProviderNonRetryable"
)

// Activities holds activity dependencies.
type Activities struct {
	Drivers DriverResolver
	Now     func() time.Time
}

// CollectInventory reads the provider's full inventory. Persisting it into
// the SEMAR inventory cache (conditional writes, orphan detection —
// roadmap §3.6) is added with the store in Phase 1.
func (a *Activities) CollectInventory(ctx context.Context, in SyncInput) (InventorySummary, error) {
	d, err := a.Drivers.Driver(in.ProviderID)
	if err != nil {
		return InventorySummary{}, temporal.NewNonRetryableApplicationError(err.Error(), errTypeProviderNonRetryable, err)
	}
	s := InventorySummary{ProviderID: in.ProviderID}

	hyps, err := d.ListHypervisors(ctx)
	if err != nil {
		return InventorySummary{}, classify("list hypervisors", err)
	}
	vms, err := d.ListVMs(ctx)
	if err != nil {
		return InventorySummary{}, classify("list vms", err)
	}
	images, err := d.ListImages(ctx)
	if err != nil {
		return InventorySummary{}, classify("list images", err)
	}
	nets, err := d.ListNetworks(ctx)
	if err != nil {
		return InventorySummary{}, classify("list networks", err)
	}
	vols, err := d.ListVolumes(ctx)
	if err != nil {
		return InventorySummary{}, classify("list volumes", err)
	}

	s.Hypervisors, s.VMs, s.Images, s.Networks, s.Volumes = len(hyps), len(vms), len(images), len(nets), len(vols)
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	s.ObservedAt = now().UTC()
	return s, nil
}

// classify maps driver errors to Temporal application errors so that only
// transient failures are retried.
func classify(op string, err error) error {
	msg := op + ": " + err.Error()
	if contract.IsRetryable(err) {
		return temporal.NewApplicationErrorWithCause(msg, errTypeProviderRetryable, err)
	}
	return temporal.NewNonRetryableApplicationError(msg, errTypeProviderNonRetryable, err)
}

// SyncActivityOptions bounds one CollectInventory attempt and its retries.
var SyncActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 2 * time.Minute,
	RetryPolicy: &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2,
		MaximumInterval:    30 * time.Second,
		MaximumAttempts:    5,
	},
}

// SyncProviderWorkflow runs one inventory sync for a provider. It is started
// by the per-provider Schedule (schedule.go).
func SyncProviderWorkflow(ctx workflow.Context, in SyncInput) (InventorySummary, error) {
	ctx = workflow.WithActivityOptions(ctx, SyncActivityOptions)
	var a *Activities
	var s InventorySummary
	if err := workflow.ExecuteActivity(ctx, a.CollectInventory, in).Get(ctx, &s); err != nil {
		return InventorySummary{}, err
	}
	workflow.GetLogger(ctx).Info("inventory synced",
		"provider", s.ProviderID, "hypervisors", s.Hypervisors, "vms", s.VMs,
		"images", s.Images, "networks", s.Networks, "volumes", s.Volumes)
	return s, nil
}
