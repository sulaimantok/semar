package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/metrics"
	"github.com/sulaimantok/semar/internal/requestid"
	"github.com/sulaimantok/semar/internal/store"
)

// TaskQueueForSite returns the task queue that runs provider calls for a site
// (roadmap §3.5): a remote site is served by a semar-worker listening on it.
func TaskQueueForSite(siteID string) string { return "site-" + siteID }

// SyncInput identifies the provider to synchronise. It carries IDs only,
// never credentials (roadmap §3.4).
type SyncInput struct {
	ProviderID string
}

// InventorySummary is the result of one sync run. Fields are only ever
// added (never renamed) so recorded workflow histories keep decoding.
type InventorySummary struct {
	ProviderID  string
	Hypervisors int
	VMs         int
	Images      int
	Networks    int
	Volumes     int
	ObservedAt  time.Time
	Created     int
	Updated     int
	Orphaned    int
	Restored    int
	// Stale is set when a newer sync had already been applied.
	Stale bool
	// Skipped is set when the provider was removed; its schedule is deleted
	// at the next reconciliation.
	Skipped bool
}

// DriverResolver returns the driver for a provider.
type DriverResolver interface {
	Driver(ctx context.Context, providerID string) (contract.CloudDriver, error)
}

// StaticDrivers is a DriverResolver backed by a fixed map (tests).
type StaticDrivers map[string]contract.CloudDriver

// ErrUnknownProvider is returned for providers that are not registered.
var ErrUnknownProvider = errors.New("unknown provider")

// Driver implements DriverResolver.
func (s StaticDrivers) Driver(_ context.Context, providerID string) (contract.CloudDriver, error) {
	d, ok := s[providerID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, providerID)
	}
	return d, nil
}

// InventoryStore persists sync results; *store.Store implements it.
type InventoryStore interface {
	ApplySnapshot(ctx context.Context, snap store.Snapshot, orphanAfter int) (store.SyncResult, error)
	RecordSyncFailure(ctx context.Context, providerID, reason string) (store.SyncHealth, error)
}

var _ InventoryStore = (*store.Store)(nil)

// Error types reported to Temporal; non-retryable errors stop the retry policy.
const (
	errTypeProviderRetryable    = "ProviderRetryable"
	errTypeProviderNonRetryable = "ProviderNonRetryable"
)

// DefaultOrphanAfter is the number of consecutive syncs a resource may be
// missing before it is marked orphaned.
const DefaultOrphanAfter = 3

// Activities holds activity dependencies.
type Activities struct {
	Drivers DriverResolver
	// Inventory receives snapshots; nil only collects (tests).
	Inventory   InventoryStore
	OrphanAfter int
	Now         func() time.Time
	// Metrics records sync outcomes; nil disables them.
	Metrics *SyncMetrics
}

// SyncMetrics are the worker's inventory sync metrics.
type SyncMetrics struct {
	syncs       *metrics.Vec
	duration    *metrics.Histogram
	lastSuccess *metrics.Vec
	resources   *metrics.Vec
}

// NewSyncMetrics registers the sync metrics in reg.
func NewSyncMetrics(reg *metrics.Registry) *SyncMetrics {
	return &SyncMetrics{
		syncs: reg.Counter("semar_inventory_syncs_total",
			"Inventory syncs by provider and outcome (success, failure, skipped, stale).", "provider", "outcome"),
		duration: reg.Histogram("semar_inventory_sync_duration_seconds",
			"Duration of inventory syncs, listing and persisting.", metrics.SyncBuckets, "provider"),
		lastSuccess: reg.Gauge("semar_inventory_last_success_timestamp_seconds",
			"Time of the last successful sync per provider.", "provider"),
		resources: reg.Gauge("semar_inventory_vms",
			"VMs reported by the last successful sync per provider.", "provider"),
	}
}

func (m *SyncMetrics) record(provider, outcome string, took time.Duration, vms int) {
	if m == nil {
		return
	}
	m.syncs.Inc(provider, outcome)
	if outcome == "success" || outcome == "stale" {
		m.duration.Observe(took.Seconds(), provider)
	}
	if outcome == "success" {
		m.lastSuccess.Set(float64(time.Now().Unix()), provider)
		m.resources.Set(float64(vms), provider)
	}
}

