package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

// SettingsStore is the persistence the Settings endpoints need; *store.Store
// implements it.
type SettingsStore interface {
	ListSites(ctx context.Context) ([]model.Site, error)
	CreateSite(ctx context.Context, site model.Site) (model.Site, error)
	ListProjects(ctx context.Context) ([]model.Project, error)
	GetProject(ctx context.Context, id string) (model.Project, error)
	CreateProject(ctx context.Context, p model.Project) (model.Project, error)
	ListProjectMappings(ctx context.Context, projectID string) ([]model.ProjectMapping, error)
	CreateProjectMapping(ctx context.Context, m model.ProjectMapping) (model.ProjectMapping, error)
	ListProviders(ctx context.Context) ([]model.Provider, error)
	GetProvider(ctx context.Context, id string) (model.Provider, error)
	CreateProvider(ctx context.Context, p model.Provider) (model.Provider, error)
	DeleteProvider(ctx context.Context, id string) error
}

var _ SettingsStore = (*store.Store)(nil)

// apiError carries an HTTP status and client-safe detail out of a handler.
type apiError struct {
	status int
	detail string
}

func (e *apiError) Error() string { return e.detail }

func badRequest(format string, args ...any) error {
	return &apiError{status: http.StatusBadRequest, detail: fmt.Sprintf(format, args...)}
}

// problemFor maps handler and store errors to a problem. Unexpected errors
// are logged and returned as 500 without detail.
func (s *server) problemFor(ctx context.Context, instance string, err error) (int, gen.Problem) {
	status, detail := http.StatusInternalServerError, ""
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		status, detail = ae.status, ae.detail
	case errors.Is(err, store.ErrNotFound):
		status, detail = http.StatusNotFound, "not found"
	case errors.Is(err, store.ErrConflict):
		status, detail = http.StatusConflict, "already exists"
	case errors.Is(err, store.ErrInvalidReference):
		status, detail = http.StatusUnprocessableEntity, "a referenced entity does not exist"
	case errors.Is(err, store.ErrInvalid):
		status, detail = http.StatusBadRequest, strings.TrimPrefix(err.Error(), store.ErrInvalid.Error()+": ")
	default:
		s.opts.Logger.ErrorContext(ctx, "settings request failed",
			slog.String("instance", instance), slog.Any("error", err), slog.String("request_id", RequestIDFrom(ctx)))
	}
	return status, newProblem(ctx, instance, status, detail)
}

func (s *server) settings() (SettingsStore, error) {
	if s.opts.Settings == nil {
		return nil, &apiError{status: http.StatusServiceUnavailable, detail: "settings store not configured"}
	}
	return s.opts.Settings, nil
}

// --- Sites ---

func (s *server) ListSites(ctx context.Context, _ gen.ListSitesRequestObject) (gen.ListSitesResponseObject, error) {
	const instance = "/api/v1/sites"
	sites, err := s.listSites(ctx)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.ListSitesdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.ListSites200JSONResponse(gen.SiteList{Items: sites}), nil
}

func (s *server) listSites(ctx context.Context) ([]gen.Site, error) {
	st, err := s.settings()
	if err != nil {
		return nil, err
	}
	sites, err := st.ListSites(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Site, 0, len(sites))
	for _, site := range sites {
		out = append(out, toSite(site))
	}
	return out, nil
}

func (s *server) CreateSite(ctx context.Context, req gen.CreateSiteRequestObject) (gen.CreateSiteResponseObject, error) {
	const instance = "/api/v1/sites"
	site, err := s.createSite(ctx, req.Body)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.CreateSitedefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.CreateSite201JSONResponse(site), nil
}

func (s *server) createSite(ctx context.Context, body *gen.SiteCreate) (gen.Site, error) {
	st, err := s.settings()
	if err != nil {
		return gen.Site{}, err
	}
	if body == nil || strings.TrimSpace(body.Name) == "" {
		return gen.Site{}, badRequest("name is required")
	}
	site := model.Site{ID: body.Id, Name: strings.TrimSpace(body.Name)}
	if body.Labels != nil {
		site.Labels = *body.Labels
	}
	created, err := st.CreateSite(ctx, site)
	if err != nil {
		return gen.Site{}, err
	}
	return toSite(created), nil
}

