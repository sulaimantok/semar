// Package openstack is the read-only OpenStack driver (roadmap §3.1). It
// talks to Keystone, Nova, Placement, Glance, Neutron and Cinder directly
// over their REST APIs, pins a microversion on every call, and takes
// hypervisor capacity from Placement (technical notes §1).
package openstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
	"github.com/sulaimantok/semar/internal/driver/httpx"
	"github.com/sulaimantok/semar/internal/requestid"
)

// Config describes how to reach one OpenStack region.
type Config struct {
	// AuthURL is the Keystone v3 URL, e.g. https://keystone.example.com:5000/v3.
	AuthURL string
	Region  string
	// Interface selects catalog endpoints: public (default), internal or admin.
	Interface string

	// Application credential (project-scoped): the default for discovery.
	ApplicationCredentialID     string
	ApplicationCredentialSecret string
	// Password authentication with a system-scoped token, for a reader
	// service user that lists every project's resources.
	Username       string
	Password       string
	UserDomainName string

	HTTPClient *http.Client
	Now        func() time.Time
}

// Secret field names accepted in a provider credential.
const (
	SecretAppCredID     = "application_credential_id"
	SecretAppCredSecret = "application_credential_secret"
	SecretUsername      = "username"
	SecretPassword      = "password"
	SecretUserDomain    = "user_domain_name"
)

// ConfigFromSecret fills the credential fields of cfg from a stored secret.
func ConfigFromSecret(cfg Config, secret map[string]string) (Config, error) {
	switch {
	case secret[SecretAppCredID] != "" && secret[SecretAppCredSecret] != "":
		cfg.ApplicationCredentialID, cfg.ApplicationCredentialSecret = secret[SecretAppCredID], secret[SecretAppCredSecret]
	case secret[SecretUsername] != "" && secret[SecretPassword] != "":
		cfg.Username, cfg.Password = secret[SecretUsername], secret[SecretPassword]
		cfg.UserDomainName = secret[SecretUserDomain]
		if cfg.UserDomainName == "" {
			cfg.UserDomainName = "Default"
		}
	default:
		return cfg, fmt.Errorf("%w: OpenStack credential needs %s/%s or %s/%s",
			contract.ErrUnauthorized, SecretAppCredID, SecretAppCredSecret, SecretUsername, SecretPassword)
	}
	return cfg, nil
}

// Microversion bounds. 2.53 gives hypervisor UUIDs (= Placement resource
// provider UUIDs) and embeds the flavor in servers (2.47); hypervisors are
// read at most at 2.87, the last version that still reports cpu_info.
var (
	minCompute        = version{2, 53}
	maxComputeServers = version{2, 96}
	maxComputeHosts   = version{2, 87}
)

const (
	pageSize = 1000
	// placementConcurrency bounds parallel per-host Placement calls.
	placementConcurrency = 8
	tokenSlack           = 5 * time.Minute
)

// Driver is safe for concurrent use. It caches the Keystone token and the
// discovered API versions.
type Driver struct {
	cfg Config

	mu       sync.Mutex
	tok      *token
	versions *apiVersions
}

var (
	_ contract.CloudDriver         = (*Driver)(nil)
	_ contract.IncrementalVMLister = (*Driver)(nil)
)

