package workflow

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeSchedules struct {
	ids []string
	// elsewhere holds schedules whose task queue belongs to another site.
	elsewhere []string
	ensured   []string
	removed   []string
	failFor   string
	listError error
}

func (f *fakeSchedules) ensure(_ context.Context, p string) error {
	if p == f.failFor {
		return errors.New("boom")
	}
	f.ensured = append(f.ensured, p)
	if !slices.Contains(f.ids, SyncScheduleID(p)) {
		f.ids = append(f.ids, SyncScheduleID(p))
	}
	return nil
}

func (f *fakeSchedules) list(context.Context) ([]string, error) {
	return slices.Clone(f.ids), f.listError
}

func (f *fakeSchedules) servedHere(_ context.Context, id string) (bool, error) {
	return !slices.Contains(f.elsewhere, id), nil
}

func (f *fakeSchedules) remove(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	f.ids = slices.DeleteFunc(f.ids, func(s string) bool { return s == id })
	return nil
}

func TestReconcileCreatesMissingAndRemovesDeleted(t *testing.T) {
	ops := &fakeSchedules{ids: []string{"sync-a", "sync-gone", "sync-other-site", "unrelated"}}
	res, err := reconcile(context.Background(), ops,
		[]string{"a", "b"},                      // this site
		[]string{"a", "b", "other-site"}, false) // all sites
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Ensured, []string{"b"}) {
		t.Fatalf("ensured %v, want only the missing schedule", res.Ensured)
	}
	if !slices.Equal(res.Removed, []string{"sync-gone"}) {
		t.Fatalf("removed %v; other sites' and non-sync schedules must stay", res.Removed)
	}
}

func TestReconcileRewritesOnStart(t *testing.T) {
	ops := &fakeSchedules{ids: []string{"sync-a"}}
	res, err := reconcile(context.Background(), ops, []string{"a"}, []string{"a"}, true)
	if err != nil || !slices.Equal(res.Ensured, []string{"a"}) {
		t.Fatalf("rewrite on start = %+v, %v", res, err)
	}
}

func TestReconcileContinuesPastErrors(t *testing.T) {
	ops := &fakeSchedules{failFor: "a"}
	res, err := reconcile(context.Background(), ops, []string{"a", "b"}, []string{"a", "b"}, false)
	if err == nil || !slices.Equal(res.Ensured, []string{"b"}) {
		t.Fatalf("reconcile = %+v, %v; want b ensured and an error for a", res, err)
	}
	ops = &fakeSchedules{listError: errors.New("temporal down")}
	if _, err := reconcile(context.Background(), ops, []string{"a"}, []string{"a"}, false); err == nil {
		t.Fatal("list failure ignored")
	}
}

func TestReconcileMovesSchedulesOfProvidersThatChangedSite(t *testing.T) {
	// "moved" was served by another site's worker and now belongs here; its
	// schedule must be rewritten onto this site's task queue.
	ops := &fakeSchedules{ids: []string{"sync-stay", "sync-moved"}, elsewhere: []string{"sync-moved"}}
	res, err := reconcile(context.Background(), ops, []string{"stay", "moved"}, []string{"stay", "moved"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Ensured, []string{"moved"}) || len(res.Removed) != 0 {
		t.Fatalf("reconcile = %+v, want only the moved provider rewritten", res)
	}
}