// --- Projects ---

func (s *server) ListProjects(ctx context.Context, _ gen.ListProjectsRequestObject) (gen.ListProjectsResponseObject, error) {
	const instance = "/api/v1/projects"
	st, err := s.settings()
	var projects []model.Project
	if err == nil {
		projects, err = st.ListProjects(ctx)
	}
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.ListProjectsdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	out := make([]gen.Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, toProject(p))
	}
	return gen.ListProjects200JSONResponse(gen.ProjectList{Items: out}), nil
}

func (s *server) CreateProject(ctx context.Context, req gen.CreateProjectRequestObject) (gen.CreateProjectResponseObject, error) {
	const instance = "/api/v1/projects"
	created, err := s.createProject(ctx, req.Body)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.CreateProjectdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.CreateProject201JSONResponse(toProject(created)), nil
}

func (s *server) createProject(ctx context.Context, body *gen.ProjectCreate) (model.Project, error) {
	st, err := s.settings()
	if err != nil {
		return model.Project{}, err
	}
	if body == nil || strings.TrimSpace(body.Name) == "" {
		return model.Project{}, badRequest("name is required")
	}
	return st.CreateProject(ctx, model.Project{ID: body.Id, Name: strings.TrimSpace(body.Name)})
}

// --- Project mappings ---

func (s *server) ListProjectMappings(ctx context.Context, req gen.ListProjectMappingsRequestObject) (gen.ListProjectMappingsResponseObject, error) {
	instance := "/api/v1/projects/" + req.ProjectId + "/mappings"
	mappings, err := s.listProjectMappings(ctx, req.ProjectId)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.ListProjectMappingsdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	out := make([]gen.ProjectMapping, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, toProjectMapping(m))
	}
	return gen.ListProjectMappings200JSONResponse(gen.ProjectMappingList{Items: out}), nil
}

func (s *server) listProjectMappings(ctx context.Context, projectID string) ([]model.ProjectMapping, error) {
	st, err := s.settings()
	if err != nil {
		return nil, err
	}
	if _, err := st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return st.ListProjectMappings(ctx, projectID)
}

func (s *server) CreateProjectMapping(ctx context.Context, req gen.CreateProjectMappingRequestObject) (gen.CreateProjectMappingResponseObject, error) {
	instance := "/api/v1/projects/" + req.ProjectId + "/mappings"
	created, err := s.createProjectMapping(ctx, req.ProjectId, req.Body)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.CreateProjectMappingdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.CreateProjectMapping201JSONResponse(toProjectMapping(created)), nil
}

func (s *server) createProjectMapping(ctx context.Context, projectID string, body *gen.ProjectMappingCreate) (model.ProjectMapping, error) {
	st, err := s.settings()
	if err != nil {
		return model.ProjectMapping{}, err
	}
	if body == nil || strings.TrimSpace(body.ExternalScope) == "" {
		return model.ProjectMapping{}, badRequest("externalScope is required")
	}
	if _, err := st.GetProject(ctx, projectID); err != nil {
		return model.ProjectMapping{}, err
	}
	// Mappings to removed providers are rejected even though the row exists.
	if _, err := st.GetProvider(ctx, body.ProviderId); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.ProjectMapping{}, &apiError{status: http.StatusUnprocessableEntity, detail: "provider " + body.ProviderId + " does not exist"}
		}
		return model.ProjectMapping{}, err
	}
	return st.CreateProjectMapping(ctx, model.ProjectMapping{
		ProjectID:     projectID,
		ProviderID:    body.ProviderId,
		ExternalScope: strings.TrimSpace(body.ExternalScope),
	})
}

// --- Providers ---

