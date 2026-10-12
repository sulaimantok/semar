package auth

import "context"

// Role is a SEMAR role (roadmap §3.4).
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleOperator || r == RoleViewer }

// RoleBinding grants a role globally (empty ProjectID and ProviderID) or
// within a project and/or provider.
type RoleBinding struct {
	Role       Role
	ProjectID  string
	ProviderID string
}

// Global reports whether the binding is not scoped.
func (b RoleBinding) Global() bool { return b.ProjectID == "" && b.ProviderID == "" }

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID   string
	Username string
	Bindings []RoleBinding
}

// IsGlobalAdmin reports whether the principal holds an unscoped admin role.
func (p *Principal) IsGlobalAdmin() bool {
	if p == nil {
		return false
	}
	for _, b := range p.Bindings {
		if b.Role == RoleAdmin && b.Global() {
			return true
		}
	}
	return false
}

type principalKey struct{}

// WithPrincipal returns ctx carrying p.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the request's principal, or nil when anonymous.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
