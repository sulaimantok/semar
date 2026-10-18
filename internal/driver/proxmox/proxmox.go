// Package proxmox is the read-only Proxmox VE driver (roadmap §3.1). One
// GET /cluster/resources call returns the nodes, VMs and templates of a
// whole cluster (technical notes §1); the endpoints of several nodes give
// API failover.
package proxmox

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/httpx"
)

// Secret field names accepted in a provider credential (an API token).
const (
	SecretTokenID     = "token_id"
	SecretTokenSecret = "token_secret"
)

// Config describes one PVE cluster.
type Config struct {
	// Endpoints are the API URLs of the cluster's nodes, e.g.
	// https://pve1.example.com:8006; they are tried in order on failure.
	Endpoints []string
	// TokenID is "user@realm!tokenname"; the token needs PVEAuditor on /
	// for discovery (technical notes §3).
	TokenID     string
	TokenSecret string
	HTTPClient  *http.Client
}

// ConfigFromSecret fills the credential fields of cfg from a stored secret.
func ConfigFromSecret(cfg Config, secret map[string]string) (Config, error) {
	cfg.TokenID, cfg.TokenSecret = secret[SecretTokenID], secret[SecretTokenSecret]
	if cfg.TokenID == "" || cfg.TokenSecret == "" || !strings.Contains(cfg.TokenID, "!") {
		return cfg, fmt.Errorf("%w: Proxmox credential needs %s (user@realm!name) and %s",
			contract.ErrUnauthorized, SecretTokenID, SecretTokenSecret)
	}
	return cfg, nil
}

const (
	mib = 1 << 20
	// agentConcurrency bounds parallel guest-agent calls for VM IPs.
	agentConcurrency = 8
)

// Driver is safe for concurrent use.
type Driver struct {
	cfg Config
	// preferred is the index of the endpoint that answered last.
	preferred atomic.Int32
}

var _ contract.CloudDriver = (*Driver)(nil)

// New returns a driver; no request is made until the first call.
func New(cfg Config) (*Driver, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, errors.New("proxmox: at least one endpoint is required")
	}
	for i, e := range cfg.Endpoints {
		cfg.Endpoints[i] = strings.TrimRight(strings.TrimSpace(e), "/")
	}
	if cfg.HTTPClient == nil {
		var err error
		if cfg.HTTPClient, err = httpx.Client(30*time.Second, ""); err != nil {
			return nil, err
		}
	}
	return &Driver{cfg: cfg}, nil
}

// Type implements contract.CloudDriver.
func (d *Driver) Type() contract.ProviderType { return contract.ProviderProxmox }

// Capabilities implements contract.CloudDriver.
func (d *Driver) Capabilities() contract.Capabilities {
	return contract.Capabilities{
		Architectures:  []contract.Arch{contract.ArchAMD64, contract.ArchARM64},
		Volumes:        true,
		SnapshotRevert: true,
		SnapshotMemory: true,
		Hotplug:        true,
		ConsoleVNC:     true,
		ConsoleSerial:  true,
	}
}

// get calls GET /api2/json<path> and decodes its "data" member. Transport
// failures and 5xx answers move on to the next endpoint; authorization and
// not-found answers do not, since every node would give the same answer.
func (d *Driver) get(ctx context.Context, path string, out any) error {
	_, err := d.do(ctx, http.MethodGet, path, nil, out)
	return err
}

// do sends a request with failover and returns the endpoint that answered.
func (d *Driver) do(ctx context.Context, method, path string, form url.Values, out any) (string, error) {
	n := len(d.cfg.Endpoints)
	start := int(d.preferred.Load())
	var lastErr error
	for i := range n {
		idx := (start + i) % n
		env := struct {
			Data any `json:"data"`
		}{Data: out}
		_, err := httpx.Do(ctx, d.cfg.HTTPClient, httpx.Request{
			Method: method,
			URL:    d.cfg.Endpoints[idx] + "/api2/json" + path,
			Header: d.authHeader(),
			Form:   form,
		}, &env)
		if err == nil {
			d.preferred.Store(int32(idx)) //nolint:gosec // G115: idx < len(Endpoints), a handful of nodes
			return d.cfg.Endpoints[idx], nil
		}
		if ctx.Err() != nil || !errors.Is(err, contract.ErrUnavailable) {
			return "", err
		}
		lastErr = err
	}
	return "", lastErr
}

