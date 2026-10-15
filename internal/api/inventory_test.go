package api

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
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

func TestCapacityTotals(t *testing.T) {
	a := newInventoryAPI(t)
	var c gen.Capacity
	a.expect(a.call("GET", "/api/v1/capacity", ""), http.StatusOK, &c)
	want := gen.VMCapacity{Total: 4, Vcpus: 8, MemoryMiB: 0,
		ByStatus: []gen.CountByKey{{Key: "running", Count: 4}},
		ByArch:   []gen.CountByKey{{Key: "arm64", Count: 4}}}
	if !reflect.DeepEqual(c.Vms, want) {
		t.Fatalf("vms = %+v, want %+v", c.Vms, want)
	}
	if c.Hypervisors == nil || c.Hypervisors.Total != 1 || c.Hypervisors.ByArch[0] != (gen.CountByKey{Key: "arm64", Count: 1}) {
		t.Fatalf("hypervisors = %+v", c.Hypervisors)
	}

	a.expect(a.call("GET", "/api/v1/capacity?providerId=os-jkt", ""), http.StatusOK, &c)
	if c.Vms.Total != 1 || c.Hypervisors == nil || c.Hypervisors.Total != 0 {
		t.Fatalf("os-jkt capacity = %+v", c)
	}
	c = gen.Capacity{}
	a.expect(a.call("GET", "/api/v1/capacity?projectId=payment", ""), http.StatusOK, &c)
	if c.Vms.Total != 2 || c.Hypervisors != nil {
		t.Fatalf("payment capacity = %+v", c)
	}

	// Orphaned VMs are not counted.
	if _, err := a.st.ApplySnapshot(t.Context(), store.Snapshot{ProviderID: "os-jkt", ObservedAt: syncedAt.Add(time.Minute)}, 1); err != nil {
		t.Fatal(err)
	}
	a.expect(a.call("GET", "/api/v1/capacity?providerId=os-jkt", ""), http.StatusOK, &c)
	if c.Vms.Total != 0 || len(c.Vms.ByArch) != 0 {
		t.Fatalf("capacity after orphaning = %+v", c.Vms)
	}
}

func TestResourcesSiteAndArchFilters(t *testing.T) {
	a := newInventoryAPI(t)
	a.expect(a.call("POST", "/api/v1/sites", `{"id":"site-b","name":"Site B"}`), http.StatusCreated, nil)
	a.expect(a.call("POST", "/api/v1/providers", `{"id":"edge","type":"fake","name":"Edge","siteId":"site-b","endpoints":["https://edge.example.com"]}`), http.StatusCreated, nil)
	snap := store.Snapshot{ProviderID: "edge", ObservedAt: syncedAt, Resources: []store.ObservedResource{
		{Kind: store.KindVM, ExternalID: "e1", Name: "e1", Arch: "amd64", Status: "running", Spec: map[string]any{"vcpus": 1}},
		{Kind: store.KindHypervisor, ExternalID: "ehv", Name: "edge-hv", Arch: "amd64", Status: "up", Spec: map[string]any{"vcpus": 16}},
	}}
	if _, err := a.st.ApplySnapshot(t.Context(), snap, 3); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(a.resourceIDs(t, "kind=vm&siteId=site-b"), " "); got != "e1" {
		t.Fatalf("site filter = %q", got)
	}
	if got := strings.Join(a.resourceIDs(t, "kind=vm&arch=amd64"), " "); got != "e1" {
		t.Fatalf("arch filter = %q", got)
	}
	if got := strings.Join(a.resourceIDs(t, "kind=vm&siteId=unknown-site"), " "); got != "" {
		t.Fatalf("unknown site = %q", got)
	}
	a.expect(a.call("GET", "/api/v1/resources?kind=vm&arch=sparc", ""), http.StatusBadRequest, nil)

	var c gen.Capacity
	a.expect(a.call("GET", "/api/v1/capacity?siteId=site-b", ""), http.StatusOK, &c)
	if c.Vms.Total != 1 || c.Hypervisors == nil || c.Hypervisors.Total != 1 || c.Hypervisors.Vcpus != 16 {
		t.Fatalf("site capacity = %+v / %+v", c.Vms, c.Hypervisors)
	}
}

func TestGetResourceWithHistoryAndVisibility(t *testing.T) {
	a := newInventoryAPI(t)
	// A second sync renames p1, and a third drops u1 until it is orphaned.
	next := store.Snapshot{ProviderID: "pve-lab", ObservedAt: syncedAt.Add(time.Minute), Resources: []store.ObservedResource{
		{Kind: store.KindVM, ExternalID: "p1", Name: "p1-renamed", ExternalScope: "pool:payment", Arch: "arm64", Status: "running"},
		{Kind: store.KindVM, ExternalID: "s1", Name: "s1", ExternalScope: "pool:search", Arch: "arm64", Status: "running",
			Spec: map[string]any{"vcpus": 2, "ipAddresses": []string{"10.0.0.1"}}},
	}}
	if _, err := a.st.ApplySnapshot(t.Context(), next, 1); err != nil {
		t.Fatal(err)
	}

	var d gen.ResourceDetail
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/vm/p1", ""), http.StatusOK, &d)
	if d.Resource.Name != "p1-renamed" || d.Resource.ProjectId == nil || *d.Resource.ProjectId != "payment" {
		t.Fatalf("resource = %+v", d.Resource)
	}
	if len(d.Changes) != 2 || d.Changes[0].Change != gen.Updated || d.Changes[1].Change != gen.Created ||
		d.Changes[0].ProjectId == nil || *d.Changes[0].ProjectId != "payment" {
		t.Fatalf("changes = %+v, want updated then created", d.Changes)
	}

	// Orphaned resources stay reachable for admins and operators.
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/vm/u1", ""), http.StatusOK, &d)
	if d.Resource.OrphanedAt == nil || d.Changes[0].Change != gen.Orphaned {
		t.Fatalf("orphaned resource = %+v, changes %+v", d.Resource, d.Changes)
	}
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/vm/ghost", ""), http.StatusNotFound, nil)
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/router/p1", ""), http.StatusBadRequest, nil)

	// A project viewer sees their project's VM; others are indistinguishable
	// from missing ones.
	a.loginAs(t, "payviewer", auth.RoleBinding{Role: auth.RoleViewer, ProjectID: "payment"})
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/vm/p1", ""), http.StatusOK, nil)
	var p404 gen.Problem
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/vm/s1", ""), http.StatusNotFound, &p404)
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/hypervisor/hv1", ""), http.StatusNotFound, nil)
	var ghost gen.Problem
	a.expect(a.call("GET", "/api/v1/resources/pve-lab/vm/ghost", ""), http.StatusNotFound, &ghost)
	if *p404.Detail != *ghost.Detail {
		t.Fatalf("hidden and missing resources differ: %q vs %q", *p404.Detail, *ghost.Detail)
	}
}
