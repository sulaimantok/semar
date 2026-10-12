package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/store"
)

// CredentialStore is the persistence the credential endpoints need;
// *store.Store implements it.
type CredentialStore interface {
	GetProvider(ctx context.Context, id string) (model.Provider, error)
	PutCredential(ctx context.Context, ks secret.Store, c model.ProviderCredential, plaintext []byte) (model.ProviderCredential, error)
	ListCredentials(ctx context.Context, providerID string) ([]model.ProviderCredential, error)
	DeleteCredential(ctx context.Context, providerID string, purpose model.CredentialPurpose, scope string) error
}

var _ CredentialStore = (*store.Store)(nil)

func credentialsInstance(providerID string) string {
	return "/api/v1/providers/" + providerID + "/credentials"
}

func (s *server) credentials() (CredentialStore, error) {
	if s.opts.Credentials == nil || s.opts.Secrets == nil {
		return nil, &apiError{status: http.StatusServiceUnavailable, detail: "credential storage not configured"}
	}
	return s.opts.Credentials, nil
}

// activeProvider returns 404 for unknown or removed providers.
func activeProvider(ctx context.Context, st CredentialStore, id string) error {
	if _, err := st.GetProvider(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &apiError{status: http.StatusNotFound, detail: "provider " + id + " not found"}
		}
		return err
	}
	return nil
}

func (s *server) ListCredentials(ctx context.Context, req gen.ListCredentialsRequestObject) (gen.ListCredentialsResponseObject, error) {
	items, err := s.listCredentials(ctx, req.ProviderId)
	if err != nil {
		code, p := s.problemFor(ctx, credentialsInstance(req.ProviderId), err)
		return gen.ListCredentialsdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.ListCredentials200JSONResponse(gen.CredentialList{Items: items}), nil
}

func (s *server) listCredentials(ctx context.Context, providerID string) ([]gen.Credential, error) {
	st, err := s.credentials()
	if err != nil {
		return nil, err
	}
	if err := activeProvider(ctx, st, providerID); err != nil {
		return nil, err
	}
	creds, err := st.ListCredentials(ctx, providerID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Credential, 0, len(creds))
	for _, c := range creds {
		gc, err := toCredential(c)
		if err != nil {
			return nil, err
		}
		out = append(out, gc)
	}
	return out, nil
}

func (s *server) PutCredential(ctx context.Context, req gen.PutCredentialRequestObject) (gen.PutCredentialResponseObject, error) {
	c, err := s.putCredential(ctx, req.ProviderId, req.Body)
	if err != nil {
		code, p := s.problemFor(ctx, credentialsInstance(req.ProviderId), err)
		return gen.PutCredentialdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.PutCredential200JSONResponse(c), nil
}

func (s *server) putCredential(ctx context.Context, providerID string, body *gen.CredentialPut) (gen.Credential, error) {
	st, err := s.credentials()
	if err != nil {
		return gen.Credential{}, err
	}
	if body == nil {
		return gen.Credential{}, badRequest("request body is required")
	}
	purpose := model.CredentialPurpose(body.Purpose)
	if err := model.ValidateCredentialScope(purpose, body.Scope); err != nil {
		return gen.Credential{}, badRequest("%s", err.Error())
	}
	if body.Secret == nil || len(*body.Secret) == 0 {
		return gen.Credential{}, badRequest("secret must contain at least one field")
	}
	for k := range *body.Secret {
		if strings.TrimSpace(k) == "" {
			return gen.Credential{}, badRequest("secret field names must not be empty")
		}
	}
	if err := activeProvider(ctx, st, providerID); err != nil {
		return gen.Credential{}, err
	}

	plaintext, err := json.Marshal(*body.Secret)
	if err != nil {
		return gen.Credential{}, err
	}
	// Best effort: do not leave the serialised secret around after sealing.
	defer clear(plaintext)

	c := model.ProviderCredential{ProviderID: providerID, Purpose: purpose, Scope: body.Scope, ExpiresAt: body.ExpiresAt}
	if body.Owner != nil {
		c.Owner = strings.TrimSpace(*body.Owner)
	}
	var stored model.ProviderCredential
	err = s.audited(ctx, "credential.put", providerID+"/"+string(purpose)+"/"+body.Scope, func() (err error) {
		stored, err = st.PutCredential(ctx, s.opts.Secrets, c, plaintext)
		return err
	})
	if err != nil {
		return gen.Credential{}, err
	}
	return toCredential(stored)
}

func (s *server) DeleteCredential(ctx context.Context, req gen.DeleteCredentialRequestObject) (gen.DeleteCredentialResponseObject, error) {
	err := s.deleteCredential(ctx, req.ProviderId, model.CredentialPurpose(req.Params.Purpose), req.Params.Scope)
	if err != nil {
		code, p := s.problemFor(ctx, credentialsInstance(req.ProviderId), err)
		return gen.DeleteCredentialdefaultApplicationProblemPlusJSONResponse{StatusCode: code, Body: p}, nil
	}
	return gen.DeleteCredential204Response{}, nil
}

func (s *server) deleteCredential(ctx context.Context, providerID string, purpose model.CredentialPurpose, scope string) error {
	st, err := s.credentials()
	if err != nil {
		return err
	}
	if !purpose.Valid() {
		return badRequest("unknown credential purpose %q", purpose)
	}
	return s.audited(ctx, "credential.delete", providerID+"/"+string(purpose)+"/"+scope, func() error {
		return st.DeleteCredential(ctx, providerID, purpose, scope)
	})
}

func toCredential(c model.ProviderCredential) (gen.Credential, error) {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return gen.Credential{}, err
	}
	return gen.Credential{
		Id:         id,
		Purpose:    gen.CredentialPurpose(c.Purpose),
		Scope:      c.Scope,
		Owner:      c.Owner,
		KekVersion: c.KEKVersion,
		ExpiresAt:  c.ExpiresAt,
		CreatedAt:  c.CreatedAt,
		RotatedAt:  c.RotatedAt,
	}, nil
}
