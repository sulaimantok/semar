package workflow

import (
	"bytes"
	"testing"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func mustCodec(t *testing.T, current string, keys map[string][]byte) *AESGCMCodec {
	t.Helper()
	c, err := NewAESGCMCodec(current, keys)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCodecRoundTripHidesPlaintext(t *testing.T) {
	c := mustCodec(t, "k1", map[string][]byte{"k1": key(1)})
	dc := NewDataConverter(c)

	in := SyncInput{ProviderID: "prov-os-jkt-secret-marker"}
	p, err := dc.ToPayload(in)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(p.GetData(), []byte("secret-marker")) {
		t.Fatal("encoded payload contains plaintext")
	}
	if got := string(p.GetMetadata()[metadataKeyID]); got != "k1" {
		t.Fatalf("key id = %q, want k1", got)
	}

	var out SyncInput
	if err := dc.FromPayload(p, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}

func TestCodecRotationDecodesOldPayloads(t *testing.T) {
	old := NewDataConverter(mustCodec(t, "k1", map[string][]byte{"k1": key(1)}))
	p, err := old.ToPayload(SyncInput{ProviderID: "p"})
	if err != nil {
		t.Fatal(err)
	}

	rotated := NewDataConverter(mustCodec(t, "k2", map[string][]byte{"k1": key(1), "k2": key(2)}))
	var out SyncInput
	if err := rotated.FromPayload(p, &out); err != nil {
		t.Fatalf("decode payload written with previous key: %v", err)
	}

	withoutOld := NewDataConverter(mustCodec(t, "k2", map[string][]byte{"k2": key(2)}))
	if err := withoutOld.FromPayload(p, &out); err == nil {
		t.Fatal("decoded a payload whose key was dropped")
	}
}

func TestCodecRejectsTampering(t *testing.T) {
	c := mustCodec(t, "k1", map[string][]byte{"k1": key(1)})
	enc, err := c.Encode([]*commonpb.Payload{{Data: []byte("hello")}})
	if err != nil {
		t.Fatal(err)
	}
	enc[0].Data[len(enc[0].Data)-1] ^= 0xff
	if _, err := c.Decode(enc); err == nil {
		t.Fatal("tampered ciphertext decoded")
	}

	enc, _ = c.Encode([]*commonpb.Payload{{Data: []byte("hello")}})
	enc[0].Metadata[metadataKeyID] = []byte("k9")
	if _, err := c.Decode(enc); err == nil {
		t.Fatal("payload with unknown key id decoded")
	}
}

func TestCodecPassesThroughUnencrypted(t *testing.T) {
	c := mustCodec(t, "k1", map[string][]byte{"k1": key(1)})
	plain, err := converter.GetDefaultDataConverter().ToPayload("x")
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Decode([]*commonpb.Payload{plain})
	if err != nil || out[0] != plain {
		t.Fatalf("Decode(unencrypted) = %v, %v; want unchanged", out, err)
	}
}

func TestNewAESGCMCodecValidatesKeys(t *testing.T) {
	if _, err := NewAESGCMCodec("k1", map[string][]byte{"k2": key(2)}); err == nil {
		t.Error("accepted missing current key")
	}
	if _, err := NewAESGCMCodec("k1", map[string][]byte{"k1": []byte("short")}); err == nil {
		t.Error("accepted short key")
	}
}
