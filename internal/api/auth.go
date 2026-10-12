package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/store"
)

// AuthStore is the persistence authentication needs; *store.Store
// implements it.
type AuthStore interface {
	UserByUsername(ctx context.Context, username string) (store.User, error)
	CreateSession(ctx context.Context, tokenHash []byte, userID string, expiresAt time.Time) error
	PrincipalForSession(ctx context.Context, tokenHash []byte) (*auth.Principal, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteExpiredSessions(ctx context.Context) (int64, error)
}

// AuditLog appends audit events; *store.Store implements it.
type AuditLog interface {
	AppendAudit(ctx context.Context, e store.AuditEvent) (store.AuditEvent, error)
}

var (
	_ AuthStore = (*store.Store)(nil)
	_ AuditLog  = (*store.Store)(nil)
)

// access is the minimum caller an operation requires.
type access int

const (
	accessPublic access = iota
	accessAuthenticated
	accessAdmin
)

// operationAccess lists every operation's policy, keyed by the generated
// operation name. Operations missing here are refused (fail closed); a test
// checks that every operation in the spec is listed.
var operationAccess = map[string]access{
	"GetHealthz":           accessPublic,
	"GetReadyz":            accessPublic,
	"GetVersion":           accessPublic,
	"Login":                accessPublic,
	"Logout":               accessPublic,
	"GetMe":                accessAuthenticated,
	"ListSites":            accessAuthenticated,
	"ListProjects":         accessAuthenticated,
	"ListProjectMappings":  accessAuthenticated,
	"ListProviders":        accessAuthenticated,
	"GetProvider":          accessAuthenticated,
	"CreateSite":           accessAdmin,
	"CreateProject":        accessAdmin,
	"CreateProjectMapping": accessAdmin,
	"CreateProvider":       accessAdmin,
	"DeleteProvider":       accessAdmin,
}

// authorize is a strict-server middleware enforcing operationAccess.
func (s *server) authorize(next gen.StrictHandlerFunc, operation string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		required, known := operationAccess[operation]
		if !known {
			s.opts.Logger.ErrorContext(ctx, "operation has no access policy; refusing", slog.String("operation", operation))
			writeProblem(w, r, http.StatusForbidden, "operation not permitted")
			return nil, nil
		}
		p := auth.PrincipalFrom(ctx)
		if required >= accessAuthenticated && p == nil {
			writeProblem(w, r, http.StatusUnauthorized, "authentication required")
			return nil, nil
		}
		if required == accessAdmin && !p.IsGlobalAdmin() {
			writeProblem(w, r, http.StatusForbidden, "admin role required")
			return nil, nil
		}
		return next(ctx, w, r, request)
	}
}

type sessionHashKey struct{}

// withAuthentication resolves the session cookie to a principal. Invalid or
// expired sessions leave the request anonymous.
func withAuthentication(st AuthStore, log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.SessionCookie)
		if err != nil || st == nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		hash := auth.HashSessionToken(cookie.Value)
		p, err := st.PrincipalForSession(r.Context(), hash)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				log.ErrorContext(r.Context(), "session lookup failed", slog.Any("error", err),
					slog.String("request_id", RequestIDFrom(r.Context())))
			}
			next.ServeHTTP(w, r)
			return
		}
		ctx := auth.WithPrincipal(r.Context(), p)
		ctx = context.WithValue(ctx, sessionHashKey{}, hash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CSRFHeader must accompany every state-changing request. Browsers cannot
// add it to cross-site requests without a CORS preflight, which SEMAR never
// allows; together with SameSite=Strict cookies this blocks CSRF.
const CSRFHeader = "X-Requested-By"

// withCSRFProtection rejects unsafe requests without CSRFHeader or with a
// cross-origin Origin header.
func withCSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(CSRFHeader) == "" {
			writeProblem(w, r, http.StatusForbidden, "missing "+CSRFHeader+" header")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				writeProblem(w, r, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// dummyHash is verified against for unknown users so that login takes the
// same time whether or not the username exists.
var (
	dummyHashOnce sync.Once
	dummyHash     string
)

func timingDummyHash() string {
	dummyHashOnce.Do(func() {
		dummyHash, _ = auth.HashPassword("semar-timing-equaliser-password", auth.DefaultArgon2)
	})
	return dummyHash
}

const maxActorLen = 64

// actorFromInput bounds a client-supplied name before it is logged.
func actorFromInput(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > maxActorLen {
		s = s[:maxActorLen]
	}
	if s == "" {
		return "anonymous"
	}
	return s
}

func (s *server) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	const instance = "/api/v1/auth/login"
	fail := func(status int, detail string) (gen.LoginResponseObject, error) {
		return gen.LogindefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: newProblem(ctx, instance, status, detail)}, nil
	}
	if s.opts.Auth == nil {
		return fail(http.StatusServiceUnavailable, "authentication not configured")
	}
	if req.Body == nil {
		return fail(http.StatusBadRequest, "username and password are required")
	}
	username := strings.ToLower(strings.TrimSpace(req.Body.Username))

	u, err := s.opts.Auth.UserByUsername(ctx, username)
	ok := false
	switch {
	case err == nil && !u.Disabled:
		ok, err = auth.VerifyPassword(u.PasswordHash, req.Body.Password)
		if err != nil {
			s.opts.Logger.ErrorContext(ctx, "stored password hash unreadable", slog.String("user", u.Username), slog.Any("error", err))
			ok = false
		}
	case err == nil || errors.Is(err, store.ErrNotFound):
		_, _ = auth.VerifyPassword(timingDummyHash(), req.Body.Password)
	default:
		s.opts.Logger.ErrorContext(ctx, "login lookup failed", slog.Any("error", err))
		return fail(http.StatusInternalServerError, "")
	}
	if !ok {
		_ = s.audit(ctx, actorFromInput(username), "auth.login", username, store.AuditFailure, nil)
		return fail(http.StatusUnauthorized, "invalid username or password")
	}

	token, hash, err := auth.NewSessionToken()
	if err != nil {
		return fail(http.StatusInternalServerError, "")
	}
	if _, err := s.opts.Auth.DeleteExpiredSessions(ctx); err != nil {
		s.opts.Logger.WarnContext(ctx, "expired session cleanup failed", slog.Any("error", err))
	}
	expires := time.Now().Add(s.opts.SessionTTL)
	if err := s.opts.Auth.CreateSession(ctx, hash, u.ID, expires); err != nil {
		s.opts.Logger.ErrorContext(ctx, "create session failed", slog.Any("error", err))
		return fail(http.StatusInternalServerError, "")
	}
	p, err := s.opts.Auth.PrincipalForSession(ctx, hash)
	if err != nil {
		s.opts.Logger.ErrorContext(ctx, "load new session failed", slog.Any("error", err))
		return fail(http.StatusInternalServerError, "")
	}
	_ = s.audit(ctx, u.Username, "auth.login", u.Username, store.AuditSuccess, nil)

	cookie := s.sessionCookie(token, expires).String()
	return gen.Login200JSONResponse{Body: toMe(p), Headers: gen.Login200ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *server) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	if hash, ok := ctx.Value(sessionHashKey{}).([]byte); ok && s.opts.Auth != nil {
		if err := s.opts.Auth.DeleteSession(ctx, hash); err != nil {
			s.opts.Logger.ErrorContext(ctx, "delete session failed", slog.Any("error", err))
			const instance = "/api/v1/auth/logout"
			return gen.LogoutdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       newProblem(ctx, instance, http.StatusInternalServerError, ""),
			}, nil
		}
		p := auth.PrincipalFrom(ctx)
		_ = s.audit(ctx, p.Username, "auth.logout", p.Username, store.AuditSuccess, nil)
	}
	cleared := s.sessionCookie("", time.Unix(0, 0)) //nolint:gosec // see sessionCookie
	cleared.MaxAge = -1
	cookie := cleared.String()
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *server) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	return gen.GetMe200JSONResponse(toMe(auth.PrincipalFrom(ctx))), nil
}

