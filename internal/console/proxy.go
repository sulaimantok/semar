// Package console bridges a browser WebSocket to a provider console
// (roadmap §3.4). The provider URL, ticket and password stay in this
// process: the browser speaks to SEMAR only.
//
// Browser protocol, per kind:
//   - vnc: binary messages carrying the RFB stream (noVNC). When the target
//     has a VNC password, SEMAR answers the provider's authentication and
//     offers the browser a session without one.
//   - serial: binary messages carry terminal bytes both ways; a text message
//     {"cols":N,"rows":M} resizes the terminal (xterm.js).
package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/httpx"
)

// Close codes sent to the browser (4000–4999 are application-defined).
const (
	CloseIdle        websocket.StatusCode = 4001
	CloseMaxDuration websocket.StatusCode = 4002
	CloseRevoked     websocket.StatusCode = 4003
	CloseUpstream    websocket.StatusCode = 4004
)

// Reasons reported in Result and the audit log.
const (
	ReasonBrowserClosed  = "browser_closed"
	ReasonUpstreamClosed = "provider_closed"
	ReasonIdle           = "idle_timeout"
	ReasonMaxDuration    = "max_duration"
	ReasonRevoked        = "access_revoked"
	ReasonError          = "error"
)

// Session is one console connection.
type Session struct {
	Browser *websocket.Conn
	Target  contract.ConsoleTarget
	// IdleTimeout closes the session after this long without data in
	// either direction. MaxDuration bounds the whole session.
	IdleTimeout time.Duration
	MaxDuration time.Duration
	// Check is called every RecheckEvery; an error ends the session (the
	// user logged out or lost the role).
	Check        func(ctx context.Context) error
	RecheckEvery time.Duration
	// Dial connects to the provider; DialUpstream by default.
	Dial func(ctx context.Context, t contract.ConsoleTarget) (*websocket.Conn, error)
}

// Result summarises a finished session for the audit log.
type Result struct {
	Reason    string
	Duration  time.Duration
	ToBrowser int64
	ToVM      int64
}

// DialUpstream opens the provider WebSocket.
func DialUpstream(ctx context.Context, t contract.ConsoleTarget) (*websocket.Conn, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if t.TLS != nil {
		tr.TLSClientConfig = t.TLS.Clone()
	}
	c, resp, err := websocket.Dial(ctx, t.URL, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: tr, Timeout: 30 * time.Second,
			// The handshake carries provider credentials: redirects are
			// followed only within the same origin.
			CheckRedirect: httpx.SameOriginRedirects},
		HTTPHeader:   t.Header,
		Subprotocols: t.Subprotocols,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, fmt.Errorf("console: connect to provider (HTTP %d): %w: %w", status, contract.ErrUnavailable, err)
	}
	// Framebuffer updates can be large.
	c.SetReadLimit(64 << 20)
	return c, nil
}

type tracker struct {
	last      atomic.Int64
	toBrowser atomic.Int64
	toVM      atomic.Int64
}

func (t *tracker) touch() { t.last.Store(time.Now().UnixNano()) }

func (t *tracker) idleFor() time.Duration { return time.Since(time.Unix(0, t.last.Load())) }

// Run bridges the session until either side closes, a limit is reached or
// Check fails.
func (s *Session) Run(ctx context.Context) (Result, error) {
	start := time.Now()
	dial := s.Dial
	if dial == nil {
		dial = DialUpstream
	}
	up, err := dial(ctx, s.Target)
	if err != nil {
		_ = s.Browser.Close(CloseUpstream, "the provider console is not reachable")
		return Result{Reason: ReasonError, Duration: time.Since(start)}, err
	}

	// I/O runs on a context that is not cancelled with the session:
	// cancelling a read would drop the connection before the close frame
	// carrying the reason is sent. Ending the session closes the
	// connections instead, which unblocks the I/O.
	ioCtx := context.WithoutCancel(ctx)
	sess, cancel := context.WithCancel(ctx)
	defer cancel()
	tr := &tracker{}
	tr.touch()

	var reason atomic.Value
	end := func(r string) {
		reason.CompareAndSwap(nil, r)
		cancel()
	}
	go s.watch(sess, tr, end)

	bridged := make(chan error, 1)
	go func() {
		switch s.Target.Kind {
		case contract.ConsoleVNC:
			bridged <- s.bridgeVNC(ioCtx, up, tr, end)
		case contract.ConsoleSerial:
			bridged <- s.bridgeSerial(ioCtx, sess, up, tr, end)
		default:
			end(ReasonError)
			bridged <- fmt.Errorf("console: unknown kind %q", s.Target.Kind)
		}
	}()
	<-sess.Done()
	if ctx.Err() != nil {
		end(ReasonBrowserClosed) // the server is shutting down or the request ended
	}

	r, _ := reason.Load().(string)
	code, msg := websocket.StatusNormalClosure, "console closed"
	switch r {
	case ReasonIdle:
		code, msg = CloseIdle, "closed after inactivity"
	case ReasonMaxDuration:
		code, msg = CloseMaxDuration, "maximum console duration reached"
	case ReasonRevoked:
		code, msg = CloseRevoked, "access to this console was revoked"
	case ReasonUpstreamClosed, ReasonError:
		code, msg = CloseUpstream, "the provider closed the console"
	}
	_ = s.Browser.Close(code, msg)
	_ = up.CloseNow()
	var bridgeErr error
	select {
	case bridgeErr = <-bridged:
	case <-time.After(5 * time.Second):
	}
	res := Result{Reason: r, Duration: time.Since(start), ToBrowser: tr.toBrowser.Load(), ToVM: tr.toVM.Load()}
	if r == ReasonError {
		return res, bridgeErr
	}
	return res, nil
}

