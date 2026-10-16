// Package auditsink streams SEMAR's audit log to an external HTTP endpoint
// (a SIEM collector, a log pipeline) so that a copy of every event lives
// outside SEMAR's database (tracker 1.1).
//
// Delivery is at least once and in order: events are sent in batches as
// JSON, signed with HMAC-SHA256 when a secret is set, and the cursor
// advances only after a 2xx answer. Each event carries its hash and the
// previous event's hash, so the receiver can verify the chain and drop
// duplicates by ID. Only one semar-api replica streams at a time.
package auditsink

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/sulaimantok/semar/internal/store"
)

// Store is what the streamer needs; *store.Store implements it.
type Store interface {
	ListAuditEvents(ctx context.Context, afterID int64, limit int) ([]store.AuditEvent, error)
	AuditSinkCursor(ctx context.Context, sink string) (int64, error)
	SetAuditSinkCursor(ctx context.Context, sink string, lastID int64) error
	TryLeadership(ctx context.Context, key string) (release func(), ok bool, err error)
}

var _ Store = (*store.Store)(nil)

// SignatureHeader carries "sha256=<hex HMAC of the body>".
const SignatureHeader = "X-SEMAR-Signature"

// Event is the wire form of an audit event.
type Event struct {
	ID         int64             `json:"id"`
	OccurredAt time.Time         `json:"occurredAt"`
	Actor      string            `json:"actor"`
	Action     string            `json:"action"`
	Target     string            `json:"target"`
	Outcome    string            `json:"outcome"`
	RequestID  string            `json:"requestId,omitempty"`
	Details    map[string]string `json:"details,omitempty"`
	PrevHash   string            `json:"prevHash"`
	Hash       string            `json:"hash"`
}

// Batch is one delivery.
type Batch struct {
	Sink   string  `json:"sink"`
	Events []Event `json:"events"`
}

// Streamer delivers the audit log to one webhook.
type Streamer struct {
	Store  Store
	URL    string
	Secret string
	// Name identifies the sink's cursor. Defaults to "webhook".
	Name     string
	Client   *http.Client
	Logger   *slog.Logger
	Interval time.Duration // between polls when caught up; default 5s
	Batch    int           // events per delivery; default 100
}

func (s *Streamer) defaults() {
	if s.Name == "" {
		s.Name = "webhook"
	}
	if s.Client == nil {
		s.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.Interval <= 0 {
		s.Interval = 5 * time.Second
	}
	if s.Batch <= 0 {
		s.Batch = 100
	}
}

// Run streams until ctx ends. Failures are retried with exponential
// backoff (up to five minutes); the cursor only moves on success.
func (s *Streamer) Run(ctx context.Context) {
	s.defaults()
	backoff := s.Interval
	for {
		wait := s.Interval
		if err := s.lead(ctx); err != nil && ctx.Err() == nil {
			s.Logger.WarnContext(ctx, "audit sink delivery failed; retrying", slog.String("sink", s.Name),
				slog.Any("error", err), slog.Duration("retry_in", backoff))
			wait = backoff
			backoff = min(backoff*2, 5*time.Minute)
		} else {
			backoff = s.Interval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// lead delivers while holding the sink's leadership lock; it returns when
// not leader, caught up, or on the first failure.
func (s *Streamer) lead(ctx context.Context) error {
	release, ok, err := s.Store.TryLeadership(ctx, "semar.auditsink."+s.Name)
	if err != nil || !ok {
		return err
	}
	defer release()
	for {
		n, err := s.DeliverOnce(ctx)
		if err != nil || n < s.Batch {
			return err
		}
	}
}

// DeliverOnce sends the next batch, if any, and returns how many events
// were acknowledged.
func (s *Streamer) DeliverOnce(ctx context.Context) (int, error) {
	s.defaults()
	after, err := s.Store.AuditSinkCursor(ctx, s.Name)
	if err != nil {
		return 0, err
	}
	events, err := s.Store.ListAuditEvents(ctx, after, s.Batch)
	if err != nil || len(events) == 0 {
		return 0, err
	}
	b := Batch{Sink: s.Name, Events: make([]Event, len(events))}
	for i, e := range events {
		b.Events[i] = Event{
			ID: e.ID, OccurredAt: e.OccurredAt, Actor: e.Actor, Action: e.Action, Target: e.Target,
			Outcome: string(e.Outcome), RequestID: e.RequestID, Details: e.Details,
			PrevHash: hex.EncodeToString(e.PrevHash), Hash: hex.EncodeToString(e.Hash),
		}
	}
	body, err := json.Marshal(b)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SEMAR-Delivery", strconv.FormatInt(events[0].ID, 10)+"-"+strconv.FormatInt(events[len(events)-1].ID, 10))
	if s.Secret != "" {
		req.Header.Set(SignatureHeader, Sign(s.Secret, body))
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("audit sink: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, fmt.Errorf("audit sink answered HTTP %d", resp.StatusCode)
	}
	if err := s.Store.SetAuditSinkCursor(ctx, s.Name, events[len(events)-1].ID); err != nil {
		return 0, err
	}
	return len(events), nil
}

// Sign returns the signature header value for body.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Verify checks a signature header in constant time; receivers can use it.
func Verify(secret string, body []byte, header string) bool {
	return hmac.Equal([]byte(Sign(secret, body)), []byte(header))
}