// CollectInventory reads the provider's full inventory and applies it to the
// inventory cache (roadmap §3.6). Listing and persisting happen in one
// activity so a large inventory never travels through Temporal payloads.
func (a *Activities) CollectInventory(ctx context.Context, in SyncInput) (s InventorySummary, err error) {
	start := time.Now()
	defer func() {
		outcome := "success"
		switch {
		case err != nil:
			outcome = "failure"
		case s.Skipped:
			outcome = "skipped"
		case s.Stale:
			outcome = "stale"
		}
		a.Metrics.record(in.ProviderID, outcome, time.Since(start), s.VMs)
	}()
	// Provider calls carry the run ID as correlation ID, so a sync can be
	// traced from Temporal to the provider's logs (X-OpenStack-Request-ID).
	ctx = requestid.With(ctx, runID(ctx))

	d, err := a.Drivers.Driver(ctx, in.ProviderID)
	if errors.Is(err, ErrUnknownProvider) {
		// Removed since the schedule fired: expected, not a failure.
		activity.GetLogger(ctx).Info("provider removed; sync skipped", "provider", in.ProviderID)
		return InventorySummary{ProviderID: in.ProviderID, Skipped: true}, nil
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrDriverNotImplemented):
			return InventorySummary{}, temporal.NewNonRetryableApplicationError(err.Error(), errTypeProviderNonRetryable, err)
		case errors.Is(err, registry.ErrNoCredential):
			// A configuration gap, not a failing sync: say so in the sync
			// status and skip until a discovery credential is stored, without
			// an activity error on every interval.
			activity.GetLogger(ctx).Warn("no discovery credential; sync skipped", "provider", in.ProviderID)
			a.recordFailure(ctx, in.ProviderID, err)
			return InventorySummary{ProviderID: in.ProviderID, Skipped: true}, nil
		case errors.Is(err, contract.ErrUnauthorized):
			// A stored credential that cannot be used (malformed for this
			// provider type): retrying cannot help.
			a.recordFailure(ctx, in.ProviderID, err)
			return InventorySummary{}, temporal.NewNonRetryableApplicationError(err.Error(), errTypeProviderNonRetryable, err)
		}
		return InventorySummary{}, temporal.NewApplicationErrorWithCause(err.Error(), errTypeProviderRetryable, err)
	}
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	// Taken before listing: everything in the snapshot is at least this fresh.
	observedAt := now().UTC()

	inv, err := collect(ctx, d)
	if err != nil {
		a.recordFailure(ctx, in.ProviderID, err)
		return InventorySummary{}, err
	}
	s = InventorySummary{
		ProviderID:  in.ProviderID,
		Hypervisors: len(inv.hypervisors),
		VMs:         len(inv.vms),
		Images:      len(inv.images),
		Networks:    len(inv.networks),
		Volumes:     len(inv.volumes),
		ObservedAt:  observedAt,
	}
	if a.Inventory == nil {
		return s, nil
	}

	orphanAfter := a.OrphanAfter
	if orphanAfter <= 0 {
		orphanAfter = DefaultOrphanAfter
	}
	res, err := a.Inventory.ApplySnapshot(ctx, inv.snapshot(in.ProviderID, observedAt), orphanAfter)
	switch {
	case errors.Is(err, store.ErrStaleSnapshot):
		// A newer sync already landed; this result is obsolete, not a failure.
		s.Stale = true
		return s, nil
	case errors.Is(err, store.ErrNotFound):
		// Removed while this sync was running.
		return InventorySummary{ProviderID: in.ProviderID, Skipped: true}, nil
	case err != nil:
		return InventorySummary{}, temporal.NewApplicationErrorWithCause("apply snapshot: "+err.Error(), errTypeProviderRetryable, err)
	}
	s.Created, s.Updated, s.Orphaned, s.Restored = res.Created, res.Updated, res.Orphaned, res.Restored
	return s, nil
}

// runID returns the workflow run ID of the current activity, or "" outside
// an activity (direct calls in tests).
func runID(ctx context.Context) (id string) {
	defer func() {
		if recover() != nil {
			id = ""
		}
	}()
	return activity.GetInfo(ctx).WorkflowExecution.RunID
}

func (a *Activities) recordFailure(ctx context.Context, providerID string, cause error) {
	if a.Inventory == nil {
		return
	}
	if _, err := a.Inventory.RecordSyncFailure(ctx, providerID, cause.Error()); err != nil {
		activity.GetLogger(ctx).Warn("record sync failure", "provider", providerID, "error", err)
	}
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
		"images", s.Images, "networks", s.Networks, "volumes", s.Volumes,
		"created", s.Created, "updated", s.Updated, "orphaned", s.Orphaned, "stale", s.Stale)
	return s, nil
}
