package console

import (
	"bytes"
	"crypto/des" //nolint:gosec // G502: RFB "VNC authentication" is defined with DES (RFC 6143 §7.2.2)
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// RFB security types (RFC 6143 §7.2).
const (
	rfbSecNone    = 1
	rfbSecVNCAuth = 2
)

var rfbVersion = []byte("RFB 003.008\n")

// ErrRFBAuth is returned when the provider rejects the VNC password.
var ErrRFBAuth = errors.New("console: VNC authentication rejected by the provider")

// relayRFBAuth performs the RFB handshake on both sides so that the
// provider's VNC password (a Proxmox ticket) stays on the server: SEMAR
// authenticates to upstream with the password and offers the browser a
// session without authentication. It returns after the security phase;
// ClientInit/ServerInit and everything after are piped unchanged.
func relayRFBAuth(upstream, browser io.ReadWriter, password string) error {
	if err := upstreamAuth(upstream, password); err != nil {
		return err
	}
	return offerNoAuth(browser)
}

// upstreamAuth negotiates RFB 3.8 (or 3.7) with the provider and answers
// VNC authentication when the server asks for it.
func upstreamAuth(c io.ReadWriter, password string) error {
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c, ver); err != nil {
		return fmt.Errorf("rfb: read server version: %w", err)
	}
	if !bytes.HasPrefix(ver, []byte("RFB 003.00")) || (ver[10] != '8' && ver[10] != '7') {
		return fmt.Errorf("rfb: unsupported server version %q", ver)
	}
	if _, err := c.Write(ver); err != nil { // echo the server's version (3.7 or 3.8)
		return err
	}
	var n [1]byte
	if _, err := io.ReadFull(c, n[:]); err != nil {
		return fmt.Errorf("rfb: read security types: %w", err)
	}
	if n[0] == 0 {
		return fmt.Errorf("rfb: server refused the connection: %s", readReason(c))
	}
	types := make([]byte, n[0])
	if _, err := io.ReadFull(c, types); err != nil {
		return err
	}
	switch {
	case bytes.IndexByte(types, rfbSecVNCAuth) >= 0 && password != "":
		if _, err := c.Write([]byte{rfbSecVNCAuth}); err != nil {
			return err
		}
		challenge := make([]byte, 16)
		if _, err := io.ReadFull(c, challenge); err != nil {
			return fmt.Errorf("rfb: read challenge: %w", err)
		}
		resp, err := vncAuthResponse(challenge, password)
		if err != nil {
			return err
		}
		if _, err := c.Write(resp); err != nil {
			return err
		}
	case bytes.IndexByte(types, rfbSecNone) >= 0:
		if _, err := c.Write([]byte{rfbSecNone}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("rfb: no supported security type in %v", types)
	}
	var result [4]byte
	if _, err := io.ReadFull(c, result[:]); err != nil {
		return fmt.Errorf("rfb: read security result: %w", err)
	}
	if binary.BigEndian.Uint32(result[:]) != 0 {
		return fmt.Errorf("%w: %s", ErrRFBAuth, readReason(c))
	}
	return nil
}

// offerNoAuth runs the browser side of the security phase with only the
// "None" security type.
func offerNoAuth(c io.ReadWriter) error {
	if _, err := c.Write(rfbVersion); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c, ver); err != nil {
		return fmt.Errorf("rfb: read client version: %w", err)
	}
	if !bytes.Equal(ver, rfbVersion) && !bytes.Equal(ver, []byte("RFB 003.007\n")) {
		return fmt.Errorf("rfb: unsupported client version %q", ver)
	}
	if _, err := c.Write([]byte{1, rfbSecNone}); err != nil {
		return err
	}
	var choice [1]byte
	if _, err := io.ReadFull(c, choice[:]); err != nil {
		return err
	}
	if choice[0] != rfbSecNone {
		return fmt.Errorf("rfb: client chose security type %d", choice[0])
	}
	// RFB 3.8 sends a SecurityResult for "None"; 3.7 does not.
	if bytes.Equal(ver, rfbVersion) {
		_, err := c.Write([]byte{0, 0, 0, 0})
		return err
	}
	return nil
}

func readReason(r io.Reader) string {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return "(no reason)"
	}
	l := binary.BigEndian.Uint32(n[:])
	if l > 1024 {
		return "(reason too long)"
	}
	msg := make([]byte, l)
	if _, err := io.ReadFull(r, msg); err != nil {
		return "(no reason)"
	}
	return string(msg)
}

// vncAuthResponse encrypts the challenge with the password as DES key, using
// the bit-reversed key bytes VNC requires.
func vncAuthResponse(challenge []byte, password string) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, password)
	for i, b := range key {
		key[i] = reverseBits(b)
	}
	block, err := des.NewCipher(key) //nolint:gosec // G405: protocol-mandated DES, see import
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16)
	block.Encrypt(out[:8], challenge[:8])
	block.Encrypt(out[8:], challenge[8:])
	return out, nil
}

func reverseBits(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r = r<<1 | b&1
		b >>= 1
	}
	return r
}
