package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ResourceKind is the type of an inventory resource.
type ResourceKind string

const (
	KindHypervisor ResourceKind = "hypervisor"
	KindVM         ResourceKind = "vm"
	KindImage      ResourceKind = "image"
	KindNetwork    ResourceKind = "network"
	KindVolume     ResourceKind = "volume"
	// KindSecurityGroup is listed by drivers implementing
	// contract.SecurityGroupLister.
	KindSecurityGroup ResourceKind = "security_group"
)

// Valid reports whether k is a known kind.
func (k ResourceKind) Valid() bool {
	switch k {
	case KindHypervisor, KindVM, KindImage, KindNetwork, KindVolume, KindSecurityGroup:
		return true
	}
	return false
}

// ObservedResource is one resource as reported by a provider.
type ObservedResource struct {
	Kind          ResourceKind
	ExternalID    string
	Name          string
	ExternalScope string
	Arch          string
	Status        string
	Spec          map[string]any
}

// Snapshot is a complete inventory of one provider at ObservedAt. It must be
// complete: resources missing from it count as missed by this sync.
type Snapshot struct {
	ProviderID string
	ObservedAt time.Time
	Resources  []ObservedResource
}

// SyncResult summarises what ApplySnapshot changed.
type SyncResult struct {
	Created   int
	Updated   int
	Unchanged int
	Restored  int
	Orphaned  int
	// Missed counts resources absent from this snapshot but not yet orphaned.
	Missed int
	// Leased counts resources skipped because a workflow holds their lease.
	Leased int
}

// ErrStaleSnapshot is returned for a snapshot that is not newer than the
// last one applied (e.g. a retried activity finishing late).
var ErrStaleSnapshot = errors.New("store: snapshot is older than the last applied sync")

type resourceKey struct {
	kind ResourceKind
	id   string
}

type existingResource struct {
	hash     []byte
	orphaned bool
	missed   int
	leased   bool
}

