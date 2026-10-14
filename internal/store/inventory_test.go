package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/model"
	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

var t0 = time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)

func vm(id, name, scope string, vcpus int) store.ObservedResource {
	return store.ObservedResource{Kind: store.KindVM, ExternalID: id, Name: name, ExternalScope: scope,
		Arch: "amd64", Status: "running", Spec: map[string]any{"vcpus": vcpus}}
}

func snapshot(at time.Time, rs ...store.ObservedResource) store.Snapshot {
	return store.Snapshot{ProviderID: "pve-lab", ObservedAt: at, Resources: rs}
}

func inventoryStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	mustCreate(t, st, provider("pve-lab", "fp-pve"))
	return st
}

func listVMs(t *testing.T, st *store.Store, f store.ResourceFilter) []store.Resource {
	t.Helper()
	f.Kind = store.KindVM
	rs, err := st.ListResources(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func changes(t *testing.T, st *store.Store, id string) []string {
	t.Helper()
	cs, err := st.ListResourceChanges(context.Background(), "pve-lab", store.KindVM, id)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Change)
	}
	return out
}

func TestApplySnapshotCreatesUpdatesAndRecordsHistory(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()

	res, err := st.ApplySnapshot(ctx, snapshot(t0, vm("100", "web", "", 2), vm("101", "db", "", 4)), 3)
	if err != nil || res.Created != 2 {
		t.Fatalf("first sync = %+v, %v", res, err)
	}
	res, err = st.ApplySnapshot(ctx, snapshot(t0.Add(time.Minute), vm("100", "web", "", 2), vm("101", "db", "", 8)), 3)
	if err != nil || res.Unchanged != 1 || res.Updated != 1 {
		t.Fatalf("second sync = %+v, %v", res, err)
	}
	if got := changes(t, st, "101"); len(got) != 2 || got[1] != "updated" {
		t.Fatalf("history of 101 = %v", got)
	}
	if got := changes(t, st, "100"); len(got) != 1 {
		t.Fatalf("unchanged resource got history entries: %v", got)
	}
	vms := listVMs(t, st, store.ResourceFilter{})
	if len(vms) != 2 || vms[1].Spec["vcpus"].(float64) != 8 || !vms[1].ObservedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("cached VMs = %+v", vms)
	}
	status, err := st.GetSyncStatus(ctx, "pve-lab")
	if err != nil || status.Status != store.SyncHealthy || !status.LastObservedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("sync status = %+v, %v", status, err)
	}
}

func TestApplySnapshotRejectsStaleSnapshots(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	if _, err := st.ApplySnapshot(ctx, snapshot(t0, vm("100", "new", "", 2)), 3); err != nil {
		t.Fatal(err)
	}
	// A retried activity finishing late must not overwrite newer data.
	for _, at := range []time.Time{t0, t0.Add(-time.Minute)} {
		if _, err := st.ApplySnapshot(ctx, snapshot(at, vm("100", "old", "", 1)), 3); !errors.Is(err, store.ErrStaleSnapshot) {
			t.Fatalf("snapshot at %s: %v, want ErrStaleSnapshot", at, err)
		}
	}
	if vms := listVMs(t, st, store.ResourceFilter{}); vms[0].Name != "new" {
		t.Fatalf("stale snapshot overwrote data: %+v", vms[0])
	}
}