// New returns a driver; no request is made until the first call.
func New(cfg Config) (*Driver, error) {
	if cfg.AuthURL == "" {
		return nil, errors.New("openstack: AuthURL is required")
	}
	if cfg.Interface == "" {
		cfg.Interface = "public"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.AuthURL = strings.TrimRight(cfg.AuthURL, "/")
	return &Driver{cfg: cfg}, nil
}

// Type implements contract.CloudDriver.
func (d *Driver) Type() contract.ProviderType { return contract.ProviderOpenStack }

// Capabilities implements contract.CloudDriver.
func (d *Driver) Capabilities() contract.Capabilities {
	return contract.Capabilities{
		Architectures:    []contract.Arch{contract.ArchAMD64, contract.ArchARM64},
		FloatingIP:       true,
		SecurityGroups:   true,
		Volumes:          true,
		SnapshotRevert:   true,
		RevertLatestOnly: true,
		ConsoleVNC:       true,
		ConsoleSerial:    true,
	}
}

// --- Keystone ---

type token struct {
	value   string
	expires time.Time
	// endpoints maps service type to its catalog URL for this region.
	endpoints map[string]string
	// computeEndpointID identifies the region's compute endpoint in the
	// Keystone catalog; it survives URL renames, unlike the URL itself.
	computeEndpointID string
}

type authResponse struct {
	Token struct {
		ExpiresAt time.Time `json:"expires_at"`
		Catalog   []struct {
			Type      string `json:"type"`
			Endpoints []struct {
				ID        string `json:"id"`
				Interface string `json:"interface"`
				Region    string `json:"region"`
				RegionID  string `json:"region_id"`
				URL       string `json:"url"`
			} `json:"endpoints"`
		} `json:"catalog"`
	} `json:"token"`
}

func (d *Driver) authBody() any {
	if d.cfg.ApplicationCredentialID != "" {
		return map[string]any{"auth": map[string]any{"identity": map[string]any{
			"methods": []string{"application_credential"},
			"application_credential": map[string]string{
				"id": d.cfg.ApplicationCredentialID, "secret": d.cfg.ApplicationCredentialSecret,
			},
		}}}
	}
	return map[string]any{"auth": map[string]any{
		"identity": map[string]any{
			"methods": []string{"password"},
			"password": map[string]any{"user": map[string]any{
				"name": d.cfg.Username, "password": d.cfg.Password,
				"domain": map[string]string{"name": d.cfg.UserDomainName},
			}},
		},
		"scope": map[string]any{"system": map[string]bool{"all": true}},
	}}
}

// allProjects reports whether listings span every project. Only a
// system-scoped token may; an application credential sees its project.
func (d *Driver) allProjects() bool { return d.cfg.ApplicationCredentialID == "" }

func (d *Driver) authenticate(ctx context.Context) (*token, error) {
	var out authResponse
	h, err := httpx.Do(ctx, d.cfg.HTTPClient, httpx.Request{
		Method: http.MethodPost, URL: d.cfg.AuthURL + "/auth/tokens", Body: d.authBody(), Header: d.reqHeader(ctx),
	}, &out)
	if err != nil {
		return nil, fmt.Errorf("keystone: %w", err)
	}
	t := &token{value: h.Get("X-Subject-Token"), expires: out.Token.ExpiresAt, endpoints: map[string]string{}}
	if t.value == "" {
		return nil, fmt.Errorf("keystone: no X-Subject-Token in response: %w", contract.ErrUnavailable)
	}
	for _, svc := range out.Token.Catalog {
		for _, e := range svc.Endpoints {
			if e.Interface != d.cfg.Interface || (d.cfg.Region != "" && e.RegionID != d.cfg.Region && e.Region != d.cfg.Region) {
				continue
			}
			if _, seen := t.endpoints[svc.Type]; seen {
				continue
			}
			t.endpoints[svc.Type] = strings.TrimRight(e.URL, "/")
			if svc.Type == "compute" {
				t.computeEndpointID = e.ID
			}
		}
	}
	if t.endpoints["compute"] == "" {
		return nil, fmt.Errorf("keystone: no %s compute endpoint for region %q in the catalog: %w",
			d.cfg.Interface, d.cfg.Region, contract.ErrNotFound)
	}
	return t, nil
}

// currentToken returns a cached token, renewing it shortly before expiry.
func (d *Driver) currentToken(ctx context.Context, renew bool) (*token, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !renew && d.tok != nil && d.cfg.Now().Add(tokenSlack).Before(d.tok.expires) {
		return d.tok, nil
	}
	t, err := d.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	d.tok = t
	return t, nil
}

func (d *Driver) reqHeader(ctx context.Context) http.Header {
	h := http.Header{}
	if id := requestid.OpenStack(requestid.From(ctx)); id != "" {
		h.Set("X-OpenStack-Request-ID", id)
	}
	return h
}

// call sends an authenticated request to a catalog service. path is
// relative to the service endpoint; a 401 renews the token once.
func (d *Driver) call(ctx context.Context, service, path, microversion string, out any) error {
	for attempt := 0; ; attempt++ {
		t, err := d.currentToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		base, ok := t.endpoints[service]
		if !ok {
			return fmt.Errorf("no %s endpoint in the catalog: %w", service, contract.ErrNotFound)
		}
		h := d.reqHeader(ctx)
		h.Set("X-Auth-Token", t.value)
		if microversion != "" {
			h.Set("OpenStack-API-Version", microversion)
		}
		_, err = httpx.Do(ctx, d.cfg.HTTPClient, httpx.Request{Method: http.MethodGet, URL: base + path, Header: h}, out)
		var se *httpx.StatusError
		if attempt == 0 && errors.As(err, &se) && se.Status == http.StatusUnauthorized {
			continue
		}
		return err
	}
}

func (d *Driver) hasService(ctx context.Context, service string) (bool, error) {
	t, err := d.currentToken(ctx, false)
	if err != nil {
		return false, err
	}
	_, ok := t.endpoints[service]
	return ok, nil
}

// --- Versions ---

type version struct{ major, minor int }

func parseVersion(s string) (version, bool) {
	var v version
	if _, err := fmt.Sscanf(strings.TrimPrefix(s, "v"), "%d.%d", &v.major, &v.minor); err != nil {
		return version{}, false
	}
	return v, true
}

func (v version) String() string { return fmt.Sprintf("%d.%d", v.major, v.minor) }

func (v version) less(o version) bool {
	return v.major < o.major || (v.major == o.major && v.minor < o.minor)
}

func minVersion(a, b version) version {
	if a.less(b) {
		return a
	}
	return b
}

type apiVersions struct {
	compute   version
	placement version
	// raw holds the reported maxima for Provider.max_microversions.
	raw map[string]string
}

var versionSegment = regexp.MustCompile(`^v[0-9]+(\.[0-9]+)?$`)

// versionRoot returns the URL of a service's version document: the
// endpoint cut before its version segment, so both
// https://cinder:8776/v3/<project> and https://cloud/volume/v3/<project>
// work.
func versionRoot(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, s := range segs {
		if versionSegment.MatchString(s) {
			segs = segs[:i]
			break
		}
	}
	u.Path, u.RawQuery = strings.Join(segs, "/"), ""
	return strings.TrimRight(u.String(), "/")
}

func (d *Driver) discoverVersions(ctx context.Context) (*apiVersions, error) {
	d.mu.Lock()
	cached := d.versions
	d.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	t, err := d.currentToken(ctx, false)
	if err != nil {
		return nil, err
	}
	v := &apiVersions{raw: map[string]string{}}

	// Nova answers its versioned endpoint (…/v2.1) with {"version": …}.
	var compute struct {
		Version struct {
			Version string `json:"version"`
		} `json:"version"`
	}
	h := d.reqHeader(ctx)
	h.Set("X-Auth-Token", t.value)
	if _, err := httpx.Do(ctx, d.cfg.HTTPClient, httpx.Request{Method: http.MethodGet, URL: t.endpoints["compute"], Header: h}, &compute); err != nil {
		return nil, fmt.Errorf("compute versions: %w", err)
	}
	cv, ok := parseVersion(compute.Version.Version)
	if !ok {
		return nil, fmt.Errorf("compute reports no microversion support: %w", contract.ErrUnavailable)
	}
	if cv.less(minCompute) {
		return nil, fmt.Errorf("compute API %s is older than the required %s: %w", cv, minCompute, contract.ErrUnavailable)
	}
	v.compute = cv
	v.raw["compute"] = cv.String()

	if base, ok := t.endpoints["placement"]; ok {
		var placement struct {
			Versions []struct {
				MaxVersion string `json:"max_version"`
			} `json:"versions"`
		}
		if _, err := httpx.Do(ctx, d.cfg.HTTPClient, httpx.Request{Method: http.MethodGet, URL: base + "/", Header: h}, &placement); err == nil &&
			len(placement.Versions) > 0 {
			if pv, ok := parseVersion(placement.Versions[0].MaxVersion); ok {
				v.placement = pv
				v.raw["placement"] = pv.String()
			}
		}
	}
	for svc, key := range map[string]string{"volumev3": "volume", "image": "image", "network": "network"} {
		base, ok := t.endpoints[svc]
		if !ok {
			continue
		}
		var doc struct {
			Versions []struct {
				ID      string `json:"id"`
				Status  string `json:"status"`
				Version string `json:"version"`
			} `json:"versions"`
		}
		req := httpx.Request{Method: http.MethodGet, URL: versionRoot(base) + "/", Header: h, AllowMultipleChoices: true}
		if _, err := httpx.Do(ctx, d.cfg.HTTPClient, req, &doc); err != nil {
			continue
		}
		for _, e := range doc.Versions {
			if strings.EqualFold(e.Status, "CURRENT") {
				if e.Version != "" {
					v.raw[key] = e.Version
				} else {
					v.raw[key] = strings.TrimPrefix(e.ID, "v")
				}
			}
		}
	}

	d.mu.Lock()
	d.versions = v
	d.mu.Unlock()
	return v, nil
}

// Validate implements contract.CloudDriver: it authenticates afresh and
// reports the compute endpoint's catalog ID as the cloud identity.
func (d *Driver) Validate(ctx context.Context) (contract.ProviderInfo, error) {
	t, err := d.currentToken(ctx, true)
	if err != nil {
		return contract.ProviderInfo{}, err
	}
	d.mu.Lock()
	d.versions = nil
	d.mu.Unlock()
	v, err := d.discoverVersions(ctx)
	if err != nil {
		return contract.ProviderInfo{}, err
	}
	id := t.computeEndpointID
	if id == "" {
		id = t.endpoints["compute"]
	}
	return contract.ProviderInfo{
		Fingerprint: "openstack:" + d.cfg.Region + ":" + id,
		APIVersions: v.raw,
	}, nil
}

func computeHeader(v version) string { return "compute " + v.String() }
