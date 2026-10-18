package fake

import (
	"bufio"
	"context"
	"crypto/des" //nolint:gosec // G502: RFB VNC authentication is defined with DES
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

// ConsolePassword is the VNC password the fake console demands. The SEMAR
// proxy must answer it, exactly as it answers a Proxmox ticket.
const ConsolePassword = "fake-ticket"

var (
	consoleOnce sync.Once
	consoleURL  string
	consoleErr  error
)

// consoleServer starts, once per process, a loopback server with a minimal
// RFB server (/vnc) and an echo terminal (/serial).
func consoleServer() (string, error) {
	consoleOnce.Do(func() {
		var lc net.ListenConfig
		ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			consoleErr = err
			return
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/vnc", serveFakeVNC)
		mux.HandleFunc("/serial", serveFakeSerial)
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		consoleURL = "ws://" + ln.Addr().String()
	})
	return consoleURL, consoleErr
}

// OpenConsole implements contract.ConsoleOpener.
func (d *Driver) OpenConsole(ctx context.Context, vmID string, kind contract.ConsoleKind) (contract.ConsoleTarget, error) {
	if err := d.inject(ctx, "open console"); err != nil {
		return contract.ConsoleTarget{}, err
	}
	d.mu.Lock()
	vm, ok := d.vms[vmID]
	d.mu.Unlock()
	if !ok {
		return contract.ConsoleTarget{}, fmt.Errorf("vm %s: %w", vmID, contract.ErrNotFound)
	}
	base, err := consoleServer()
	if err != nil {
		return contract.ConsoleTarget{}, fmt.Errorf("fake console: %w: %w", contract.ErrUnavailable, err)
	}
	q := "?" + url.Values{"vm": {vm.ExternalID}, "name": {vm.Name}}.Encode()
	switch kind {
	case contract.ConsoleVNC:
		return contract.ConsoleTarget{Kind: kind, URL: base + "/vnc" + q, Subprotocols: []string{"binary"},
			VNCPassword: ConsolePassword, ExpiresAt: time.Now().Add(time.Minute)}, nil
	case contract.ConsoleSerial:
		return contract.ConsoleTarget{Kind: kind, URL: base + "/serial" + q, Framing: contract.SerialRaw,
			ExpiresAt: time.Now().Add(time.Minute)}, nil
	default:
		return contract.ConsoleTarget{}, contract.ErrConsoleUnsupported
	}
}

// --- Fake RFB server ---

const (
	fbWidth  = 640
	fbHeight = 360
)

func serveFakeVNC(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	conn := websocket.NetConn(r.Context(), c, websocket.MessageBinary)
	_ = runFakeRFB(conn, r.URL.Query().Get("name"))
}

// runFakeRFB speaks enough RFB 3.8 for noVNC: VNC authentication, a
// 640×360 true-colour framebuffer of colour bars, and a colour change on
// every key press so that input is visibly delivered.
func runFakeRFB(c io.ReadWriter, name string) error {
	br := bufio.NewReader(c)
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(br, ver); err != nil {
		return err
	}
	if _, err := c.Write([]byte{1, 2}); err != nil { // one security type: VNC auth
		return err
	}
	if _, err := br.ReadByte(); err != nil {
		return err
	}
	challenge := []byte("semar-fake-chall")
	if _, err := c.Write(challenge); err != nil {
		return err
	}
	resp := make([]byte, 16)
	if _, err := io.ReadFull(br, resp); err != nil {
		return err
	}
	if !checkVNCAuth(challenge, resp, ConsolePassword) {
		reason := "authentication failed"
		out := binary.BigEndian.AppendUint32([]byte{0, 0, 0, 1}, uint32(len(reason))) //nolint:gosec // G115: constant short string
		_, _ = c.Write(append(out, reason...))
		return errors.New("fake rfb: wrong password")
	}
	if _, err := c.Write([]byte{0, 0, 0, 0}); err != nil {
		return err
	}
	if _, err := br.ReadByte(); err != nil { // ClientInit (shared flag)
		return err
	}
	title := "SEMAR fake console — " + name
	init := []byte{}
	init = binary.BigEndian.AppendUint16(init, fbWidth)
	init = binary.BigEndian.AppendUint16(init, fbHeight)
	// 32 bpp, depth 24, little endian, true colour, max 255, shifts 16/8/0.
	init = append(init, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0)
	if len(title) > 255 {
		title = title[:255]
	}
	init = binary.BigEndian.AppendUint32(init, uint32(len(title))) //nolint:gosec // G115: bounded to 255 above
	init = append(init, title...)
	if _, err := c.Write(init); err != nil {
		return err
	}

	tint := byte(0)
	for {
		typ, err := br.ReadByte()
		if err != nil {
			return err
		}
		switch typ {
		case 0: // SetPixelFormat
			_, err = br.Discard(19)
		case 2: // SetEncodings
			var hdr [3]byte
			if _, err = io.ReadFull(br, hdr[:]); err == nil {
				_, err = br.Discard(4 * int(binary.BigEndian.Uint16(hdr[1:])))
			}
		case 3: // FramebufferUpdateRequest
			var req [9]byte
			if _, err = io.ReadFull(br, req[:]); err == nil && req[0] == 0 {
				err = writeFrame(c, tint)
			}
		case 4: // KeyEvent
			var ev [7]byte
			if _, err = io.ReadFull(br, ev[:]); err == nil && ev[0] == 1 {
				tint += 40
				err = writeFrame(c, tint)
			}
		case 5: // PointerEvent
			_, err = br.Discard(5)
		case 6: // ClientCutText
			var hdr [7]byte
			if _, err = io.ReadFull(br, hdr[:]); err == nil {
				_, err = br.Discard(int(binary.BigEndian.Uint32(hdr[3:])))
			}
		default:
			return fmt.Errorf("fake rfb: unsupported client message %d", typ)
		}
		if err != nil {
			return err
		}
	}
}

func writeFrame(w io.Writer, tint byte) error {
	bars := [][3]byte{{230, 80, 60}, {240, 180, 50}, {80, 190, 110}, {60, 140, 230}, {150, 90, 210}}
	out := make([]byte, 0, 16+fbWidth*fbHeight*4)
	out = append(out, 0, 0, 0, 1) // FramebufferUpdate, one rectangle
	out = binary.BigEndian.AppendUint16(out, 0)
	out = binary.BigEndian.AppendUint16(out, 0)
	out = binary.BigEndian.AppendUint16(out, fbWidth)
	out = binary.BigEndian.AppendUint16(out, fbHeight)
	out = binary.BigEndian.AppendUint32(out, 0) // raw encoding
	for y := 0; y < fbHeight; y++ {
		for x := 0; x < fbWidth; x++ {
			c := bars[x*len(bars)/fbWidth]
			// Pixel bytes in little-endian 0x00RRGGBB order: B, G, R, 0.
			out = append(out, c[2]+tint, c[1]+tint/2, c[0], 0)
		}
	}
	_, err := w.Write(out)
	return err
}

func checkVNCAuth(challenge, response []byte, password string) bool {
	key := make([]byte, 8)
	copy(key, password)
	for i, b := range key {
		var r byte
		for j := 0; j < 8; j++ {
			r = r<<1 | b&1
			b >>= 1
		}
		key[i] = r
	}
	block, err := des.NewCipher(key) //nolint:gosec // G405: protocol-mandated DES
	if err != nil {
		return false
	}
	want := make([]byte, 16)
	block.Encrypt(want[:8], challenge[:8])
	block.Encrypt(want[8:], challenge[8:])
	return string(want) == string(response)
}

// --- Fake serial terminal ---

func serveFakeSerial(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	ctx := r.Context()
	name := r.URL.Query().Get("name")
	write := func(s string) error { return c.Write(ctx, websocket.MessageBinary, []byte(s)) }
	if write("\r\nSEMAR fake serial console — "+name+"\r\n\r\n"+name+" login: ") != nil {
		return
	}
	var line []byte
	for {
		_, msg, err := c.Read(ctx)
		if err != nil {
			return
		}
		for _, b := range msg {
			switch b {
			case '\r', '\n':
				reply := "\r\n"
				if cmd := strings.TrimSpace(string(line)); cmd != "" {
					reply += "you typed: " + cmd + "\r\n"
				}
				line = line[:0]
				if write(reply+"$ ") != nil {
					return
				}
			case 0x7f:
				if len(line) > 0 {
					line = line[:len(line)-1]
					_ = write("\b \b")
				}
			default:
				line = append(line, b)
				_ = write(string([]byte{b}))
			}
		}
	}
}
