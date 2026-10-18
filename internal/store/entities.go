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

// GetProject returns one project.
func (s *Store) GetProject(ctx context.Context, id string) (model.Project, error) {
	var p model.Project
	err := s.pool.QueryRow(ctx, `SELECT id, name, created_at, updated_at FROM projects WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt)
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
	tls_fingerprint, capabilities, max_microversions, coalesce(cloud_fingerprint, ''), validated_at,
	created_at, updated_at, deleted_at`

func scanProvider(r pgx.Row) (model.Provider, error) {
	var p model.Provider
	var typ string
	var caps, micro []byte
	err := r.Scan(&p.ID, &typ, &p.Name, &p.SiteID, &p.Endpoints, &p.RegionName, &p.Fingerprint,
		&p.TLSFingerprint, &caps, &micro, &p.CloudFingerprint, &p.ValidatedAt, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt)
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

// UpdateProvider changes an active provider's name, site, endpoints, region
// and pinned TLS fingerprint; ID and type are immutable. p.Fingerprint must be re-derived by
// the caller. A change of endpoints or region clears the connection check,
// since the new address has not been verified yet. Moving onto another
// provider's fingerprint fails with ErrConflict.
func (s *Store) UpdateProvider(ctx context.Context, p model.Provider) (model.Provider, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE providers SET
		  name = $2, site_id = $3, endpoints = $4, region_name = $5, fingerprint = $6, tls_fingerprint = $7,
		  cloud_fingerprint = CASE WHEN endpoints = $4 AND region_name = $5 THEN cloud_fingerprint END,
		  validated_at      = CASE WHEN endpoints = $4 AND region_name = $5 THEN validated_at END,
		  updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING `+providerColumns,
		p.ID, p.Name, nullIfEmpty(p.SiteID), p.Endpoints, p.RegionName, p.Fingerprint, p.TLSFingerprint)
	updated, err := scanProvider(row)
	return updated, mapErr(err)
}

// RecordProviderValidation stores the outcome of a successful connection
// check. A cloud fingerprint already held by another active provider fails
// with ErrConflict.
func (s *Store) RecordProviderValidation(ctx context.Context, id, cloudFingerprint string, apiVersions map[string]string) (model.Provider, error) {
	if apiVersions == nil {
		apiVersions = map[string]string{}
	}
	micro, err := json.Marshal(apiVersions)
	if err != nil {
		return model.Provider{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE providers SET cloud_fingerprint = $2, max_microversions = $3, validated_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING `+providerColumns, id, nullIfEmpty(cloudFingerprint), micro)
	p, err := scanProvider(row)
	return p, mapErr(err)
}

// ProviderByCloudFingerprint returns the active provider holding a cloud
// fingerprint.
func (s *Store) ProviderByCloudFingerprint(ctx context.Context, cloudFingerprint string) (model.Provider, error) {
	p, err := scanProvider(s.pool.QueryRow(ctx,
		`SELECT `+providerColumns+` FROM providers WHERE cloud_fingerprint = $1 AND deleted_at IS NULL`, cloudFingerprint))
	return p, mapErr(err)
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

// DeleteProvider soft-deletes a provider so its history stays intact, and
// deletes its credentials in the same transaction: secrets for a cloud SEMAR
// no longer manages must not linger.
func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE providers SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM provider_credentials WHERE provider_id = $1`, id); err != nil {
		return mapErr(err)
	}
	return mapErr(tx.Commit(ctx))
}

// CreateProjectMapping maps an external scope to a project. Mapping a scope
// that is already mapped fails with ErrConflict.
func (s *Store) CreateProjectMapping(ctx context.Context, m model.ProjectMapping) (model.ProjectMapping, error) {
	if err := ValidateMappingScope(m.ExternalScope); err != nil {
		return model.ProjectMapping{}, err
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO project_mappings (project_id, provider_id, external_scope)
		VALUES ($1, $2, $3) RETURNING id, created_at`,
		m.ProjectID, m.ProviderID, m.ExternalScope).Scan(&m.ID, &m.CreatedAt)
	return m, mapErr(err)
}

// UpdateProjectMapping changes a mapping of projectID. A scope already
// mapped on that provider fails with ErrConflict.
func (s *Store) UpdateProjectMapping(ctx context.Context, projectID string, id int64, providerID, externalScope string) (model.ProjectMapping, error) {
	if err := ValidateMappingScope(externalScope); err != nil {
		return model.ProjectMapping{}, err
	}
	m := model.ProjectMapping{ID: id, ProjectID: projectID}
	err := s.pool.QueryRow(ctx, `
		UPDATE project_mappings SET provider_id = $3, external_scope = $4
		WHERE id = $1 AND project_id = $2
		RETURNING provider_id, external_scope, created_at`, id, projectID, providerID, externalScope).
		Scan(&m.ProviderID, &m.ExternalScope, &m.CreatedAt)
	return m, mapErr(err)
}

// GetProjectMapping returns one mapping of projectID.
func (s *Store) GetProjectMapping(ctx context.Context, projectID string, id int64) (model.ProjectMapping, error) {
	m := model.ProjectMapping{ID: id, ProjectID: projectID}
	err := s.pool.QueryRow(ctx, `SELECT provider_id, external_scope, created_at FROM project_mappings WHERE id = $1 AND project_id = $2`,
		id, projectID).Scan(&m.ProviderID, &m.ExternalScope, &m.CreatedAt)
	return m, mapErr(err)
}

// DeleteProjectMapping removes a mapping of projectID.
func (s *Store) DeleteProjectMapping(ctx context.Context, projectID string, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM project_mappings WHERE id = $1 AND project_id = $2`, id, projectID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListProjectMappings returns a project's mappings ordered by provider and scope.
func (s *Store) ListProjectMappings(ctx context.Context, projectID string) ([]model.ProjectMapping, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, provider_id, external_scope, created_at FROM project_mappings
		WHERE project_id = $1 ORDER BY provider_id, external_scope`, projectID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ProjectMapping, error) {
		var m model.ProjectMapping
		err := r.Scan(&m.ID, &m.ProjectID, &m.ProviderID, &m.ExternalScope, &m.CreatedAt)
		return m, err
	})
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
