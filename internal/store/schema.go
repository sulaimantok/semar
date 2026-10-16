package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSchemaBehind is returned while migrations this binary needs are not
// applied yet (for example during an upgrade, before `semar-api migrate`).
var ErrSchemaBehind = errors.New("store: database schema is older than this binary")

// requiredVersion is the highest migration embedded in this binary.
func requiredVersion() (int64, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return 0, err
	}
	var latest int64
	for _, e := range entries {
		prefix, _, ok := strings.Cut(path.Base(e.Name()), "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			continue
		}
		latest = max(latest, v)
	}
	return latest, nil
}

// SchemaVersion reports the applied migration version and the version this
// binary requires.
func (s *Store) SchemaVersion(ctx context.Context) (applied, required int64, err error) {
	if required, err = requiredVersion(); err != nil {
		return 0, 0, err
	}
	err = s.pool.QueryRow(ctx, `SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&applied)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // undefined_table: never migrated
		return 0, required, nil
	}
	return applied, required, mapErr(err)
}

// CheckSchema fails with ErrSchemaBehind while migrations are pending. It
// serves as a readiness check.
func (s *Store) CheckSchema(ctx context.Context) error {
	applied, required, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if applied < required {
		return fmt.Errorf("%w: applied %d, required %d", ErrSchemaBehind, applied, required)
	}
	return nil
}

// WaitForSchema blocks until the migrations this binary needs are applied,
// calling waiting after each check that finds them pending. Processes that
// do not migrate themselves (the worker; the API with migrations off) use
// it so that they never query columns that do not exist yet.
func (s *Store) WaitForSchema(ctx context.Context, every time.Duration, waiting func(applied, required int64)) error {
	for {
		applied, required, err := s.SchemaVersion(ctx)
		if err == nil && applied >= required {
			return nil
		}
		if err == nil && waiting != nil {
			waiting(applied, required)
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return fmt.Errorf("wait for schema: %w (last error: %w)", ctx.Err(), err)
			}
			return fmt.Errorf("wait for schema: %w: applied %d, required %d", ctx.Err(), applied, required)
		case <-time.After(every):
		}
	}
}