func (d *Driver) authHeader() http.Header {
	return http.Header{"Authorization": {"PVEAPIToken=" + d.cfg.TokenID + "=" + d.cfg.TokenSecret}}
}

// Validate implements contract.CloudDriver. The identity is the cluster
// name plus its node names (a standalone node: its name and address), so
// two clusters that share a name are still told apart.
func (d *Driver) Validate(ctx context.Context) (contract.ProviderInfo, error) {
	var v struct {
		Version string `json:"version"`
		Release string `json:"release"`
	}
	if err := d.get(ctx, "/version", &v); err != nil {
		return contract.ProviderInfo{}, err
	}
	var status []struct {
		Type string `json:"type"`
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := d.get(ctx, "/cluster/status", &status); err != nil {
		return contract.ProviderInfo{}, err
	}
	var cluster string
	var nodes []string
	var nodeIP string
	for _, s := range status {
		switch s.Type {
		case "cluster":
			cluster = s.Name
		case "node":
			nodes = append(nodes, s.Name)
			nodeIP = s.IP
		}
	}
	sort.Strings(nodes)
	var fp string
	switch {
	case cluster != "":
		fp = "proxmox:cluster:" + cluster + ":" + strings.Join(nodes, ",")
	case len(nodes) == 1:
		fp = "proxmox:node:" + nodes[0] + "@" + nodeIP
	default:
		return contract.ProviderInfo{}, fmt.Errorf("cluster status lists no cluster or node: %w", contract.ErrUnavailable)
	}
	return contract.ProviderInfo{Fingerprint: fp, APIVersions: map[string]string{"pve": v.Version}}, nil
}

// --- Inventory ---

type resource struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Node     string  `json:"node"`
	Status   string  `json:"status"`
	Name     string  `json:"name"`
	VMID     int     `json:"vmid"`
	MaxCPU   float64 `json:"maxcpu"`
	MaxMem   int64   `json:"maxmem"`
	Pool     string  `json:"pool"`
	Tags     string  `json:"tags"`
	Template int     `json:"template"`
	Lock     string  `json:"lock"`
}

type inventory struct {
	nodes []resource
	vms   []resource
	tmpls []resource
	arch  map[string]contract.Arch
}

// inventory reads the cluster's resources and each online node's arch.
func (d *Driver) inventory(ctx context.Context) (inventory, error) {
	var rs []resource
	if err := d.get(ctx, "/cluster/resources", &rs); err != nil {
		return inventory{}, err
	}
	inv := inventory{arch: map[string]contract.Arch{}}
	for _, r := range rs {
		switch {
		case r.Type == "node":
			inv.nodes = append(inv.nodes, r)
		case r.Type == "qemu" && r.Template == 1:
			inv.tmpls = append(inv.tmpls, r)
		case r.Type == "qemu":
			inv.vms = append(inv.vms, r)
		}
	}
	for _, n := range inv.nodes {
		inv.arch[n.Node] = contract.ArchUnknown
		if n.Status != "online" {
			continue
		}
		var st struct {
			CurrentKernel struct {
				Machine string `json:"machine"`
			} `json:"current-kernel"`
		}
		if err := d.get(ctx, "/nodes/"+url.PathEscape(n.Node)+"/status", &st); err != nil {
			if ctx.Err() != nil {
				return inventory{}, ctx.Err()
			}
			continue // arch stays unknown; inventory is still complete
		}
		inv.arch[n.Node] = contract.NormalizeArch(st.CurrentKernel.Machine)
	}
	return inv, nil
}

// ListHypervisors implements contract.CloudDriver. Used capacity is what
// the node's VMs are allocated, matching how OpenStack reports usage.
func (d *Driver) ListHypervisors(ctx context.Context) ([]contract.HypervisorNode, error) {
	inv, err := d.inventory(ctx)
	if err != nil {
		return nil, err
	}
	type alloc struct {
		cpu int
		mem int64
	}
	used := map[string]alloc{}
	for _, vm := range inv.vms {
		a := used[vm.Node]
		a.cpu += int(vm.MaxCPU)
		a.mem += vm.MaxMem / mib
		used[vm.Node] = a
	}
	out := make([]contract.HypervisorNode, len(inv.nodes))
	for i, n := range inv.nodes {
		status := contract.NodeUnknown
		switch n.Status {
		case "online":
			status = contract.NodeUp
		case "offline":
			status = contract.NodeDown
		}
		out[i] = contract.HypervisorNode{
			ExternalID: n.Node, Name: n.Node, Arch: inv.arch[n.Node], Status: status,
			VCPUs: int(n.MaxCPU), VCPUsUsed: used[n.Node].cpu,
			MemoryMiB: n.MaxMem / mib, MemoryMiBUsed: used[n.Node].mem,
		}
	}
	return out, nil
}

