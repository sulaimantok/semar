package api

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

// consoleAPI is a live HTTP server with a fake provider whose VMs are in the
// inventory, so consoles can be opened end to end.
type consoleAPI struct {
	*settingsAPI
	srv  *httptest.Server
	vmID string
}

func newConsoleAPI(t *testing.T) *consoleAPI {
	t.Helper()
	a := newSettingsAPI(t)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"fake-c","type":"fake","name":"Fake","endpoints":["https://fake.example.com"]}`), http.StatusCreated, nil)
	a.expect(a.call("POST", "/api/v1/projects", `{"id":"payment","name":"Payment"}`), http.StatusCreated, nil)
	ctx := context.Background()
	d, err := registry.New(model.Provider{ID: "fake-c", Type: contract.ProviderFake})
	if err != nil {
		t.Fatal(err)
	}
	vms, _ := d.ListVMs(ctx)
	if _, err := a.st.CreateProjectMapping(ctx, model.ProjectMapping{ProjectID: "payment", ProviderID: "fake-c", ExternalScope: vms[0].ExternalScope}); err != nil {
		t.Fatal(err)
	}
	snap := store.Snapshot{ProviderID: "fake-c", ObservedAt: time.Now()}
	for _, vm := range vms {
		snap.Resources = append(snap.Resources, store.ObservedResource{Kind: store.KindVM, ExternalID: vm.ExternalID, Name: vm.Name,
			ExternalScope: vm.ExternalScope, Arch: string(vm.Arch), Status: string(vm.Status)})
	}
	if _, err := a.st.ApplySnapshot(ctx, snap, 3); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.h)
	t.Cleanup(srv.Close)
	return &consoleAPI{settingsAPI: a, srv: srv, vmID: vms[0].ExternalID}
}

func (c *consoleAPI) token(t *testing.T, kind string, want int) gen.ConsoleSession {
	t.Helper()
	var s gen.ConsoleSession
	var out any
	if want == http.StatusCreated {
		out = &s
	}
	c.expect(c.call("POST", "/api/v1/resources/fake-c/vm/"+c.vmID+"/console", `{"kind":"`+kind+`"}`), want, out)
	return s
}

// dial connects like a browser and returns the handshake's HTTP status.
func (c *consoleAPI) dial(t *testing.T, token, origin string) (*websocket.Conn, int, error) {
	t.Helper()
	h := http.Header{"Origin": {origin}}
	if c.cookie != nil {
		h.Set("Cookie", c.cookie.Name+"="+c.cookie.Value)
	}
	conn, resp, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(c.srv.URL, "http")+ConsoleWebSocketPath, &websocket.DialOptions{
		HTTPHeader: h, Subprotocols: []string{"binary", consoleTokenProtocol + token},
	})
	if conn != nil {
		conn.SetReadLimit(16 << 20)
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	return conn, status, err
}

func TestConsoleVNCEndToEnd(t *testing.T) {
	c := newConsoleAPI(t)
	s := c.token(t, "vnc", http.StatusCreated)
	if s.WebsocketPath != ConsoleWebSocketPath || time.Until(s.ExpiresAt) > ConsoleTokenTTL || len(s.Token) < 40 {
		t.Fatalf("session = %+v", s)
	}
	conn, status, err := c.dial(t, s.Token, c.srv.URL)
	if err != nil {
		t.Fatalf("dial: %v (HTTP %d)", err, status)
	}
	if conn.Subprotocol() != "binary" {
		t.Fatalf("subprotocol %q: the token must not be echoed back", conn.Subprotocol())
	}
	nc := websocket.NetConn(t.Context(), conn, websocket.MessageBinary)
	ver := make([]byte, 12)
	_, _ = io.ReadFull(nc, ver)
	_, _ = nc.Write(ver)
	sec := make([]byte, 2)
	_, _ = io.ReadFull(nc, sec)
	if sec[1] != 1 {
		t.Fatalf("browser offered security %v; the provider password must stay server-side", sec)
	}
	_, _ = nc.Write([]byte{1})
	_, _ = io.ReadFull(nc, make([]byte, 4))
	_, _ = nc.Write([]byte{1})
	init := make([]byte, 24)
	if _, err := io.ReadFull(nc, init); err != nil || binary.BigEndian.Uint16(init) != 640 {
		t.Fatalf("ServerInit %v, %v", init, err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")

	// Single use: the same token cannot open a second console.
	if _, status, err := c.dial(t, s.Token, c.srv.URL); err == nil || status != http.StatusForbidden {
		t.Fatalf("reused token: %v (status %d)", err, status)
	}
	waitForAudit(t, c.settingsAPI, "admin console.close fake-c/vm/"+c.vmID+" success")
	for _, want := range []string{
		"admin console.token fake-c/vm/" + c.vmID + " success",
		"admin console.open fake-c/vm/" + c.vmID + " success",
	} {
		if !containsString(c.auditActions(t), want) {
			t.Errorf("audit log lacks %q", want)
		}
	}
}

func TestConsoleSerialEndToEnd(t *testing.T) {
	c := newConsoleAPI(t)
	s := c.token(t, "serial", http.StatusCreated)
	conn, _, err := c.dial(t, s.Token, c.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, banner, err := conn.Read(t.Context())
	if err != nil || !strings.Contains(string(banner), "login:") {
		t.Fatalf("banner %q, %v", banner, err)
	}
	_ = conn.Write(t.Context(), websocket.MessageBinary, []byte("a"))
	if _, echo, _ := conn.Read(t.Context()); string(echo) != "a" {
		t.Fatalf("echo %q", echo)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestConsoleUpgradeRefusals(t *testing.T) {
	c := newConsoleAPI(t)
	s := c.token(t, "vnc", http.StatusCreated)

	// Cross-origin page, no token, unknown token: refused before the upgrade.
	for name, tc := range map[string]struct{ token, origin string }{
		"foreign origin": {s.Token, "https://evil.example"},
		"no token":       {"", c.srv.URL},
		"unknown token":  {"bm90LWEtdG9rZW4", c.srv.URL},
	} {
		if _, status, err := c.dial(t, tc.token, tc.origin); err == nil || status != http.StatusForbidden {
			t.Errorf("%s: %v (status %d), want 403", name, err, status)
		}
	}

	// A token is bound to the session that requested it.
	other := *c.settingsAPI
	other.cookie = nil
	other.t = t
	other.login("admin", adminPassword)
	stolen := &consoleAPI{settingsAPI: &other, srv: c.srv, vmID: c.vmID}
	if _, status, err := stolen.dial(t, s.Token, c.srv.URL); err == nil || status != http.StatusForbidden {
		t.Fatalf("token from another session: %v (status %d)", err, status)
	}
	// Anonymous: no session at all.
	anon := &consoleAPI{settingsAPI: &settingsAPI{t: t, h: c.h, st: c.st}, srv: c.srv, vmID: c.vmID}
	if _, status, err := anon.dial(t, s.Token, c.srv.URL); err == nil || status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %v (status %d)", err, status)
	}
	// The token was not consumed by the refused attempts.
	if conn, _, err := c.dial(t, s.Token, c.srv.URL); err != nil {
		t.Fatalf("legitimate use after refusals: %v", err)
	} else {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
}

func TestConsoleRolesAndValidation(t *testing.T) {
	c := newConsoleAPI(t)
	c.expect(c.call("POST", "/api/v1/resources/fake-c/vm/"+c.vmID+"/console", `{"kind":"rdp"}`), http.StatusBadRequest, nil)
	c.expect(c.call("POST", "/api/v1/resources/fake-c/vm/nope/console", `{"kind":"vnc"}`), http.StatusNotFound, nil)
	c.expect(c.call("POST", "/api/v1/projects", `{"id":"search","name":"Search"}`), http.StatusCreated, nil)

	c.loginAs(t, "viewer", auth.RoleBinding{Role: auth.RoleViewer, ProjectID: "payment"})
	c.token(t, "vnc", http.StatusForbidden)
	c.loginAs(t, "operator", auth.RoleBinding{Role: auth.RoleOperator, ProjectID: "payment"})
	c.token(t, "vnc", http.StatusCreated)
	c.loginAs(t, "other", auth.RoleBinding{Role: auth.RoleOperator, ProjectID: "search"})
	c.token(t, "vnc", http.StatusNotFound)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// waitForAudit waits for an event written when a session ends.
func waitForAudit(t *testing.T, a *settingsAPI, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if containsString(a.auditActions(t), want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("audit log lacks %q: %v", want, a.auditActions(t))
}
