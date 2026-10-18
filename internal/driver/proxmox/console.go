package proxmox

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

var _ contract.ConsoleOpener = (*Driver)(nil)

// OpenConsole implements contract.ConsoleOpener (technical notes §4):
// vncproxy or termproxy hand out a port and a ticket, then the console is
// a WebSocket to vncwebsocket on the node's API. The API token header
// cannot be set by a browser, and the VNC ticket doubles as the RFB
// password, so both stay with the SEMAR proxy. The token needs VM.Console.
func (d *Driver) OpenConsole(ctx context.Context, vmID string, kind contract.ConsoleKind) (contract.ConsoleTarget, error) {
	vmid, err := strconv.Atoi(vmID)
	if err != nil {
		return contract.ConsoleTarget{}, fmt.Errorf("vm %q: %w", vmID, contract.ErrNotFound)
	}
	var rs []resource
	if err := d.get(ctx, "/cluster/resources?type=vm", &rs); err != nil {
		return contract.ConsoleTarget{}, err
	}
	node := ""
	for _, r := range rs {
		if r.Type == "qemu" && r.VMID == vmid && r.Template == 0 {
			node = r.Node
		}
	}
	if node == "" {
		return contract.ConsoleTarget{}, fmt.Errorf("vm %s: %w", vmID, contract.ErrNotFound)
	}
	base := "/nodes/" + url.PathEscape(node) + "/qemu/" + strconv.Itoa(vmid)

	var ticket struct {
		Port   any    `json:"port"` // a number or a numeric string, depending on the version
		Ticket string `json:"ticket"`
		User   string `json:"user"`
	}
	op := "/vncproxy"
	form := url.Values{"websocket": {"1"}}
	if kind == contract.ConsoleSerial {
		op, form = "/termproxy", url.Values{}
	} else if kind != contract.ConsoleVNC {
		return contract.ConsoleTarget{}, contract.ErrConsoleUnsupported
	}
	endpoint, err := d.do(ctx, http.MethodPost, base+op, form, &ticket)
	if err != nil {
		return contract.ConsoleTarget{}, fmt.Errorf("%s: %w", strings.TrimPrefix(op, "/"), err)
	}
	port := fmt.Sprint(ticket.Port)
	if _, err := strconv.Atoi(port); err != nil || ticket.Ticket == "" {
		return contract.ConsoleTarget{}, fmt.Errorf("%s returned no port or ticket: %w", op, contract.ErrUnavailable)
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return contract.ConsoleTarget{}, err
	}
	u.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	u.Path = "/api2/json" + base + "/vncwebsocket"
	u.RawQuery = url.Values{"port": {port}, "vncticket": {ticket.Ticket}}.Encode()

	t := contract.ConsoleTarget{
		Kind: kind, URL: u.String(), Header: d.authHeader(), TLS: d.tlsConfig(),
		Subprotocols: []string{"binary"},
		// Proxmox tickets are valid for two hours; the session is
		// bounded by SEMAR's own limits.
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	if kind == contract.ConsoleVNC {
		t.VNCPassword = ticket.Ticket
	} else {
		t.Framing = contract.SerialPVETermproxy
		t.SerialAck = ticket.User + ":" + ticket.Ticket + "\n"
	}
	return t, nil
}

// tlsConfig reuses the API client's TLS settings (including a pinned
// certificate) for the console WebSocket.
func (d *Driver) tlsConfig() *tls.Config {
	if tr, ok := d.cfg.HTTPClient.Transport.(*http.Transport); ok && tr.TLSClientConfig != nil {
		return tr.TLSClientConfig.Clone()
	}
	return nil
}
