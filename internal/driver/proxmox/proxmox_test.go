package proxmox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/contracttest"
	"github.com/sulaimantok/semar/internal/driver/httpx"
)

const token = "PVEAPIToken=semar@pve!discovery=s3cret" //nolint:gosec // G101: test fixture

// fakePVE answers the subset of the PVE API the driver uses, shaped after
// the API viewer (api2/json, everything wrapped in "data").
type fakePVE struct {
	t        *testing.T
	calls    atomic.Int32
	noSDN    bool
	standalo bool
}

func data(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
}

func (f *fakePVE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	if r.Header.Get("Authorization") != token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/api2/json")
	switch {
	case p == "/version":
		data(w, map[string]any{"version": "9.2.1", "release": "9.2"})
	case p == "/cluster/status":
		if f.standalo {
			data(w, []map[string]any{{"type": "node", "name": "pve", "ip": "192.0.2.5", "online": 1}})
			return
		}
		data(w, []map[string]any{
			{"type": "cluster", "name": "lab", "nodes": 3, "quorate": 1},
			{"type": "node", "name": "pve2", "ip": "192.0.2.12"}, {"type": "node", "name": "pve1", "ip": "192.0.2.11"},
			{"type": "node", "name": "pve3", "ip": "192.0.2.13"},
		})
	case p == "/cluster/resources":
		nodes := []map[string]any{
			{"id": "node/pve1", "type": "node", "node": "pve1", "status": "online", "maxcpu": 32, "maxmem": 128 << 30},
			{"id": "node/pve2", "type": "node", "node": "pve2", "status": "online", "maxcpu": 64, "maxmem": 256 << 30},
			{"id": "node/pve3", "type": "node", "node": "pve3", "status": "offline", "maxcpu": 16, "maxmem": 64 << 30},
		}
		if r.URL.Query().Get("type") == "node" {
			data(w, nodes)
			return
		}
		data(w, append(nodes, []map[string]any{
			{"id": "qemu/100", "type": "qemu", "vmid": 100, "name": "web-1", "node": "pve1", "status": "running",
				"maxcpu": 4, "maxmem": 8 << 30, "pool": "payment", "tags": "prod;web", "template": 0},
			{"id": "qemu/101", "type": "qemu", "vmid": 101, "name": "db-1", "node": "pve1", "status": "stopped",
				"maxcpu": 8, "maxmem": 16 << 30, "template": 0},
			{"id": "qemu/102", "type": "qemu", "vmid": 102, "name": "arm-1", "node": "pve2", "status": "running",
				"maxcpu": 2, "maxmem": 4 << 30, "template": 0, "lock": "clone"},
			{"id": "qemu/9000", "type": "qemu", "vmid": 9000, "name": "ubuntu-tmpl", "node": "pve2", "status": "stopped",
				"maxcpu": 2, "maxmem": 2 << 30, "template": 1, "pool": "shared"},
			{"id": "lxc/200", "type": "lxc", "vmid": 200, "name": "ct-1", "node": "pve1", "status": "running"},
			{"id": "storage/pve1/local", "type": "storage", "node": "pve1", "storage": "local"},
		}...))
	case p == "/nodes/pve1/status":
		data(w, map[string]any{"current-kernel": map[string]any{"machine": "x86_64", "release": "6.14.8-2-pve"}})
	case p == "/nodes/pve2/status":
		data(w, map[string]any{"current-kernel": map[string]any{"machine": "aarch64"}})
	case p == "/nodes/pve1/qemu/100/agent/network-get-interfaces":
		data(w, map[string]any{"result": []map[string]any{
			{"name": "lo", "ip-addresses": []map[string]any{{"ip-address": "127.0.0.1"}, {"ip-address": "::1"}}},
			{"name": "eth0", "ip-addresses": []map[string]any{{"ip-address": "192.0.2.50"}, {"ip-address": "fe80::1"}, {"ip-address": "2001:db8::50"}}},
		}})
	case strings.HasSuffix(p, "/agent/network-get-interfaces"):
		// QEMU guest agent not running.
		w.WriteHeader(http.StatusInternalServerError)
	case p == "/nodes/pve1/network", p == "/nodes/pve2/network":
		data(w, []map[string]any{
			{"iface": "vmbr0", "type": "bridge", "cidr": "192.0.2.0/24"},
			{"iface": "vmbr1", "type": "OVSBridge", "cidr6": "2001:db8:1::/64"},
			{"iface": "eno1", "type": "eth"},
		})
	case p == "/cluster/sdn/vnets":
		if f.noSDN {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		data(w, []map[string]any{{"vnet": "vnet10", "zone": "evpn", "alias": "tenant-a"}})
	default:
		f.t.Errorf("fake PVE: unexpected %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func newDriver(t *testing.T, endpoints ...string) *Driver {
	t.Helper()
	cfg, err := ConfigFromSecret(Config{Endpoints: endpoints},
		map[string]string{SecretTokenID: "semar@pve!discovery", SecretTokenSecret: "s3cret"}) //nolint:gosec // G101: test fixture
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReadOnlyContract(t *testing.T) {
	srv := httptest.NewServer(&fakePVE{t: t})
	t.Cleanup(srv.Close)
	contracttest.RunReadOnly(t, func(t *testing.T) contract.CloudDriver { return newDriver(t, srv.URL) })
}

func TestInventoryMapping(t *testing.T) {
	srv := httptest.NewServer(&fakePVE{t: t})
	t.Cleanup(srv.Close)
	d := newDriver(t, srv.URL)
	ctx := context.Background()

	hs, err := d.ListHypervisors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 3 || hs[0].Arch != contract.ArchAMD64 || hs[1].Arch != contract.ArchARM64 ||
		hs[2].Arch != contract.ArchUnknown || hs[2].Status != contract.NodeDown {
		t.Fatalf("nodes = %+v", hs)
	}
	// pve1 runs web-1 (4 vCPU, 8 GiB) and db-1 (8 vCPU, 16 GiB).
	if hs[0].VCPUs != 32 || hs[0].VCPUsUsed != 12 || hs[0].MemoryMiB != 128*1024 || hs[0].MemoryMiBUsed != 24*1024 {
		t.Fatalf("pve1 capacity = %+v", hs[0])
	}

	vms, err := d.ListVMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(vms) != 3 {
		t.Fatalf("VMs = %+v, want 3 (no templates, no containers)", vms)
	}
	web, db, arm := vms[0], vms[1], vms[2]
	if web.ExternalID != "100" || web.Status != contract.VMRunning || web.Arch != contract.ArchAMD64 ||
		web.HypervisorID != "pve1" || web.ExternalScope != "pool:payment" || web.MemoryMiB != 8192 ||
		len(web.Tags) != 2 || strings.Join(web.IPAddresses, ",") != "192.0.2.50,2001:db8::50" {
		t.Fatalf("web-1 = %+v", web)
	}
	if db.Status != contract.VMStopped || db.ExternalScope != "" || db.IPAddresses != nil {
		t.Fatalf("db-1 = %+v", db)
	}
	if arm.Status != contract.VMBuilding || arm.Arch != contract.ArchARM64 || arm.IPAddresses != nil {
		t.Fatalf("arm-1 = %+v (no guest agent: no IPs)", arm)
	}

	imgs, err := d.ListImages(ctx)
	if err != nil || len(imgs) != 1 || imgs[0].ExternalID != "9000" || imgs[0].Arch != contract.ArchARM64 || imgs[0].ExternalScope != "pool:shared" {
		t.Fatalf("images = %+v, %v", imgs, err)
	}
	nets, err := d.ListNetworks(ctx)
	if err != nil || len(nets) != 3 || nets[0].ExternalID != "bridge/vmbr0" || nets[1].CIDRs[0] != "2001:db8:1::/64" ||
		nets[2].ExternalID != "sdn/vnet10" || nets[2].Name != "tenant-a" {
		t.Fatalf("networks = %+v, %v", nets, err)
	}
}

func TestValidateIdentity(t *testing.T) {
	f := &fakePVE{t: t}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	info, err := newDriver(t, srv.URL).Validate(context.Background())
	if err != nil || info.Fingerprint != "proxmox:cluster:lab:pve1,pve2,pve3" || info.APIVersions["pve"] != "9.2.1" {
		t.Fatalf("cluster identity = %+v, %v", info, err)
	}
	f.standalo = true
	info, err = newDriver(t, srv.URL).Validate(context.Background())
	if err != nil || info.Fingerprint != "proxmox:node:pve@192.0.2.5" {
		t.Fatalf("standalone identity = %+v, %v", info, err)
	}
}

func TestFailsOverToTheNextNode(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(down.Close)
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close() // connection refused
	f := &fakePVE{t: t}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)

	d := newDriver(t, gone.URL, down.URL, up.URL)
	if _, err := d.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The healthy node is remembered: the next call goes straight to it.
	before := f.calls.Load()
	if _, err := d.ListImages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.preferred.Load() != 2 || f.calls.Load() == before {
		t.Fatalf("preferred endpoint = %d", d.preferred.Load())
	}

	// An authorization failure is the same on every node: no failover.
	bad, _ := ConfigFromSecret(Config{Endpoints: []string{up.URL, down.URL}},
		map[string]string{SecretTokenID: "semar@pve!discovery", SecretTokenSecret: "wrong"}) //nolint:gosec // G101: test fixture
	bd, _ := New(bad)
	if _, err := bd.Validate(context.Background()); !errors.Is(err, contract.ErrUnauthorized) {
		t.Fatalf("wrong token: %v", err)
	}
	all := newDriver(t, gone.URL, down.URL)
	if _, err := all.ListVMs(context.Background()); !errors.Is(err, contract.ErrUnavailable) {
		t.Fatalf("all nodes down: %v", err)
	}
}

func TestPinnedSelfSignedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(&fakePVE{t: t})
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().Raw)
	pin := strings.ToUpper(hex.EncodeToString(sum[:]))
	pin = strings.Join(splitEvery(pin, 2), ":") // as shown by `pvenode cert info`

	client, err := httpx.Client(0, pin)
	if err != nil {
		t.Fatal(err)
	}
	d := newDriver(t, srv.URL)
	d.cfg.HTTPClient = client
	if _, err := d.Validate(context.Background()); err != nil {
		t.Fatalf("pinned certificate refused: %v", err)
	}

	wrong, _ := httpx.Client(0, strings.Repeat("ab", 32))
	d.cfg.HTTPClient = wrong
	if _, err := d.Validate(context.Background()); !errors.Is(err, contract.ErrUnavailable) {
		t.Fatalf("wrong pin: %v", err)
	}
	unpinned, _ := httpx.Client(0, "")
	d.cfg.HTTPClient = unpinned
	if _, err := d.Validate(context.Background()); err == nil {
		t.Fatal("self-signed certificate accepted without a pin")
	}
	if _, err := httpx.Client(0, "not-a-fingerprint"); err == nil {
		t.Fatal("malformed pin accepted")
	}
}

func TestSecretValidation(t *testing.T) {
	//nolint:gosec // G101: test fixtures
	for _, s := range []map[string]string{{}, {SecretTokenID: "semar@pve", SecretTokenSecret: "x"}, {SecretTokenID: "semar@pve!t"}} {
		if _, err := ConfigFromSecret(Config{}, s); !errors.Is(err, contract.ErrUnauthorized) {
			t.Errorf("ConfigFromSecret(%v) = %v", s, err)
		}
	}
}

func splitEvery(s string, n int) []string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return append(out, s)
}

