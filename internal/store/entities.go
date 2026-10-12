package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/model"
)

// CreateSite inserts a site.
func (s *Store) CreateSite(ctx context.Context, site model.Site) (model.Site, error) {
	if err := model.ValidateID("site", site.ID); err != nil {
		return model.Site{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if site.Labels == nil {
		site.Labels = map[string]string{}
	}
	labels, err := json.Marshal(site.Labels)
	if err != nil {
		return model.Site{}, err
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO sites (id, name, labels) VALUES ($1, $2, $3) RETURNING created_at, updated_at`,
		site.ID, site.Name, labels).Scan(&site.CreatedAt, &site.UpdatedAt)
	return site, mapErr(err)
}

// ListSites returns all sites ordered by ID.
func (s *Store) ListSites(ctx context.Context) ([]model.Site, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, labels, created_at, updated_at FROM sites ORDER BY id`)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Site, error) {
		var site model.Site
		var labels []byte
		if err := r.Scan(&site.ID, &site.Name, &labels, &site.CreatedAt, &site.UpdatedAt); err != nil {
			return model.Site{}, err
		}
		return site, json.Unmarshal(labels, &site.Labels)
	})
}

// CreateProject inserts a project.
func (s *Store) CreateProject(ctx context.Context, p model.Project) (model.Project, error) {
	if err := model.ValidateID("project", p.ID); err != nil {
		return model.Project{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO projects (id, name) VALUES ($1, $2) RETURNING created_at, updated_at`,
		p.ID, p.Name).Scan(&p.CreatedAt, &p.UpdatedAt)
	return p, mapErr(err)
}

// ListProjects returns all projects ordered by ID.
func (s *Store) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at, updated_at FROM projects ORDER BY id`)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Project, error) {
		var p model.Project
		err := r.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt)
		return p, err
	})
}

const providerColumns = `id, type, name, coalesce(site_id, ''), endpoints, region_name, fingerprint,
	tls_fingerprint, capabilities, max_microversions, created_at, updated_at, deleted_at`

func scanProvider(r pgx.Row) (model.Provider, error) {
	var p model.Provider
	var typ string
	var caps, micro []byte
	err := r.Scan(&p.ID, &typ, &p.Name, &p.SiteID, &p.Endpoints, &p.RegionName, &p.Fingerprint,
		&p.TLSFingerprint, &caps, &micro, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt)
	if err != nil {
		return model.Provider{}, err
	}
	p.Type = contract.ProviderType(typ)
	p.Capabilities = caps
	if err := json.Unmarshal(micro, &p.MaxMicroversions); err != nil {
		return model.Provider{}, err
	}
	return p, nil
}

// CreateProvider registers a provider. Registering a cloud whose fingerprint
// matches an active provider fails with ErrConflict.
func (s *Store) CreateProvider(ctx context.Context, p model.Provider) (model.Provider, error) {
	if err := model.ValidateID("provider", p.ID); err != nil {
		return model.Provider{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(p.Capabilities) == 0 {
		p.Capabilities = json.RawMessage(`{}`)
	}
	if p.MaxMicroversions == nil {
		p.MaxMicroversions = map[string]string{}
	}
	micro, err := json.Marshal(p.MaxMicroversions)
	if err != nil {
		return model.Provider{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO providers (id, type, name, site_id, endpoints, region_name, fingerprint,
		                       tls_fingerprint, capabilities, max_microversions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+providerColumns,
		p.ID, string(p.Type), p.Name, nullIfEmpty(p.SiteID), p.Endpoints, p.RegionName, p.Fingerprint,
		p.TLSFingerprint, []byte(p.Capabilities), micro)
	created, err := scanProvider(row)
	return created, mapErr(err)
}

// GetProvider returns an active provider.
func (s *Store) GetProvider(ctx context.Context, id string) (model.Provider, error) {
	p, err := scanProvider(s.pool.QueryRow(ctx,
		`SELECT `+providerColumns+` FROM providers WHERE id = $1 AND deleted_at IS NULL`, id))
	return p, mapErr(err)
}

// ListProviders returns active providers ordered by ID.
func (s *Store) ListProviders(ctx context.Context) ([]model.Provider, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+providerColumns+` FROM providers WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Provider, error) { return scanProvider(r) })
}

// DeleteProvider soft-deletes a provider so its history stays intact.
func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE providers SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateProjectMapping maps an external scope to a project. Mapping a scope
// that is already mapped fails with ErrConflict.
func (s *Store) CreateProjectMapping(ctx context.Context, m model.ProjectMapping) (model.ProjectMapping, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO project_mappings (project_id, provider_id, external_scope)
		VALUES ($1, $2, $3) RETURNING id, created_at`,
		m.ProjectID, m.ProviderID, m.ExternalScope).Scan(&m.ID, &m.CreatedAt)
	return m, mapErr(err)
}

// ProjectForScope returns the project an external scope maps to, or
// ErrNotFound when it is unassigned.
func (s *Store) ProjectForScope(ctx context.Context, providerID, externalScope string) (string, error) {
	var projectID string
	err := s.pool.QueryRow(ctx,
		`SELECT project_id FROM project_mappings WHERE provider_id = $1 AND external_scope = $2`,
		providerID, externalScope).Scan(&projectID)
	return projectID, mapErr(err)
}
