package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/openstack"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

// DriverFactory builds the driver for a provider. The default is a
// registry.Resolver over Options.Credentials and Options.Secrets, the same
// resolution the worker uses.
type DriverFactory func(ctx context.Context, p model.Provider) (contract.CloudDriver, error)

func (s *server) ValidateProvider(ctx context.Context, req gen.ValidateProviderRequestObject) (gen.ValidateProviderResponseObject, error) {
	instance := "/api/v1/providers/" + req.ProviderId + "/validate"
	var p model.Provider
	err := s.audited(ctx, "provider.validate", req.ProviderId, func() (err error) {
		p, err = s.validateProvider(ctx, req.ProviderId)
		return err
	})
	if err != nil {
		code, prob := s.problemFor(ctx, instance, err)
		return gen.ValidateProviderdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: prob}, nil
	}
	return gen.ValidateProvider200JSONResponse(toProvider(p)), nil
}

// validateProvider connects through the driver and records the cloud's
// identity. Driver error text can name internal hosts, so it is logged and
// only a classification reaches the client.
func (s *server) validateProvider(ctx context.Context, id string) (model.Provider, error) {
	st, err := s.settings()
	if err != nil {
		return model.Provider{}, err
	}
	p, err := st.GetProvider(ctx, id)
	if err != nil {
		return model.Provider{}, err
	}
	d, err := s.opts.Drivers(ctx, p)
	switch {
	case errors.Is(err, registry.ErrNotImplemented):
		return model.Provider{}, &apiError{status: http.StatusNotImplemented, detail: "no driver for provider type " + string(p.Type) + " yet"}
	case errors.Is(err, registry.ErrCredentialUnreadable):
		s.opts.Logger.ErrorContext(ctx, "provider credential cannot be decrypted", slog.String("provider", id), slog.Any("error", err))
		return model.Provider{}, &apiError{status: http.StatusServiceUnavailable,
			detail: "the stored credential cannot be decrypted with the configured SEMAR_KEK; see the key rotation runbook"}
	case errors.Is(err, registry.ErrNoCredential):
		return model.Provider{}, &apiError{status: http.StatusUnprocessableEntity, detail: "store a discovery credential for this provider first"}
	case errors.Is(err, contract.ErrUnauthorized):
		// The stored credential is malformed for this provider type.
		return model.Provider{}, &apiError{status: http.StatusUnprocessableEntity, detail: "the discovery credential is incomplete for this provider type"}
	}
	if err != nil {
		return model.Provider{}, err
	}

	vctx, cancel := context.WithTimeout(ctx, s.opts.ValidateTimeout)
	defer cancel()
	info, err := d.Validate(vctx)
	if err != nil {
		s.opts.Logger.WarnContext(ctx, "provider connection check failed",
			slog.String("provider", id), slog.Any("error", err), slog.String("request_id", RequestIDFrom(ctx)))
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return model.Provider{}, &apiError{status: http.StatusGatewayTimeout, detail: "the provider did not answer in time"}
		case errors.Is(err, contract.ErrUnauthorized):
			return model.Provider{}, &apiError{status: http.StatusBadGateway, detail: "the provider refused the credentials"}
		case errors.Is(err, openstack.ErrNoEndpoint):
			return model.Provider{}, &apiError{status: http.StatusUnprocessableEntity,
				detail: "the identity service answered, but its catalog has no compute endpoint for this region; check the region name (the server log lists the regions offered)"}
		case errors.Is(err, contract.ErrRateLimited):
			return model.Provider{}, &apiError{status: http.StatusBadGateway, detail: "the provider is rate limiting requests; try again later"}
		default:
			return model.Provider{}, &apiError{status: http.StatusBadGateway, detail: "the provider could not be reached"}
		}
	}
	if info.Fingerprint == "" {
		return model.Provider{}, &apiError{status: http.StatusBadGateway, detail: "the driver reported no cloud identity"}
	}

	other, err := st.ProviderByCloudFingerprint(ctx, info.Fingerprint)
	switch {
	case err == nil && other.ID != p.ID:
		return model.Provider{}, &apiError{status: http.StatusConflict, detail: "this cloud is already registered as provider " + other.ID}
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return model.Provider{}, err
	}
	updated, err := st.RecordProviderValidation(ctx, p.ID, info.Fingerprint, info.APIVersions)
	if errors.Is(err, store.ErrConflict) {
		// Lost a race with a concurrent check of the same cloud.
		return model.Provider{}, &apiError{status: http.StatusConflict, detail: "this cloud is already registered as another provider"}
	}
	return updated, err
}
