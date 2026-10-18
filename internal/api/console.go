package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/console"
	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

// ConsoleTokenStore persists console tokens; *store.Store implements it.
type ConsoleTokenStore interface {
	CreateConsoleToken(ctx context.Context, t store.ConsoleToken) error
	ConsumeConsoleToken(ctx context.Context, tokenHash, sessionHash []byte) (store.ConsoleToken, error)
}

var _ ConsoleTokenStore = (*store.Store)(nil)

// ConsoleDriverFactory builds a driver allowed to open consoles of a VM in
// vmScope (registry.Resolver.ConsoleDriver by default).
type ConsoleDriverFactory func(ctx context.Context, p model.Provider, vmScope string) (contract.CloudDriver, error)

const (
	// ConsoleTokenTTL is the lifetime of a console token (roadmap §3.4:
	// at most 30 seconds).
	ConsoleTokenTTL = 30 * time.Second
	// ConsoleWebSocketPath is where browsers connect.
	ConsoleWebSocketPath = "/api/v1/console/ws"
	// consoleTokenProtocol prefixes the WebSocket subprotocol carrying the
	// token, which keeps it out of URLs, proxy logs and browser history.
	consoleTokenProtocol = "semar.token." //nolint:gosec // G101: a protocol prefix, not a credential
)

// consoleVisibility is the part of the caller's visibility that may open
// consoles: Admin and Operator bindings only. Viewers can see VMs but not
// interact with them.
func consoleVisibility(p *auth.Principal) store.Visibility {
	if p == nil {
		return store.Visibility{}
	}
	q := &auth.Principal{UserID: p.UserID, Username: p.Username}
	for _, b := range p.Bindings {
		if b.Role == auth.RoleAdmin || b.Role == auth.RoleOperator {
			q.Bindings = append(q.Bindings, b)
		}
	}
	return visibilityFor(q, store.KindVM)
}

func consoleTarget(providerID, externalID string) string { return providerID + "/vm/" + externalID }

// consoleSlots bounds open consoles per user and per process: each one
// holds a provider connection and goroutines.
type consoleSlots struct {
	mu      sync.Mutex
	perUser map[string]int
	total   int
	maxUser int
	maxAll  int
}

func newConsoleSlots(maxUser, maxAll int) *consoleSlots {
	return &consoleSlots{perUser: map[string]int{}, maxUser: maxUser, maxAll: maxAll}
}

// acquire reserves a slot for user; release must be called exactly once.
func (c *consoleSlots) acquire(user string) (release func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.perUser[user] >= c.maxUser || c.total >= c.maxAll {
		return nil, false
	}
	c.perUser[user]++
	c.total++
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.total--
			if c.perUser[user]--; c.perUser[user] <= 0 {
				delete(c.perUser, user)
			}
		})
	}, true
}

func (s *server) CreateConsoleSession(ctx context.Context, req gen.CreateConsoleSessionRequestObject) (gen.CreateConsoleSessionResponseObject, error) {
	instance := "/api/v1/resources/" + req.ProviderId + "/vm/" + req.ExternalId + "/console"
	var out gen.ConsoleSession
	err := s.audited(ctx, "console.token", consoleTarget(req.ProviderId, req.ExternalId), func() (err error) {
		out, err = s.createConsoleSession(ctx, req)
		return err
	})
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.CreateConsoleSessiondefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.CreateConsoleSession201JSONResponse(out), nil
}

