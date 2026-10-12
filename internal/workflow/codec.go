// Package workflow holds SEMAR's Temporal workflows, activities, schedules and
// the payload codec. Conventions: roadmap §3.8, ADR-0002.
package workflow

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/proto"
)

const (
	metadataEncoding = "encoding"
	metadataKeyID    = "encryption-key-id"
	encodingAESGCM   = "binary/encrypted-aes256gcm"
)

// AESGCMCodec encrypts every Temporal payload so workflow inputs and results
// are never stored in plaintext in Temporal's database or shown in its UI.
//
// Payloads are encrypted with the current key; older keys are kept for
// decryption so keys can be rotated (ADR-0004). In production the keys come
// from SecretStore, never from workflow payloads.
type AESGCMCodec struct {
	currentID string
	aeads     map[string]cipher.AEAD
}

var _ converter.PayloadCodec = (*AESGCMCodec)(nil)

// NewAESGCMCodec builds a codec that encrypts with keys[currentID] and can
// decrypt with any key in keys. Every key must be 32 bytes (AES-256).
func NewAESGCMCodec(currentID string, keys map[string][]byte) (*AESGCMCodec, error) {
	if _, ok := keys[currentID]; !ok {
		return nil, fmt.Errorf("payload codec: current key %q not provided", currentID)
	}
	c := &AESGCMCodec{currentID: currentID, aeads: make(map[string]cipher.AEAD, len(keys))}
	for id, key := range keys {
		if len(key) != 32 {
			return nil, fmt.Errorf("payload codec: key %q must be 32 bytes, got %d", id, len(key))
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("payload codec: key %q: %w", id, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("payload codec: key %q: %w", id, err)
		}
		c.aeads[id] = aead
	}
	return c, nil
}

// Encode encrypts each payload with the current key.
func (c *AESGCMCodec) Encode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	aead := c.aeads[c.currentID]
	out := make([]*commonpb.Payload, len(payloads))
	for i, p := range payloads {
		plain, err := proto.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("payload codec: marshal: %w", err)
		}
		nonce := make([]byte, aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return nil, fmt.Errorf("payload codec: nonce: %w", err)
		}
		sealed := aead.Seal(nonce, nonce, plain, []byte(c.currentID))
		out[i] = &commonpb.Payload{
			Metadata: map[string][]byte{
				metadataEncoding: []byte(encodingAESGCM),
				metadataKeyID:    []byte(c.currentID),
			},
			Data: sealed,
		}
	}
	return out, nil
}

// Decode decrypts payloads produced by Encode. Payloads that are not
// encrypted by this codec are returned unchanged.
func (c *AESGCMCodec) Decode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	out := make([]*commonpb.Payload, len(payloads))
	for i, p := range payloads {
		if string(p.GetMetadata()[metadataEncoding]) != encodingAESGCM {
			out[i] = p
			continue
		}
		keyID := string(p.GetMetadata()[metadataKeyID])
		aead, ok := c.aeads[keyID]
		if !ok {
			return nil, fmt.Errorf("payload codec: unknown key %q", keyID)
		}
		data := p.GetData()
		if len(data) < aead.NonceSize() {
			return nil, errors.New("payload codec: ciphertext too short")
		}
		plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(keyID))
		if err != nil {
			return nil, fmt.Errorf("payload codec: decrypt with key %q: %w", keyID, err)
		}
		var decoded commonpb.Payload
		if err := proto.Unmarshal(plain, &decoded); err != nil {
			return nil, fmt.Errorf("payload codec: unmarshal: %w", err)
		}
		out[i] = &decoded
	}
	return out, nil
}

// NewDataConverter wraps Temporal's default converter with codec.
func NewDataConverter(codec converter.PayloadCodec) converter.DataConverter {
	return converter.NewCodecDataConverter(converter.GetDefaultDataConverter(), codec)
}
