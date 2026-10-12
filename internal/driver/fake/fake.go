// Package fake is an in-memory CloudDriver with deterministic inventory and
// fault injection. It backs UI/dev work without a real cloud and lets tests
// exercise retries, timeouts and sync races (roadmap §3.1).
package fake

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

// Faults configures injected failures. The zero value injects nothing.
type Faults struct {
	// Latency is added to every call; the call returns early with ctx.Err()
	// if the context ends first.
	Latency time.Duration
	// ErrorRate is the probability (0..1) that a call fails with
	// contract.ErrUnavailable.
	ErrorRate float64
	// FailNext makes the next N calls fail with contract.ErrUnavailable.
	FailNext int
}

// Config controls the generated inventory. Zero fields take defaults.
type Config struct {
	Seed             uint64
	Hypervisors      int
	VMsPerHypervisor int
	Architectures    []contract.Arch
	// Scopes are the external ownership scopes (OpenStack project / PVE pool)
	// VMs are spread across.
	Scopes []string
	Faults Faults
}

func (c Config) withDefaults() Config {
	if c.Seed == 0 {
		c.Seed = 1
	}
	if c.Hypervisors <= 0 {
		c.Hypervisors = 3
	}
	if c.VMsPerHypervisor <= 0 {
		c.VMsPerHypervisor = 5
	}
	if len(c.Architectures) == 0 {
		c.Architectures = []contract.Arch{contract.ArchAMD64, contract.ArchARM64}
	}
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"proj-a", "proj-b"}
	}
	return c
}

// baseTime is the UpdatedAt origin of generated VMs, so inventories built
// from the same seed are identical.
var baseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Driver is the fake CloudDriver. It is safe for concurrent use.
type Driver struct {
	seed  uint64
	archs []contract.Arch
	now   func() time.Time

	mu          sync.Mutex
	rng         *rand.Rand
	faults      Faults
	failNext    int
	hypervisors map[string]contract.HypervisorNode
	vms         map[string]contract.VirtualMachine
	images      map[string]contract.MachineImage
	networks    map[string]contract.VirtualNetwork
	volumes     map[string]contract.Volume
}

var (
	_ contract.CloudDriver         = (*Driver)(nil)
	_ contract.IncrementalVMLister = (*Driver)(nil)
)

// New builds a fake driver with inventory generated from cfg.
func New(cfg Config) *Driver {
	cfg = cfg.withDefaults()
	d := &Driver{
		seed:        cfg.Seed,
		archs:       slices.Clone(cfg.Architectures),
		now:         time.Now,
		rng:         rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x5e3a)), //nolint:gosec // deterministic test data, not security-sensitive
		faults:      cfg.Faults,
		failNext:    cfg.Faults.FailNext,
		hypervisors: make(map[string]contract.HypervisorNode),
		vms:         make(map[string]contract.VirtualMachine),
		images:      make(map[string]contract.MachineImage),
		networks:    make(map[string]contract.VirtualNetwork),
		volumes:     make(map[string]contract.Volume),
	}
	d.generate(cfg)
	return d
}

func (d *Driver) generate(cfg Config) {
	for _, a := range cfg.Architectures {
		id := fmt.Sprintf("fake-img-ubuntu-24.04-%s", a)
		d.images[id] = contract.MachineImage{ExternalID: id, Name: "ubuntu-24.04-" + string(a), Arch: a}
	}
	for i, s := range cfg.Scopes {
		id := "fake-net-" + s
		d.networks[id] = contract.VirtualNetwork{
			ExternalID:    id,
			Name:          s + "-net",
			ExternalScope: s,
			CIDRs:         []string{fmt.Sprintf("10.%d.0.0/16", i+1)},
		}
	}

	statuses := []contract.VMStatus{contract.VMRunning, contract.VMRunning, contract.VMRunning, contract.VMStopped, contract.VMPaused, contract.VMError}
	sizes := []int{1, 2, 4, 8}
	n := 0
	for h := 0; h < cfg.Hypervisors; h++ {
		arch := cfg.Architectures[h%len(cfg.Architectures)]
		node := contract.HypervisorNode{
			ExternalID: fmt.Sprintf("fake-hv-%03d", h),
			Name:       fmt.Sprintf("hv-%03d", h),
			Arch:       arch,
			Status:     contract.NodeUp,
			VCPUs:      64,
			MemoryMiB:  256 * 1024,
		}
		for v := 0; v < cfg.VMsPerHypervisor; v++ {
			scopeIdx := d.rng.IntN(len(cfg.Scopes))
			vcpus := sizes[d.rng.IntN(len(sizes))]
			vm := contract.VirtualMachine{
				ExternalID:    fmt.Sprintf("fake-vm-%05d", n),
				Name:          fmt.Sprintf("vm-%05d", n),
				Status:        statuses[d.rng.IntN(len(statuses))],
				Arch:          arch,
				HypervisorID:  node.ExternalID,
				ExternalScope: cfg.Scopes[scopeIdx],
				VCPUs:         vcpus,
				MemoryMiB:     int64(vcpus) * 2048,
				ImageID:       fmt.Sprintf("fake-img-ubuntu-24.04-%s", arch),
				IPAddresses:   []string{fmt.Sprintf("10.%d.%d.%d", scopeIdx+1, n/250, n%250+2)},
				Tags:          map[string]string{"env": "fake"},
				UpdatedAt:     baseTime.Add(time.Duration(n) * time.Minute),
			}
			d.vms[vm.ExternalID] = vm
			node.VCPUsUsed += vm.VCPUs
			node.MemoryMiBUsed += vm.MemoryMiB

			vol := contract.Volume{
				ExternalID:    fmt.Sprintf("fake-vol-%05d", n),
				Name:          vm.Name + "-root",
				SizeGiB:       20,
				ExternalScope: vm.ExternalScope,
				AttachedTo:    vm.ExternalID,
			}
			d.volumes[vol.ExternalID] = vol
			n++
		}
		d.hypervisors[node.ExternalID] = node
	}
	d.volumes["fake-vol-detached"] = contract.Volume{
		ExternalID:    "fake-vol-detached",
		Name:          "detached",
		SizeGiB:       100,
		ExternalScope: cfg.Scopes[0],
	}
}

