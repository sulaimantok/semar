package config

import (
	"errors"
	"fmt"
	"strings"
)

// loadKeys reads a current 32-byte key from keyVar and optional previous
// keys from prevVar ("id=<base64>,id=<base64>"), returning them by ID. It is
// shared by the worker payload keys and the API's credential KEKs, which
// rotate the same way: add a new current key and keep old ones until
// nothing encrypted with them remains.
func loadKeys(getenv func(string) string, currentID, keyVar, prevVar string) (map[string][]byte, error) {
	if currentID == "" {
		return nil, fmt.Errorf("%s: key id must not be empty", keyVar)
	}
	key, err := decodeKey(getenv(keyVar))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keyVar, err)
	}
	keys := map[string][]byte{currentID: key}

	v := getenv(prevVar)
	if v == "" {
		return keys, nil
	}
	for _, pair := range strings.Split(v, ",") {
		id, b64, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok || id == "" {
			return nil, errors.New(prevVar + ": want id=base64[,id=base64]")
		}
		if _, dup := keys[id]; dup {
			return nil, fmt.Errorf("%s: duplicate key id %q", prevVar, id)
		}
		k, err := decodeKey(b64)
		if err != nil {
			return nil, fmt.Errorf("%s[%s]: %w", prevVar, id, err)
		}
		keys[id] = k
	}
	return keys, nil
}
