package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// SessionCookie is the name of the session cookie.
const SessionCookie = "semar_session"

// NewSessionToken returns a random token for the client and the hash the
// server stores. The token itself is never persisted.
func NewSessionToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashSessionToken(token), nil
}

// HashSessionToken returns the stored form of a session token.
func HashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