// watch enforces idle timeout, maximum duration and periodic rechecks.
func (s *Session) watch(ctx context.Context, tr *tracker, end func(string)) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	deadline := time.Time{}
	if s.MaxDuration > 0 {
		deadline = time.Now().Add(s.MaxDuration)
	}
	nextCheck := time.Now().Add(s.RecheckEvery)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			switch {
			case s.IdleTimeout > 0 && tr.idleFor() > s.IdleTimeout:
				end(ReasonIdle)
				return
			case !deadline.IsZero() && now.After(deadline):
				end(ReasonMaxDuration)
				return
			case s.Check != nil && s.RecheckEvery > 0 && now.After(nextCheck):
				nextCheck = now.Add(s.RecheckEvery)
				if err := s.Check(ctx); err != nil && ctx.Err() == nil {
					end(ReasonRevoked)
					return
				}
			}
		}
	}
}

// countingConn records traffic and activity on a stream.
type countingConn struct {
	net.Conn
	tr      *tracker
	counter *atomic.Int64
}

func (c countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.tr.touch()
		c.counter.Add(int64(n))
	}
	return n, err
}

// bridgeVNC pipes the RFB streams; it returns once both directions ended.
func (s *Session) bridgeVNC(ctx context.Context, up *websocket.Conn, tr *tracker, end func(string)) error {
	b := websocket.NetConn(ctx, s.Browser, websocket.MessageBinary)
	u := websocket.NetConn(ctx, up, websocket.MessageBinary)
	if s.Target.VNCPassword != "" {
		if err := relayRFBAuth(u, b, s.Target.VNCPassword); err != nil {
			end(ReasonError)
			return err
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(u, countingConn{Conn: b, tr: tr, counter: &tr.toVM})
		end(ReasonBrowserClosed)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, countingConn{Conn: u, tr: tr, counter: &tr.toBrowser})
		end(ReasonUpstreamClosed)
	}()
	wg.Wait()
	return nil
}

type resize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// termproxyKeepAlive is how often Proxmox's termproxy expects a ping.
const termproxyKeepAlive = 30 * time.Second

// bridgeSerial relays terminal bytes, translating Proxmox's termproxy
// framing; it returns once both directions ended. sess only stops the
// keep-alive ticker.
func (s *Session) bridgeSerial(ctx, sess context.Context, up *websocket.Conn, tr *tracker, end func(string)) error {
	pve := s.Target.Framing == contract.SerialPVETermproxy
	if pve {
		if err := up.Write(ctx, websocket.MessageText, []byte(s.Target.SerialAck)); err != nil {
			end(ReasonError)
			return fmt.Errorf("console: termproxy login: %w", err)
		}
		// termproxy answers the login line with "OK" before any output.
		_, msg, err := up.Read(ctx)
		if err != nil || string(msg) != "OK" {
			end(ReasonError)
			return fmt.Errorf("console: termproxy refused the ticket: %w", errors.Join(err, contract.ErrUnauthorized))
		}
		go func() {
			t := time.NewTicker(termproxyKeepAlive)
			defer t.Stop()
			for {
				select {
				case <-sess.Done():
					return
				case <-t.C:
					_ = up.Write(ctx, websocket.MessageText, []byte("2"))
				}
			}
		}()
	}

	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		for {
			typ, msg, err := s.Browser.Read(ctx)
			if err != nil {
				end(ReasonBrowserClosed)
				return
			}
			tr.touch()
			var out []byte
			switch {
			case typ == websocket.MessageText:
				var rs resize
				if json.Unmarshal(msg, &rs) != nil || rs.Cols <= 0 || rs.Rows <= 0 || !pve {
					continue
				}
				out = []byte("1:" + strconv.Itoa(rs.Cols) + ":" + strconv.Itoa(rs.Rows) + ":")
			case pve:
				out = append([]byte("0:"+strconv.Itoa(len(msg))+":"), msg...)
				tr.toVM.Add(int64(len(msg)))
			default:
				out = msg
				tr.toVM.Add(int64(len(msg)))
			}
			if err := up.Write(ctx, websocket.MessageBinary, out); err != nil {
				end(ReasonUpstreamClosed)
				return
			}
		}
	}()
	for {
		_, msg, err := up.Read(ctx)
		if err != nil {
			end(ReasonUpstreamClosed)
			break
		}
		tr.touch()
		tr.toBrowser.Add(int64(len(msg)))
		if err := s.Browser.Write(ctx, websocket.MessageBinary, msg); err != nil {
			end(ReasonBrowserClosed)
			break
		}
	}
	<-inputDone
	return nil
}