// ApplySnapshot writes a provider's snapshot to the inventory cache in one
// transaction (roadmap §3.6):
//   - snapshots not newer than the last applied one are rejected;
//   - resources leased by a workflow are left untouched;
//   - project ownership is re-derived from project_mappings (exact scope,
//     then tag and name rules, see ownership);
//   - a resource is orphaned only after orphanAfter consecutive misses and is
//     never deleted, so history stays intact;
//   - every create, change, orphaning and restore is appended to
//     resource_changes.
func (s *Store) ApplySnapshot(ctx context.Context, snap Snapshot, orphanAfter int) (SyncResult, error) {
	var res SyncResult
	if orphanAfter < 1 {
		orphanAfter = 1
	}
	if snap.ObservedAt.IsZero() {
		return res, fmt.Errorf("%w: snapshot without observed_at", ErrInvalid)
	}
	observedAt := snap.ObservedAt.UTC().Truncate(time.Microsecond)
	seen := make(map[resourceKey]bool, len(snap.Resources))
	for _, r := range snap.Resources {
		k := resourceKey{r.Kind, r.ExternalID}
		if !r.Kind.Valid() || r.ExternalID == "" {
			return res, fmt.Errorf("%w: resource kind %q id %q", ErrInvalid, r.Kind, r.ExternalID)
		}
		if seen[k] {
			return res, fmt.Errorf("%w: duplicate %s %s in snapshot", ErrInvalid, r.Kind, r.ExternalID)
		}
		seen[k] = true
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('semar.sync.' || $1))`, snap.ProviderID); err != nil {
		return res, mapErr(err)
	}
	var active bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM providers WHERE id = $1 AND deleted_at IS NULL)`, snap.ProviderID).Scan(&active); err != nil {
		return res, mapErr(err)
	}
	if !active {
		return res, ErrNotFound
	}
	var last *time.Time
	err = tx.QueryRow(ctx, `SELECT last_observed_at FROM provider_sync_status WHERE provider_id = $1`, snap.ProviderID).Scan(&last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return res, mapErr(err)
	}
	if last != nil && !observedAt.After(*last) {
		return res, ErrStaleSnapshot
	}

	scopes, err := scopeProjects(ctx, tx, snap.ProviderID)
	if err != nil {
		return res, err
	}
	owners := newOwnership(scopes)
	existing, err := lockExisting(ctx, tx, snap.ProviderID)
	if err != nil {
		return res, err
	}

	batch := &pgx.Batch{}
	for _, r := range snap.Resources {
		k := resourceKey{r.Kind, r.ExternalID}
		old, found := existing[k]
		if found && old.leased {
			res.Leased++
			continue
		}
		var project *string
		if p, ok := owners.projectOf(r); ok {
			project = &p
		}
		arch := r.Arch
		if arch == "" {
			arch = "unknown"
		}
		spec := r.Spec
		if spec == nil {
			spec = map[string]any{}
		}
		specJSON, hash, err := resourceHash(r.Name, r.ExternalScope, project, arch, r.Status, spec)
		if err != nil {
			return res, err
		}

		change := ""
		switch {
		case !found:
			change = "created"
			res.Created++
		case old.orphaned:
			change = "restored"
			res.Restored++
		case string(old.hash) != string(hash):
			change = "updated"
			res.Updated++
		default:
			res.Unchanged++
		}
		batch.Queue(`
			INSERT INTO resources (provider_id, kind, external_id, name, external_scope, project_id, arch, status,
			                       spec, spec_hash, observed_at, first_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
			ON CONFLICT (provider_id, kind, external_id) DO UPDATE SET
			  name = EXCLUDED.name, external_scope = EXCLUDED.external_scope, project_id = EXCLUDED.project_id,
			  arch = EXCLUDED.arch, status = EXCLUDED.status, spec = EXCLUDED.spec, spec_hash = EXCLUDED.spec_hash,
			  observed_at = EXCLUDED.observed_at, missed_syncs = 0, orphaned_at = NULL`,
			snap.ProviderID, string(r.Kind), r.ExternalID, r.Name, r.ExternalScope, project, arch, r.Status,
			specJSON, hash, observedAt)
		if change != "" {
			batch.Queue(`
				INSERT INTO resource_changes (provider_id, kind, external_id, change, observed_at, project_id, spec)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				snap.ProviderID, string(r.Kind), r.ExternalID, change, observedAt, project, specJSON)
		}
	}

	for k, old := range existing {
		if seen[k] || old.orphaned || old.leased {
			continue
		}
		missed := old.missed + 1
		if missed < orphanAfter {
			res.Missed++
			batch.Queue(`UPDATE resources SET missed_syncs = $4 WHERE provider_id = $1 AND kind = $2 AND external_id = $3`,
				snap.ProviderID, string(k.kind), k.id, missed)
			continue
		}
		res.Orphaned++
		batch.Queue(`UPDATE resources SET missed_syncs = $4, orphaned_at = $5 WHERE provider_id = $1 AND kind = $2 AND external_id = $3`,
			snap.ProviderID, string(k.kind), k.id, missed, observedAt)
		batch.Queue(`
			INSERT INTO resource_changes (provider_id, kind, external_id, change, observed_at, project_id, spec)
			SELECT provider_id, kind, external_id, 'orphaned', $4, project_id, spec FROM resources
			WHERE provider_id = $1 AND kind = $2 AND external_id = $3`,
			snap.ProviderID, string(k.kind), k.id, observedAt)
	}

	batch.Queue(`
		INSERT INTO provider_sync_status (provider_id, status, last_attempt_at, last_success_at, last_observed_at, consecutive_failures, last_error)
		VALUES ($1, 'healthy', now(), now(), $2, 0, '')
		ON CONFLICT (provider_id) DO UPDATE SET status = 'healthy', last_attempt_at = now(), last_success_at = now(),
		  last_observed_at = EXCLUDED.last_observed_at, consecutive_failures = 0, last_error = ''`,
		snap.ProviderID, observedAt)

	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return SyncResult{}, mapErr(err)
	}
	return res, mapErr(tx.Commit(ctx))
}

func scopeProjects(ctx context.Context, tx pgx.Tx, providerID string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT external_scope, project_id FROM project_mappings WHERE provider_id = $1`, providerID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var scope, project string
		if err := rows.Scan(&scope, &project); err != nil {
			return nil, mapErr(err)
		}
		m[scope] = project
	}
	return m, mapErr(rows.Err())
}

func lockExisting(ctx context.Context, tx pgx.Tx, providerID string) (map[resourceKey]existingResource, error) {
	rows, err := tx.Query(ctx, `
		SELECT kind, external_id, spec_hash, orphaned_at IS NOT NULL, missed_syncs, pending_operation_id IS NOT NULL
		FROM resources WHERE provider_id = $1 FOR UPDATE`, providerID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	m := map[resourceKey]existingResource{}
	for rows.Next() {
		var kind, id string
		var e existingResource
		if err := rows.Scan(&kind, &id, &e.hash, &e.orphaned, &e.missed, &e.leased); err != nil {
			return nil, mapErr(err)
		}
		m[resourceKey{ResourceKind(kind), id}] = e
	}
	return m, mapErr(rows.Err())
}

// resourceHash returns the spec JSON and a hash over every stored attribute,
// so any change (including re-derived ownership) is recorded once.
func resourceHash(name, scope string, project *string, arch, status string, spec map[string]any) ([]byte, []byte, error) {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: resource spec: %w", ErrInvalid, err)
	}
	proj := ""
	if project != nil {
		proj = *project
	}
	canonical, err := json.Marshal([]any{name, scope, proj, arch, status, json.RawMessage(specJSON)})
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(canonical)
	return specJSON, sum[:], nil
}

// SyncHealth is a provider's connectivity status.
type SyncHealth string

const (
	SyncHealthy     SyncHealth = "healthy"
	SyncDegraded    SyncHealth = "degraded"
	SyncUnreachable SyncHealth = "unreachable"
)

// unreachableAfter consecutive failures turn a provider unreachable.
const unreachableAfter = 3

const maxSyncErrorLen = 500

// RecordSyncFailure counts a failed sync and returns the resulting status:
// degraded after the first failure, unreachable after unreachableAfter.
func (s *Store) RecordSyncFailure(ctx context.Context, providerID, reason string) (SyncHealth, error) {
	if len(reason) > maxSyncErrorLen {
		reason = reason[:maxSyncErrorLen]
	}
	var status string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO provider_sync_status (provider_id, status, last_attempt_at, consecutive_failures, last_error)
		VALUES ($1, 'degraded', now(), 1, $2)
		ON CONFLICT (provider_id) DO UPDATE SET
		  last_attempt_at = now(),
		  consecutive_failures = provider_sync_status.consecutive_failures + 1,
		  last_error = EXCLUDED.last_error,
		  status = CASE WHEN provider_sync_status.consecutive_failures + 1 >= $3 THEN 'unreachable' ELSE 'degraded' END
		RETURNING status`, providerID, reason, unreachableAfter).Scan(&status)
	return SyncHealth(status), mapErr(err)
}

// SyncStatus is a provider's last sync outcome.
type SyncStatus struct {
	ProviderID          string
	Status              SyncHealth
	LastAttemptAt       time.Time
	LastSuccessAt       *time.Time
	LastObservedAt      *time.Time
	ConsecutiveFailures int
	LastError           string
}

// GetSyncStatus returns a provider's sync status, or ErrNotFound before the
// first sync attempt.
func (s *Store) GetSyncStatus(ctx context.Context, providerID string) (SyncStatus, error) {
	st := SyncStatus{ProviderID: providerID}
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT status, last_attempt_at, last_success_at, last_observed_at, consecutive_failures, last_error
		FROM provider_sync_status WHERE provider_id = $1`, providerID).
		Scan(&status, &st.LastAttemptAt, &st.LastSuccessAt, &st.LastObservedAt, &st.ConsecutiveFailures, &st.LastError)
	st.Status = SyncHealth(status)
	return st, mapErr(err)
}