func TestApplySnapshotOrphansAfterRepeatedMissesAndRestores(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	at := t0
	apply := func(rs ...store.ObservedResource) store.SyncResult {
		t.Helper()
		at = at.Add(time.Minute)
		res, err := st.ApplySnapshot(ctx, snapshot(at, rs...), 3)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	apply(vm("100", "web", "", 2), vm("101", "db", "", 4))
	if r := apply(vm("100", "web", "", 2)); r.Missed != 1 || r.Orphaned != 0 {
		t.Fatalf("1st miss = %+v", r)
	}
	apply(vm("100", "web", "", 2))
	if r := apply(vm("100", "web", "", 2)); r.Orphaned != 1 {
		t.Fatalf("3rd miss = %+v, want orphaned", r)
	}
	if vms := listVMs(t, st, store.ResourceFilter{}); len(vms) != 1 {
		t.Fatalf("orphaned VM still listed: %+v", vms)
	}
	if vms := listVMs(t, st, store.ResourceFilter{IncludeOrphaned: true}); len(vms) != 2 || vms[1].OrphanedAt == nil {
		t.Fatalf("orphaned VM not kept: %+v", vms)
	}
	if r := apply(vm("100", "web", "", 2), vm("101", "db", "", 4)); r.Restored != 1 {
		t.Fatalf("reappearing VM = %+v, want restored", r)
	}
	if got := changes(t, st, "101"); len(got) != 3 || got[1] != "orphaned" || got[2] != "restored" {
		t.Fatalf("history of 101 = %v", got)
	}
}

func TestApplySnapshotDerivesProjectsFromMappings(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	if _, err := st.CreateProject(ctx, model.Project{ID: "payment", Name: "Payment"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApplySnapshot(ctx, snapshot(t0, vm("100", "web", "pool:payment", 2), vm("101", "x", "pool:other", 2)), 3); err != nil {
		t.Fatal(err)
	}
	if vms := listVMs(t, st, store.ResourceFilter{}); vms[0].ProjectID != "" {
		t.Fatalf("project assigned before mapping: %+v", vms[0])
	}
	if _, err := st.CreateProjectMapping(ctx, model.ProjectMapping{ProjectID: "payment", ProviderID: "pve-lab", ExternalScope: "pool:payment"}); err != nil {
		t.Fatal(err)
	}
	res, err := st.ApplySnapshot(ctx, snapshot(t0.Add(time.Minute), vm("100", "web", "pool:payment", 2), vm("101", "x", "pool:other", 2)), 3)
	if err != nil || res.Updated != 1 {
		t.Fatalf("sync after mapping = %+v, %v (ownership change must be recorded)", res, err)
	}
	payment := listVMs(t, st, store.ResourceFilter{ProjectIDs: []string{"payment"}})
	if len(payment) != 1 || payment[0].ExternalID != "100" {
		t.Fatalf("payment VMs = %+v", payment)
	}
	if none := listVMs(t, st, store.ResourceFilter{ProjectIDs: []string{}}); len(none) != 0 {
		t.Fatalf("empty project scope returned %d VMs, want none", len(none))
	}
}

func TestApplySnapshotRespectsLeases(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	if _, err := st.ApplySnapshot(ctx, snapshot(t0, vm("100", "web", "", 2)), 1); err != nil {
		t.Fatal(err)
	}
	if err := st.SetResourceLease(ctx, "pve-lab", store.KindVM, "100", "wf-resize-1"); err != nil {
		t.Fatal(err)
	}
	// Neither a changed spec nor its absence may touch a leased resource.
	if r, err := st.ApplySnapshot(ctx, snapshot(t0.Add(time.Minute), vm("100", "web", "", 16)), 1); err != nil || r.Leased != 1 {
		t.Fatalf("leased update = %+v, %v", r, err)
	}
	if r, err := st.ApplySnapshot(ctx, snapshot(t0.Add(2*time.Minute)), 1); err != nil || r.Orphaned != 0 {
		t.Fatalf("leased miss = %+v, %v", r, err)
	}
	if vms := listVMs(t, st, store.ResourceFilter{}); len(vms) != 1 || vms[0].Spec["vcpus"].(float64) != 2 {
		t.Fatalf("leased VM was modified: %+v", vms)
	}
	if err := st.SetResourceLease(ctx, "pve-lab", store.KindVM, "100", ""); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.ApplySnapshot(ctx, snapshot(t0.Add(3*time.Minute), vm("100", "web", "", 16)), 1); r.Updated != 1 {
		t.Fatalf("after lease release = %+v", r)
	}
}

func TestApplySnapshotValidation(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	cases := map[string]store.Snapshot{
		"duplicate":   snapshot(t0, vm("1", "a", "", 1), vm("1", "b", "", 1)),
		"bad kind":    snapshot(t0, store.ObservedResource{Kind: "router", ExternalID: "1"}),
		"empty id":    snapshot(t0, store.ObservedResource{Kind: store.KindVM}),
		"no observed": {ProviderID: "pve-lab"},
	}
	for name, snap := range cases {
		if _, err := st.ApplySnapshot(ctx, snap, 3); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if _, err := st.ApplySnapshot(ctx, store.Snapshot{ProviderID: "ghost", ObservedAt: t0}, 3); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown provider: %v, want ErrNotFound", err)
	}
}

func TestSyncFailuresDegradeThenUnreachable(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	for i, want := range []store.SyncHealth{store.SyncDegraded, store.SyncDegraded, store.SyncUnreachable} {
		got, err := st.RecordSyncFailure(ctx, "pve-lab", "dial tcp: timeout")
		if err != nil || got != want {
			t.Fatalf("failure %d = %s, %v; want %s", i+1, got, err, want)
		}
	}
	if _, err := st.ApplySnapshot(ctx, snapshot(t0), 3); err != nil {
		t.Fatal(err)
	}
	s, err := st.GetSyncStatus(ctx, "pve-lab")
	if err != nil || s.Status != store.SyncHealthy || s.ConsecutiveFailures != 0 || s.LastError != "" {
		t.Fatalf("after recovery = %+v, %v", s, err)
	}
}

func TestListResourcesPaginates(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	var rs []store.ObservedResource
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		rs = append(rs, vm(id, id, "", 1))
	}
	if _, err := st.ApplySnapshot(ctx, snapshot(t0, rs...), 3); err != nil {
		t.Fatal(err)
	}
	var got []string
	after := ""
	for {
		page := listVMs(t, st, store.ResourceFilter{Limit: 2, AfterProvider: "pve-lab", AfterExternalID: after})
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			got = append(got, r.ExternalID)
		}
		after = page[len(page)-1].ExternalID
	}
	if len(got) != 5 || got[0] != "a" || got[4] != "e" {
		t.Fatalf("paged IDs = %v", got)
	}
}

func TestDeletedProviderCannotSync(t *testing.T) {
	st := inventoryStore(t)
	ctx := context.Background()
	if err := st.DeleteProvider(ctx, "pve-lab"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApplySnapshot(ctx, snapshot(t0, vm("1", "a", "", 1)), 3); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("sync of deleted provider: %v, want ErrNotFound", err)
	}
}
