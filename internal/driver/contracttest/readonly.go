// Package contracttest is the shared contract test suite every CloudDriver
// must pass. Drivers call RunReadOnly from their own _test.go files; the same
// suite runs against the Fake Driver on every PR and against lab providers
// nightly (roadmap §3.1).
package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

// Factory returns a fresh driver for one subtest.
type Factory func(t *testing.T) contract.CloudDriver

// cancelDeadline bounds how long a driver may take to notice cancellation.
const cancelDeadline = 2 * time.Second

// RunReadOnly runs the read-only (Phase 1) contract against drivers built by
// newDriver.
func RunReadOnly(t *testing.T, newDriver Factory) {
	t.Helper()

	t.Run("ValidateReturnsStableFingerprint", func(t *testing.T) {
		d := newDriver(t)
		a := mustValidate(t, d)
		b := mustValidate(t, d)
		if a.Fingerprint == "" {
			t.Fatal("empty fingerprint")
		}
		if a.Fingerprint != b.Fingerprint {
			t.Fatalf("fingerprint not stable: %q then %q", a.Fingerprint, b.Fingerprint)
		}
	})

	t.Run("CapabilitiesDeclareKnownArchitectures", func(t *testing.T) {
		caps := newDriver(t).Capabilities()
		if len(caps.Architectures) == 0 {
			t.Fatal("no architectures declared")
		}
		for _, a := range caps.Architectures {
			if a != contract.ArchAMD64 && a != contract.ArchARM64 {
				t.Errorf("capability architecture %q is not a concrete arch", a)
			}
		}
	})

	t.Run("HypervisorsAreWellFormed", func(t *testing.T) {
		d := newDriver(t)
		caps := d.Capabilities()
		for _, h := range mustListHypervisors(t, d) {
			if !h.Arch.Valid() {
				t.Errorf("hypervisor %s: invalid arch %q", h.ExternalID, h.Arch)
			}
			if h.Arch != contract.ArchUnknown && !caps.SupportsArch(h.Arch) {
				t.Errorf("hypervisor %s: arch %q not declared in capabilities", h.ExternalID, h.Arch)
			}
			if h.VCPUsUsed < 0 || h.MemoryMiBUsed < 0 || h.VCPUs < 0 || h.MemoryMiB < 0 {
				t.Errorf("hypervisor %s: negative capacity", h.ExternalID)
			}
		}
	})

	t.Run("VMsAreWellFormed", func(t *testing.T) {
		d := newDriver(t)
		hyps := indexHypervisors(mustListHypervisors(t, d))
		vms := mustListVMs(t, d)
		requireUniqueIDs(t, "vm", vmIDs(vms))
		for _, vm := range vms {
			if !vm.Status.Valid() {
				t.Errorf("vm %s: invalid status %q", vm.ExternalID, vm.Status)
			}
			if !vm.Arch.Valid() {
				t.Errorf("vm %s: invalid arch %q", vm.ExternalID, vm.Arch)
			}
			if vm.VCPUs < 0 || vm.MemoryMiB < 0 {
				t.Errorf("vm %s: negative size", vm.ExternalID)
			}
			if vm.HypervisorID == "" {
				continue
			}
			h, ok := hyps[vm.HypervisorID]
			if !ok {
				t.Errorf("vm %s: hypervisor %q not listed", vm.ExternalID, vm.HypervisorID)
				continue
			}
			// VMs cannot run on a host of a different architecture (roadmap §3.3).
			if vm.Arch != contract.ArchUnknown && h.Arch != contract.ArchUnknown && vm.Arch != h.Arch {
				t.Errorf("vm %s: arch %q differs from hypervisor %s arch %q", vm.ExternalID, vm.Arch, h.ExternalID, h.Arch)
			}
		}
	})

	t.Run("ListsReturnCallerOwnedSlices", func(t *testing.T) {
		d := newDriver(t)
		first := mustListVMs(t, d)
		if len(first) == 0 {
			t.Skip("no VMs to check")
		}
		want := first[0].Name
		first[0].Name = "mutated-by-caller"
		if first[0].Tags != nil {
			first[0].Tags["mutated-by-caller"] = "x"
		}
		second := mustListVMs(t, d)
		for _, vm := range second {
			if vm.ExternalID != first[0].ExternalID {
				continue
			}
			if vm.Name != want {
				t.Errorf("caller mutation leaked into driver state: name %q", vm.Name)
			}
			if _, ok := vm.Tags["mutated-by-caller"]; ok {
				t.Error("caller mutation leaked into driver tags")
			}
		}
	})

	t.Run("ImagesNetworksVolumesAreWellFormed", func(t *testing.T) {
		d := newDriver(t)
		ctx := context.Background()

		images, err := d.ListImages(ctx)
		if err != nil {
			t.Fatalf("ListImages: %v", err)
		}
		ids := make([]string, 0, len(images))
		for _, img := range images {
			ids = append(ids, img.ExternalID)
			if !img.Arch.Valid() {
				t.Errorf("image %s: invalid arch %q", img.ExternalID, img.Arch)
			}
		}
		requireUniqueIDs(t, "image", ids)

		nets, err := d.ListNetworks(ctx)
		if err != nil {
			t.Fatalf("ListNetworks: %v", err)
		}
		ids = ids[:0]
		for _, n := range nets {
			ids = append(ids, n.ExternalID)
		}
		requireUniqueIDs(t, "network", ids)

		vols, err := d.ListVolumes(ctx)
		if err != nil {
			t.Fatalf("ListVolumes: %v", err)
		}
		vms := make(map[string]bool)
		for _, id := range vmIDs(mustListVMs(t, d)) {
			vms[id] = true
		}
		ids = ids[:0]
		for _, v := range vols {
			ids = append(ids, v.ExternalID)
			if v.SizeGiB < 0 {
				t.Errorf("volume %s: negative size", v.ExternalID)
			}
			if v.AttachedTo != "" && !vms[v.AttachedTo] {
				t.Errorf("volume %s: attached to unlisted vm %q", v.ExternalID, v.AttachedTo)
			}
		}
		requireUniqueIDs(t, "volume", ids)
	})

	t.Run("HonoursCanceledContext", func(t *testing.T) {
		d := newDriver(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan error, 1)
		go func() {
			_, err := d.ListVMs(ctx)
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("ListVMs with canceled ctx: got %v, want context.Canceled", err)
			}
		case <-time.After(cancelDeadline):
			t.Fatalf("ListVMs ignored canceled context for %s", cancelDeadline)
		}
	})

	t.Run("IncrementalListingIsConsistent", func(t *testing.T) {
		d := newDriver(t)
		inc, ok := d.(contract.IncrementalVMLister)
		if !ok {
			t.Skip("driver does not implement IncrementalVMLister")
		}
		ctx := context.Background()
		all := vmIDs(mustListVMs(t, d))
		sinceZero, err := inc.ListVMsChangedSince(ctx, time.Time{})
		if err != nil {
			t.Fatalf("ListVMsChangedSince(zero): %v", err)
		}
		if !sameSet(all, vmIDs(sinceZero)) {
			t.Errorf("ListVMsChangedSince(zero) returned %d VMs, ListVMs returned %d", len(sinceZero), len(all))
		}
		future, err := inc.ListVMsChangedSince(ctx, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("ListVMsChangedSince(future): %v", err)
		}
		if len(future) != 0 {
			t.Errorf("ListVMsChangedSince(future) returned %d VMs, want 0", len(future))
		}
	})
}

