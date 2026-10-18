package openstack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

// page fetches every page of a marker-paginated listing. get returns the
// IDs of one page; listing stops at a short page, which also covers
// services with pagination disabled.
func page(ctx context.Context, get func(ctx context.Context, marker string) ([]string, error)) error {
	marker := ""
	for {
		ids, err := get(ctx, marker)
		if err != nil {
			return err
		}
		if len(ids) < pageSize {
			return nil
		}
		next := ids[len(ids)-1]
		if next == marker {
			return fmt.Errorf("pagination did not advance past %q: %w", marker, contract.ErrUnavailable)
		}
		marker = next
	}
}

func query(base map[string]string, marker string) string {
	q := url.Values{}
	for k, v := range base {
		q.Set(k, v)
	}
	q.Set("limit", strconv.Itoa(pageSize))
	if marker != "" {
		q.Set("marker", marker)
	}
	return "?" + q.Encode()
}

// --- Hypervisors ---

type novaHypervisor struct {
	ID       string          `json:"id"`
	Hostname string          `json:"hypervisor_hostname"`
	State    string          `json:"state"`
	CPUInfo  json.RawMessage `json:"cpu_info"`
	// Reported until microversion 2.87; used when Placement is missing.
	VCPUs        int   `json:"vcpus"`
	VCPUsUsed    int   `json:"vcpus_used"`
	MemoryMB     int64 `json:"memory_mb"`
	MemoryMBUsed int64 `json:"memory_mb_used"`
}