// Resource is one cached inventory resource.
type Resource struct {
	ProviderID    string
	Kind          ResourceKind
	ExternalID    string
	Name          string
	ExternalScope string
	ProjectID     string
	Arch          string
	Status        string
	Spec          map[string]any
	ObservedAt    time.Time
	FirstSeenAt   time.Time
	OrphanedAt    *time.Time
}

// Grant is one scoped permission to see resources: a project, a provider,
// or a project within a provider. A grant without a project covers only
// resources that are assigned to some project.
type Grant struct {
	ProjectID  string
	ProviderID string
}

// Visibility is the set of resources a caller may see. It is applied in the
// query itself so that RBAC cannot be bypassed by a handler (roadmap §3.4).
type Visibility struct {
	// All includes every resource, unassigned ones too (global admin and
	// operator, who triage unassigned resources).
	All bool
	// AllAssigned includes every resource that belongs to a project (global
	// viewer).
	AllAssigned bool
	Grants      []Grant
}

// queryArgs collects positional arguments while a query is assembled.
type queryArgs struct{ args []any }

// add appends v and returns its placeholder.
func (q *queryArgs) add(v any) string {
	q.args = append(q.args, v)
	return q.last()
}

// last returns the placeholder of the most recently added argument.
func (q *queryArgs) last() string { return fmt.Sprintf("$%d", len(q.args)) }