func mustValidate(t *testing.T, d contract.CloudDriver) contract.ProviderInfo {
	t.Helper()
	info, err := d.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return info
}

func mustListHypervisors(t *testing.T, d contract.CloudDriver) []contract.HypervisorNode {
	t.Helper()
	hs, err := d.ListHypervisors(context.Background())
	if err != nil {
		t.Fatalf("ListHypervisors: %v", err)
	}
	ids := make([]string, 0, len(hs))
	for _, h := range hs {
		ids = append(ids, h.ExternalID)
	}
	requireUniqueIDs(t, "hypervisor", ids)
	return hs
}

func mustListVMs(t *testing.T, d contract.CloudDriver) []contract.VirtualMachine {
	t.Helper()
	vms, err := d.ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	return vms
}

func indexHypervisors(hs []contract.HypervisorNode) map[string]contract.HypervisorNode {
	m := make(map[string]contract.HypervisorNode, len(hs))
	for _, h := range hs {
		m[h.ExternalID] = h
	}
	return m
}

func vmIDs(vms []contract.VirtualMachine) []string {
	ids := make([]string, 0, len(vms))
	for _, vm := range vms {
		ids = append(ids, vm.ExternalID)
	}
	return ids
}

func requireUniqueIDs(t *testing.T, kind string, ids []string) {
	t.Helper()
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			t.Errorf("%s with empty external ID", kind)
			continue
		}
		if seen[id] {
			t.Errorf("duplicate %s external ID %q", kind, id)
		}
		seen[id] = true
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}