// cpuArch reads cpu_info.arch, which Nova returns as an object (2.28+) or
// as a JSON-encoded string (older).
func cpuArch(raw json.RawMessage) contract.Arch {
	var info struct {
		Arch string `json:"arch"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil || json.Unmarshal([]byte(s), &info) != nil {
			return contract.ArchUnknown
		}
	}
	return contract.NormalizeArch(info.Arch)
}

func (d *Driver) listNovaHypervisors(ctx context.Context) ([]novaHypervisor, error) {
	v, err := d.discoverVersions(ctx)
	if err != nil {
		return nil, err
	}
	mv := computeHeader(minVersion(v.compute, maxComputeHosts))
	var all []novaHypervisor
	err = page(ctx, func(ctx context.Context, marker string) ([]string, error) {
		var out struct {
			Hypervisors []novaHypervisor `json:"hypervisors"`
		}
		if err := d.call(ctx, "compute", "/os-hypervisors/detail"+query(nil, marker), mv, &out); err != nil {
			return nil, err
		}
		ids := make([]string, len(out.Hypervisors))
		for i, h := range out.Hypervisors {
			ids[i] = h.ID
		}
		all = append(all, out.Hypervisors...)
		return ids, nil
	})
	return all, err
}

type placementInventory struct {
	Total           int64   `json:"total"`
	Reserved        int64   `json:"reserved"`
	AllocationRatio float64 `json:"allocation_ratio"`
}

func (i placementInventory) capacity() int64 {
	ratio := i.AllocationRatio
	if ratio <= 0 {
		ratio = 1
	}
	return int64(float64(i.Total-i.Reserved) * ratio)
}

type placementCapacity struct {
	vcpus, vcpusUsed, memMB, memMBUsed int64
	arch                               contract.Arch
}

// placementFor reads one compute node's inventory, usage and arch traits.
// The resource provider UUID is the hypervisor ID (microversion 2.53+).
func (d *Driver) placementFor(ctx context.Context, rp string) (placementCapacity, error) {
	c := placementCapacity{arch: contract.ArchUnknown}
	var inv struct {
		Inventories map[string]placementInventory `json:"inventories"`
	}
	if err := d.call(ctx, "placement", "/resource_providers/"+url.PathEscape(rp)+"/inventories", "placement 1.0", &inv); err != nil {
		return c, err
	}
	var use struct {
		Usages map[string]int64 `json:"usages"`
	}
	if err := d.call(ctx, "placement", "/resource_providers/"+url.PathEscape(rp)+"/usages", "placement 1.0", &use); err != nil {
		return c, err
	}
	c.vcpus, c.memMB = inv.Inventories["VCPU"].capacity(), inv.Inventories["MEMORY_MB"].capacity()
	c.vcpusUsed, c.memMBUsed = use.Usages["VCPU"], use.Usages["MEMORY_MB"]

	var traits struct {
		Traits []string `json:"traits"`
	}
	// Traits need placement 1.6; a failure only loses the arch hint.
	if err := d.call(ctx, "placement", "/resource_providers/"+url.PathEscape(rp)+"/traits", "placement 1.6", &traits); err == nil {
		for _, t := range traits.Traits {
			switch t {
			case "HW_ARCH_X86_64":
				c.arch = contract.ArchAMD64
			case "HW_ARCH_AARCH64":
				c.arch = contract.ArchARM64
			}
		}
	} else if ctx.Err() != nil {
		return c, ctx.Err()
	}
	return c, nil
}

// ListHypervisors implements contract.CloudDriver. Capacity comes from
// Placement (allocatable = (total - reserved) × allocation ratio; used =
// allocations), falling back to Nova's own fields when Placement is absent.
func (d *Driver) ListHypervisors(ctx context.Context) ([]contract.HypervisorNode, error) {
	hs, err := d.listNovaHypervisors(ctx)
	if err != nil {
		return nil, err
	}
	hasPlacement, err := d.hasService(ctx, "placement")
	if err != nil {
		return nil, err
	}
	out := make([]contract.HypervisorNode, len(hs))
	errs := make([]error, len(hs))
	sem := make(chan struct{}, placementConcurrency)
	var wg sync.WaitGroup
	for i, h := range hs {
		n := contract.HypervisorNode{
			ExternalID: h.ID, Name: h.Hostname, Arch: cpuArch(h.CPUInfo),
			Status: nodeStatus(h.State),
			VCPUs:  h.VCPUs, VCPUsUsed: h.VCPUsUsed, MemoryMiB: h.MemoryMB, MemoryMiBUsed: h.MemoryMBUsed,
		}
		out[i] = n
		if !hasPlacement {
			continue
		}
		wg.Add(1)
		go func(i int, rp string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			c, err := d.placementFor(ctx, rp)
			if errors.Is(err, contract.ErrNotFound) {
				return // not a Placement provider (e.g. ironic); keep Nova's numbers
			}
			if err != nil {
				errs[i] = fmt.Errorf("placement %s: %w", rp, err)
				return
			}
			out[i].VCPUs, out[i].VCPUsUsed = int(c.vcpus), int(c.vcpusUsed)
			out[i].MemoryMiB, out[i].MemoryMiBUsed = c.memMB, c.memMBUsed
			if out[i].Arch == contract.ArchUnknown {
				out[i].Arch = c.arch
			}
		}(i, h.ID)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

func nodeStatus(state string) contract.NodeStatus {
	switch strings.ToLower(state) {
	case "up":
		return contract.NodeUp
	case "down":
		return contract.NodeDown
	default:
		return contract.NodeUnknown
	}
}

// --- Servers ---

type novaServer struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	TenantID string            `json:"tenant_id"`
	Updated  time.Time         `json:"updated"`
	Host     string            `json:"OS-EXT-SRV-ATTR:hypervisor_hostname"`
	Metadata map[string]string `json:"metadata"`
	Tags     []string          `json:"tags"`
	// Image is {"id": …} or "" for a server booted from a volume.
	Image  json.RawMessage `json:"image"`
	Flavor struct {
		VCPUs int   `json:"vcpus"`
		RAM   int64 `json:"ram"`
	} `json:"flavor"`
	Addresses map[string][]struct {
		Addr string `json:"addr"`
	} `json:"addresses"`
	VolumesAttached []struct {
		ID string `json:"id"`
	} `json:"os-extended-volumes:volumes_attached"`
}

func (s novaServer) imageID() string {
	var img struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(s.Image, &img) != nil {
		return ""
	}
	return img.ID
}

// vmStatus maps Nova server states to the contract's normalized states.
func vmStatus(s string) contract.VMStatus {
	switch strings.ToUpper(s) {
	case "ACTIVE", "RESCUE":
		return contract.VMRunning
	case "SHUTOFF", "SUSPENDED", "SHELVED", "SHELVED_OFFLOADED", "STOPPED":
		return contract.VMStopped
	case "PAUSED":
		return contract.VMPaused
	case "BUILD", "REBUILD", "RESIZE", "VERIFY_RESIZE", "REVERT_RESIZE", "MIGRATING", "REBOOT", "HARD_REBOOT", "PASSWORD":
		return contract.VMBuilding
	case "ERROR":
		return contract.VMError
	default:
		return contract.VMUnknown
	}
}

// ListVMs implements contract.CloudDriver.
func (d *Driver) ListVMs(ctx context.Context) ([]contract.VirtualMachine, error) {
	return d.listServers(ctx, time.Time{})
}

// ListVMsChangedSince implements contract.IncrementalVMLister with Nova's
// changes-since filter. Deleted servers are dropped; deletions are found
// by full syncs (roadmap §3.6).
func (d *Driver) ListVMsChangedSince(ctx context.Context, since time.Time) ([]contract.VirtualMachine, error) {
	return d.listServers(ctx, since)
}

func (d *Driver) listServers(ctx context.Context, since time.Time) ([]contract.VirtualMachine, error) {
	v, err := d.discoverVersions(ctx)
	if err != nil {
		return nil, err
	}
	mv := computeHeader(minVersion(v.compute, maxComputeServers))
	params := map[string]string{}
	if d.allProjects() {
		params["all_tenants"] = "1"
	}
	if !since.IsZero() {
		params["changes-since"] = since.UTC().Format(time.RFC3339)
	}
	var servers []novaServer
	err = page(ctx, func(ctx context.Context, marker string) ([]string, error) {
		var out struct {
			Servers []novaServer `json:"servers"`
		}
		if err := d.call(ctx, "compute", "/servers/detail"+query(params, marker), mv, &out); err != nil {
			return nil, err
		}
		ids := make([]string, len(out.Servers))
		for i, s := range out.Servers {
			ids[i] = s.ID
		}
		servers = append(servers, out.Servers...)
		return ids, nil
	})
	if err != nil {
		return nil, err
	}

	// Arch: image properties first, then the host's arch (technical notes §1).
	hosts, err := d.listNovaHypervisors(ctx)
	if err != nil && !errors.Is(err, contract.ErrUnauthorized) {
		return nil, err
	}
	hostID := make(map[string]string, len(hosts))
	hostArch := make(map[string]contract.Arch, len(hosts))
	for _, h := range hosts {
		hostID[h.Hostname] = h.ID
		hostArch[h.ID] = cpuArch(h.CPUInfo)
	}
	imageArch := map[string]contract.Arch{}
	if images, err := d.ListImages(ctx); err == nil {
		for _, img := range images {
			imageArch[img.ExternalID] = img.Arch
		}
	} else if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	out := make([]contract.VirtualMachine, 0, len(servers))
	for _, s := range servers {
		if strings.EqualFold(s.Status, "DELETED") || strings.EqualFold(s.Status, "SOFT_DELETED") {
			continue
		}
		vm := contract.VirtualMachine{
			ExternalID: s.ID, Name: s.Name, Status: vmStatus(s.Status), Arch: contract.ArchUnknown,
			HypervisorID: hostID[s.Host], ExternalScope: s.TenantID,
			VCPUs: s.Flavor.VCPUs, MemoryMiB: s.Flavor.RAM, ImageID: s.imageID(), UpdatedAt: s.Updated,
		}
		if a, ok := imageArch[vm.ImageID]; ok && a != contract.ArchUnknown {
			vm.Arch = a
		} else if a := hostArch[vm.HypervisorID]; a != "" {
			vm.Arch = a
		}
		for _, addrs := range s.Addresses {
			for _, a := range addrs {
				vm.IPAddresses = append(vm.IPAddresses, a.Addr)
			}
		}
		if len(s.Metadata) > 0 || len(s.Tags) > 0 {
			vm.Tags = make(map[string]string, len(s.Metadata)+len(s.Tags))
			for k, v := range s.Metadata {
				vm.Tags[k] = v
			}
			for _, t := range s.Tags {
				vm.Tags[t] = ""
			}
		}
		out = append(out, vm)
	}
	return out, nil
}

// --- Images ---

type glanceImage struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Owner        string `json:"owner"`
	Architecture string `json:"architecture"`
	HWArch       string `json:"hw_architecture"`
}

// ListImages implements contract.CloudDriver.
func (d *Driver) ListImages(ctx context.Context) ([]contract.MachineImage, error) {
	if ok, err := d.hasService(ctx, "image"); err != nil || !ok {
		return []contract.MachineImage{}, err
	}
	var out []contract.MachineImage
	err := page(ctx, func(ctx context.Context, marker string) ([]string, error) {
		var resp struct {
			Images []glanceImage `json:"images"`
		}
		if err := d.call(ctx, "image", "/v2/images"+query(nil, marker), "", &resp); err != nil {
			return nil, err
		}
		ids := make([]string, len(resp.Images))
		for i, img := range resp.Images {
			ids[i] = img.ID
			arch := img.HWArch
			if arch == "" {
				arch = img.Architecture
			}
			out = append(out, contract.MachineImage{
				ExternalID: img.ID, Name: img.Name, Arch: contract.NormalizeArch(arch), ExternalScope: img.Owner,
			})
		}
		return ids, nil
	})
	if out == nil {
		out = []contract.MachineImage{}
	}
	return out, err
}

// --- Networks ---

// ListNetworks implements contract.CloudDriver; CIDRs come from subnets.
func (d *Driver) ListNetworks(ctx context.Context) ([]contract.VirtualNetwork, error) {
	if ok, err := d.hasService(ctx, "network"); err != nil || !ok {
		return []contract.VirtualNetwork{}, err
	}
	type network struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ProjectID string `json:"project_id"`
	}
	var nets []network
	err := page(ctx, func(ctx context.Context, marker string) ([]string, error) {
		var resp struct {
			Networks []network `json:"networks"`
		}
		if err := d.call(ctx, "network", "/v2.0/networks"+query(nil, marker), "", &resp); err != nil {
			return nil, err
		}
		ids := make([]string, len(resp.Networks))
		for i, n := range resp.Networks {
			ids[i] = n.ID
		}
		nets = append(nets, resp.Networks...)
		return ids, nil
	})
	if err != nil {
		return nil, err
	}
	cidrs := map[string][]string{}
	err = page(ctx, func(ctx context.Context, marker string) ([]string, error) {
		var resp struct {
			Subnets []struct {
				ID        string `json:"id"`
				NetworkID string `json:"network_id"`
				CIDR      string `json:"cidr"`
			} `json:"subnets"`
		}
		if err := d.call(ctx, "network", "/v2.0/subnets"+query(nil, marker), "", &resp); err != nil {
			return nil, err
		}
		ids := make([]string, len(resp.Subnets))
		for i, s := range resp.Subnets {
			ids[i] = s.ID
			cidrs[s.NetworkID] = append(cidrs[s.NetworkID], s.CIDR)
		}
		return ids, nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]contract.VirtualNetwork, len(nets))
	for i, n := range nets {
		out[i] = contract.VirtualNetwork{ExternalID: n.ID, Name: n.Name, ExternalScope: n.ProjectID, CIDRs: cidrs[n.ID]}
	}
	return out, nil
}

// --- Volumes ---

// ListVolumes implements contract.CloudDriver.
func (d *Driver) ListVolumes(ctx context.Context) ([]contract.Volume, error) {
	if ok, err := d.hasService(ctx, "volumev3"); err != nil || !ok {
		return []contract.Volume{}, err
	}
	params := map[string]string{}
	if d.allProjects() {
		params["all_tenants"] = "1"
	}
	var out []contract.Volume
	err := page(ctx, func(ctx context.Context, marker string) ([]string, error) {
		var resp struct {
			Volumes []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Size        int    `json:"size"`
				Tenant      string `json:"os-vol-tenant-attr:tenant_id"`
				Attachments []struct {
					ServerID string `json:"server_id"`
				} `json:"attachments"`
			} `json:"volumes"`
		}
		if err := d.call(ctx, "volumev3", "/volumes/detail"+query(params, marker), "volume 3.0", &resp); err != nil {
			return nil, err
		}
		ids := make([]string, len(resp.Volumes))
		for i, v := range resp.Volumes {
			ids[i] = v.ID
			vol := contract.Volume{ExternalID: v.ID, Name: v.Name, SizeGiB: v.Size, ExternalScope: v.Tenant}
			if len(v.Attachments) > 0 {
				vol.AttachedTo = v.Attachments[0].ServerID
			}
			out = append(out, vol)
		}
		return ids, nil
	})
	if out == nil {
		out = []contract.Volume{}
	}
	return out, err
}

// --- Security groups ---

var _ contract.SecurityGroupLister = (*Driver)(nil)

// ListSecurityGroups implements contract.SecurityGroupLister.
func (d *Driver) ListSecurityGroups(ctx context.Context) ([]contract.SecurityGroup, error) {
	if ok, err := d.hasService(ctx, "network"); err != nil || !ok {
		return []contract.SecurityGroup{}, err
	}
	out := []contract.SecurityGroup{}
	err := page(ctx, func(ctx context.Context, marker string) ([]string, error) {
		var resp struct {
			SecurityGroups []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				ProjectID   string `json:"project_id"`
				Rules       []struct {
					Direction     string `json:"direction"`
					Protocol      string `json:"protocol"`
					PortMin       *int   `json:"port_range_min"`
					PortMax       *int   `json:"port_range_max"`
					RemoteIP      string `json:"remote_ip_prefix"`
					RemoteGroupID string `json:"remote_group_id"`
					EtherType     string `json:"ethertype"`
				} `json:"security_group_rules"`
			} `json:"security_groups"`
		}
		if err := d.call(ctx, "network", "/v2.0/security-groups"+query(nil, marker), "", &resp); err != nil {
			return nil, err
		}
		ids := make([]string, len(resp.SecurityGroups))
		for i, g := range resp.SecurityGroups {
			ids[i] = g.ID
			sg := contract.SecurityGroup{ExternalID: g.ID, Name: g.Name, Description: g.Description, ExternalScope: g.ProjectID,
				Rules: make([]contract.SecurityGroupRule, len(g.Rules))}
			for j, r := range g.Rules {
				rule := contract.SecurityGroupRule{Direction: r.Direction, Protocol: r.Protocol, EtherType: r.EtherType, Remote: r.RemoteIP}
				if r.RemoteGroupID != "" {
					rule.Remote = "group:" + r.RemoteGroupID
				}
				if r.PortMin != nil {
					rule.PortMin = *r.PortMin
				}
				if r.PortMax != nil {
					rule.PortMax = *r.PortMax
				}
				sg.Rules[j] = rule
			}
			out = append(out, sg)
		}
		return ids, nil
	})
	return out, err
}
