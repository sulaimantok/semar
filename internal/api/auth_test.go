package api

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
)

func TestEveryOperationHasAnAccessPolicy(t *testing.T) {
	ops := reflect.TypeOf((*gen.StrictServerInterface)(nil)).Elem()
	for i := 0; i < ops.NumMethod(); i++ {
		name := ops.Method(i).Name
		if _, ok := operationAccess[name]; !ok {
			t.Errorf("operation %s has no entry in operationAccess (it would be refused)", name)
		}
	}
	if len(operationAccess) != ops.NumMethod() {
		t.Errorf("operationAccess has %d entries, the spec has %d operations", len(operationAccess), ops.NumMethod())
	}
}

func TestLoginSetsSecureSessionCookie(t *testing.T) {
	a := newAnonymousAPI(t)
	rec := a.login("Admin", adminPassword) // usernames are case-insensitive
	var me gen.Me
	a.expect(rec, http.StatusOK, &me)
	if me.Username != "admin" || len(me.Roles) != 1 || me.Roles[0].Role != gen.Admin {
		t.Fatalf("me = %+v", me)
	}
	c := a.cookie
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Value == "" {
		t.Fatalf("session cookie = %+v", c)
	}
	var again gen.Me
	a.expect(a.call("GET", "/api/v1/auth/me", ""), http.StatusOK, &again)
	if again.Username != "admin" {
		t.Fatalf("GET /me = %+v", again)
	}
}

func TestLoginFailuresAreUniformAndAudited(t *testing.T) {
	a := newAnonymousAPI(t)
	var wrong, unknown gen.Problem
	a.expect(a.login("admin", "not-the-password"), http.StatusUnauthorized, &wrong)
	a.expect(a.login("nobody", "whatever-password"), http.StatusUnauthorized, &unknown)
	if *wrong.Detail != *unknown.Detail {
		t.Fatalf("failure messages differ: %q vs %q (reveals which usernames exist)", *wrong.Detail, *unknown.Detail)
	}
	if a.cookie != nil {
		t.Fatal("failed login set a session cookie")
	}
	a.expect(a.callWith("POST", "/api/v1/auth/login", "{", http.Header{CSRFHeader: {"x"}}), http.StatusBadRequest, nil)

	if n, err := a.st.VerifyAuditChain(context.Background()); err != nil || n != 2 {
		t.Fatalf("audit chain = %d events, %v; want 2 failed logins", n, err)
	}
}

func TestViewerCannotMutateSettings(t *testing.T) {
	a := newAnonymousAPI(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("viewer-password-1", fastArgon2)
	u, err := a.st.CreateUser(ctx, "vera", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.CreateRoleBinding(ctx, u.ID, auth.RoleBinding{Role: auth.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	a.expect(a.login("vera", "viewer-password-1"), http.StatusOK, nil)

	a.expect(a.call("GET", "/api/v1/sites", ""), http.StatusOK, nil)
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"jkt","name":"Jakarta"}`), http.StatusForbidden, nil)
	a.expect(a.call("DELETE", "/api/v1/providers/x", ""), http.StatusForbidden, nil)
}

func TestCSRFProtection(t *testing.T) {
	a := newSettingsAPI(t)
	// No CSRF header → refused even with a valid session.
	a.expect(a.callWith("POST", "/api/v1/sites", `{"id":"jkt","name":"J"}`, nil), http.StatusForbidden, nil)
	// Cross-origin browser request → refused.
	a.expect(a.callWith("POST", "/api/v1/sites", `{"id":"jkt","name":"J"}`,
		http.Header{CSRFHeader: {"x"}, "Origin": {"https://evil.example"}}), http.StatusForbidden, nil)
	// Same origin (httptest host is example.com) → allowed.
	a.expect(a.callWith("POST", "/api/v1/sites", `{"id":"jkt","name":"J"}`,
		http.Header{CSRFHeader: {"x"}, "Origin": {"http://example.com"}}), http.StatusCreated, nil)
	// Safe methods need no header.
	a.expect(a.callWith("GET", "/api/v1/sites", "", nil), http.StatusOK, nil)
}

func TestLogoutRevokesSession(t *testing.T) {
	a := newSettingsAPI(t)
	old := a.cookie
	rec := a.call("POST", "/api/v1/auth/logout", "")
	a.expect(rec, http.StatusNoContent, nil)
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not clear the cookie")
	}
	// Replaying the old token must not work.
	a.cookie = old
	a.expect(a.call("GET", "/api/v1/auth/me", ""), http.StatusUnauthorized, nil)
	// A forged token is anonymous too.
	a.cookie = &http.Cookie{Name: auth.SessionCookie, Value: "forged"} //nolint:gosec // deliberately forged test cookie
	a.expect(a.call("GET", "/api/v1/auth/me", ""), http.StatusUnauthorized, nil)
}

func TestSettingsMutationsAreAudited(t *testing.T) {
	a := newSettingsAPI(t)
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"jkt","name":"Jakarta"}`), http.StatusCreated, nil)
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"jkt","name":"again"}`), http.StatusConflict, nil)

	events := a.auditActions(t)
	want := []string{
		"admin auth.login admin success",
		"admin site.create jkt requested",
		"admin site.create jkt success",
		"admin site.create jkt requested",
		"admin site.create jkt failure",
	}
	if strings.Join(events, "\n") != strings.Join(want, "\n") {
		t.Fatalf("audit events:\n%s\nwant:\n%s", strings.Join(events, "\n"), strings.Join(want, "\n"))
	}
	if _, err := a.st.VerifyAuditChain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