// clause returns the SQL predicate (over alias r) admitting the resources v
// may see. Every inventory query, lists and aggregates alike, goes through it
// so that RBAC is applied identically everywhere (roadmap §3.4).
func (v Visibility) clause(q *queryArgs) string {
	projects := make([]string, len(v.Grants))
	providers := make([]string, len(v.Grants))
	for i, g := range v.Grants {
		projects[i], providers[i] = g.ProjectID, g.ProviderID
	}
	return `(
		        ` + q.add(v.All) + `
		     OR (` + q.add(v.AllAssigned) + ` AND r.project_id IS NOT NULL)
		     OR EXISTS (
		          SELECT 1 FROM unnest(` + q.add(projects) + `::text[], ` + q.add(providers) + `::text[]) AS g(project_id, provider_id)
		          WHERE r.project_id IS NOT NULL
		            AND (g.project_id = '' OR g.project_id = r.project_id)
		            AND (g.provider_id = '' OR g.provider_id = r.provider_id)))`
}

// siteClause admits resources of providers in siteID; "" admits all.
func siteClause(q *queryArgs, siteID string) string {
	return `(` + q.add(siteID) + ` = '' OR r.provider_id IN (
		SELECT id FROM providers WHERE site_id = ` + q.last() + ` AND deleted_at IS NULL))`
}

// ResourceFilter selects resources for ListResources.
type ResourceFilter struct {
	Kind       ResourceKind
	ProviderID string
	// SiteID narrows results to providers of this site.
	SiteID string
	// Arch narrows results to one architecture (amd64, arm64, unknown).
	Arch string
	// ProjectIDs narrows results to these projects (a user-chosen filter);
	// nil means no narrowing. RBAC is Visibility, not this field.
	ProjectIDs []string
	// Visibility limits results to what the caller may see. The zero value
	// sees nothing.
	Visibility      Visibility
	IncludeOrphaned bool
	// After is the keyset cursor: (provider_id, external_id) of the last row.
	AfterProvider, AfterExternalID string
	Limit                          int
}

// ListResources returns cached resources of one kind ordered by provider and
// external ID, using keyset pagination.
func (s *Store) ListResources(ctx context.Context, f ResourceFilter) ([]Resource, error) {
	if !f.Kind.Valid() {
		return nil, fmt.Errorf("%w: resource kind %q", ErrInvalid, f.Kind)
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 1000
	}
	q := &queryArgs{}
	sql := `
		SELECT provider_id, kind, external_id, name, external_scope, coalesce(project_id, ''), arch, status,
		       spec, observed_at, first_seen_at, orphaned_at
		FROM resources r
		WHERE kind = ` + q.add(string(f.Kind)) + `
		  AND (` + q.add(f.ProviderID) + ` = '' OR provider_id = ` + q.last() + `)
		  AND (` + q.add(f.ProjectIDs) + `::text[] IS NULL OR project_id = ANY(` + q.last() + `))
		  AND ` + siteClause(q, f.SiteID) + `
		  AND (` + q.add(f.Arch) + ` = '' OR arch = ` + q.last() + `)
		  AND (` + q.add(f.IncludeOrphaned) + ` OR orphaned_at IS NULL)
		  AND (provider_id, external_id) > (` + q.add(f.AfterProvider) + `, ` + q.add(f.AfterExternalID) + `)
		  AND ` + f.Visibility.clause(q) + `
		ORDER BY provider_id, external_id
		LIMIT ` + q.add(f.Limit)
	rows, err := s.pool.Query(ctx, sql, q.args...)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Resource, error) {
		var res Resource
		var kind string
		var spec []byte
		if err := r.Scan(&res.ProviderID, &kind, &res.ExternalID, &res.Name, &res.ExternalScope, &res.ProjectID,
			&res.Arch, &res.Status, &spec, &res.ObservedAt, &res.FirstSeenAt, &res.OrphanedAt); err != nil {
			return Resource{}, err
		}
		res.Kind = ResourceKind(kind)
		return res, json.Unmarshal(spec, &res.Spec)
	})
}