func (s *server) ListProviders(ctx context.Context, _ gen.ListProvidersRequestObject) (gen.ListProvidersResponseObject, error) {
	const instance = "/api/v1/providers"
	st, err := s.settings()
	var providers []model.Provider
	if err == nil {
		providers, err = st.ListProviders(ctx)
	}
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.ListProvidersdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	out := make([]gen.Provider, 0, len(providers))
	for _, p := range providers {
		out = append(out, toProvider(p))
	}
	return gen.ListProviders200JSONResponse(gen.ProviderList{Items: out}), nil
}

func (s *server) CreateProvider(ctx context.Context, req gen.CreateProviderRequestObject) (gen.CreateProviderResponseObject, error) {
	const instance = "/api/v1/providers"
	created, err := s.createProvider(ctx, req.Body)
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.CreateProviderdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.CreateProvider201JSONResponse(toProvider(created)), nil
}

func (s *server) createProvider(ctx context.Context, body *gen.ProviderCreate) (model.Provider, error) {
	st, err := s.settings()
	if err != nil {
		return model.Provider{}, err
	}
	if body == nil || strings.TrimSpace(body.Name) == "" {
		return model.Provider{}, badRequest("name is required")
	}
	if !body.Type.Valid() {
		return model.Provider{}, badRequest("type must be one of openstack, proxmox, fake")
	}
	p := model.Provider{
		ID:        body.Id,
		Type:      contract.ProviderType(body.Type),
		Name:      strings.TrimSpace(body.Name),
		Endpoints: body.Endpoints,
	}
	if body.SiteId != nil {
		p.SiteID = *body.SiteId
	}
	if body.RegionName != nil {
		p.RegionName = strings.TrimSpace(*body.RegionName)
	}
	p.Fingerprint, err = model.ProviderFingerprint(p.Type, p.Endpoints, p.RegionName)
	if err != nil {
		return model.Provider{}, badRequest("%s", err.Error())
	}
	created, err := st.CreateProvider(ctx, p)
	if errors.Is(err, store.ErrConflict) {
		return model.Provider{}, &apiError{status: http.StatusConflict, detail: "a provider with this id, or for the same cloud and region, is already registered"}
	}
	return created, err
}

func (s *server) GetProvider(ctx context.Context, req gen.GetProviderRequestObject) (gen.GetProviderResponseObject, error) {
	instance := "/api/v1/providers/" + req.ProviderId
	st, err := s.settings()
	var p model.Provider
	if err == nil {
		p, err = st.GetProvider(ctx, req.ProviderId)
	}
	if err != nil {
		code, prob := s.problemFor(ctx, instance, err)
		return gen.GetProviderdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: prob}, nil
	}
	return gen.GetProvider200JSONResponse(toProvider(p)), nil
}

func (s *server) DeleteProvider(ctx context.Context, req gen.DeleteProviderRequestObject) (gen.DeleteProviderResponseObject, error) {
	instance := "/api/v1/providers/" + req.ProviderId
	st, err := s.settings()
	if err == nil {
		err = st.DeleteProvider(ctx, req.ProviderId)
	}
	if err != nil {
		code, p := s.problemFor(ctx, instance, err)
		return gen.DeleteProviderdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.DeleteProvider204Response{}, nil
}

// --- Conversions ---

func toSite(s model.Site) gen.Site {
	labels := s.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return gen.Site{Id: s.ID, Name: s.Name, Labels: labels, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

func toProject(p model.Project) gen.Project {
	return gen.Project{Id: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

func toProjectMapping(m model.ProjectMapping) gen.ProjectMapping {
	return gen.ProjectMapping{Id: m.ID, ProjectId: m.ProjectID, ProviderId: m.ProviderID, ExternalScope: m.ExternalScope, CreatedAt: m.CreatedAt}
}

func toProvider(p model.Provider) gen.Provider {
	out := gen.Provider{
		Id:          p.ID,
		Type:        gen.ProviderType(p.Type),
		Name:        p.Name,
		Endpoints:   p.Endpoints,
		RegionName:  p.RegionName,
		Fingerprint: p.Fingerprint,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	if p.SiteID != "" {
		site := p.SiteID
		out.SiteId = &site
	}
	return out
}
