package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/secret"
)

const credentialsTable = "provider_credentials"

func credentialAAD(id string, purpose model.CredentialPurpose) []byte {
	return secret.AAD(credentialsTable, id, string(purpose))
}

// PutCredential stores, or replaces, the credential for (provider, purpose,
// scope), sealing plaintext with ks. A replaced credential keeps its row ID
// so its AAD binding stays stable; plaintext never reaches the database.
func (s *Store) PutCredential(ctx context.Context, ks secret.Store, c model.ProviderCredential, plaintext []byte) (model.ProviderCredential, error) {
	if !c.Purpose.Valid() {
		return model.ProviderCredential{}, fmt.Errorf("%w: credential purpose %q", ErrInvalid, c.Purpose)
	}
	// A concurrent first insert of the same (provider, purpose, scope) makes
	// our insert a no-op; retry so we seal against the winner's row ID.
	for attempt := 0; attempt < 3; attempt++ {
		out, err := s.putCredentialOnce(ctx, ks, c, plaintext)
		if !errors.Is(err, errRetryPut) {
			return out, err
		}
	}
	return model.ProviderCredential{}, fmt.Errorf("%w: credential write kept racing", ErrConflict)
}

var errRetryPut = errors.New("retry put credential")

func (s *Store) putCredentialOnce(ctx context.Context, ks secret.Store, c model.ProviderCredential, plaintext []byte) (model.ProviderCredential, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ProviderCredential{}, mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
		SELECT id FROM provider_credentials
		WHERE provider_id = $1 AND purpose = $2 AND scope = $3 FOR UPDATE`,
		c.ProviderID, string(c.Purpose), c.Scope).Scan(&id)
	existing := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return model.ProviderCredential{}, mapErr(err)
	}
	if !existing {
		if id, err = newUUID(); err != nil {
			return model.ProviderCredential{}, err
		}
	}

	sealed, err := ks.Seal(plaintext, credentialAAD(id, c.Purpose))
	if err != nil {
		return model.ProviderCredential{}, err
	}

	var row pgx.Row
	if existing {
		row = tx.QueryRow(ctx, `
			UPDATE provider_credentials
			SET owner = $2, kek_version = $3, wrapped_dek = $4, ciphertext = $5, expires_at = $6, rotated_at = now()
			WHERE id = $1
			RETURNING id, created_at, rotated_at`,
			id, c.Owner, sealed.KEKVersion, sealed.WrappedDEK, sealed.Ciphertext, c.ExpiresAt)
	} else {
		row = tx.QueryRow(ctx, `
			INSERT INTO provider_credentials
			  (id, provider_id, purpose, scope, owner, kek_version, wrapped_dek, ciphertext, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (provider_id, purpose, scope) DO NOTHING
			RETURNING id, created_at, rotated_at`,
			id, c.ProviderID, string(c.Purpose), c.Scope, c.Owner, sealed.KEKVersion, sealed.WrappedDEK, sealed.Ciphertext, c.ExpiresAt)
	}
	if err := row.Scan(&c.ID, &c.CreatedAt, &c.RotatedAt); err != nil {
		if !existing && errors.Is(err, pgx.ErrNoRows) {
			return model.ProviderCredential{}, errRetryPut
		}
		return model.ProviderCredential{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ProviderCredential{}, mapErr(err)
	}
	c.KEKVersion = sealed.KEKVersion
	return c, nil
}

// OpenCredential returns the credential's metadata and decrypted secret.
func (s *Store) OpenCredential(ctx context.Context, ks secret.Store, providerID string, purpose model.CredentialPurpose, scope string) (model.ProviderCredential, []byte, error) {
	c := model.ProviderCredential{ProviderID: providerID, Purpose: purpose, Scope: scope}
	var sealed secret.Sealed
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner, kek_version, wrapped_dek, ciphertext, expires_at, created_at, rotated_at
		FROM provider_credentials WHERE provider_id = $1 AND purpose = $2 AND scope = $3`,
		providerID, string(purpose), scope).
		Scan(&c.ID, &c.Owner, &sealed.KEKVersion, &sealed.WrappedDEK, &sealed.Ciphertext, &c.ExpiresAt, &c.CreatedAt, &c.RotatedAt)
	if err != nil {
		return model.ProviderCredential{}, nil, mapErr(err)
	}
	c.KEKVersion = sealed.KEKVersion
	plaintext, err := ks.Open(sealed, credentialAAD(c.ID, purpose))
	if err != nil {
		return model.ProviderCredential{}, nil, fmt.Errorf("open credential %s: %w", c.ID, err)
	}
	return c, plaintext, nil
}

// RewrapCredentials rewraps every credential not sealed with ks's current
// KEK, in batches, and returns how many were rewrapped. Ciphertexts are not
// touched (ADR-0004). It is safe to run concurrently with other writers.
func (s *Store) RewrapCredentials(ctx context.Context, ks secret.Store) (int, error) {
	const batch = 100
	total := 0
	for {
		n, err := s.rewrapBatch(ctx, ks, batch)
		total += n
		if err != nil || n < batch {
			return total, err
		}
	}
}

func (s *Store) rewrapBatch(ctx context.Context, ks secret.Store, limit int) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, purpose, kek_version, wrapped_dek, ciphertext FROM provider_credentials
		WHERE kek_version <> $1 ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`,
		ks.CurrentVersion(), limit)
	if err != nil {
		return 0, mapErr(err)
	}
	type item struct {
		id      string
		purpose model.CredentialPurpose
		sealed  secret.Sealed
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
		var it item
		var purpose string
		err := r.Scan(&it.id, &purpose, &it.sealed.KEKVersion, &it.sealed.WrappedDEK, &it.sealed.Ciphertext)
		it.purpose = model.CredentialPurpose(purpose)
		return it, err
	})
	if err != nil {
		return 0, mapErr(err)
	}

	for _, it := range items {
		rewrapped, _, err := ks.Rewrap(it.sealed, credentialAAD(it.id, it.purpose))
		if err != nil {
			return 0, fmt.Errorf("rewrap credential %s: %w", it.id, err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE provider_credentials SET kek_version = $2, wrapped_dek = $3 WHERE id = $1`,
			it.id, rewrapped.KEKVersion, rewrapped.WrappedDEK); err != nil {
			return 0, mapErr(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, mapErr(err)
	}
	return len(items), nil
}

// ListCredentials returns the metadata of a provider's credentials, ordered
// by purpose and scope. Secrets are never read.
func (s *Store) ListCredentials(ctx context.Context, providerID string) ([]model.ProviderCredential, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, provider_id, purpose, scope, owner, kek_version, expires_at, created_at, rotated_at
		FROM provider_credentials WHERE provider_id = $1 ORDER BY purpose, scope`, providerID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ProviderCredential, error) {
		var c model.ProviderCredential
		var purpose string
		err := r.Scan(&c.ID, &c.ProviderID, &purpose, &c.Scope, &c.Owner, &c.KEKVersion, &c.ExpiresAt, &c.CreatedAt, &c.RotatedAt)
		c.Purpose = model.CredentialPurpose(purpose)
		return c, err
	})
}

// DeleteCredential removes the credential for (provider, purpose, scope).
func (s *Store) DeleteCredential(ctx context.Context, providerID string, purpose model.CredentialPurpose, scope string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM provider_credentials WHERE provider_id = $1 AND purpose = $2 AND scope = $3`,
		providerID, string(purpose), scope)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