func vmStatus(r resource) contract.VMStatus {
	switch {
	case r.Lock == "create" || r.Lock == "clone":
		return contract.VMBuilding
	case r.Status == "running":
		return contract.VMRunning
	case r.Status == "stopped":
		return contract.VMStopped
	case r.Status == "paused" || r.Status == "suspended":
		return contract.VMPaused
	default:
		return contract.VMUnknown
	}
}

func scope(pool string) string {
	if pool == "" {
		return ""
	}
	return "pool:" + pool
}

func tags(s string) map[string]string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == ' ' })
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		out[f] = ""
	}
	return out
}

// ListVMs implements contract.CloudDriver. VMIDs are unique within one
// cluster; LXC containers and templates are not VMs. IP addresses come
// from the QEMU guest agent and are left empty when it is not running.
func (d *Driver) ListVMs(ctx context.Context) ([]contract.VirtualMachine, error) {
	inv, err := d.inventory(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]contract.VirtualMachine, len(inv.vms))
	for i, r := range inv.vms {
		out[i] = contract.VirtualMachine{
			ExternalID: strconv.Itoa(r.VMID), Name: r.Name, Status: vmStatus(r), Arch: inv.arch[r.Node],
			HypervisorID: r.Node, ExternalScope: scope(r.Pool),
			VCPUs: int(r.MaxCPU), MemoryMiB: r.MaxMem / mib, Tags: tags(r.Tags),
		}
		if out[i].Arch == "" {
			out[i].Arch = contract.ArchUnknown
		}
	}

	sem := make(chan struct{}, agentConcurrency)
	var wg sync.WaitGroup
	for i, r := range inv.vms {
		if r.Status != "running" {
			continue
		}
		wg.Add(1)
		go func(i int, r resource) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			out[i].IPAddresses = d.guestIPs(ctx, r.Node, r.VMID)
		}(i, r)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// guestIPs asks the guest agent for addresses, skipping loopback and
// link-local ones. Any failure (no agent, agent not running) yields none.
func (d *Driver) guestIPs(ctx context.Context, node string, vmid int) []string {
	var res struct {
		Result []struct {
			Name        string `json:"name"`
			IPAddresses []struct {
				IP string `json:"ip-address"`
			} `json:"ip-addresses"`
		} `json:"result"`
	}
	path := "/nodes/" + url.PathEscape(node) + "/qemu/" + strconv.Itoa(vmid) + "/agent/network-get-interfaces"
	if err := d.get(ctx, path, &res); err != nil {
		return nil
	}
	var ips []string
	for _, iface := range res.Result {
		for _, a := range iface.IPAddresses {
			ip := net.ParseIP(a.IP)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, a.IP)
		}
	}
	return ips
}

// ListImages implements contract.CloudDriver: templates are the images.
func (d *Driver) ListImages(ctx context.Context) ([]contract.MachineImage, error) {
	inv, err := d.inventory(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]contract.MachineImage, len(inv.tmpls))
	for i, t := range inv.tmpls {
		arch := inv.arch[t.Node]
		if arch == "" {
			arch = contract.ArchUnknown
		}
		out[i] = contract.MachineImage{ExternalID: strconv.Itoa(t.VMID), Name: t.Name, Arch: arch, ExternalScope: scope(t.Pool)}
	}
	return out, nil
}

