package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/model"
)

const secretMarker = "SUPER-SECRET-VALUE-42"

func newCredentialAPI(t *testing.T) *settingsAPI {
	t.Helper()
	a := newSettingsAPI(t)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"os-jkt","type":"openstack","name":"OS",
		"endpoints":["https://keystone.jkt:5000/v3"],"regionName":"RegionOne"}`), http.StatusCreated, nil)
	return a
}

func TestPutCredentialIsWriteOnlyAndSealed(t *testing.T) {
	a := newCredentialAPI(t)
	body := `{"purpose":"discovery","scope":"provider","owner":"svc-semar",
		"secret":{"application_credential_id":"abc","application_credential_secret":"` + secretMarker + `"}}`

	rec := a.call("PUT", "/api/v1/providers/os-jkt/credentials", body)
	raw := rec.Body.String()
	var c gen.Credential
	a.expect(rec, http.StatusOK, &c)
	if strings.Contains(raw, secretMarker) || strings.Contains(raw, "secret\"") {
		t.Fatalf("response leaks the secret: %s", raw)
	}
	if c.Purpose != gen.Discovery || c.Scope != "provider" || c.Owner != "svc-semar" || c.KekVersion != "t1" {
		t.Fatalf("credential = %+v", c)
	}

	// The stored secret decrypts to exactly what was sent.
	_, plain, err := a.st.OpenCredential(context.Background(), a.keyring, "os-jkt", model.PurposeDiscovery, "provider")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(plain, &got); err != nil || got["application_credential_secret"] != secretMarker {
		t.Fatalf("stored secret = %v, %v", got, err)
	}

	// Replacing keeps the same credential ID.
	var replaced gen.Credential
	a.expect(a.call("PUT", "/api/v1/providers/os-jkt/credentials",
		`{"purpose":"discovery","scope":"provider","secret":{"k":"v2"}}`), http.StatusOK, &replaced)
	if replaced.Id != c.Id || !replaced.RotatedAt.After(c.RotatedAt) {
		t.Fatalf("replace: %+v vs %+v", replaced, c)
	}

	var list gen.CredentialList
	a.expect(a.call("GET", "/api/v1/providers/os-jkt/credentials", ""), http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].Id != c.Id {
		t.Fatalf("list = %+v", list.Items)
	}

	// Nothing in the audit log carries the secret.
	events, err := a.st.ListAuditEvents(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		blob, _ := json.Marshal(e)
		if strings.Contains(string(blob), secretMarker) {
			t.Fatalf("audit event %d contains the secret", e.ID)
		}
	}
	if !strings.Contains(strings.Join(a.auditActions(t), "\n"), "admin credential.put os-jkt/discovery/provider success") {
		t.Fatalf("credential.put not audited: %v", a.auditActions(t))
	}
}

func TestPutCredentialValidation(t *testing.T) {
	a := newCredentialAPI(t)
	cases := map[string]struct {
		path, body string
		status     int
	}{
		"scope not allowed for purpose": {"/api/v1/providers/os-jkt/credentials", `{"purpose":"discovery","scope":"project:x","secret":{"k":"v"}}`, 400},
		"mutate needs project or pool":  {"/api/v1/providers/os-jkt/credentials", `{"purpose":"mutate","scope":"provider","secret":{"k":"v"}}`, 400},
		"unknown purpose":               {"/api/v1/providers/os-jkt/credentials", `{"purpose":"root","scope":"provider","secret":{"k":"v"}}`, 400},
		"empty secret":                  {"/api/v1/providers/os-jkt/credentials", `{"purpose":"discovery","scope":"provider","secret":{}}`, 400},
		"missing secret":                {"/api/v1/providers/os-jkt/credentials", `{"purpose":"discovery","scope":"provider"}`, 400},
		"unknown provider":              {"/api/v1/providers/ghost/credentials", `{"purpose":"discovery","scope":"provider","secret":{"k":"v"}}`, 404},
		"oversized body":                {"/api/v1/providers/os-jkt/credentials", `{"purpose":"discovery","scope":"provider","secret":{"k":"` + strings.Repeat("x", 2<<20) + `"}}`, 400},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a.t = t
			a.expect(a.call("PUT", c.path, c.body), c.status, nil)
		})
	}
}

func TestCredentialsForRemovedProviderAndDelete(t *testing.T) {
	a := newCredentialAPI(t)
	a.expect(a.call("PUT", "/api/v1/providers/os-jkt/credentials",
		`{"purpose":"mutate","scope":"project:0a1b","secret":{"k":"v"}}`), http.StatusOK, nil)
	a.expect(a.call("DELETE", "/api/v1/providers/os-jkt/credentials?purpose=mutate&scope=project:0a1b", ""), http.StatusNoContent, nil)
	a.expect(a.call("DELETE", "/api/v1/providers/os-jkt/credentials?purpose=mutate&scope=project:0a1b", ""), http.StatusNotFound, nil)
	a.expect(a.call("DELETE", "/api/v1/providers/os-jkt/credentials?scope=provider", ""), http.StatusBadRequest, nil)

	a.expect(a.call("DELETE", "/api/v1/providers/os-jkt", ""), http.StatusNoContent, nil)
	a.expect(a.call("PUT", "/api/v1/providers/os-jkt/credentials",
		`{"purpose":"discovery","scope":"provider","secret":{"k":"v"}}`), http.StatusNotFound, nil)
	a.expect(a.call("GET", "/api/v1/providers/os-jkt/credentials", ""), http.StatusNotFound, nil)
}

func TestViewerCannotTouchCredentials(t *testing.T) {
	a := newCredentialAPI(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("viewer-password-1", fastArgon2)
	u, err := a.st.CreateUser(ctx, "vera", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.CreateRoleBinding(ctx, u.ID, auth.RoleBinding{Role: auth.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	a.cookie = nil
	a.expect(a.login("vera", "viewer-password-1"), http.StatusOK, nil)
	a.expect(a.call("GET", "/api/v1/providers/os-jkt/credentials", ""), http.StatusForbidden, nil)
	a.expect(a.call("PUT", "/api/v1/providers/os-jkt/credentials",
		`{"purpose":"discovery","scope":"provider","secret":{"k":"v"}}`), http.StatusForbidden, nil)
}
