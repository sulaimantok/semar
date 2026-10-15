package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

// SyncScheduleID is the Schedule ID for a provider's inventory sync.
func SyncScheduleID(providerID string) string { return syncSchedulePrefix + providerID }

// EnsureSyncSchedule creates or updates the provider's sync Schedule. Overlap
// policy SKIP makes the sync a singleton: a run that is still in progress
// causes the next trigger to be skipped, never run concurrently (roadmap §3.6).
//
// An existing schedule is rewritten on every call so that its action is
// re-encoded with the current payload key after a key rotation.
func EnsureSyncSchedule(ctx context.Context, c client.Client, providerID, taskQueue string, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("sync schedule %s: interval must be positive", providerID)
	}
	id := SyncScheduleID(providerID)
	spec := client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: every}}}
	action := &client.ScheduleWorkflowAction{
		ID:        id,
		Workflow:  SyncProviderWorkflow,
		Args:      []any{SyncInput{ProviderID: providerID}},
		TaskQueue: taskQueue,
	}

	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:      id,
		Spec:    spec,
		Action:  action,
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create sync schedule %s: %w", id, err)
	}

	err = c.ScheduleClient().GetHandle(ctx, id).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(in client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			s := in.Description.Schedule
			s.Spec = &spec
			s.Action = action
			if s.Policy == nil {
				s.Policy = &client.SchedulePolicies{}
			}
			s.Policy.Overlap = enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
			return &client.ScheduleUpdate{Schedule: &s}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("update sync schedule %s: %w", id, err)
	}
	return nil
}

// syncSchedulePrefix marks schedules owned by SEMAR's inventory sync.
const syncSchedulePrefix = "sync-"

// scheduleOps is what schedule reconciliation needs from Temporal.
type scheduleOps interface {
	ensure(ctx context.Context, providerID string) error
	list(ctx context.Context) ([]string, error)
	remove(ctx context.Context, scheduleID string) error
	// servedHere reports whether an existing schedule starts its workflows
	// on this worker's task queue.
	servedHere(ctx context.Context, scheduleID string) (bool, error)
}

type temporalSchedules struct {
	c         client.Client
	taskQueue string
	every     time.Duration
}

func (t temporalSchedules) ensure(ctx context.Context, providerID string) error {
	return EnsureSyncSchedule(ctx, t.c, providerID, t.taskQueue, t.every)
}

func (t temporalSchedules) list(ctx context.Context) ([]string, error) {
	it, err := t.c.ScheduleClient().List(ctx, client.ScheduleListOptions{PageSize: 100})
	if err != nil {
		return nil, err
	}
	var ids []string
	for it.HasNext() {
		e, err := it.Next()
		if err != nil {
			return nil, err
		}
		ids = append(ids, e.ID)
	}
	return ids, nil
}

func (t temporalSchedules) remove(ctx context.Context, scheduleID string) error {
	return t.c.ScheduleClient().GetHandle(ctx, scheduleID).Delete(ctx)
}

func (t temporalSchedules) servedHere(ctx context.Context, scheduleID string) (bool, error) {
	d, err := t.c.ScheduleClient().GetHandle(ctx, scheduleID).Describe(ctx)
	if err != nil {
		return false, err
	}
	a, ok := d.Schedule.Action.(*client.ScheduleWorkflowAction)
	return ok && a.TaskQueue == t.taskQueue, nil
}

// ReconcileResult reports what a reconciliation changed.
type ReconcileResult struct {
	Ensured []string
	Removed []string
}

// ReconcileSchedules makes Temporal's sync schedules match the database:
//   - own lists the active providers served by this worker's site; each gets
//     a schedule on this site's task queue. An existing schedule on another
//     queue (the provider moved to this site) is rewritten; rewriteExisting
//     rewrites every existing one, which re-encrypts their payload after a
//     key rotation — do it on start;
//   - active lists the active providers of every site; sync schedules for
//     any other provider (removed from SEMAR) are deleted.
func ReconcileSchedules(ctx context.Context, c client.Client, taskQueue string, every time.Duration, own, active []string, rewriteExisting bool) (ReconcileResult, error) {
	return reconcile(ctx, temporalSchedules{c: c, taskQueue: taskQueue, every: every}, own, active, rewriteExisting)
}

func reconcile(ctx context.Context, ops scheduleOps, own, active []string, rewriteExisting bool) (ReconcileResult, error) {
	var res ReconcileResult
	existing, err := ops.list(ctx)
	if err != nil {
		return res, fmt.Errorf("list schedules: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, id := range existing {
		have[id] = true
	}
	var errs []error
	for _, p := range own {
		if have[SyncScheduleID(p)] && !rewriteExisting {
			here, err := ops.servedHere(ctx, SyncScheduleID(p))
			if err != nil {
				errs = append(errs, fmt.Errorf("describe schedule %s: %w", SyncScheduleID(p), err))
				continue
			}
			if here {
				continue
			}
		}
		if err := ops.ensure(ctx, p); err != nil {
			errs = append(errs, err)
			continue
		}
		res.Ensured = append(res.Ensured, p)
	}
	keep := make(map[string]bool, len(active))
	for _, p := range active {
		keep[SyncScheduleID(p)] = true
	}
	for _, id := range existing {
		if !strings.HasPrefix(id, syncSchedulePrefix) || keep[id] {
			continue
		}
		if err := ops.remove(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("delete schedule %s: %w", id, err))
			continue
		}
		res.Removed = append(res.Removed, id)
	}
	return res, errors.Join(errs...)
}
