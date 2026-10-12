package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuditOutcome is the result recorded for an audited action.
type AuditOutcome string

const (
	AuditRequested AuditOutcome = "requested"
	AuditSuccess   AuditOutcome = "success"
	AuditFailure   AuditOutcome = "failure"
)

// AuditEvent is one entry of the append-only audit log (roadmap §3.4).
type AuditEvent struct {
	ID         int64
	OccurredAt time.Time
	Actor      string
	Action     string
	Target     string
	Outcome    AuditOutcome
	RequestID  string
	// Details holds flat, non-secret context. Values must never contain
	// credentials or tokens.
	Details  map[string]string
	PrevHash []byte
	Hash     []byte
}

// genesisHash is the prev_hash of the first event.
var genesisHash = make([]byte, sha256.Size)

// eventHash computes SHA-256(prev_hash || canonical event). The canonical
// form is a JSON array; json.Marshal sorts map keys, so it is deterministic.
func eventHash(prev []byte, e AuditEvent) ([]byte, error) {
	details := e.Details
	if details == nil {
		details = map[string]string{}
	}
	canonical, err := json.Marshal([]any{
		e.OccurredAt.UTC().Format(time.RFC3339Nano), e.Actor, e.Action, e.Target,
		string(e.Outcome), e.RequestID, details,
	})
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(prev)
	h.Write(canonical)
	return h.Sum(nil), nil
}

// AppendAudit appends e to the hash chain. Appends are serialised with a
// transaction-scoped advisory lock so the chain never forks.
func (s *Store) AppendAudit(ctx context.Context, e AuditEvent) (AuditEvent, error) {
	switch e.Outcome {
	case AuditRequested, AuditSuccess, AuditFailure:
	default:
		return AuditEvent{}, fmt.Errorf("%w: audit outcome %q", ErrInvalid, e.Outcome)
	}
	if e.Actor == "" || e.Action == "" {
		return AuditEvent{}, fmt.Errorf("%w: audit actor and action are required", ErrInvalid)
	}
	// PostgreSQL stores microseconds; hash exactly what is stored.
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	e.OccurredAt = e.OccurredAt.UTC().Truncate(time.Microsecond)
	if e.Details == nil {
		e.Details = map[string]string{}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuditEvent{}, mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('semar.audit_chain'))`); err != nil {
		return AuditEvent{}, mapErr(err)
	}
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&e.PrevHash)
	if errors.Is(err, pgx.ErrNoRows) {
		e.PrevHash = genesisHash
	} else if err != nil {
		return AuditEvent{}, mapErr(err)
	}
	if e.Hash, err = eventHash(e.PrevHash, e); err != nil {
		return AuditEvent{}, err
	}
	details, err := json.Marshal(e.Details)
	if err != nil {
		return AuditEvent{}, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO audit_events (occurred_at, actor, action, target, outcome, request_id, details, prev_hash, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		e.OccurredAt, e.Actor, e.Action, e.Target, string(e.Outcome), e.RequestID, details, e.PrevHash, e.Hash).
		Scan(&e.ID)
	if err != nil {
		return AuditEvent{}, mapErr(err)
	}
	return e, mapErr(tx.Commit(ctx))
}

// ErrAuditChainBroken is returned when the audit log has been altered.
var ErrAuditChainBroken = errors.New("audit chain broken")

// VerifyAuditChain recomputes every hash in order and returns the number of
// verified events. It fails at the first event whose content or link does
// not match.
func (s *Store) VerifyAuditChain(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, occurred_at, actor, action, target, outcome, request_id, details, prev_hash, hash
		FROM audit_events ORDER BY id`)
	if err != nil {
		return 0, mapErr(err)
	}
	defer rows.Close()

	prev := genesisHash
	n := 0
	for rows.Next() {
		var e AuditEvent
		var outcome string
		var details []byte
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.Actor, &e.Action, &e.Target, &outcome, &e.RequestID, &details, &e.PrevHash, &e.Hash); err != nil {
			return n, mapErr(err)
		}
		e.Outcome = AuditOutcome(outcome)
		if err := json.Unmarshal(details, &e.Details); err != nil {
			return n, fmt.Errorf("%w at event %d: details: %w", ErrAuditChainBroken, e.ID, err)
		}
		if !bytes.Equal(e.PrevHash, prev) {
			return n, fmt.Errorf("%w at event %d: link to previous event does not match", ErrAuditChainBroken, e.ID)
		}
		want, err := eventHash(prev, e)
		if err != nil {
			return n, err
		}
		if !bytes.Equal(want, e.Hash) {
			return n, fmt.Errorf("%w at event %d: content was modified", ErrAuditChainBroken, e.ID)
		}
		prev = e.Hash
		n++
	}
	return n, mapErr(rows.Err())
}

// ListAuditEvents returns up to limit events with ID greater than afterID,
// oldest first (keyset pagination).
func (s *Store) ListAuditEvents(ctx context.Context, afterID int64, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, occurred_at, actor, action, target, outcome, request_id, details, prev_hash, hash
		FROM audit_events WHERE id > $1 ORDER BY id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AuditEvent, error) {
		var e AuditEvent
		var outcome string
		var details []byte
		if err := r.Scan(&e.ID, &e.OccurredAt, &e.Actor, &e.Action, &e.Target, &outcome, &e.RequestID, &details, &e.PrevHash, &e.Hash); err != nil {
			return AuditEvent{}, err
		}
		e.Outcome = AuditOutcome(outcome)
		return e, json.Unmarshal(details, &e.Details)
	})
}