// ListNetworks implements contract.CloudDriver: Linux and OVS bridges of
// every online node (merged by name, since a bridge name is the same
// network on every node) and, where SDN is configured, its VNets.
func (d *Driver) ListNetworks(ctx context.Context) ([]contract.VirtualNetwork, error) {
	var nodes []resource
	if err := d.get(ctx, "/cluster/resources?type=node", &nodes); err != nil {
		return nil, err
	}
	byName := map[string]*contract.VirtualNetwork{}
	var order []string
	for _, n := range nodes {
		if n.Status != "online" {
			continue
		}
		var ifaces []struct {
			Iface string `json:"iface"`
			Type  string `json:"type"`
			CIDR  string `json:"cidr"`
			CIDR6 string `json:"cidr6"`
		}
		if err := d.get(ctx, "/nodes/"+url.PathEscape(n.Node)+"/network", &ifaces); err != nil {
			return nil, err
		}
		for _, f := range ifaces {
			if f.Type != "bridge" && f.Type != "OVSBridge" {
				continue
			}
			nw, ok := byName[f.Iface]
			if !ok {
				nw = &contract.VirtualNetwork{ExternalID: "bridge/" + f.Iface, Name: f.Iface}
				byName[f.Iface] = nw
				order = append(order, f.Iface)
			}
			for _, c := range []string{f.CIDR, f.CIDR6} {
				if c != "" && !slices.Contains(nw.CIDRs, c) {
					nw.CIDRs = append(nw.CIDRs, c)
				}
			}
		}
	}
	out := make([]contract.VirtualNetwork, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}

	var vnets []struct {
		VNet  string `json:"vnet"`
		Alias string `json:"alias"`
		Zone  string `json:"zone"`
	}
	switch err := d.get(ctx, "/cluster/sdn/vnets", &vnets); {
	case err == nil:
		for _, v := range vnets {
			name := v.Alias
			if name == "" {
				name = v.VNet
			}
			out = append(out, contract.VirtualNetwork{ExternalID: "sdn/" + v.VNet, Name: name})
		}
	case ctx.Err() != nil:
		return nil, ctx.Err()
	}
	// Without SDN (or the permission to read it) only bridges are listed.
	return out, nil
}

// diskKey matches the configuration keys of VM disks.
var diskKey = regexp.MustCompile(`^(scsi|virtio|sata|ide)[0-9]+$|^(efidisk|tpmstate)0$`)

// ListVolumes implements contract.CloudDriver: the disks attached to VMs,
// read from each VM's configuration (one call per VM, bounded in
// parallel). CD-ROMs and empty slots are skipped; disks not attached to
// any VM are not listed.
func (d *Driver) ListVolumes(ctx context.Context) ([]contract.Volume, error) {
	inv, err := d.inventory(ctx)
	if err != nil {
		return nil, err
	}
	perVM := make([][]contract.Volume, len(inv.vms))
	errs := make([]error, len(inv.vms))
	sem := make(chan struct{}, agentConcurrency)
	var wg sync.WaitGroup
	for i, vm := range inv.vms {
		wg.Add(1)
		go func(i int, vm resource) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			var cfg map[string]any
			path := "/nodes/" + url.PathEscape(vm.Node) + "/qemu/" + strconv.Itoa(vm.VMID) + "/config"
			if err := d.get(ctx, path, &cfg); err != nil {
				errs[i] = fmt.Errorf("vm %d config: %w", vm.VMID, err)
				return
			}
			perVM[i] = disks(cfg, vm)
		}(i, vm)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	out := []contract.Volume{}
	for _, v := range perVM {
		out = append(out, v...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExternalID < out[j].ExternalID })
	return out, nil
}

// disks parses the disk entries of a VM configuration, such as
// "local-lvm:vm-100-disk-0,size=32G".
func disks(cfg map[string]any, vm resource) []contract.Volume {
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		if diskKey.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []contract.Volume
	for _, k := range keys {
		v, _ := cfg[k].(string)
		volid, opts, _ := strings.Cut(v, ",")
		if volid == "" || volid == "none" || strings.Contains(","+opts+",", ",media=cdrom,") || !strings.Contains(volid, ":") {
			continue
		}
		size := 0
		for _, o := range strings.Split(opts, ",") {
			if s, ok := strings.CutPrefix(o, "size="); ok {
				size = sizeGiB(s)
			}
		}
		out = append(out, contract.Volume{
			ExternalID: "disk/" + volid, Name: volid + " (" + k + ")", SizeGiB: size,
			ExternalScope: scope(vm.Pool), AttachedTo: strconv.Itoa(vm.VMID),
		})
	}
	return out
}

// sizeGiB converts a Proxmox size such as "32G", "512M" or "1T" to GiB,
// rounding up.
func sizeGiB(s string) int {
	if s == "" {
		return 0
	}
	unit := s[len(s)-1]
	n, err := strconv.ParseFloat(strings.TrimRight(s, "KMGTkmgt"), 64)
	if err != nil {
		return 0
	}
	switch unit {
	case 'K', 'k':
		n /= 1 << 20
	case 'M', 'm':
		n /= 1 << 10
	case 'T', 't':
		n *= 1 << 10
	case 'G', 'g':
	default:
		n /= 1 << 30 // bytes
	}
	return int(math.Ceil(n))
}
