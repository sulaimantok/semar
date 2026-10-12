package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

// SyncScheduleID is the Schedule ID for a provider's inventory sync.
func SyncScheduleID(providerID string) string { return "sync-" + providerID }

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