func (s *server) sessionCookie(value string, expires time.Time) *http.Cookie {
	// Secure follows SEMAR_COOKIE_SECURE (default true; off only for plain
	// HTTP on localhost). HttpOnly and SameSite=Strict are always set.
	return &http.Cookie{ //nolint:gosec // Secure is configurable on purpose, see above
		Name:     auth.SessionCookie,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.opts.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
}

func toMe(p *auth.Principal) gen.Me {
	me := gen.Me{Roles: []gen.RoleBinding{}}
	if p == nil {
		return me
	}
	me.Username = p.Username
	for _, b := range p.Bindings {
		rb := gen.RoleBinding{Role: gen.Role(b.Role)}
		if b.ProjectID != "" {
			project := b.ProjectID
			rb.ProjectId = &project
		}
		if b.ProviderID != "" {
			provider := b.ProviderID
			rb.ProviderId = &provider
		}
		me.Roles = append(me.Roles, rb)
	}
	return me
}

// audit appends one event. actor defaults to the request's principal.
func (s *server) audit(ctx context.Context, actor, action, target string, outcome store.AuditOutcome, details map[string]string) error {
	if s.opts.Audit == nil {
		return nil
	}
	if actor == "" {
		actor = "anonymous"
		if p := auth.PrincipalFrom(ctx); p != nil {
			actor = p.Username
		}
	}
	_, err := s.opts.Audit.AppendAudit(ctx, store.AuditEvent{
		Actor: actor, Action: action, Target: target, Outcome: outcome,
		RequestID: RequestIDFrom(ctx), Details: details,
	})
	if err != nil {
		s.opts.Logger.ErrorContext(ctx, "audit append failed",
			slog.String("action", action), slog.String("target", target), slog.Any("error", err))
	}
	return err
}

// audited records "requested" before fn runs — refusing to act if that
// cannot be recorded — and "success" or "failure" afterwards (roadmap §3.4).
func (s *server) audited(ctx context.Context, action, target string, fn func() error) error {
	if err := s.audit(ctx, "", action, target, store.AuditRequested, nil); err != nil {
		return errors.New("audit log unavailable; action not performed")
	}
	err := fn()
	if err != nil {
		_ = s.audit(ctx, "", action, target, store.AuditFailure, map[string]string{"error": errorClass(err)})
		return err
	}
	_ = s.audit(ctx, "", action, target, store.AuditSuccess, nil)
	return nil
}

// errorClass is a short, non-sensitive label for an error in audit details.
func errorClass(err error) string {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		return http.StatusText(ae.status)
	case errors.Is(err, store.ErrNotFound):
		return "not_found"
	case errors.Is(err, store.ErrConflict):
		return "conflict"
	case errors.Is(err, store.ErrInvalidReference):
		return "invalid_reference"
	case errors.Is(err, store.ErrInvalid):
		return "invalid"
	default:
		return "internal"
	}
}
