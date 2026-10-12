package secret

import (
	"bytes"
	"errors"
	"testing"
)

func kek(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func mustKeyring(t *testing.T, current string, keks map[string][]byte) *Keyring {
	t.Helper()
	k, err := NewKeyring(current, keks)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	k := mustKeyring(t, "v1", map[string][]byte{"v1": kek(1)})
	aad := AAD("provider_credentials", "row-1", "discovery")
	plain := []byte(`{"application_credential_secret":"s3cr3t"}`)

	s, err := k.Seal(plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if s.KEKVersion != "v1" || bytes.Contains(s.Ciphertext, []byte("s3cr3t")) || bytes.Contains(s.WrappedDEK, []byte("s3cr3t")) {
		t.Fatalf("sealed secret leaks plaintext or wrong version: %+v", s)
	}
	got, err := k.Open(s, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("Open = %q, want %q", got, plain)
	}
}

func TestEachSealUsesFreshDEK(t *testing.T) {
	k := mustKeyring(t, "v1", map[string][]byte{"v1": kek(1)})
	aad := AAD("t", "r", "p")
	a, _ := k.Seal([]byte("same"), aad)
	b, _ := k.Seal([]byte("same"), aad)
	if bytes.Equal(a.WrappedDEK, b.WrappedDEK) || bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Fatal("two seals of the same plaintext produced identical output")
	}
}

func TestAADBindsSecretToRow(t *testing.T) {
	k := mustKeyring(t, "v1", map[string][]byte{"v1": kek(1)})
	s, err := k.Seal([]byte("x"), AAD("provider_credentials", "row-1", "discovery"))
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range [][]byte{
		AAD("provider_credentials", "row-2", "discovery"),
		AAD("provider_credentials", "row-1", "mutate"),
	} {
		if _, err := k.Open(s, other); err == nil {
			t.Errorf("opened with AAD %q", other)
		}
	}
}

func TestRotationRewrapsWithoutReencrypting(t *testing.T) {
	aad := AAD("t", "r", "p")
	old := mustKeyring(t, "v1", map[string][]byte{"v1": kek(1)})
	s, err := old.Seal([]byte("payload"), aad)
	if err != nil {
		t.Fatal(err)
	}

	rotated := mustKeyring(t, "v2", map[string][]byte{"v1": kek(1), "v2": kek(2)})
	if got, err := rotated.Open(s, aad); err != nil || string(got) != "payload" {
		t.Fatalf("open with previous KEK: %q, %v", got, err)
	}
	r, changed, err := rotated.Rewrap(s, aad)
	if err != nil || !changed {
		t.Fatalf("Rewrap = %v, %v", changed, err)
	}
	if r.KEKVersion != "v2" || !bytes.Equal(r.Ciphertext, s.Ciphertext) {
		t.Fatalf("rewrap changed data or version: %+v", r)
	}
	if _, changed, _ := rotated.Rewrap(r, aad); changed {
		t.Fatal("rewrap of a current secret reported a change")
	}

	onlyNew := mustKeyring(t, "v2", map[string][]byte{"v2": kek(2)})
	if got, err := onlyNew.Open(r, aad); err != nil || string(got) != "payload" {
		t.Fatalf("open rewrapped secret after dropping v1: %q, %v", got, err)
	}
	if _, err := onlyNew.Open(s, aad); !errors.Is(err, ErrUnknownKEK) {
		t.Fatalf("open v1 secret without v1 KEK: %v, want ErrUnknownKEK", err)
	}
}

func TestTamperingIsDetected(t *testing.T) {
	k := mustKeyring(t, "v1", map[string][]byte{"v1": kek(1)})
	aad := AAD("t", "r", "p")
	s, _ := k.Seal([]byte("x"), aad)

	bad := s
	bad.Ciphertext = append([]byte(nil), s.Ciphertext...)
	bad.Ciphertext[len(bad.Ciphertext)-1] ^= 1
	if _, err := k.Open(bad, aad); err == nil {
		t.Error("tampered ciphertext opened")
	}

	bad = s
	bad.WrappedDEK = append([]byte(nil), s.WrappedDEK...)
	bad.WrappedDEK[0] ^= 1
	if _, err := k.Open(bad, aad); err == nil {
		t.Error("tampered wrapped DEK opened")
	}

	// Relabelling the KEK version must not allow unwrapping with another key.
	two := mustKeyring(t, "v1", map[string][]byte{"v1": kek(1), "v2": kek(1)})
	relabelled := s
	relabelled.KEKVersion = "v2"
	if _, err := two.Open(relabelled, aad); err == nil {
		t.Error("secret opened after relabelling its KEK version")
	}
}

func TestNewKeyringValidates(t *testing.T) {
	cases := map[string]struct {
		current string
		keks    map[string][]byte
	}{
		"missing current": {"v2", map[string][]byte{"v1": kek(1)}},
		"short key":       {"v1", map[string][]byte{"v1": []byte("short")}},
		"empty version":   {"v1", map[string][]byte{"v1": kek(1), "": kek(2)}},
	}
	for name, c := range cases {
		if _, err := NewKeyring(c.current, c.keks); err == nil {
			t.Errorf("%s: NewKeyring succeeded", name)
		}
	}
}
