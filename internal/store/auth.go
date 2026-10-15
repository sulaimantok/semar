package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sulaimantok/semar/internal/auth"
)

// User is a local SEMAR account. PasswordHash is never exposed by the API.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Disabled     bool
	CreatedAt    time.Time
}

// CreateUser inserts a user; a duplicate username fails with ErrConflict.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	return createUser(ctx, s.pool, username, passwordHash)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func createUser(ctx context.Context, q queryRower, username, passwordHash string) (User, error) {
	id, err := newUUID()
	if err != nil {
		return User{}, err
	}
	u := User{ID: id, Username: username, PasswordHash: passwordHash}
	err = q.QueryRow(ctx,
		`INSERT INTO users (id, username, password_hash) VALUES ($1, $2, $3) RETURNING created_at`,
		id, username, passwordHash).Scan(&u.CreatedAt)
	return u, mapErr(err)
}

// UserByUsername returns a user including its password hash.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, disabled, created_at FROM users WHERE username = $1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Disabled, &u.CreatedAt)
	return u, mapErr(err)
}

// EnsureBootstrapAdmin creates username as a global admin if, and only if,
// no user exists yet. It reports whether the user was created. An advisory
// lock makes it safe when several replicas start at once.
func (s *Store) EnsureBootstrapAdmin(ctx context.Context, username, passwordHash string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('semar.bootstrap_admin'))`); err != nil {
		return false, mapErr(err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return false, mapErr(err)
	}
	if exists {
		return false, nil
	}
	u, err := createUser(ctx, tx, username, passwordHash)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO role_bindings (subject_type, subject_id, role) VALUES ('user', $1, 'admin')`, u.ID); err != nil {
		return false, mapErr(err)
	}
	return true, mapErr(tx.Commit(ctx))
}

// CreateRoleBinding grants role to a user, optionally scoped to a project
// and/or provider.
func (s *Store) CreateRoleBinding(ctx context.Context, userID string, b auth.RoleBinding) error {
	if !b.Role.Valid() {
		return fmt.Errorf("%w: role %q", ErrInvalid, b.Role)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO role_bindings (subject_type, subject_id, role, project_id, provider_id)
		VALUES ('user', $1, $2, $3, $4)`,
		userID, string(b.Role), nullIfEmpty(b.ProjectID), nullIfEmpty(b.ProviderID))
	return mapErr(err)
}

// CreateSession stores a session for userID under the token's hash.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, tokenHash, userID, expiresAt)
	return mapErr(err)
}

// PrincipalForSession resolves an unexpired session of an enabled user to a
// principal with its role bindings, or returns ErrNotFound.
func (s *Store) PrincipalForSession(ctx context.Context, tokenHash []byte) (*auth.Principal, error) {
	p := &auth.Principal{}
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND NOT u.disabled`, tokenHash).
		Scan(&p.UserID, &p.Username)
	if err != nil {
		return nil, mapErr(err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT role, coalesce(project_id, ''), coalesce(provider_id, '') FROM role_bindings
		WHERE subject_type = 'user' AND subject_id = $1 ORDER BY id`, p.UserID)
	if err != nil {
		return nil, mapErr(err)
	}
	p.Bindings, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (auth.RoleBinding, error) {
		var b auth.RoleBinding
		var role string
		err := r.Scan(&role, &b.ProjectID, &b.ProviderID)
		b.Role = auth.Role(role)
		return b, err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return p, nil
}

// DeleteSession removes a session; deleting an unknown session is not an error.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return mapErr(err)
}

// DeleteExpiredSessions removes expired sessions and returns how many.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, mapErr(err)
	}
	return tag.RowsAffected(), nil
}

// SetUserDisabled enables or disables a user.
func (s *Store) SetUserDisabled(ctx context.Context, userID string, disabled bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET disabled = $2, updated_at = now() WHERE id = $1`, userID, disabled)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetPassword sets a user's password hash, re-enables the account and
// ends every session of the user, in one transaction (break-glass,
// docs/runbooks/break-glass.md). It returns the number of sessions ended.
func (s *Store) ResetPassword(ctx context.Context, username, passwordHash string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx, `
		UPDATE users SET password_hash = $2, disabled = false, updated_at = now()
		WHERE username = $1 RETURNING id`, username, passwordHash).Scan(&id)
	if err != nil {
		return 0, mapErr(err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id)
	if err != nil {
		return 0, mapErr(err)
	}
	return tag.RowsAffected(), mapErr(tx.Commit(ctx))
}
