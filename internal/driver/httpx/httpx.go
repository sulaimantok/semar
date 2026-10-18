// Package httpx holds the HTTP plumbing shared by provider drivers: JSON
// requests, classification of failures into the contract sentinels, and
// TLS certificate pinning for self-signed endpoints.
package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

// MaxResponseBytes bounds a provider response body.
const MaxResponseBytes = 64 << 20

// StatusError is a non-2xx answer. It wraps the matching contract sentinel,
// so callers can use errors.Is(err, contract.ErrUnauthorized) and so on.
type StatusError struct {
	Method, URL string
	Status      int
	// Message is the provider's own explanation, when it sent one (Nova's
	// {"badRequest": {"message": …}}, Keystone's {"error": {"message": …}}),
	// truncated. It is for server logs; clients only see a classification.
	Message  string
	sentinel error
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: HTTP %d: %v: %s", e.Method, e.URL, e.Status, e.sentinel, e.Message)
	}
	return fmt.Sprintf("%s %s: HTTP %d: %v", e.Method, e.URL, e.Status, e.sentinel)
}

// providerMessage extracts a short message from an OpenStack or Proxmox
// error body: {"<fault>": {"message": "…"}}, {"error": {"message": …}},
// {"message": …} or {"errors": {…}}.
func providerMessage(body []byte) string {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return ""
	}
	var msg string
	for k, raw := range top {
		var inner struct {
			Message string `json:"message"`
		}
		switch {
		case k == "message":
			_ = json.Unmarshal(raw, &msg)
		case k == "errors":
			msg = string(raw)
		case json.Unmarshal(raw, &inner) == nil && inner.Message != "":
			msg = inner.Message
		}
		if msg != "" {
			break
		}
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

func (e *StatusError) Unwrap() error { return e.sentinel }

// Classify maps an HTTP status to a contract sentinel.
func Classify(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return contract.ErrUnauthorized
	case http.StatusNotFound:
		return contract.ErrNotFound
	case http.StatusTooManyRequests:
		return contract.ErrRateLimited
	default:
		return contract.ErrUnavailable
	}
}

// Request describes one JSON call.
type Request struct {
	Method string
	URL    string
	Header http.Header
	// Body is marshalled as JSON when non-nil.
	Body any
	// Form is sent URL-encoded when non-nil (and Body is nil), as the
	// Proxmox API expects for POST.
	Form url.Values
	// AllowMultipleChoices accepts HTTP 300, which OpenStack services use
	// for their version documents.
	AllowMultipleChoices bool
}

// Do sends r and decodes a 2xx JSON answer into out (when non-nil). It
// returns the response headers. Context cancellation is returned as is;
// transport failures wrap contract.ErrUnavailable.
func Do(ctx context.Context, c *http.Client, r Request, out any) (http.Header, error) {
	var body io.Reader
	contentType := ""
	switch {
	case r.Body != nil:
		b, err := json.Marshal(r.Body)
		if err != nil {
			return nil, err
		}
		body, contentType = bytes.NewReader(b), "application/json"
	case r.Form != nil:
		body, contentType = strings.NewReader(r.Form.Encode()), "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%s %s: %w: %w", r.Method, redact(r.URL), contract.ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	ok := resp.StatusCode >= 200 && resp.StatusCode <= 299 ||
		r.AllowMultipleChoices && resp.StatusCode == http.StatusMultipleChoices
	if !ok {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return resp.Header, &StatusError{Method: r.Method, URL: redact(r.URL), Status: resp.StatusCode,
			Message: providerMessage(body), sentinel: Classify(resp.StatusCode)}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return resp.Header, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxResponseBytes)).Decode(out); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%s %s: decode: %w: %w", r.Method, redact(r.URL), contract.ErrUnavailable, err)
	}
	return resp.Header, nil
}

// redact drops the query string, which can carry markers or filters that
// do not belong in logs.
func redact(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

// NormalizeFingerprint turns "AB:CD:…" or "abcd…" into lowercase hex.
func NormalizeFingerprint(fp string) (string, error) {
	h := strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(strings.TrimSpace(fp)))
	if b, err := hex.DecodeString(h); err != nil || len(b) != sha256.Size {
		return "", errors.New("TLS fingerprint must be a SHA-256 certificate fingerprint (64 hex digits, colons allowed)")
	}
	return h, nil
}

// Client returns an HTTP client for one provider. With a TLS fingerprint
// the server certificate is pinned instead of verified against the system
// roots, which is how self-signed Proxmox certificates are trusted.
func Client(timeout time.Duration, tlsFingerprint string) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if tlsFingerprint != "" {
		want, err := NormalizeFingerprint(tlsFingerprint)
		if err != nil {
			return nil, err
		}
		// Verification is replaced, not skipped: the leaf must match the pin.
		// VerifyConnection (unlike VerifyPeerCertificate) also runs on
		// resumed sessions, so a resumed session cannot bypass the pin.
		tr.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // G402: certificate pinned in VerifyConnection
		tr.TLSClientConfig.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("no server certificate")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if hex.EncodeToString(sum[:]) != want {
				return errors.New("server certificate does not match the pinned TLS fingerprint")
			}
			return nil
		}
	}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: SameOriginRedirects}, nil
}

// ErrRedirect is returned when a provider redirects to another origin.
var ErrRedirect = errors.New("provider redirected to another host or scheme, which SEMAR does not follow")

// SameOriginRedirects follows redirects only to the same scheme and host
// (Nova answers /v2.1 with a redirect to /v2.1/). Go forwards custom
// headers such as OpenStack's X-Auth-Token to whatever host a redirect
// names, so following one elsewhere could hand a token to another host,
// and a scheme change could send it in clear text.
func SameOriginRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("%w: too many redirects", ErrRedirect)
	}
	first := via[0].URL
	if req.URL.Scheme != first.Scheme || !strings.EqualFold(req.URL.Host, first.Host) {
		return ErrRedirect
	}
	return nil
}
