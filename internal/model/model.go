// Package model holds SEMAR's location and ownership entities (roadmap §3.5):
// where a resource lives (Site → Provider) and who owns it (Project).
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/sulaimantok/semar/internal/driver/contract"
)

// validID matches IDs used in URLs, task queues and schedule IDs.
var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidateID reports whether id is a valid entity ID.
func ValidateID(kind, id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("invalid %s id %q: use 1-63 lowercase letters, digits or '-', starting with a letter or digit", kind, id)
	}
	return nil
}

// Site is an optional physical location / failure domain.
type Site struct {
	ID        string
	Name      string
	Labels    map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Project is the ownership scope used by RBAC, quotas and cost.
type Project struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Provider is one cloud endpoint (or one PVE cluster's endpoints) in one
// region.
type Provider struct {
	ID     string
	Type   contract.ProviderType
	Name   string
	SiteID string // empty when not assigned to a site
	// Endpoints holds one URL (Keystone) or several PVE node URLs for API
	// failover.
	Endpoints  []string
	RegionName string
	// Fingerprint is the stable identity of the backing cloud; registering
	// the same cloud twice is rejected (roadmap §3.5).
	Fingerprint      string
	TLSFingerprint   string
	Capabilities     json.RawMessage
	MaxMicroversions map[string]string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

// CredentialPurpose is what a provider credential may be used for (ADR-0005).
type CredentialPurpose string

const (
	PurposeDiscovery CredentialPurpose = "discovery"
	PurposeMutate    CredentialPurpose = "mutate"
	PurposeCAPI      CredentialPurpose = "capi"
	PurposeCluster   CredentialPurpose = "cluster"
)

// Valid reports whether p is a known purpose.
func (p CredentialPurpose) Valid() bool {
	switch p {
	case PurposeDiscovery, PurposeMutate, PurposeCAPI, PurposeCluster:
		return true
	}
	return false
}

// ProviderCredential is the metadata of a stored credential. The secret
// itself is only ever handled sealed (see package secret).
type ProviderCredential struct {
	ID         string
	ProviderID string
	Purpose    CredentialPurpose
	// Scope is "provider", "project:<external id>", "pool:<name>" or
	// "cluster:<id>".
	Scope      string
	Owner      string
	KEKVersion string
	ExpiresAt  *time.Time
	CreatedAt  time.Time
	RotatedAt  time.Time
}

// ProjectMapping maps one provider-side scope (OpenStack project, PVE pool
// or tag rule) to a SEMAR Project. Each external scope maps to at most one
// Project.
type ProjectMapping struct {
	ID            int64
	ProjectID     string
	ProviderID    string
	ExternalScope string
	CreatedAt     time.Time
}

// ProviderFingerprint derives a provider's identity from its type, endpoint
// hosts and region, so registering the same cloud twice is detected without
// trusting client input. Endpoint order, scheme, case and trailing slashes do
// not matter.
//
// This is a provisional identity: once real drivers exist, the fingerprint
// returned by CloudDriver.Validate (e.g. the PVE cluster name) replaces it.
func ProviderFingerprint(typ contract.ProviderType, endpoints []string, region string) (string, error) {
	if len(endpoints) == 0 {
		return "", errors.New("at least one endpoint is required")
	}
	hosts := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		u, err := url.Parse(strings.TrimSpace(e))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "", fmt.Errorf("endpoint %q must be an absolute http(s) URL", e)
		}
		hosts = append(hosts, strings.ToLower(u.Host)+strings.TrimRight(u.EscapedPath(), "/"))
	}
	slices.Sort(hosts)
	hosts = slices.Compact(hosts)
	return string(typ) + "://" + strings.Join(hosts, ",") + "#" + region, nil
}
