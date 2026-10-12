package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// fast keeps tests quick; production uses DefaultArgon2.
var fast = Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}

func TestPasswordHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery", fast)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=1024,t=1,p=1$") || strings.Contains(h, "horse") {
		t.Fatalf("unexpected hash format: %s", h)
	}
	for pw, want := range map[string]bool{"correct horse battery": true, "correct horse batterY": false, "": false} {
		ok, err := VerifyPassword(h, pw)
		if err != nil || ok != want {
			t.Errorf("VerifyPassword(%q) = %v, %v; want %v", pw, ok, err, want)
		}
	}
	other, _ := HashPassword("correct horse battery", fast)
	if other == h {
		t.Fatal("same password produced the same hash (salt not random)")
	}
}

func TestDefaultParamsVerify(t *testing.T) {
	h, err := HashPassword("a-long-enough-password", DefaultArgon2)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword(h, "a-long-enough-password"); !ok || err != nil {
		t.Fatalf("verify with default params: %v, %v", ok, err)
	}
}

func TestPasswordRules(t *testing.T) {
	if _, err := HashPassword("short", fast); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("short password: %v", err)
	}
	for _, bad := range []string{"", "plain", "$bcrypt$x$y$z$w", "$argon2id$v=18$m=1,t=1,p=1$AA$AA"} {
		if _, err := VerifyPassword(bad, "whatever-password"); err == nil {
			t.Errorf("VerifyPassword accepted hash %q", bad)
		}
	}
}

func TestSessionToken(t *testing.T) {
	tok, hash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 40 || !bytes.Equal(hash, HashSessionToken(tok)) || bytes.Contains(hash, []byte(tok)) {
		t.Fatalf("token %q / hash %x", tok, hash)
	}
	tok2, _, _ := NewSessionToken()
	if tok == tok2 {
		t.Fatal("tokens repeat")
	}
}

func TestPrincipal(t *testing.T) {
	var nilP *Principal
	if nilP.IsGlobalAdmin() {
		t.Fatal("nil principal is admin")
	}
	scoped := &Principal{Bindings: []RoleBinding{{Role: RoleAdmin, ProjectID: "payment"}}}
	if scoped.IsGlobalAdmin() {
		t.Fatal("project-scoped admin treated as global admin")
	}
	global := &Principal{Username: "admin", Bindings: []RoleBinding{{Role: RoleViewer}, {Role: RoleAdmin}}}
	if !global.IsGlobalAdmin() {
		t.Fatal("global admin not recognised")
	}
	ctx := WithPrincipal(context.Background(), global)
	if PrincipalFrom(ctx) != global || PrincipalFrom(context.Background()) != nil {
		t.Fatal("principal context round trip failed")
	}
}
