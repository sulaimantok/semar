package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

// fastArgon2 keeps password hashing cheap in tests.
var fastArgon2 = auth.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}

const adminPassword = "admin-password-for-tests"

// settingsAPI is an HTTP handler backed by a throwaway database, acting as
// one signed-in browser session.
type settingsAPI struct {
	t       *testing.T
	h       http.Handler
	st      *store.Store
	keyring *secret.Keyring
	cookie  *http.Cookie
}

// newAnonymousAPI returns an API client with a bootstrapped admin account
// but no session.
func newAnonymousAPI(t *testing.T) *settingsAPI {
	t.Helper()
	st := storetest.New(t)
	hash, err := auth.HashPassword(adminPassword, fastArgon2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureBootstrapAdmin(context.Background(), "admin", hash); err != nil {
		t.Fatal(err)
	}
	keyring, err := secret.NewKeyring("t1", map[string][]byte{"t1": bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return &settingsAPI{t: t, st: st, keyring: keyring, h: NewHandler(Options{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Settings:    st,
		Auth:        st,
		Audit:       st,
		Credentials: st,
		Secrets:     keyring,
		Inventory:   st,
	})}
}

// newSettingsAPI returns a client signed in as the bootstrap admin.
func newSettingsAPI(t *testing.T) *settingsAPI {
	t.Helper()
	a := newAnonymousAPI(t)
	a.login("admin", adminPassword)
	return a
}

// login signs in and keeps the session cookie for later calls.
func (a *settingsAPI) login(username, password string) *httptest.ResponseRecorder {
	a.t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	rec := a.call("POST", "/api/v1/auth/login", string(body))
	if rec.Code == http.StatusOK {
		for _, c := range rec.Result().Cookies() {
			if c.Name == auth.SessionCookie {
				a.cookie = c
			}
		}
	}
	return rec
}

// call sends a request as the browser would: with the session cookie and
// the CSRF header on unsafe methods.
func (a *settingsAPI) call(method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	return a.callWith(method, path, body, http.Header{CSRFHeader: {"semar-web"}})
}

func (a *settingsAPI) callWith(method, path, body string, header http.Header) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequestWithContext(a.t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if a.cookie != nil {
		req.AddCookie(a.cookie)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return rec
}

// expect asserts the status and decodes a JSON body into out (if non-nil).
func (a *settingsAPI) expect(rec *httptest.ResponseRecorder, status int, out any) {
	a.t.Helper()
	if rec.Code != status {
		a.t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body.String())
	}
	if status >= 400 {
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			a.t.Fatalf("error content-type = %q, want application/problem+json", ct)
		}
	}
	if out != nil {
		if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
			a.t.Fatal(err)
		}
	}
}

func TestSettingsSites(t *testing.T) {
	a := newSettingsAPI(t)

	var site gen.Site
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"jkt","name":" Jakarta IDC ","labels":{"env":"prod"}}`), 201, &site)
	if site.Id != "jkt" || site.Name != "Jakarta IDC" || site.Labels["env"] != "prod" || site.CreatedAt.IsZero() {
		t.Fatalf("created site = %+v", site)
	}

	var p gen.Problem
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"jkt","name":"again"}`), 409, &p)
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"Bad ID","name":"x"}`), 400, &p)
	if p.Detail == nil || !strings.Contains(*p.Detail, "invalid site id") {
		t.Fatalf("invalid id detail = %v", p.Detail)
	}
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"lab","name":"   "}`), 400, nil)
	a.expect(a.call("POST", "/api/v1/sites", `{not json`), 400, nil)

	var list gen.SiteList
	a.expect(a.call("GET", "/api/v1/sites", ""), 200, &list)
	if len(list.Items) != 1 {
		t.Fatalf("sites = %+v", list.Items)
	}
}

