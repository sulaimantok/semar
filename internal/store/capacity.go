package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// CapacityFilter selects what CapacitySummary aggregates. Orphaned resources
// are never counted.
type CapacityFilter struct {
	ProviderID string
	// SiteID narrows both aggregates to providers of this site.
	SiteID string
	// ProjectIDs narrows the VM aggregate to these projects; nil means no
	// narrowing. Hypervisor capacity is not per project, so it is skipped
	// when this is set.
	ProjectIDs []string
	// VMs limits the VM aggregate to what the caller may see.
	VMs Visibility
	// Hypervisors includes physical capacity; only global bindings see it.
	Hypervisors bool
}

// CountByKey is one bucket of a grouped count.
type CountByKey struct {
	Key   string
	Count int64
}

// VMCapacity aggregates the VMs a caller may see.
type VMCapacity struct {
	Total     int64
	VCPUs     int64
	MemoryMiB int64
	ByStatus  []CountByKey
	ByArch    []CountByKey
}

// HypervisorCapacity aggregates physical capacity.
type HypervisorCapacity struct {
	Total         int64
	VCPUs         int64
	VCPUsUsed     int64
	MemoryMiB     int64
	MemoryMiBUsed int64
	ByArch        []CountByKey
}

// Capacity is the Capacity Overview. Hypervisors is nil when the filter
// excludes physical capacity.
type Capacity struct {
	VMs         VMCapacity
	Hypervisors *HypervisorCapacity
}

// specNumber reads a numeric spec field, ignoring malformed values so one
// bad driver row cannot fail the whole aggregate.
func specNumber(field string) string {
	return `coalesce(sum(CASE WHEN jsonb_typeof(r.spec->'` + field + `') = 'number' THEN (r.spec->>'` + field + `')::numeric END), 0)::bigint`
}

// CapacitySummary aggregates cached inventory. VM totals use the same
// visibility predicate as ListResources, so aggregates never include
// resources the caller could not list (roadmap §3.4).
func (s *Store) CapacitySummary(ctx context.Context, f CapacityFilter) (Capacity, error) {
	var c Capacity

	q := &queryArgs{}
	vmWhere := `r.kind = 'vm' AND r.orphaned_at IS NULL
		AND (` + q.add(f.ProviderID) + ` = '' OR r.provider_id = ` + q.last() + `)
		AND (` + q.add(f.ProjectIDs) + `::text[] IS NULL OR r.project_id = ANY(` + q.last() + `))
		AND ` + siteClause(q, f.SiteID) + `
		AND ` + f.VMs.clause(q)
	err := s.pool.QueryRow(ctx, `
		SELECT count(*), `+specNumber("vcpus")+`, `+specNumber("memoryMiB")+`
		FROM resources r WHERE `+vmWhere, q.args...).Scan(&c.VMs.Total, &c.VMs.VCPUs, &c.VMs.MemoryMiB)
	if err != nil {
		return c, mapErr(err)
	}
	if c.VMs.ByStatus, err = s.countBy(ctx, "r.status", vmWhere, q.args); err != nil {
		return c, err
	}
	if c.VMs.ByArch, err = s.countBy(ctx, "r.arch", vmWhere, q.args); err != nil {
		return c, err
	}

	if !f.Hypervisors || f.ProjectIDs != nil {
		return c, nil
	}
	h := &HypervisorCapacity{}
	hq := &queryArgs{}
	hvWhere := `r.kind = 'hypervisor' AND r.orphaned_at IS NULL
		AND (` + hq.add(f.ProviderID) + ` = '' OR r.provider_id = ` + hq.last() + `)
		AND ` + siteClause(hq, f.SiteID)
	err = s.pool.QueryRow(ctx, `
		SELECT count(*), `+specNumber("vcpus")+`, `+specNumber("vcpusUsed")+`,
		       `+specNumber("memoryMiB")+`, `+specNumber("memoryMiBUsed")+`
		FROM resources r WHERE `+hvWhere, hq.args...).Scan(&h.Total, &h.VCPUs, &h.VCPUsUsed, &h.MemoryMiB, &h.MemoryMiBUsed)
	if err != nil {
		return c, mapErr(err)
	}
	if h.ByArch, err = s.countBy(ctx, "r.arch", hvWhere, hq.args); err != nil {
		return c, err
	}
	c.Hypervisors = h
	return c, nil
}

// countBy groups the rows matching where by column, largest bucket first.
func (s *Store) countBy(ctx context.Context, column, where string, args []any) ([]CountByKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+column+`, count(*) FROM resources r WHERE `+where+`
		GROUP BY 1 ORDER BY 2 DESC, 1`, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CountByKey, error) {
		var b CountByKey
		err := r.Scan(&b.Key, &b.Count)
		return b, err
	})
	return out, mapErr(err)
}
