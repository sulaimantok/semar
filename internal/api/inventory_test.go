package api

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
)

var syncedAt = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// newInventoryAPI registers two providers and two projects and syncs:
// pve-lab: p1 (payment), s1 (search), u1 (unassigned), hypervisor hv1
// os-jkt:  p2 (payment)
func newInventoryAPI(t *testing.T) *settingsAPI {
	t.Helper()
	a := newSettingsAPI(t)
	ctx := context.Background()
	for _, body := range []string{
		`{"id":"pve-lab","type":"proxmox","name":"PVE","endpoints":["https://pve1.lab:8006"]}`,
		`{"id":"os-jkt","type":"openstack","name":"OS","endpoints":["https://keystone.jkt:5000/v3"],"regionName":"RegionOne"}`,
	} {
		a.expect(a.call("POST", "/api/v1/providers", body), http.StatusCreated, nil)
	}
	for _, p := range []string{"payment", "search"} {
		a.expect(a.call("POST", "/api/v1/projects", `{"id":"`+p+`","name":"`+p+`"}`), http.StatusCreated, nil)
	}
	for _, m := range []model.ProjectMapping{
		{ProjectID: "payment", ProviderID: "pve-lab", ExternalScope: "pool:payment"},
		{ProjectID: "search", ProviderID: "pve-lab", ExternalScope: "pool:search"},
		{ProjectID: "payment", ProviderID: "os-jkt", ExternalScope: "proj-pay"},
	} {
		if _, err := a.st.CreateProjectMapping(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	vm := func(id, scope string) store.ObservedResource {
		return store.ObservedResource{Kind: store.KindVM, ExternalID: id, Name: id, ExternalScope: scope, Arch: "arm64",
			Status: "running", Spec: map[string]any{"vcpus": 2, "ipAddresses": []string{"10.0.0.1"}}}
	}
	pve := store.Snapshot{ProviderID: "pve-lab", ObservedAt: syncedAt, Resources: []store.ObservedResource{
		vm("p1", "pool:payment"), vm("s1", "pool:search"), vm("u1", ""),
		{Kind: store.KindHypervisor, ExternalID: "hv1", Name: "pve1", Arch: "arm64", Status: "up"},
	}}
	osSnap := store.Snapshot{ProviderID: "os-jkt", ObservedAt: syncedAt, Resources: []store.ObservedResource{vm("p2", "proj-pay")}}
	for _, snap := range []store.Snapshot{pve, osSnap} {
		if _, err := a.st.ApplySnapshot(ctx, snap, 3); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

// loginAs creates a user with the given bindings and switches the session.
func (a *settingsAPI) loginAs(t *testing.T, username string, bindings ...auth.RoleBinding) {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(username+"-password-1", fastArgon2)
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.st.CreateUser(ctx, username, hash)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bindings {
		if err := a.st.CreateRoleBinding(ctx, u.ID, b); err != nil {
			t.Fatal(err)
		}
	}
	a.cookie = nil
	a.expect(a.login(username, username+"-password-1"), http.StatusOK, nil)
}

func (a *settingsAPI) resourceIDs(t *testing.T, query string) []string {
	t.Helper()
	var list gen.ResourceList
	a.expect(a.call("GET", "/api/v1/resources?"+query, ""), http.StatusOK, &list)
	ids := []string{}
	for _, r := range list.Items {
		ids = append(ids, r.ExternalId)
	}
	slices.Sort(ids)
	return ids
}

func TestResourcesAreScopedByRoleBindings(t *testing.T) {
	a := newInventoryAPI(t)
	cases := []struct {
		user     string
		bindings []auth.RoleBinding
		vms, hvs string
	}{
		{"op", []auth.RoleBinding{{Role: auth.RoleOperator}}, "p1 p2 s1 u1", "hv1"},
		{"globalviewer", []auth.RoleBinding{{Role: auth.RoleViewer}}, "p1 p2 s1", "hv1"},
		{"payviewer", []auth.RoleBinding{{Role: auth.RoleViewer, ProjectID: "payment"}}, "p1 p2", ""},
		{"pvelead", []auth.RoleBinding{{Role: auth.RoleOperator, ProviderID: "pve-lab"}}, "p1 s1", ""},
		{"nobody", nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.user, func(t *testing.T) {
			a.t = t
			a.loginAs(t, c.user, c.bindings...)
			if got := strings.Join(a.resourceIDs(t, "kind=vm"), " "); got != c.vms {
				t.Errorf("VMs = %q, want %q", got, c.vms)
			}
			if got := strings.Join(a.resourceIDs(t, "kind=hypervisor"), " "); got != c.hvs {
				t.Errorf("hypervisors = %q, want %q", got, c.hvs)
			}
		})
	}
}

func TestResourcesFiltersAndShape(t *testing.T) {
	a := newInventoryAPI(t)
	if got := strings.Join(a.resourceIDs(t, "kind=vm&providerId=os-jkt"), " "); got != "p2" {
		t.Fatalf("provider filter = %q", got)
	}
	if got := strings.Join(a.resourceIDs(t, "kind=vm&projectId=payment"), " "); got != "p1 p2" {
		t.Fatalf("project filter = %q", got)
	}
	var list gen.ResourceList
	a.expect(a.call("GET", "/api/v1/resources?kind=vm&providerId=os-jkt", ""), http.StatusOK, &list)
	r := list.Items[0]
	if r.ProjectId == nil || *r.ProjectId != "payment" || r.Arch != "arm64" || r.Spec["vcpus"] != float64(2) ||
		!r.ObservedAt.Equal(syncedAt) || r.OrphanedAt != nil {
		t.Fatalf("resource = %+v", r)
	}
}

func TestResourcesPagination(t *testing.T) {
	a := newInventoryAPI(t)
	var all []string
	query := "kind=vm&limit=1"
	for pages := 0; pages < 10; pages++ {
		var list gen.ResourceList
		a.expect(a.call("GET", "/api/v1/resources?"+query, ""), http.StatusOK, &list)
		for _, r := range list.Items {
			all = append(all, r.ProviderId+"/"+r.ExternalId)
		}
		if list.NextCursor == nil {
			break
		}
		query = "kind=vm&limit=1&cursor=" + url.QueryEscape(*list.NextCursor)
	}
	want := []string{"os-jkt/p2", "pve-lab/p1", "pve-lab/s1", "pve-lab/u1"}
	if !slices.Equal(all, want) {
		t.Fatalf("paged = %v, want %v", all, want)
	}
}

func TestResourcesValidation(t *testing.T) {
	a := newInventoryAPI(t)
	for _, q := range []string{"kind=router", "", "kind=vm&limit=0", "kind=vm&limit=501", "kind=vm&cursor=not-a-cursor"} {
		a.expect(a.call("GET", "/api/v1/resources?"+q, ""), http.StatusBadRequest, nil)
	}
	a.cookie = nil
	a.expect(a.call("GET", "/api/v1/resources?kind=vm", ""), http.StatusUnauthorized, nil)
}

func TestProviderSyncStatus(t *testing.T) {
	a := newInventoryAPI(t)
	var st gen.SyncStatus
	a.expect(a.call("GET", "/api/v1/providers/pve-lab/sync-status", ""), http.StatusOK, &st)
	if st.Status != gen.Healthy || st.LastObservedAt == nil || !st.LastObservedAt.Equal(syncedAt) {
		t.Fatalf("sync status = %+v", st)
	}
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"fresh","type":"fake","name":"Fresh","endpoints":["https://fresh.lab"]}`), http.StatusCreated, nil)
	a.expect(a.call("GET", "/api/v1/providers/fresh/sync-status", ""), http.StatusNotFound, nil)
	a.expect(a.call("GET", "/api/v1/providers/ghost/sync-status", ""), http.StatusNotFound, nil)
}
