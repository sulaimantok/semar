package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// AuditSinkCursor returns the last event ID a sink acknowledged (0 before
// the first delivery).
func (s *Store) AuditSinkCursor(ctx context.Context, sink string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT last_id FROM audit_sink_cursors WHERE sink = $1`, sink).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, mapErr(err)
}

// SetAuditSinkCursor records the last acknowledged event ID; it never moves
// the cursor backwards.
func (s *Store) SetAuditSinkCursor(ctx context.Context, sink string, lastID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_sink_cursors (sink, last_id) VALUES ($1, $2)
		ON CONFLICT (sink) DO UPDATE SET last_id = greatest(audit_sink_cursors.last_id, EXCLUDED.last_id), updated_at = now()`,
		sink, lastID)
	return mapErr(err)
}

// TryLeadership takes a session-level advisory lock named key on a
// dedicated connection, so that only one of several replicas does a job.
// It reports false when another process holds it. Release frees the lock
// and the connection; if the connection is lost the lock is freed too.
func (s *Store) TryLeadership(ctx context.Context, key string) (release func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, mapErr(err)
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, key).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, mapErr(err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtext($1))`, key)
		conn.Release()
	}, true, nil
}
