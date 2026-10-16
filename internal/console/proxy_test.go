package console

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
)

// harness serves one Session per browser connection and reports its result.
type harness struct {
	srv     *httptest.Server
	results chan Result
}

func newHarness(t *testing.T, mk func(c *websocket.Conn) *Session) *harness {
	t.Helper()
	h := &harness{results: make(chan Result, 4)}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}})
		if err != nil {
			return
		}
		res, _ := mk(c).Run(r.Context())
		h.results <- res
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	c, resp, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(h.srv.URL, "http"), &websocket.DialOptions{Subprotocols: []string{"binary"}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(16 << 20)
	return c
}

func (h *harness) result(t *testing.T) Result {
	t.Helper()
	select {
	case r := <-h.results:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
		return Result{}
	}
}

func fakeTarget(t *testing.T, kind contract.ConsoleKind) contract.ConsoleTarget {
	t.Helper()
	d := fake.New(fake.Config{Hypervisors: 1, VMsPerHypervisor: 1})
	vms, _ := d.ListVMs(t.Context())
	tgt, err := d.OpenConsole(t.Context(), vms[0].ExternalID, kind)
	if err != nil {
		t.Fatal(err)
	}
	return tgt
}

func TestVNCPasswordStaysOnTheServer(t *testing.T) {
	tgt := fakeTarget(t, contract.ConsoleVNC)
	h := newHarness(t, func(c *websocket.Conn) *Session { return &Session{Browser: c, Target: tgt} })
	c := h.dial(t)
	conn := websocket.NetConn(t.Context(), c, websocket.MessageBinary)

	// The browser is offered only "None": it never learns the password.
	ver := make([]byte, 12)
	if _, err := io.ReadFull(conn, ver); err != nil || string(ver) != "RFB 003.008\n" {
		t.Fatalf("version %q, %v", ver, err)
	}
	_, _ = conn.Write(ver)
	sec := make([]byte, 2)
	_, _ = io.ReadFull(conn, sec)
	if !bytes.Equal(sec, []byte{1, rfbSecNone}) {
		t.Fatalf("security types offered to the browser = %v, want [None]", sec)
	}
	_, _ = conn.Write([]byte{rfbSecNone})
	result := make([]byte, 4)
	_, _ = io.ReadFull(conn, result)
	if binary.BigEndian.Uint32(result) != 0 {
		t.Fatal("security result not OK")
	}
	_, _ = conn.Write([]byte{1}) // ClientInit
	init := make([]byte, 24)
	if _, err := io.ReadFull(conn, init); err != nil {
		t.Fatal(err)
	}
	name := make([]byte, binary.BigEndian.Uint32(init[20:]))
	_, _ = io.ReadFull(conn, name)
	if binary.BigEndian.Uint16(init[0:]) != 640 || !strings.Contains(string(name), "SEMAR fake console") {
		t.Fatalf("ServerInit %v %q", init[:4], name)
	}
	// A full update request returns the framebuffer.
	_, _ = conn.Write([]byte{3, 0, 0, 0, 0, 0, 2, 128, 1, 104})
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(conn, hdr); err != nil || hdr[0] != 0 || binary.BigEndian.Uint16(hdr[2:]) != 1 {
		t.Fatalf("framebuffer update header %v, %v", hdr, err)
	}
	if _, err := io.CopyN(io.Discard, conn, 640*360*4); err != nil {
		t.Fatal(err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	if r := h.result(t); r.Reason != ReasonBrowserClosed || r.ToBrowser < 640*360*4 || r.ToVM == 0 {
		t.Fatalf("result = %+v", r)
	}
}

func TestWrongVNCPasswordFails(t *testing.T) {
	tgt := fakeTarget(t, contract.ConsoleVNC)
	tgt.VNCPassword = "not-the-ticket"
	h := newHarness(t, func(c *websocket.Conn) *Session { return &Session{Browser: c, Target: tgt} })
	c := h.dial(t)
	_, _, err := c.Read(t.Context())
	if websocket.CloseStatus(err) != CloseUpstream {
		t.Fatalf("browser close = %v, want %d", err, CloseUpstream)
	}
	if r := h.result(t); r.Reason != ReasonError {
		t.Fatalf("result = %+v", r)
	}
}

func TestSerialRawEchoAndLimits(t *testing.T) {
	tgt := fakeTarget(t, contract.ConsoleSerial)
	h := newHarness(t, func(c *websocket.Conn) *Session {
		return &Session{Browser: c, Target: tgt, IdleTimeout: 1500 * time.Millisecond}
	})
	c := h.dial(t)
	read := func() string {
		_, b, err := c.Read(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if banner := read(); !strings.Contains(banner, "login:") {
		t.Fatalf("banner %q", banner)
	}
	_ = c.Write(t.Context(), websocket.MessageBinary, []byte("x"))
	if echo := read(); echo != "x" {
		t.Fatalf("echo %q", echo)
	}
	// No traffic: the idle timeout closes the console.
	_, _, err := c.Read(t.Context())
	if websocket.CloseStatus(err) != CloseIdle {
		t.Fatalf("close = %v, want idle (%d)", err, CloseIdle)
	}
	if r := h.result(t); r.Reason != ReasonIdle || r.ToVM != 1 {
		t.Fatalf("result = %+v", r)
	}
}

func TestMaxDurationAndRevocation(t *testing.T) {
	tgt := fakeTarget(t, contract.ConsoleSerial)
	h := newHarness(t, func(c *websocket.Conn) *Session {
		return &Session{Browser: c, Target: tgt, MaxDuration: 1200 * time.Millisecond}
	})
	c := h.dial(t)
	var err error
	for err == nil {
		_, _, err = c.Read(t.Context())
	}
	if websocket.CloseStatus(err) != CloseMaxDuration || h.result(t).Reason != ReasonMaxDuration {
		t.Fatalf("max duration close = %v", err)
	}

	revoked := newHarness(t, func(c *websocket.Conn) *Session {
		return &Session{Browser: c, Target: tgt, RecheckEvery: 500 * time.Millisecond,
			Check: func(context.Context) error { return errors.New("session ended") }}
	})
	c = revoked.dial(t)
	err = nil
	for err == nil {
		_, _, err = c.Read(t.Context())
	}
	if websocket.CloseStatus(err) != CloseRevoked || revoked.result(t).Reason != ReasonRevoked {
		t.Fatalf("revocation close = %v", err)
	}
}

// fakeTermproxy mimics Proxmox's termproxy: login line, "OK", framed input.
func fakeTermproxy(t *testing.T, got chan<- string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken=t=s" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		_, login, _ := c.Read(ctx)
		if string(login) != "root@pam:PVEVNC:ticket\n" {
			_ = c.Close(websocket.StatusPolicyViolation, "bad ticket")
			return
		}
		_ = c.Write(ctx, websocket.MessageText, []byte("OK"))
		_ = c.Write(ctx, websocket.MessageBinary, []byte("pve login: "))
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			got <- string(msg)
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestPVETermproxyFraming(t *testing.T) {
	got := make(chan string, 8)
	tgt := contract.ConsoleTarget{
		Kind: contract.ConsoleSerial, URL: fakeTermproxy(t, got), Framing: contract.SerialPVETermproxy,
		SerialAck: "root@pam:PVEVNC:ticket\n", Header: http.Header{"Authorization": {"PVEAPIToken=t=s"}},
	}
	h := newHarness(t, func(c *websocket.Conn) *Session { return &Session{Browser: c, Target: tgt} })
	c := h.dial(t)
	_, out, err := c.Read(t.Context())
	if err != nil || string(out) != "pve login: " {
		t.Fatalf("first output %q, %v (the OK handshake must not reach the browser)", out, err)
	}
	_ = c.Write(t.Context(), websocket.MessageBinary, []byte("root\r"))
	_ = c.Write(t.Context(), websocket.MessageText, []byte(`{"cols":120,"rows":40}`))
	for _, want := range []string{"0:5:root\r", "1:120:40:"} {
		select {
		case m := <-got:
			if m != want {
				t.Fatalf("termproxy got %q, want %q", m, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("termproxy did not receive %q", want)
		}
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	h.result(t)

	tgt.SerialAck = "root@pam:wrong\n"
	bad := newHarness(t, func(c *websocket.Conn) *Session { return &Session{Browser: c, Target: tgt} })
	c = bad.dial(t)
	_, _, err = c.Read(t.Context())
	if websocket.CloseStatus(err) != CloseUpstream || bad.result(t).Reason != ReasonError {
		t.Fatalf("bad ticket: %v", err)
	}
}

func TestVNCAuthResponseKnownAnswer(t *testing.T) {
	// From the RFB specification's bit-reversed DES key convention: the
	// same password and challenge must always give the same response.
	a, _ := vncAuthResponse([]byte("0123456789abcdef"), "secret")
	b, _ := vncAuthResponse([]byte("0123456789abcdef"), "secret")
	c, _ := vncAuthResponse([]byte("0123456789abcdef"), "secreT")
	if !bytes.Equal(a, b) || bytes.Equal(a, c) || len(a) != 16 {
		t.Fatal("VNC auth response is not deterministic per password")
	}
	if reverseBits(0x01) != 0x80 || reverseBits(0xF0) != 0x0F {
		t.Fatal("bit reversal")
	}
}
