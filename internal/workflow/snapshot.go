package workflow

import (
	"context"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/store"
)

// inventory is one provider's complete listing.
type inventory struct {
	hypervisors []contract.HypervisorNode
	vms         []contract.VirtualMachine
	images      []contract.MachineImage
	networks    []contract.VirtualNetwork
	volumes     []contract.Volume
}

// collect lists everything; any error aborts so that only complete
// snapshots are ever applied (a partial one would orphan resources).
func collect(ctx context.Context, d contract.CloudDriver) (inventory, error) {
	var inv inventory
	var err error
	if inv.hypervisors, err = d.ListHypervisors(ctx); err != nil {
		return inv, classify("list hypervisors", err)
	}
	if inv.vms, err = d.ListVMs(ctx); err != nil {
		return inv, classify("list vms", err)
	}
	if inv.images, err = d.ListImages(ctx); err != nil {
		return inv, classify("list images", err)
	}
	if inv.networks, err = d.ListNetworks(ctx); err != nil {
		return inv, classify("list networks", err)
	}
	if inv.volumes, err = d.ListVolumes(ctx); err != nil {
		return inv, classify("list volumes", err)
	}
	return inv, nil
}

// snapshot converts the driver model into store resources.
func (inv inventory) snapshot(providerID string, observedAt time.Time) store.Snapshot {
	n := len(inv.hypervisors) + len(inv.vms) + len(inv.images) + len(inv.networks) + len(inv.volumes)
	rs := make([]store.ObservedResource, 0, n)
	for _, h := range inv.hypervisors {
		rs = append(rs, store.ObservedResource{
			Kind: store.KindHypervisor, ExternalID: h.ExternalID, Name: h.Name, Arch: string(h.Arch), Status: string(h.Status),
			Spec: map[string]any{"vcpus": h.VCPUs, "vcpusUsed": h.VCPUsUsed, "memoryMiB": h.MemoryMiB, "memoryMiBUsed": h.MemoryMiBUsed},
		})
	}
	for _, v := range inv.vms {
		spec := map[string]any{"vcpus": v.VCPUs, "memoryMiB": v.MemoryMiB}
		if v.HypervisorID != "" {
			spec["hypervisorId"] = v.HypervisorID
		}
		if v.ImageID != "" {
			spec["imageId"] = v.ImageID
		}
		if len(v.IPAddresses) > 0 {
			spec["ipAddresses"] = v.IPAddresses
		}
		if len(v.Tags) > 0 {
			spec["tags"] = v.Tags
		}
		rs = append(rs, store.ObservedResource{
			Kind: store.KindVM, ExternalID: v.ExternalID, Name: v.Name, ExternalScope: v.ExternalScope,
			Arch: string(v.Arch), Status: string(v.Status), Spec: spec,
		})
	}
	for _, img := range inv.images {
		rs = append(rs, store.ObservedResource{
			Kind: store.KindImage, ExternalID: img.ExternalID, Name: img.Name, ExternalScope: img.ExternalScope,
			Arch: string(img.Arch), Spec: map[string]any{},
		})
	}
	for _, n := range inv.networks {
		spec := map[string]any{}
		if len(n.CIDRs) > 0 {
			spec["cidrs"] = n.CIDRs
		}
		rs = append(rs, store.ObservedResource{
			Kind: store.KindNetwork, ExternalID: n.ExternalID, Name: n.Name, ExternalScope: n.ExternalScope, Spec: spec,
		})
	}
	for _, v := range inv.volumes {
		spec := map[string]any{"sizeGiB": v.SizeGiB}
		if v.AttachedTo != "" {
			spec["attachedTo"] = v.AttachedTo
		}
		rs = append(rs, store.ObservedResource{
			Kind: store.KindVolume, ExternalID: v.ExternalID, Name: v.Name, ExternalScope: v.ExternalScope, Spec: spec,
		})
	}
	return store.Snapshot{ProviderID: providerID, ObservedAt: observedAt, Resources: rs}
}
