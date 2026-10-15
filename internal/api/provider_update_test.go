package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/fake"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/model"
)

// scriptedDriver answers Validate with a fixed result.
type scriptedDriver struct {
	*fake.Driver
	info contract.ProviderInfo
	err  error
}

func (d scriptedDriver) Validate(context.Context) (contract.ProviderInfo, error) {
	return d.info, d.err
}

func TestUpdateProvider(t *testing.T) {
	a := newSettingsAPI(t)
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"site-a","name":"Site A"}`), http.StatusCreated, nil)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"os-a","type":"openstack","name":"OS A","endpoints":["https://keystone-a.example.com:5000/v3"],"regionName":"RegionOne"}`), http.StatusCreated, nil)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"os-b","type":"openstack","name":"OS B","endpoints":["https://keystone-b.example.com:5000/v3"],"regionName":"RegionOne"}`), http.StatusCreated, nil)

	var p gen.Provider
	a.expect(a.call("PATCH", "/api/v1/providers/os-a", `{"name":" OS A (prod) ","siteId":"site-a"}`), http.StatusOK, &p)
	if p.Name != "OS A (prod)" || p.SiteId == nil || *p.SiteId != "site-a" || p.Type != gen.Openstack || p.RegionName != "RegionOne" {
		t.Fatalf("after rename/site = %+v", p)
	}
	before := p.Fingerprint

	// Decode into a fresh value: omitted fields would otherwise keep the
	// previous response's values.
	p = gen.Provider{}
	a.expect(a.call("PATCH", "/api/v1/providers/os-a", `{"endpoints":["https://identity-a.example.com/v3"],"siteId":""}`), http.StatusOK, &p)
	if p.Fingerprint == before || p.SiteId != nil || p.Endpoints[0] != "https://identity-a.example.com/v3" {
		t.Fatalf("after endpoint change = %+v (fingerprint was %s)", p, before)
	}

	// Pointing at another provider's cloud is a conflict, not a merge.
	a.expect(a.call("PATCH", "/api/v1/providers/os-a", `{"endpoints":["https://keystone-b.example.com:5000/v3"]}`), http.StatusConflict, nil)
	for _, body := range []string{`{}`, `{"name":"  "}`, `{"siteId":"Not Valid"}`, `{"endpoints":["keystone"]}`, `{"endpoints":[]}`} {
		a.expect(a.call("PATCH", "/api/v1/providers/os-a", body), http.StatusBadRequest, nil)
	}
	a.expect(a.call("PATCH", "/api/v1/providers/os-a", `{"siteId":"missing"}`), http.StatusUnprocessableEntity, nil)
	a.expect(a.call("PATCH", "/api/v1/providers/ghost", `{"name":"x"}`), http.StatusNotFound, nil)

	events := 0
	for _, e := range a.auditActions(t) {
		if strings.Contains(e, "provider.update os-a success") {
			events++
		}
	}
	if events != 2 {
		t.Fatalf("%d successful provider.update audit events, want 2", events)
	}
}

func TestValidateProvider(t *testing.T) {
	var next scriptedDriver
	a := newSettingsAPI(t, func(o *Options) {
		o.Drivers = func(ctx context.Context, p model.Provider) (contract.CloudDriver, error) {
			if p.ID == "real" {
				return registry.New(p)
			}
			if p.Type != contract.ProviderFake {
				return registry.New(p)
			}
			return next, nil
		}
	})
	for _, body := range []string{
		`{"id":"real","type":"fake","name":"Real","endpoints":["https://real.example.com"]}`,
		`{"id":"one","type":"fake","name":"One","endpoints":["https://one.example.com"]}`,
		`{"id":"two","type":"fake","name":"Two","endpoints":["https://two.example.com"]}`,
		`{"id":"os","type":"openstack","name":"OS","endpoints":["https://keystone.example.com/v3"],"regionName":"RegionOne"}`,
	} {
		a.expect(a.call("POST", "/api/v1/providers", body), http.StatusCreated, nil)
	}

	var p gen.Provider
	a.expect(a.call("POST", "/api/v1/providers/real/validate", ""), http.StatusOK, &p)
	if p.CloudFingerprint == nil || *p.CloudFingerprint == "" || p.ValidatedAt == nil || p.ApiVersions == nil || (*p.ApiVersions)["compute"] == "" {
		t.Fatalf("validated provider = %+v", p)
	}
	a.expect(a.call("GET", "/api/v1/providers/real", ""), http.StatusOK, &p)
	if p.ValidatedAt == nil {
		t.Fatal("validation not persisted")
	}

	next = scriptedDriver{info: contract.ProviderInfo{Fingerprint: "cloud-x"}}
	a.expect(a.call("POST", "/api/v1/providers/one/validate", ""), http.StatusOK, nil)
	// The same cloud behind a different URL is detected.
	var prob gen.Problem
	a.expect(a.call("POST", "/api/v1/providers/two/validate", ""), http.StatusConflict, &prob)
	if *prob.Detail != "this cloud is already registered as provider one" {
		t.Fatalf("conflict detail = %q", *prob.Detail)
	}

	for _, c := range []struct {
		err    error
		status int
	}{
		{contract.ErrUnauthorized, http.StatusBadGateway},
		{contract.ErrUnavailable, http.StatusBadGateway},
		{context.DeadlineExceeded, http.StatusGatewayTimeout},
	} {
		next = scriptedDriver{err: c.err}
		a.expect(a.call("POST", "/api/v1/providers/two/validate", ""), c.status, &prob)
		if prob.Detail == nil || *prob.Detail == c.err.Error() {
			t.Fatalf("%v: detail %v must classify, not echo, the driver error", c.err, prob.Detail)
		}
	}
	next = scriptedDriver{}
	a.expect(a.call("POST", "/api/v1/providers/two/validate", ""), http.StatusBadGateway, nil)
	a.expect(a.call("POST", "/api/v1/providers/os/validate", ""), http.StatusNotImplemented, nil)
	a.expect(a.call("POST", "/api/v1/providers/ghost/validate", ""), http.StatusNotFound, nil)

	// A new address must be checked again.
	a.expect(a.call("PATCH", "/api/v1/providers/one", `{"name":"One renamed"}`), http.StatusOK, &p)
	if p.ValidatedAt == nil {
		t.Fatal("rename cleared the connection check")
	}
	p = gen.Provider{}
	a.expect(a.call("PATCH", "/api/v1/providers/one", `{"endpoints":["https://one-new.example.com"]}`), http.StatusOK, &p)
	if p.ValidatedAt != nil || p.CloudFingerprint != nil {
		t.Fatalf("endpoint change kept the connection check: %+v", p)
	}
}

func TestTrustedOriginsBehindProxy(t *testing.T) {
	a := newSettingsAPI(t, func(o *Options) { o.TrustedOrigins = []string{"https://semar.example.com"} })
	body := `{"id":"site-p","name":"Proxy"}`
	// Behind the proxy the API sees an internal Host, so Host matching
	// would refuse the real UI and accept a forged Origin equal to it.
	a.expect(a.callWith("POST", "/api/v1/sites", body,
		http.Header{CSRFHeader: {"x"}, "Origin": {"http://example.com"}}), http.StatusForbidden, nil)
	a.expect(a.callWith("POST", "/api/v1/sites", body,
		http.Header{CSRFHeader: {"x"}, "Origin": {"https://evil.example"}}), http.StatusForbidden, nil)
	a.expect(a.callWith("POST", "/api/v1/sites", body,
		http.Header{CSRFHeader: {"x"}, "Origin": {"https://SEMAR.example.com"}}), http.StatusCreated, nil)
}
