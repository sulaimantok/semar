package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/store"
)

// InventoryReader is what the inventory endpoints need; *store.Store
// implements it.
type InventoryReader interface {
	ListResources(ctx context.Context, f store.ResourceFilter) ([]store.Resource, error)
	GetSyncStatus(ctx context.Context, providerID string) (store.SyncStatus, error)
}

var _ InventoryReader = (*store.Store)(nil)

const (
	defaultPageSize = 100
	maxPageSize     = 500
)

// visibilityFor turns the caller's role bindings into the query-level
// visibility of resources (roadmap §3.4):
//   - global admin/operator: everything, including unassigned resources;
//   - global viewer: every resource that belongs to a project;
//   - scoped bindings: their project and/or provider.
//
// Hypervisors belong to no project, so only global bindings see them.
func visibilityFor(p *auth.Principal, kind store.ResourceKind) store.Visibility {
	var v store.Visibility
	if p == nil {
		return v
	}
	for _, b := range p.Bindings {
		switch {
		case b.Global() && (b.Role == auth.RoleAdmin || b.Role == auth.RoleOperator):
			v.All = true
		case b.Global():
			v.AllAssigned = true
		default:
			v.Grants = append(v.Grants, store.Grant{ProjectID: b.ProjectID, ProviderID: b.ProviderID})
		}
	}
	if kind == store.KindHypervisor {
		return store.Visibility{All: v.All || v.AllAssigned}
	}
	return v
}

type pageCursor struct {
	Provider   string `json:"p"`
	ExternalID string `json:"e"`
}

func encodeCursor(c pageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (pageCursor, error) {
	var c pageCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Provider == "" || c.ExternalID == "" {
		return pageCursor{}, badRequest("invalid cursor")
	}
	return c, nil
}

func (s *server) ListResources(ctx context.Context, req gen.ListResourcesRequestObject) (gen.ListResourcesResponseObject, error) {
	const instance = "/api/v1/resources"
	list, err := s.listResources(ctx, req.Params)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.ListResourcesdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.ListResources200JSONResponse(list), nil
}

func (s *server) listResources(ctx context.Context, params gen.ListResourcesParams) (gen.ResourceList, error) {
	if s.opts.Inventory == nil {
		return gen.ResourceList{}, &apiError{status: http.StatusServiceUnavailable, detail: "inventory not configured"}
	}
	kind := store.ResourceKind(params.Kind)
	if !kind.Valid() {
		return gen.ResourceList{}, badRequest("unknown resource kind %q", params.Kind)
	}
	limit := defaultPageSize
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > maxPageSize {
			return gen.ResourceList{}, badRequest("limit must be between 1 and %d", maxPageSize)
		}
		limit = *params.Limit
	}
	f := store.ResourceFilter{
		Kind:       kind,
		Visibility: visibilityFor(auth.PrincipalFrom(ctx), kind),
		// One extra row tells whether another page exists.
		Limit: limit + 1,
	}
	if params.ProviderId != nil {
		f.ProviderID = *params.ProviderId
	}
	if params.ProjectId != nil {
		f.ProjectIDs = []string{*params.ProjectId}
	}
	if params.IncludeOrphaned != nil {
		f.IncludeOrphaned = *params.IncludeOrphaned
	}
	if params.Cursor != nil {
		c, err := decodeCursor(*params.Cursor)
		if err != nil {
			return gen.ResourceList{}, err
		}
		f.AfterProvider, f.AfterExternalID = c.Provider, c.ExternalID
	}

	rs, err := s.opts.Inventory.ListResources(ctx, f)
	if err != nil {
		return gen.ResourceList{}, err
	}
	out := gen.ResourceList{Items: make([]gen.Resource, 0, min(len(rs), limit))}
	for i, r := range rs {
		if i == limit {
			next := encodeCursor(pageCursor{Provider: rs[i-1].ProviderID, ExternalID: rs[i-1].ExternalID})
			out.NextCursor = &next
			break
		}
		out.Items = append(out.Items, toResource(r))
	}
	return out, nil
}

func (s *server) GetProviderSyncStatus(ctx context.Context, req gen.GetProviderSyncStatusRequestObject) (gen.GetProviderSyncStatusResponseObject, error) {
	instance := "/api/v1/providers/" + req.ProviderId + "/sync-status"
	st, err := s.getSyncStatus(ctx, req.ProviderId)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.GetProviderSyncStatusdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.GetProviderSyncStatus200JSONResponse(st), nil
}

func (s *server) getSyncStatus(ctx context.Context, providerID string) (gen.SyncStatus, error) {
	if s.opts.Inventory == nil || s.opts.Settings == nil {
		return gen.SyncStatus{}, &apiError{status: http.StatusServiceUnavailable, detail: "inventory not configured"}
	}
	if _, err := s.opts.Settings.GetProvider(ctx, providerID); err != nil {
		return gen.SyncStatus{}, err
	}
	st, err := s.opts.Inventory.GetSyncStatus(ctx, providerID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.SyncStatus{}, &apiError{status: http.StatusNotFound, detail: "provider has not been synced yet"}
	}
	if err != nil {
		return gen.SyncStatus{}, err
	}
	return gen.SyncStatus{
		ProviderId:          st.ProviderID,
		Status:              gen.SyncStatusStatus(st.Status),
		LastAttemptAt:       st.LastAttemptAt,
		LastSuccessAt:       st.LastSuccessAt,
		LastObservedAt:      st.LastObservedAt,
		ConsecutiveFailures: st.ConsecutiveFailures,
		LastError:           st.LastError,
	}, nil
}

func toResource(r store.Resource) gen.Resource {
	out := gen.Resource{
		ProviderId:    r.ProviderID,
		Kind:          gen.ResourceKind(r.Kind),
		ExternalId:    r.ExternalID,
		Name:          r.Name,
		ExternalScope: r.ExternalScope,
		Arch:          gen.ResourceArch(r.Arch),
		Status:        r.Status,
		Spec:          r.Spec,
		ObservedAt:    r.ObservedAt,
		FirstSeenAt:   r.FirstSeenAt,
		OrphanedAt:    r.OrphanedAt,
	}
	if out.Spec == nil {
		out.Spec = map[string]any{}
	}
	if r.ProjectID != "" {
		project := r.ProjectID
		out.ProjectId = &project
	}
	return out
}
