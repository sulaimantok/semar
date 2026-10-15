package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sulaimantok/semar/internal/auth"
	"github.com/sulaimantok/semar/internal/store"
	"github.com/sulaimantok/semar/internal/store/storetest"
)

func TestBootstrapAdminRunsOnceAcrossReplicas(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	var created atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := st.EnsureBootstrapAdmin(ctx, "admin", "$argon2id$hash")
			if err != nil {
				t.Error(err)
			}
			if ok {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("bootstrap admin created %d times, want 1", created.Load())
	}
	if ok, _ := st.EnsureBootstrapAdmin(ctx, "other", "$argon2id$hash"); ok {
		t.Fatal("bootstrap ran although a user exists")
	}
}

func TestSessionsResolvePrincipal(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if _, err := st.EnsureBootstrapAdmin(ctx, "admin", "h"); err != nil {
		t.Fatal(err)
	}
	admin, err := st.UserByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, st, provider("os-jkt", "fp"))
	if _, err := st.CreateProject(ctx, projectModel("payment")); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateRoleBinding(ctx, admin.ID, auth.RoleBinding{Role: auth.RoleViewer, ProjectID: "payment"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateRoleBinding(ctx, admin.ID, auth.RoleBinding{Role: "root"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid role: %v", err)
	}

	_, live, _ := auth.NewSessionToken()
	if err := st.CreateSession(ctx, live, admin.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, err := st.PrincipalForSession(ctx, live)
	if err != nil {
		t.Fatal(err)
	}
	if p.Username != "admin" || !p.IsGlobalAdmin() || len(p.Bindings) != 2 || p.Bindings[1].ProjectID != "payment" {
		t.Fatalf("principal = %+v", p)
	}

	_, expired, _ := auth.NewSessionToken()
	if err := st.CreateSession(ctx, expired, admin.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PrincipalForSession(ctx, expired); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired session: %v", err)
	}
	if n, err := st.DeleteExpiredSessions(ctx); err != nil || n != 1 {
		t.Fatalf("DeleteExpiredSessions = %d, %v", n, err)
	}

	if err := st.SetUserDisabled(ctx, admin.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PrincipalForSession(ctx, live); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("disabled user session: %v", err)
	}
	if err := st.SetUserDisabled(ctx, admin.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSession(ctx, live); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PrincipalForSession(ctx, live); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted session: %v", err)
	}
	if _, err := st.CreateUser(ctx, "admin", "h"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate username: %v", err)
	}
}

func TestAuditLogIsAppendOnlyAndChained(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := st.AppendAudit(ctx, store.AuditEvent{
			Actor: "admin", Action: "provider.create", Target: fmt.Sprintf("os-%d", i),
			Outcome: store.AuditSuccess, RequestID: "req", Details: map[string]string{"z": "1", "a": "2"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, err := st.VerifyAuditChain(ctx); err != nil || n != 5 {
		t.Fatalf("VerifyAuditChain = %d, %v", n, err)
	}

	// Exit criterion (roadmap §6 Phase 1): the application DB user cannot
	// rewrite history.
	for _, sql := range []string{
		`UPDATE audit_events SET actor = 'mallory' WHERE id = 2`,
		`DELETE FROM audit_events WHERE id = 3`,
		`TRUNCATE audit_events`,
	} {
		if err := st.ExecForTest(ctx, sql); err == nil {
			t.Errorf("%q succeeded on the append-only audit log", sql)
		}
	}
	if _, err := st.AppendAudit(ctx, store.AuditEvent{Actor: "a", Action: "x", Outcome: "maybe"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid outcome: %v", err)
	}
}

func TestAuditVerifyDetectsTampering(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := st.AppendAudit(ctx, store.AuditEvent{Actor: "admin", Action: "login", Outcome: store.AuditSuccess}); err != nil {
			t.Fatal(err)
		}
	}
	// Only a superuser can do this; production runs as a non-superuser role.
	if err := st.ExecForTest(ctx, `ALTER TABLE audit_events DISABLE TRIGGER audit_events_no_update_delete`); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(ctx, `UPDATE audit_events SET actor = 'mallory' WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	n, err := st.VerifyAuditChain(ctx)
	if !errors.Is(err, store.ErrAuditChainBroken) || n != 1 {
		t.Fatalf("VerifyAuditChain after tampering = %d, %v; want break at event 2", n, err)
	}
}

func TestConcurrentAuditAppendsKeepOneChain(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := st.AppendAudit(ctx, store.AuditEvent{Actor: "worker", Action: "sync", Target: fmt.Sprint(i), Outcome: store.AuditSuccess}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if n, err := st.VerifyAuditChain(ctx); err != nil || n != 20 {
		t.Fatalf("VerifyAuditChain = %d, %v", n, err)
	}
}

func TestResetPasswordEnablesAndEndsSessions(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if _, err := st.EnsureBootstrapAdmin(ctx, "admin", "old-hash"); err != nil {
		t.Fatal(err)
	}
	admin, _ := st.UserByUsername(ctx, "admin")
	if err := st.SetUserDisabled(ctx, admin.ID, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_, h, _ := auth.NewSessionToken()
		if err := st.CreateSession(ctx, h, admin.ID, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	ended, err := st.ResetPassword(ctx, "admin", "new-hash")
	if err != nil || ended != 2 {
		t.Fatalf("ResetPassword = %d, %v", ended, err)
	}
	u, _ := st.UserByUsername(ctx, "admin")
	if u.PasswordHash != "new-hash" || u.Disabled {
		t.Fatalf("user after reset = %+v", u)
	}
	if _, err := st.ResetPassword(ctx, "nobody", "h"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}