// SetFaults replaces the fault configuration and resets FailNext.
func (d *Driver) SetFaults(f Faults) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.faults = f
	d.failNext = f.FailNext
}

// UpsertVM inserts or replaces a VM and stamps UpdatedAt, simulating a change
// made outside SEMAR (drift).
func (d *Driver) UpsertVM(vm contract.VirtualMachine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	vm.UpdatedAt = d.now()
	d.vms[vm.ExternalID] = cloneVM(vm)
}

// DeleteVM removes a VM and reports whether it existed.
func (d *Driver) DeleteVM(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.vms[id]
	delete(d.vms, id)
	return ok
}

func (d *Driver) Type() contract.ProviderType { return contract.ProviderFake }

func (d *Driver) Capabilities() contract.Capabilities {
	return contract.Capabilities{
		Architectures:  slices.Clone(d.archs),
		FloatingIP:     true,
		SecurityGroups: true,
		Volumes:        true,
		ConsoleVNC:     true,
		ConsoleSerial:  true,
	}
}

func (d *Driver) Validate(ctx context.Context) (contract.ProviderInfo, error) {
	if err := d.inject(ctx, "validate"); err != nil {
		return contract.ProviderInfo{}, err
	}
	return contract.ProviderInfo{
		Fingerprint: fmt.Sprintf("fake://seed-%d", d.seed),
		APIVersions: map[string]string{"compute": "fake-1.0"},
	}, nil
}

func (d *Driver) ListHypervisors(ctx context.Context) ([]contract.HypervisorNode, error) {
	if err := d.inject(ctx, "list hypervisors"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return sortedValues(d.hypervisors, func(h contract.HypervisorNode) contract.HypervisorNode { return h }), nil
}

func (d *Driver) ListVMs(ctx context.Context) ([]contract.VirtualMachine, error) {
	if err := d.inject(ctx, "list vms"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return sortedValues(d.vms, cloneVM), nil
}

func (d *Driver) ListVMsChangedSince(ctx context.Context, since time.Time) ([]contract.VirtualMachine, error) {
	if err := d.inject(ctx, "list vms changed since"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]contract.VirtualMachine, 0)
	for _, id := range slices.Sorted(maps.Keys(d.vms)) {
		if vm := d.vms[id]; vm.UpdatedAt.After(since) {
			out = append(out, cloneVM(vm))
		}
	}
	return out, nil
}

func (d *Driver) ListImages(ctx context.Context) ([]contract.MachineImage, error) {
	if err := d.inject(ctx, "list images"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return sortedValues(d.images, func(i contract.MachineImage) contract.MachineImage { return i }), nil
}

func (d *Driver) ListNetworks(ctx context.Context) ([]contract.VirtualNetwork, error) {
	if err := d.inject(ctx, "list networks"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return sortedValues(d.networks, func(n contract.VirtualNetwork) contract.VirtualNetwork {
		n.CIDRs = slices.Clone(n.CIDRs)
		return n
	}), nil
}

func (d *Driver) ListVolumes(ctx context.Context) ([]contract.Volume, error) {
	if err := d.inject(ctx, "list volumes"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return sortedValues(d.volumes, func(v contract.Volume) contract.Volume { return v }), nil
}

// inject applies the configured faults for one call.
func (d *Driver) inject(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	latency := d.faults.Latency
	fail := false
	if d.failNext > 0 {
		d.failNext--
		fail = true
	} else if d.faults.ErrorRate > 0 && d.rng.Float64() < d.faults.ErrorRate {
		fail = true
	}
	d.mu.Unlock()

	if latency > 0 {
		timer := time.NewTimer(latency)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if fail {
		return fmt.Errorf("fake %s: %w", op, contract.ErrUnavailable)
	}
	return nil
}

func cloneVM(vm contract.VirtualMachine) contract.VirtualMachine {
	vm.IPAddresses = slices.Clone(vm.IPAddresses)
	vm.Tags = maps.Clone(vm.Tags)
	return vm
}

func sortedValues[T any](m map[string]T, clone func(T) T) []T {
	out := make([]T, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		out = append(out, clone(m[k]))
	}
	return out
}
