package store

import "testing"

func TestOwnershipPrecedence(t *testing.T) {
	o := newOwnership(map[string]string{
		"pool:payment": "payment",
		"tag:billing":  "billing",
		"tag:alpha":    "alpha",
		"name:web-*":   "web",
		"name:*":       "catch-all",
	})
	vm := func(name, scope string, tags any) ObservedResource {
		return ObservedResource{Kind: KindVM, Name: name, ExternalScope: scope, Spec: map[string]any{"tags": tags}}
	}
	for _, c := range []struct {
		r    ObservedResource
		want string
	}{
		{vm("web-1", "pool:payment", map[string]string{"billing": ""}), "payment"},        // exact scope first
		{vm("web-1", "", map[string]string{"billing": "", "alpha": ""}), "alpha"},         // lexically first tag
		{vm("web-1", "", []any{"billing"}), "billing"},                                    // JSON list form
		{vm("web-1", "", nil), "web"},                                                     // "web-*" is more specific than "*"
		{vm("db-1", "pool:other", nil), "catch-all"},                                      // name catch-all
		{ObservedResource{Kind: KindImage, Name: "web-img", ExternalScope: "pool:x"}, ""}, // rules are for VMs only
	} {
		got, _ := o.projectOf(c.r)
		if got != c.want {
			t.Errorf("projectOf(%s, %q, %v) = %q, want %q", c.r.Name, c.r.ExternalScope, c.r.Spec["tags"], got, c.want)
		}
	}
	for _, bad := range []string{"", "  ", "tag:", "name:", "name:[a-"} {
		if ValidateMappingScope(bad) == nil {
			t.Errorf("scope %q accepted", bad)
		}
	}
	for _, ok := range []string{"pool:payment", "0a1b2c3d", "tag:prod", "name:web-*"} {
		if err := ValidateMappingScope(ok); err != nil {
			t.Errorf("scope %q: %v", ok, err)
		}
	}
}
