package store

import (
	"context"
	"time"
)

// ConsoleToken is an issued console token, identified by its hash.
type ConsoleToken struct {
	TokenHash   []byte
	UserID      string
	SessionHash []byte
	ProviderID  string
	ExternalID  string
	Kind        string
	ExpiresAt   time.Time
}

// CreateConsoleToken stores a token and drops tokens that expired more
// than an hour ago.
func (s *Store) CreateConsoleToken(ctx context.Context, t ConsoleToken) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM console_tokens WHERE expires_at < now() - interval '1 hour'`); err != nil {
		return mapErr(err)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO console_tokens (token_hash, user_id, session_hash, provider_id, external_id, kind, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.TokenHash, t.UserID, t.SessionHash, t.ProviderID, t.ExternalID, t.Kind, t.ExpiresAt)
	return mapErr(err)
}

// ConsumeConsoleToken redeems a token exactly once: it must be unused,
// unexpired and presented from the session it was issued to. Anything
// else is ErrNotFound, so callers cannot tell the cases apart.
func (s *Store) ConsumeConsoleToken(ctx context.Context, tokenHash, sessionHash []byte) (ConsoleToken, error) {
	t := ConsoleToken{TokenHash: tokenHash}
	err := s.pool.QueryRow(ctx, `
		UPDATE console_tokens SET used_at = now()
		WHERE token_hash = $1 AND session_hash = $2 AND used_at IS NULL AND expires_at > now()
		RETURNING user_id, session_hash, provider_id, external_id, kind, expires_at`,
		tokenHash, sessionHash).
		Scan(&t.UserID, &t.SessionHash, &t.ProviderID, &t.ExternalID, &t.Kind, &t.ExpiresAt)
	return t, mapErr(err)
}