// GetResource returns one cached resource, orphaned or not, if v admits it.
// A resource the caller may not see is reported as ErrNotFound, so its
// existence is not revealed.
func (s *Store) GetResource(ctx context.Context, providerID string, kind ResourceKind, externalID string, v Visibility) (Resource, error) {
	q := &queryArgs{}
	sql := `
		SELECT provider_id, kind, external_id, name, external_scope, coalesce(project_id, ''), arch, status,
		       spec, observed_at, first_seen_at, orphaned_at
		FROM resources r
		WHERE provider_id = ` + q.add(providerID) + ` AND kind = ` + q.add(string(kind)) + ` AND external_id = ` + q.add(externalID) + `
		  AND ` + v.clause(q)
	var res Resource
	var k string
	var spec []byte
	err := s.pool.QueryRow(ctx, sql, q.args...).Scan(&res.ProviderID, &k, &res.ExternalID, &res.Name, &res.ExternalScope,
		&res.ProjectID, &res.Arch, &res.Status, &spec, &res.ObservedAt, &res.FirstSeenAt, &res.OrphanedAt)
	if err != nil {
		return Resource{}, mapErr(err)
	}
	res.Kind = ResourceKind(k)
	return res, json.Unmarshal(spec, &res.Spec)
}

// RecentResourceChanges returns up to limit of a resource's latest history
// entries, newest first.
func (s *Store) RecentResourceChanges(ctx context.Context, providerID string, kind ResourceKind, externalID string, limit int) ([]ResourceChange, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, change, observed_at, coalesce(project_id, '') FROM resource_changes
		WHERE provider_id = $1 AND kind = $2 AND external_id = $3 ORDER BY id DESC LIMIT $4`,
		providerID, string(kind), externalID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ResourceChange, error) {
		var c ResourceChange
		err := r.Scan(&c.ID, &c.Change, &c.ObservedAt, &c.ProjectID)
		return c, err
	})
}

// ResourceChange is one entry of a resource's history.
type ResourceChange struct {
	ID         int64
	Change     string
	ObservedAt time.Time
	ProjectID  string
}

// ListResourceChanges returns a resource's history, oldest first.
func (s *Store) ListResourceChanges(ctx context.Context, providerID string, kind ResourceKind, externalID string) ([]ResourceChange, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, change, observed_at, coalesce(project_id, '') FROM resource_changes
		WHERE provider_id = $1 AND kind = $2 AND external_id = $3 ORDER BY id`, providerID, string(kind), externalID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ResourceChange, error) {
		var c ResourceChange
		err := r.Scan(&c.ID, &c.Change, &c.ObservedAt, &c.ProjectID)
		return c, err
	})
}

// SetResourceLease marks a resource as owned by an in-flight workflow
// operation; sync then leaves it untouched (roadmap §3.6). An empty
// operationID releases the lease.
func (s *Store) SetResourceLease(ctx context.Context, providerID string, kind ResourceKind, externalID, operationID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE resources SET pending_operation_id = $4 WHERE provider_id = $1 AND kind = $2 AND external_id = $3`,
		providerID, string(kind), externalID, nullIfEmpty(operationID))
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