func (s *server) createConsoleSession(ctx context.Context, req gen.CreateConsoleSessionRequestObject) (gen.ConsoleSession, error) {
	if s.opts.Inventory == nil || s.opts.ConsoleTokens == nil {
		return gen.ConsoleSession{}, &apiError{status: http.StatusServiceUnavailable, detail: "consoles are not configured"}
	}
	if req.Body == nil || !contract.ConsoleKind(req.Body.Kind).Valid() {
		return gen.ConsoleSession{}, badRequest("kind must be vnc or serial")
	}
	p := auth.PrincipalFrom(ctx)
	sessionHash, _ := ctx.Value(sessionHashKey{}).([]byte)
	if p == nil || sessionHash == nil {
		return gen.ConsoleSession{}, &apiError{status: http.StatusUnauthorized, detail: "authentication required"}
	}
	if _, err := s.consoleVM(ctx, p, req.ProviderId, req.ExternalId); err != nil {
		return gen.ConsoleSession{}, err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return gen.ConsoleSession{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().Add(ConsoleTokenTTL).UTC()
	if err := s.opts.ConsoleTokens.CreateConsoleToken(ctx, store.ConsoleToken{
		TokenHash: hash[:], UserID: p.UserID, SessionHash: sessionHash,
		ProviderID: req.ProviderId, ExternalID: req.ExternalId, Kind: string(req.Body.Kind), ExpiresAt: expires,
	}); err != nil {
		return gen.ConsoleSession{}, err
	}
	return gen.ConsoleSession{Token: token, Kind: req.Body.Kind, ExpiresAt: expires, WebsocketPath: ConsoleWebSocketPath}, nil
}

// consoleVM returns the VM if p may open its console: 404 when p cannot
// see it at all, 403 when p sees it only as a viewer, 409 when it is no
// longer reported by the provider.
func (s *server) consoleVM(ctx context.Context, p *auth.Principal, providerID, externalID string) (store.Resource, error) {
	if _, err := s.opts.Inventory.GetResource(ctx, providerID, store.KindVM, externalID, visibilityFor(p, store.KindVM)); err != nil {
		return store.Resource{}, err
	}
	r, err := s.opts.Inventory.GetResource(ctx, providerID, store.KindVM, externalID, consoleVisibility(p))
	if errors.Is(err, store.ErrNotFound) {
		return store.Resource{}, &apiError{status: http.StatusForbidden, detail: "consoles require an Admin or Operator role for this VM"}
	}
	if err != nil {
		return store.Resource{}, err
	}
	if r.OrphanedAt != nil {
		return store.Resource{}, &apiError{status: http.StatusConflict, detail: "the VM is no longer reported by its provider"}
	}
	return r, nil
}

// tokenFromProtocols extracts the console token from the requested
// WebSocket subprotocols.
func tokenFromProtocols(r *http.Request) string {
	for _, h := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(h, ",") {
			if t, ok := strings.CutPrefix(strings.TrimSpace(p), consoleTokenProtocol); ok {
				return t
			}
		}
	}
	return ""
}

// serveConsole upgrades GET /api/v1/console/ws. Every check happens before
// the upgrade: Origin (a WebSocket is not subject to CORS), the login
// session, the single-use token bound to that session, and the role on the
// VM, re-checked because it may have changed since the token was issued.
func (s *server) serveConsole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if origin := r.Header.Get("Origin"); origin == "" || !s.originOK(origin, r.Host) {
		writeProblem(w, r, http.StatusForbidden, "cross-origin console refused")
		return
	}
	p := auth.PrincipalFrom(ctx)
	sessionHash, _ := ctx.Value(sessionHashKey{}).([]byte)
	if p == nil || sessionHash == nil || s.opts.ConsoleTokens == nil {
		writeProblem(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	token := tokenFromProtocols(r)
	sum := sha256.Sum256([]byte(token))
	tok, err := s.opts.ConsoleTokens.ConsumeConsoleToken(ctx, sum[:], sessionHash)
	if token == "" || err != nil || tok.UserID != p.UserID {
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.opts.Logger.ErrorContext(ctx, "consume console token", slog.Any("error", err))
		}
		writeProblem(w, r, http.StatusForbidden, "invalid, used or expired console token")
		return
	}
	target := consoleTarget(tok.ProviderID, tok.ExternalID)
	release, ok := s.consoles.acquire(p.UserID)
	if !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "too many open consoles; close one and try again")
		return
	}
	defer release()
	vm, err := s.consoleVM(ctx, p, tok.ProviderID, tok.ExternalID)
	if err != nil {
		code, prob := s.problemFor(ctx, ConsoleWebSocketPath, err)
		writeProblemBody(w, code, prob)
		return
	}
	provider, err := s.opts.Settings.GetProvider(ctx, tok.ProviderID)
	if err != nil {
		code, prob := s.problemFor(ctx, ConsoleWebSocketPath, err)
		writeProblemBody(w, code, prob)
		return
	}

	// Origin was checked above against SEMAR's own rule (Host or trusted
	// origins), which the library's check cannot express behind a proxy.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}, InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	kind := contract.ConsoleKind(tok.Kind)
	details := map[string]string{"kind": tok.Kind}

	d, err := s.opts.ConsoleDrivers(ctx, provider, vm.ExternalScope)
	var opener contract.ConsoleOpener
	if err == nil {
		var ok bool
		if opener, ok = d.(contract.ConsoleOpener); !ok {
			err = contract.ErrConsoleUnsupported
		}
	}
	var tgt contract.ConsoleTarget
	if err == nil {
		tgt, err = opener.OpenConsole(ctx, vm.ExternalID, kind)
	}
	if err != nil {
		s.opts.Logger.WarnContext(ctx, "open console", slog.String("target", target), slog.Any("error", err),
			slog.String("request_id", RequestIDFrom(ctx)))
		details["error"] = consoleErrorClass(err)
		_ = s.audit(ctx, "", "console.open", target, store.AuditFailure, details)
		msg := "the provider console is not available"
		if errors.Is(err, contract.ErrConsoleUnsupported) {
			msg = "this console kind is not available for the VM"
		}
		_ = c.Close(console.CloseUpstream, msg)
		return
	}
	_ = s.audit(ctx, "", "console.open", target, store.AuditSuccess, details)

	sess := &console.Session{
		Browser: c, Target: tgt,
		IdleTimeout: s.opts.ConsoleIdleTimeout, MaxDuration: s.opts.ConsoleMaxDuration,
		RecheckEvery: time.Minute,
		Check: func(ctx context.Context) error {
			fresh, err := s.opts.Auth.PrincipalForSession(ctx, sessionHash)
			if err != nil {
				return err
			}
			_, err = s.consoleVM(auth.WithPrincipal(ctx, fresh), fresh, tok.ProviderID, tok.ExternalID)
			return err
		},
	}
	res, err := sess.Run(ctx)
	closeDetails := map[string]string{
		"kind": tok.Kind, "reason": res.Reason,
		"duration_seconds": strconv.Itoa(int(res.Duration.Seconds())),
		"bytes_to_browser": strconv.FormatInt(res.ToBrowser, 10), "bytes_to_vm": strconv.FormatInt(res.ToVM, 10),
	}
	outcome := store.AuditSuccess
	if err != nil {
		outcome = store.AuditFailure
		closeDetails["error"] = consoleErrorClass(err)
	}
	// The request context may be done (the browser left); the audit
	// event must still be written.
	_ = s.audit(context.WithoutCancel(ctx), "", "console.close", target, outcome, closeDetails)
}

// consoleErrorClass is a short label for the audit log; provider error text
// (hosts, tickets) never goes there.
func consoleErrorClass(err error) string {
	switch {
	case errors.Is(err, contract.ErrConsoleUnsupported):
		return "unsupported"
	case errors.Is(err, contract.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, contract.ErrNotFound):
		return "not_found"
	case errors.Is(err, registry.ErrCredentialUnreadable):
		return "credential_unreadable"
	case errors.Is(err, console.ErrRFBAuth):
		return "vnc_auth_rejected"
	case errors.Is(err, contract.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		return "unavailable"
	default:
		return "internal"
	}
}
