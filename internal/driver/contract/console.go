package contract

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"
)

// ConsoleKind is the type of a VM console.
type ConsoleKind string

const (
	// ConsoleVNC is a graphical console (RFB over WebSocket).
	ConsoleVNC ConsoleKind = "vnc"
	// ConsoleSerial is a text console.
	ConsoleSerial ConsoleKind = "serial"
)

// Valid reports whether k is a known kind.
func (k ConsoleKind) Valid() bool { return k == ConsoleVNC || k == ConsoleSerial }

// SerialFraming is how a serial console's bytes travel over the provider
// WebSocket.
type SerialFraming string

const (
	// SerialRaw carries terminal bytes as they are (Nova serial proxy).
	SerialRaw SerialFraming = "raw"
	// SerialPVETermproxy is Proxmox's termproxy protocol: the first message
	// is "user:ticket\n", input is sent as "0:<len>:<data>", resizes as
	// "1:<cols>:<rows>:", keep-alives as "2", output arrives raw.
	SerialPVETermproxy SerialFraming = "pve-termproxy"
)

// ConsoleTarget is where the SEMAR console proxy connects on behalf of the
// browser. It holds provider URLs, tickets and passwords, so it must never
// leave the server (roadmap §3.4).
type ConsoleTarget struct {
	Kind ConsoleKind
	// URL is the provider WebSocket (ws:// or wss://).
	URL string
	// Header is sent with the WebSocket handshake (authorization, and the
	// Origin some console proxies check).
	Header       http.Header
	Subprotocols []string
	// TLS verifies the provider endpoint (for pinned certificates); nil
	// uses the system roots.
	TLS *tls.Config
	// VNCPassword, when set, is answered by the SEMAR proxy in the RFB
	// handshake so the browser never sees it; the browser is offered a
	// console without authentication.
	VNCPassword string
	// Serial framing and the login line for SerialPVETermproxy.
	Framing   SerialFraming
	SerialAck string
	// ExpiresAt is when the provider ticket stops being accepted.
	ExpiresAt time.Time
}

// ConsoleOpener is implemented by drivers that offer VM consoles. The
// target is obtained when the browser connects, never earlier, so provider
// tickets live only for the session.
type ConsoleOpener interface {
	OpenConsole(ctx context.Context, vmExternalID string, kind ConsoleKind) (ConsoleTarget, error)
}

// ErrConsoleUnsupported is returned for a console kind the VM or provider
// does not offer.
var ErrConsoleUnsupported = errors.New("provider: console kind not supported")
