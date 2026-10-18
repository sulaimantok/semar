package openstack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/contracttest"
	"github.com/sulaimantok/semar/internal/requestid"
)

// fakeCloud is an in-process OpenStack answering the subset of the APIs the
// driver uses, shaped after the upstream API references.
type fakeCloud struct {
	t   *testing.T
	srv *httptest.Server

	computeVersion string
	servers        int
	// noPlacement drops Placement from the catalog.
	noPlacement bool
	// failCompute answers every compute call with this status.
	failCompute int
	// consoleHook answers POST /servers/{id}/remote-consoles.
	consoleHook func(r *http.Request) (int, any)

	mu         sync.Mutex
	token      string
	auths      atomic.Int32
	authBodies []map[string]any
	headers    []http.Header
}

func newFakeCloud(t *testing.T) *fakeCloud {
	c := &fakeCloud{t: t, computeVersion: "2.96", servers: 1001}
	c.srv = httptest.NewServer(http.HandlerFunc(c.serve))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *fakeCloud) driver(t *testing.T, mutate ...func(*Config)) *Driver {
	cfg := Config{
		AuthURL: c.srv.URL + "/identity/v3", Region: "RegionOne",
		ApplicationCredentialID: "app-id", ApplicationCredentialSecret: "app-secret",
	}
	for _, m := range mutate {
		m(&cfg)
	}
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (c *fakeCloud) revokeToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var (
	hosts = []map[string]any{
		// x86 host reporting cpu_info; ARM host whose arch is only a trait.
		{"id": "11111111-1111-1111-1111-111111111111", "hypervisor_hostname": "cmp-x86", "state": "up", "status": "enabled",
			"cpu_info": map[string]any{"arch": "x86_64"}},
		{"id": "22222222-2222-2222-2222-222222222222", "hypervisor_hostname": "cmp-arm", "state": "down", "status": "enabled"},
	}
	inventories = map[string]any{
		"VCPU":      map[string]any{"total": 32, "reserved": 0, "allocation_ratio": 4.0},
		"MEMORY_MB": map[string]any{"total": 131072, "reserved": 4096, "allocation_ratio": 1.0},
	}
	baseTime = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

// server returns the i-th generated server. Even servers boot from the
// arm64 image on the ARM host; odd ones boot from a volume on the x86 host.
func server(i int) map[string]any {
	s := map[string]any{
		"id": fmt.Sprintf("srv-%05d", i), "name": fmt.Sprintf("vm-%d", i), "tenant_id": "proj-" + strconv.Itoa(i%3),
		"updated":   baseTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		"flavor":    map[string]any{"vcpus": 2, "ram": 4096, "original_name": "m1.medium"},
		"addresses": map[string]any{"net-a": []map[string]any{{"addr": fmt.Sprintf("192.0.2.%d", i%250+1), "version": 4}}},
		"metadata":  map[string]string{"owner": "team-a"}, "tags": []string{"prod"},
	}
	switch i % 4 {
	case 0:
		s["status"] = "ACTIVE"
	case 1:
		s["status"] = "SHUTOFF"
	case 2:
		s["status"] = "ERROR"
	default:
		s["status"] = "VERIFY_RESIZE"
	}
	if i%2 == 0 {
		s["image"] = map[string]any{"id": "img-arm"}
		s["OS-EXT-SRV-ATTR:hypervisor_hostname"] = "cmp-arm"
	} else {
		s["image"] = ""
		s["OS-EXT-SRV-ATTR:hypervisor_hostname"] = "cmp-x86"
	}
	return s
}

func paged[T any](items []T, id func(T) string, r *http.Request) []T {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	start := 0
	if m := r.URL.Query().Get("marker"); m != "" {
		start = slices.IndexFunc(items, func(it T) bool { return id(it) == m }) + 1
	}
	end := len(items)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return items[start:end]
}

func (c *fakeCloud) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.headers = append(c.headers, r.Header.Clone())
	tok := c.token
	c.mu.Unlock()
	p := r.URL.Path

	if r.Method == http.MethodPost && p == "/identity/v3/auth/tokens" {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		n := c.auths.Add(1)
		c.mu.Lock()
		c.authBodies = append(c.authBodies, body)
		c.token = "tok-" + strconv.Itoa(int(n))
		tok = c.token
		c.mu.Unlock()
		ep := func(id, path string) []map[string]any {
			return []map[string]any{
				{"id": id + "-int", "interface": "internal", "region_id": "RegionOne", "url": "http://internal.invalid" + path},
				{"id": id, "interface": "public", "region_id": "RegionOne", "url": c.srv.URL + path},
				{"id": id + "-r2", "interface": "public", "region_id": "RegionTwo", "url": "http://region-two.invalid" + path},
			}
		}
		catalog := []map[string]any{
			{"type": "compute", "endpoints": ep("ep-compute", "/compute/v2.1")},
			{"type": "image", "endpoints": ep("ep-image", "/image")},
			{"type": "network", "endpoints": ep("ep-network", "/network")},
			{"type": "volumev3", "endpoints": ep("ep-volume", "/volume/v3/proj-0")},
		}
		if !c.noPlacement {
			catalog = append(catalog, map[string]any{"type": "placement", "endpoints": ep("ep-placement", "/placement")})
		}
		w.Header().Set("X-Subject-Token", tok)
		writeJSON(w, http.StatusCreated, map[string]any{"token": map[string]any{
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "catalog": catalog,
		}})
		return
	}
	if p == "/volume/" {
		writeJSON(w, http.StatusMultipleChoices, map[string]any{"versions": []map[string]any{{"id": "v3.0", "status": "CURRENT", "version": "3.70"}}})
		return
	}
	if p == "/image/" {
		writeJSON(w, http.StatusMultipleChoices, map[string]any{"versions": []map[string]any{{"id": "v2.16", "status": "CURRENT"}}})
		return
	}
	if p == "/network/" {
		writeJSON(w, http.StatusOK, map[string]any{"versions": []map[string]any{{"id": "v2.0", "status": "CURRENT"}}})
		return
	}
	if r.Header.Get("X-Auth-Token") != tok || tok == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": 401}})
		return
	}
	if c.failCompute != 0 && strings.HasPrefix(p, "/compute") {
		writeJSON(w, c.failCompute, map[string]any{})
		return
	}

	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/remote-consoles") && c.consoleHook != nil:
		code, body := c.consoleHook(r)
		writeJSON(w, code, body)
	case p == "/compute/v2.1":
		writeJSON(w, http.StatusOK, map[string]any{"version": map[string]any{"id": "v2.1", "version": c.computeVersion, "min_version": "2.1"}})
	case p == "/compute/v2.1/os-hypervisors/detail":
		writeJSON(w, http.StatusOK, map[string]any{"hypervisors": paged(hosts, func(h map[string]any) string { return h["id"].(string) }, r)})
	case p == "/compute/v2.1/servers/detail":
		var all []map[string]any
		for i := range c.servers {
			s := server(i)
			if since := r.URL.Query().Get("changes-since"); since != "" {
				ts, _ := time.Parse(time.RFC3339, since)
				if u, _ := time.Parse(time.RFC3339, s["updated"].(string)); u.Before(ts) {
					continue
				}
			}
			all = append(all, s)
		}
		if r.URL.Query().Get("changes-since") != "" {
			all = append(all, map[string]any{"id": "srv-gone", "name": "gone", "status": "DELETED", "updated": time.Now().UTC().Format(time.RFC3339)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"servers": paged(all, func(s map[string]any) string { return s["id"].(string) }, r)})
	case p == "/placement/":
		writeJSON(w, http.StatusOK, map[string]any{"versions": []map[string]any{{"id": "v1.0", "max_version": "1.39", "min_version": "1.0"}}})
	case strings.HasSuffix(p, "/inventories"):
		writeJSON(w, http.StatusOK, map[string]any{"inventories": inventories})
	case strings.HasSuffix(p, "/usages"):
		writeJSON(w, http.StatusOK, map[string]any{"usages": map[string]int{"VCPU": 10, "MEMORY_MB": 20480}})
	case strings.HasSuffix(p, "/traits"):
		traits := []string{"COMPUTE_NET_ATTACH_INTERFACE"}
		if strings.Contains(p, "2222") {
			traits = append(traits, "HW_ARCH_AARCH64")
		}
		writeJSON(w, http.StatusOK, map[string]any{"traits": traits})
	case p == "/image/v2/images":
		writeJSON(w, http.StatusOK, map[string]any{"images": paged([]map[string]any{
			{"id": "img-arm", "name": "ubuntu-24.04-arm64", "owner": "proj-0", "hw_architecture": "aarch64"},
			{"id": "img-x86", "name": "ubuntu-24.04", "owner": "proj-0", "architecture": "x86_64"},
		}, func(m map[string]any) string { return m["id"].(string) }, r)})
	case p == "/network/v2.0/networks":
		writeJSON(w, http.StatusOK, map[string]any{"networks": []map[string]any{{"id": "net-a", "name": "net-a", "project_id": "proj-0"}}})
	case p == "/network/v2.0/security-groups":
		writeJSON(w, http.StatusOK, map[string]any{"security_groups": []map[string]any{{
			"id": "sg-1", "name": "web", "description": "HTTP from anywhere", "project_id": "proj-0",
			"security_group_rules": []map[string]any{
				{"direction": "ingress", "protocol": "tcp", "port_range_min": 443, "port_range_max": 443, "remote_ip_prefix": "0.0.0.0/0", "ethertype": "IPv4"},
				{"direction": "ingress", "remote_group_id": "sg-1", "ethertype": "IPv4"},
				{"direction": "egress", "ethertype": "IPv6"},
			},
		}}})
	case p == "/network/v2.0/subnets":
		writeJSON(w, http.StatusOK, map[string]any{"subnets": []map[string]any{
			{"id": "sub-1", "network_id": "net-a", "cidr": "192.0.2.0/24"}, {"id": "sub-2", "network_id": "net-a", "cidr": "2001:db8::/64"},
		}})
	case p == "/volume/v3/proj-0/volumes/detail":
		writeJSON(w, http.StatusOK, map[string]any{"volumes": []map[string]any{
			{"id": "vol-1", "name": "data", "size": 100, "os-vol-tenant-attr:tenant_id": "proj-1", "attachments": []map[string]any{{"server_id": "srv-00001"}}},
			{"id": "vol-2", "name": "spare", "size": 10, "attachments": []map[string]any{}},
		}})
	default:
		c.t.Errorf("fake cloud: unexpected %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestReadOnlyContract(t *testing.T) {
	c := newFakeCloud(t)
	contracttest.RunReadOnly(t, func(t *testing.T) contract.CloudDriver { return c.driver(t) })
}

func TestInventoryMapping(t *testing.T) {
	c := newFakeCloud(t)
	d := c.driver(t)
	ctx := context.Background()

	hs, err := d.ListHypervisors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 || hs[0].Arch != contract.ArchAMD64 || hs[1].Arch != contract.ArchARM64 || hs[1].Status != contract.NodeDown {
		t.Fatalf("hypervisors = %+v", hs)
	}
	// (32 - 0) × 4.0 vCPU and 131072 - 4096 MiB allocatable, from Placement.
	if hs[0].VCPUs != 128 || hs[0].VCPUsUsed != 10 || hs[0].MemoryMiB != 126976 || hs[0].MemoryMiBUsed != 20480 {
		t.Fatalf("capacity = %+v", hs[0])
	}

	vms, err := d.ListVMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(vms) != 1001 {
		t.Fatalf("%d VMs across pages, want 1001", len(vms))
	}
	byID := map[string]contract.VirtualMachine{}
	for _, vm := range vms {
		byID[vm.ExternalID] = vm
	}
	arm, bfv, resizing := byID["srv-00000"], byID["srv-00001"], byID["srv-00003"]
	if arm.Arch != contract.ArchARM64 || arm.ImageID != "img-arm" || arm.HypervisorID != hosts[1]["id"] || arm.Status != contract.VMRunning {
		t.Fatalf("image-booted VM = %+v", arm)
	}
	// Booted from a volume: no image, arch from the x86 host's cpu_info.
	if bfv.Arch != contract.ArchAMD64 || bfv.ImageID != "" || bfv.Status != contract.VMStopped {
		t.Fatalf("volume-booted VM = %+v", bfv)
	}
	if resizing.Status != contract.VMBuilding || byID["srv-00002"].Status != contract.VMError {
		t.Fatalf("status mapping: %s, %s", resizing.Status, byID["srv-00002"].Status)
	}
	if arm.VCPUs != 2 || arm.MemoryMiB != 4096 || arm.ExternalScope != "proj-0" || arm.IPAddresses[0] != "192.0.2.1" ||
		arm.Tags["owner"] != "team-a" || arm.Tags["prod"] != "" || !arm.UpdatedAt.Equal(baseTime) {
		t.Fatalf("VM fields = %+v", arm)
	}

	nets, err := d.ListNetworks(ctx)
	if err != nil || len(nets) != 1 || len(nets[0].CIDRs) != 2 {
		t.Fatalf("networks = %+v, %v", nets, err)
	}
	vols, err := d.ListVolumes(ctx)
	if err != nil || len(vols) != 2 || vols[0].AttachedTo != "srv-00001" || vols[0].SizeGiB != 100 || vols[0].ExternalScope != "proj-1" {
		t.Fatalf("volumes = %+v, %v", vols, err)
	}
}

func TestIncrementalListingDropsDeleted(t *testing.T) {
	c := newFakeCloud(t)
	vms, err := c.driver(t).ListVMsChangedSince(context.Background(), baseTime.Add(1000*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(vms) != 1 || vms[0].ExternalID != "srv-01000" {
		t.Fatalf("changed since = %+v, want only srv-01000 (deleted servers dropped)", vms)
	}
}

func TestValidateReportsIdentityAndVersions(t *testing.T) {
	c := newFakeCloud(t)
	info, err := c.driver(t).Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Fingerprint != "openstack:RegionOne:ep-compute" {
		t.Fatalf("fingerprint = %q", info.Fingerprint)
	}
	want := map[string]string{"compute": "2.96", "placement": "1.39", "volume": "3.70", "image": "2.16", "network": "2.0"}
	for k, v := range want {
		if info.APIVersions[k] != v {
			t.Errorf("APIVersions[%s] = %q, want %q (all: %v)", k, info.APIVersions[k], v, info.APIVersions)
		}
	}
	// Another URL for the same Keystone yields the same identity.
	other := c.driver(t, func(cfg *Config) { cfg.AuthURL = strings.Replace(cfg.AuthURL, "127.0.0.1", "localhost", 1) })
	if info2, err := other.Validate(context.Background()); err != nil || info2.Fingerprint != info.Fingerprint {
		t.Fatalf("second URL: %+v, %v", info2, err)
	}
}

func TestTokenIsReusedAndRenewedOn401(t *testing.T) {
	c := newFakeCloud(t)
	c.servers = 3
	d := c.driver(t)
	ctx := context.Background()
	for range 3 {
		if _, err := d.ListVMs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := c.auths.Load(); n != 1 {
		t.Fatalf("%d authentications for three listings, want 1", n)
	}
	c.revokeToken()
	if _, err := d.ListVMs(ctx); err != nil {
		t.Fatalf("after revocation: %v", err)
	}
	if n := c.auths.Load(); n != 2 {
		t.Fatalf("%d authentications after a revoked token, want 2", n)
	}
}

func TestPasswordAuthUsesSystemScopeAndAllProjects(t *testing.T) {
	c := newFakeCloud(t)
	c.servers = 2
	cfg, err := ConfigFromSecret(Config{AuthURL: c.srv.URL + "/identity/v3", Region: "RegionOne"},
		map[string]string{SecretUsername: "semar-discovery", SecretPassword: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := New(cfg)
	if _, err := d.ListVMs(context.Background()); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(c.authBodies[0])
	if !strings.Contains(string(body), `"system":{"all":true}`) || !strings.Contains(string(body), `"name":"Default"`) {
		t.Fatalf("auth body = %s", body)
	}
	if _, err := ConfigFromSecret(Config{}, map[string]string{"token": "x"}); !errors.Is(err, contract.ErrUnauthorized) {
		t.Fatalf("incomplete secret: %v", err)
	}
}

func TestMicroversionsAndRequestIDAreSent(t *testing.T) {
	c := newFakeCloud(t)
	c.servers = 1
	ctx := requestid.With(context.Background(), "0123456789abcdef0123456789abcdef")
	if _, err := c.driver(t).ListVMs(ctx); err != nil {
		t.Fatal(err)
	}
	var sawServers, sawHosts bool
	for _, h := range c.headers {
		if h.Get("X-OpenStack-Request-ID") != "req-01234567-89ab-cdef-0123-456789abcdef" {
			t.Fatalf("request without correlation ID: %v", h)
		}
		switch h.Get("OpenStack-API-Version") {
		case "compute 2.96":
			sawServers = true
		case "compute 2.87":
			sawHosts = true
		}
	}
	if !sawServers || !sawHosts {
		t.Fatalf("microversions not pinned per call (servers %v, hypervisors %v)", sawServers, sawHosts)
	}
}

func TestErrorsAreClassified(t *testing.T) {
	c := newFakeCloud(t)
	c.computeVersion = "2.40"
	if _, err := c.driver(t).ListVMs(context.Background()); !errors.Is(err, contract.ErrUnavailable) || !strings.Contains(err.Error(), "older") {
		t.Fatalf("old compute API: %v", err)
	}
	c.computeVersion = "2.96"
	for status, want := range map[int]error{503: contract.ErrUnavailable, 403: contract.ErrUnauthorized, 429: contract.ErrRateLimited} {
		c.failCompute = status
		if _, err := c.driver(t).ListVMs(context.Background()); !errors.Is(err, want) {
			t.Errorf("HTTP %d: %v, want %v", status, err, want)
		}
	}
	c.failCompute = 0
	d := c.driver(t, func(cfg *Config) { cfg.Region = "Nowhere" })
	if _, err := d.Validate(context.Background()); !errors.Is(err, contract.ErrNotFound) {
		t.Fatalf("unknown region: %v", err)
	}
}

func TestWithoutPlacementUsesNovaNumbers(t *testing.T) {
	c := newFakeCloud(t)
	c.noPlacement = true
	hs, err := c.driver(t).ListHypervisors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hs[1].Arch != contract.ArchUnknown || hs[0].VCPUs != 0 {
		t.Fatalf("hypervisors without Placement = %+v", hs)
	}
}

func TestVersionRoot(t *testing.T) {
	for in, want := range map[string]string{
		"https://cinder.example.com:8776/v3/abc":  "https://cinder.example.com:8776",
		"https://cloud.example.com/volume/v3/abc": "https://cloud.example.com/volume",
		"https://glance.example.com:9292":         "https://glance.example.com:9292",
		"https://cloud.example.com/image":         "https://cloud.example.com/image",
	} {
		if got := versionRoot(in); got != want {
			t.Errorf("versionRoot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConsoleWebSocketURL(t *testing.T) {
	for _, c := range []struct {
		in, ws, origin string
		kind           contract.ConsoleKind
	}{
		{"https://console.example.com:6080/vnc_lite.html?path=%3Ftoken%3Dabc", "wss://console.example.com:6080/?token=abc", "https://console.example.com:6080", contract.ConsoleVNC},
		{"http://console.example.com:6080/vnc_auto.html?token=abc", "ws://console.example.com:6080/?token=abc", "http://console.example.com:6080", contract.ConsoleVNC},
		{"https://cloud.example.com/novnc/vnc_lite.html?path=%3Ftoken%3Dxyz", "wss://cloud.example.com/novnc/?token=xyz", "https://cloud.example.com", contract.ConsoleVNC},
		{"ws://console.example.com:6083/?token=s1", "ws://console.example.com:6083/?token=s1", "http://console.example.com:6083", contract.ConsoleSerial},
	} {
		ws, origin, err := consoleWebSocket(c.in, c.kind)
		if err != nil || ws != c.ws || origin != c.origin {
			t.Errorf("consoleWebSocket(%q) = %q, %q, %v; want %q, %q", c.in, ws, origin, err, c.ws, c.origin)
		}
	}
	if _, _, err := consoleWebSocket("https://console.example.com:6080/vnc_lite.html", contract.ConsoleVNC); err == nil {
		t.Fatal("URL without a token accepted")
	}
}

func TestOpenConsoleAsksNovaServerSide(t *testing.T) {
	c := newFakeCloud(t)
	var body map[string]any
	c.consoleHook = func(r *http.Request) (int, any) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		proto := body["remote_console"].(map[string]any)["protocol"]
		if proto == "serial" {
			return 200, map[string]any{"remote_console": map[string]any{"protocol": "serial", "type": "serial", "url": "ws://console.example.com:6083/?token=ser"}}
		}
		return 200, map[string]any{"remote_console": map[string]any{"protocol": "vnc", "type": "novnc",
			"url": "https://console.example.com:6080/vnc_lite.html?path=%3Ftoken%3Dvnc"}}
	}
	d := c.driver(t)
	vnc, err := d.OpenConsole(context.Background(), "srv-00000", contract.ConsoleVNC)
	if err != nil || vnc.URL != "wss://console.example.com:6080/?token=vnc" || vnc.Header.Get("Origin") != "https://console.example.com:6080" || vnc.Subprotocols[0] != "binary" {
		t.Fatalf("vnc = %+v, %v", vnc, err)
	}
	ser, err := d.OpenConsole(context.Background(), "srv-00000", contract.ConsoleSerial)
	if err != nil || ser.URL != "ws://console.example.com:6083/?token=ser" || ser.Framing != contract.SerialRaw {
		t.Fatalf("serial = %+v, %v", ser, err)
	}
	if body["remote_console"].(map[string]any)["type"] != "serial" {
		t.Fatalf("request body = %v", body)
	}
}

func TestSecurityGroups(t *testing.T) {
	c := newFakeCloud(t)
	sgs, err := c.driver(t).ListSecurityGroups(context.Background())
	if err != nil || len(sgs) != 1 {
		t.Fatalf("security groups = %+v, %v", sgs, err)
	}
	g := sgs[0]
	if g.Name != "web" || g.ExternalScope != "proj-0" || len(g.Rules) != 3 ||
		g.Rules[0] != (contract.SecurityGroupRule{Direction: "ingress", Protocol: "tcp", PortMin: 443, PortMax: 443, Remote: "0.0.0.0/0", EtherType: "IPv4"}) ||
		g.Rules[1].Remote != "group:sg-1" || g.Rules[2].Protocol != "" {
		t.Fatalf("security group = %+v", g)
	}
}