func TestOpenConsoleUsesTicketsServerSide(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/api2/json")
		switch {
		case p == "/cluster/resources":
			data(w, []map[string]any{{"type": "qemu", "vmid": 100, "node": "pve1", "template": 0}})
		case r.Method == http.MethodPost && p == "/nodes/pve1/qemu/100/vncproxy":
			_ = r.ParseForm()
			form = r.PostForm.Encode()
			data(w, map[string]any{"port": "5900", "ticket": "PVEVNC:ABC+/=", "user": "semar@pve!discovery"})
		case r.Method == http.MethodPost && p == "/nodes/pve1/qemu/100/termproxy":
			data(w, map[string]any{"port": 5901, "ticket": "PVEVNC:T", "user": "semar@pve!discovery"})
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	t.Cleanup(srv.Close)
	d := newDriver(t, srv.URL)
	vnc, err := d.OpenConsole(context.Background(), "100", contract.ConsoleVNC)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	if vnc.URL != "ws://"+host+"/api2/json/nodes/pve1/qemu/100/vncwebsocket?port=5900&vncticket=PVEVNC%3AABC%2B%2F%3D" ||
		vnc.VNCPassword != "PVEVNC:ABC+/=" || vnc.Header.Get("Authorization") != token || form != "websocket=1" {
		t.Fatalf("vnc target = %+v (form %q)", vnc, form)
	}
	ser, err := d.OpenConsole(context.Background(), "100", contract.ConsoleSerial)
	if err != nil || ser.Framing != contract.SerialPVETermproxy || ser.SerialAck != "semar@pve!discovery:PVEVNC:T\n" || !strings.Contains(ser.URL, "port=5901") {
		t.Fatalf("serial target = %+v, %v", ser, err)
	}
	if _, err := d.OpenConsole(context.Background(), "999", contract.ConsoleVNC); !errors.Is(err, contract.ErrNotFound) {
		t.Fatalf("unknown VM: %v", err)
	}
}