func TestSettingsProviders(t *testing.T) {
	a := newSettingsAPI(t)
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"jkt","name":"Jakarta"}`), 201, nil)

	var p gen.Provider
	a.expect(a.call("POST", "/api/v1/providers", `{
		"id":"os-jkt","type":"openstack","name":"OpenStack Jakarta","siteId":"jkt",
		"endpoints":["https://Keystone.JKT:5000/v3/"],"regionName":"RegionOne"}`), 201, &p)
	if p.Fingerprint != "openstack://keystone.jkt:5000/v3#RegionOne" || p.SiteId == nil || *p.SiteId != "jkt" {
		t.Fatalf("created provider = %+v", p)
	}

	// Same cloud and region under another id → 409; another region → 201.
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"os-jkt-dup","type":"openstack","name":"dup",
		"endpoints":["https://keystone.jkt:5000/v3"],"regionName":"RegionOne"}`), 409, nil)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"os-jkt-r2","type":"openstack","name":"Region 2",
		"endpoints":["https://keystone.jkt:5000/v3"],"regionName":"RegionTwo"}`), 201, nil)

	a.expect(a.call("POST", "/api/v1/providers", `{"id":"bad-ep","type":"proxmox","name":"x","endpoints":["pve1:8006"]}`), 400, nil)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"bad-type","type":"vmware","name":"x","endpoints":["https://x"]}`), 400, nil)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"no-site","type":"proxmox","name":"x","siteId":"nowhere","endpoints":["https://pve1:8006"]}`), 422, nil)

	var got gen.Provider
	a.expect(a.call("GET", "/api/v1/providers/os-jkt", ""), 200, &got)
	if got.Id != "os-jkt" {
		t.Fatalf("get = %+v", got)
	}
	a.expect(a.call("DELETE", "/api/v1/providers/os-jkt", ""), 204, nil)
	a.expect(a.call("GET", "/api/v1/providers/os-jkt", ""), 404, nil)
	a.expect(a.call("DELETE", "/api/v1/providers/os-jkt", ""), 404, nil)

	var list gen.ProviderList
	a.expect(a.call("GET", "/api/v1/providers", ""), 200, &list)
	if len(list.Items) != 1 || list.Items[0].Id != "os-jkt-r2" {
		t.Fatalf("providers = %+v", list.Items)
	}
}

func TestSettingsProjectMappings(t *testing.T) {
	a := newSettingsAPI(t)
	a.expect(a.call("POST", "/api/v1/projects", `{"id":"payment","name":"Payment"}`), 201, nil)
	a.expect(a.call("POST", "/api/v1/projects", `{"id":"search","name":"Search"}`), 201, nil)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"pve-lab","type":"proxmox","name":"PVE Lab","endpoints":["https://pve1.lab:8006"]}`), 201, nil)

	var m gen.ProjectMapping
	a.expect(a.call("POST", "/api/v1/projects/payment/mappings", `{"providerId":"pve-lab","externalScope":"pool:payment"}`), 201, &m)
	if m.ProjectId != "payment" || m.ExternalScope != "pool:payment" || m.Id == 0 {
		t.Fatalf("mapping = %+v", m)
	}
	// The same scope cannot belong to a second project.
	a.expect(a.call("POST", "/api/v1/projects/search/mappings", `{"providerId":"pve-lab","externalScope":"pool:payment"}`), 409, nil)
	a.expect(a.call("POST", "/api/v1/projects/nobody/mappings", `{"providerId":"pve-lab","externalScope":"pool:x"}`), 404, nil)
	a.expect(a.call("POST", "/api/v1/projects/payment/mappings", `{"providerId":"ghost","externalScope":"pool:x"}`), 422, nil)
	a.expect(a.call("POST", "/api/v1/projects/payment/mappings", `{"providerId":"pve-lab","externalScope":" "}`), 400, nil)

	// A removed provider cannot receive new mappings.
	a.expect(a.call("DELETE", "/api/v1/providers/pve-lab", ""), 204, nil)
	a.expect(a.call("POST", "/api/v1/projects/search/mappings", `{"providerId":"pve-lab","externalScope":"pool:search"}`), 422, nil)

	var list gen.ProjectMappingList
	a.expect(a.call("GET", "/api/v1/projects/payment/mappings", ""), 200, &list)
	if len(list.Items) != 1 {
		t.Fatalf("mappings = %+v", list.Items)
	}
	a.expect(a.call("GET", "/api/v1/projects/nobody/mappings", ""), 404, nil)

	var projects gen.ProjectList
	a.expect(a.call("GET", "/api/v1/projects", ""), 200, &projects)
	if len(projects.Items) != 2 {
		t.Fatalf("projects = %+v", projects.Items)
	}
}

func TestSettingsRequireAuthentication(t *testing.T) {
	rec := do(t, newTestHandler(nil), http.MethodGet, "/api/v1/sites", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	decodeProblem(t, rec)
}

// auditActions returns "actor action target outcome" for every audit event.
func (a *settingsAPI) auditActions(t *testing.T) []string {
	t.Helper()
	events, err := a.st.ListAuditEvents(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Actor+" "+e.Action+" "+e.Target+" "+string(e.Outcome))
	}
	return out
}
