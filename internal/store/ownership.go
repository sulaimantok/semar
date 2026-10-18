package store

import (
	"path"
	"sort"
	"strings"
)

// Mapping rule prefixes. A project mapping's external scope is matched
// exactly against a resource's provider-side scope (OpenStack project ID,
// "pool:<name>"), or, for VMs, it is a rule (tracker 1.3):
//   - "tag:<tag>"      VMs carrying that tag (Proxmox tags, OpenStack
//     server tags and metadata keys);
//   - "name:<pattern>" VMs whose name matches the pattern (path.Match:
//     *, ?, [a-z]).
const (
	RuleTag  = "tag:"
	RuleName = "name:"
)

type rule struct{ value, project string }

// ownership derives a resource's project from a provider's mappings.
// Precedence: the exact scope, then tag rules (lexically first tag wins),
// then name rules, most specific first (most literal characters, so
// "web-*" beats "*"), ties broken lexically. The result is deterministic.
type ownership struct {
	exact map[string]string
	tags  []rule
	names []rule
}

func newOwnership(scopes map[string]string) ownership {
	o := ownership{exact: map[string]string{}}
	for scope, project := range scopes {
		switch {
		case strings.HasPrefix(scope, RuleTag):
			o.tags = append(o.tags, rule{strings.TrimPrefix(scope, RuleTag), project})
		case strings.HasPrefix(scope, RuleName):
			o.names = append(o.names, rule{strings.TrimPrefix(scope, RuleName), project})
		default:
			o.exact[scope] = project
		}
	}
	sort.Slice(o.tags, func(i, j int) bool { return o.tags[i].value < o.tags[j].value })
	sort.Slice(o.names, func(i, j int) bool {
		si, sj := literals(o.names[i].value), literals(o.names[j].value)
		if si != sj {
			return si > sj
		}
		return o.names[i].value < o.names[j].value
	})
	return o
}

func (o ownership) projectOf(r ObservedResource) (string, bool) {
	if r.ExternalScope != "" {
		if p, ok := o.exact[r.ExternalScope]; ok {
			return p, true
		}
	}
	if r.Kind != KindVM {
		return "", false
	}
	if len(o.tags) > 0 {
		tags := resourceTags(r.Spec["tags"])
		for _, t := range o.tags {
			if tags[t.value] {
				return t.project, true
			}
		}
	}
	for _, n := range o.names {
		if ok, _ := path.Match(n.value, r.Name); ok {
			return n.project, true
		}
	}
	return "", false
}

// literals counts the characters of a pattern that are not wildcards.
func literals(pattern string) int {
	return len(pattern) - strings.Count(pattern, "*") - strings.Count(pattern, "?")
}

// resourceTags reads the "tags" spec field, a map (driver output) or a list.
func resourceTags(v any) map[string]bool {
	out := map[string]bool{}
	switch t := v.(type) {
	case map[string]string:
		for k := range t {
			out[k] = true
		}
	case map[string]any:
		for k := range t {
			out[k] = true
		}
	case []string:
		for _, k := range t {
			out[k] = true
		}
	case []any:
		for _, k := range t {
			if s, ok := k.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// ValidateMappingScope checks a mapping's external scope, including rule
// syntax.
func ValidateMappingScope(scope string) error {
	switch {
	case strings.TrimSpace(scope) == "":
		return errInvalid("externalScope is required")
	case strings.HasPrefix(scope, RuleTag) && strings.TrimPrefix(scope, RuleTag) == "":
		return errInvalid("tag rule needs a tag: tag:<tag>")
	case strings.HasPrefix(scope, RuleName):
		p := strings.TrimPrefix(scope, RuleName)
		if p == "" {
			return errInvalid("name rule needs a pattern: name:<pattern>")
		}
		if _, err := path.Match(p, ""); err != nil {
			return errInvalid("name rule has a malformed pattern")
		}
	}
	return nil
}

func errInvalid(msg string) error { return &invalidError{msg} }

type invalidError struct{ msg string }

func (e *invalidError) Error() string { return ErrInvalid.Error() + ": " + e.msg }
func (e *invalidError) Unwrap() error { return ErrInvalid }
