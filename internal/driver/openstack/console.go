package openstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/httpx"
)

var _ contract.ConsoleOpener = (*Driver)(nil)

// minConsole is the first microversion with POST /servers/{id}/remote-consoles.
var minConsole = version{2, 6}

// OpenConsole implements contract.ConsoleOpener (technical notes §4). The
// URL returned by Nova carries a token for nova-novncproxy or
// nova-serialproxy; it is reusable until [consoleauth] token_ttl, so it is
// used only server-side and never sent to the browser.
func (d *Driver) OpenConsole(ctx context.Context, vmID string, kind contract.ConsoleKind) (contract.ConsoleTarget, error) {
	v, err := d.discoverVersions(ctx)
	if err != nil {
		return contract.ConsoleTarget{}, err
	}
	if v.compute.less(minConsole) {
		return contract.ConsoleTarget{}, contract.ErrConsoleUnsupported
	}
	body := map[string]any{"remote_console": map[string]string{"protocol": "vnc", "type": "novnc"}}
	if kind == contract.ConsoleSerial {
		body = map[string]any{"remote_console": map[string]string{"protocol": "serial", "type": "serial"}}
	} else if kind != contract.ConsoleVNC {
		return contract.ConsoleTarget{}, contract.ErrConsoleUnsupported
	}
	var out struct {
		RemoteConsole struct {
			URL string `json:"url"`
		} `json:"remote_console"`
	}
	path := "/servers/" + url.PathEscape(vmID) + "/remote-consoles"
	if err := d.send(ctx, http.MethodPost, "compute", path, computeHeader(minVersion(v.compute, maxComputeServers)), body, &out); err != nil {
		// Nova answers 400 "Unavailable console type <type>." when that
		// console proxy is not enabled (serial is off by default).
		var se *httpx.StatusError
		if errors.As(err, &se) && se.Status == http.StatusBadRequest && strings.Contains(se.Message, "Unavailable console type") {
			return contract.ConsoleTarget{}, fmt.Errorf("remote console: %w: %w", contract.ErrConsoleUnsupported, err)
		}
		return contract.ConsoleTarget{}, fmt.Errorf("remote console: %w", err)
	}
	ws, origin, err := consoleWebSocket(out.RemoteConsole.URL, kind)
	if err != nil {
		return contract.ConsoleTarget{}, err
	}
	t := contract.ConsoleTarget{
		Kind: kind, URL: ws,
		// The console proxies compare Origin with their own host
		// (novncproxy's CVE-2015-0259 check).
		Header:    http.Header{"Origin": {origin}},
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	if kind == contract.ConsoleVNC {
		t.Subprotocols = []string{"binary"}
	} else {
		t.Framing = contract.SerialRaw
	}
	return t, nil
}

// consoleWebSocket turns Nova's console URL into the proxy's WebSocket URL
// and the Origin it expects. noVNC URLs carry the token either directly
// (?token=…) or inside the path parameter (?path=%3Ftoken%3D…); the
// WebSocket is served at the proxy's root. Serial URLs are WebSockets
// already.
func consoleWebSocket(raw string, kind contract.ConsoleKind) (wsURL, origin string, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("remote console URL %q: %w", raw, contract.ErrUnavailable)
	}
	httpScheme := map[string]string{"ws": "http", "wss": "https", "http": "http", "https": "https"}[u.Scheme]
	wsScheme := map[string]string{"http": "ws", "https": "wss"}[httpScheme]
	if wsScheme == "" {
		return "", "", fmt.Errorf("remote console URL scheme %q: %w", u.Scheme, contract.ErrUnavailable)
	}
	origin = httpScheme + "://" + u.Host
	if kind == contract.ConsoleSerial {
		u.Scheme = wsScheme
		return u.String(), origin, nil
	}
	q := u.Query()
	token := q.Get("token")
	if p := q.Get("path"); token == "" && p != "" {
		if pu, err := url.Parse(p); err == nil {
			token = pu.Query().Get("token")
		}
	}
	if token == "" {
		return "", "", fmt.Errorf("remote console URL has no token: %w", contract.ErrUnavailable)
	}
	base := strings.TrimSuffix(u.Path, "/")
	if i := strings.LastIndexByte(base, '/'); i >= 0 && strings.HasSuffix(base, ".html") {
		base = base[:i] // …/vnc_lite.html → served next to it
	}
	return wsScheme + "://" + u.Host + base + "/?" + url.Values{"token": {token}}.Encode(), origin, nil
}
