package contract

import (
	"context"
	"time"
)

// CloudDriver is the read-only contract every provider driver implements
// (Phase 1). Mutating operations are added as optional interfaces in Phase 2.
//
// Implementations must:
//   - be safe for concurrent use;
//   - honour ctx cancellation and deadlines;
//   - return errors that wrap the sentinels in errors.go where applicable;
//   - return fresh slices the caller may modify.
type CloudDriver interface {
	Type() ProviderType
	Capabilities() Capabilities
	Validate(ctx context.Context) (ProviderInfo, error)

	ListHypervisors(ctx context.Context) ([]HypervisorNode, error)
	ListVMs(ctx context.Context) ([]VirtualMachine, error)
	ListImages(ctx context.Context) ([]MachineImage, error)
	ListNetworks(ctx context.Context) ([]VirtualNetwork, error)
	ListVolumes(ctx context.Context) ([]Volume, error)
}

// IncrementalVMLister is implemented by drivers that can return only VMs
// changed after a point in time (e.g. Nova changes-since). Deletions are not
// guaranteed to be reported; they are detected by full sync (roadmap §3.6).
type IncrementalVMLister interface {
	ListVMsChangedSince(ctx context.Context, since time.Time) ([]VirtualMachine, error)
}
