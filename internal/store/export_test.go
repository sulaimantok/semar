package store

import "context"

// CountCredentialsContaining counts credential rows whose stored bytes
// contain needle. Test-only: verifies plaintext never reaches the database.
func (s *Store) CountCredentialsContaining(ctx context.Context, needle []byte) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM provider_credentials
		WHERE position($1::bytea IN wrapped_dek) > 0 OR position($1::bytea IN ciphertext) > 0
		   OR position($1::bytea IN convert_to(owner || scope, 'UTF8')) > 0`, needle).Scan(&n)
	return n, err
}

// ExecForTest runs raw SQL. Test-only: simulates tampering and direct access.
func (s *Store) ExecForTest(ctx context.Context, sql string, args ...any) error {
	_, err := s.pool.Exec(ctx, sql, args...)
	return err
}
