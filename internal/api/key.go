package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The API key: one door, three callers — POST /auth/rotate, `cbx api-key`, and
// cbx-setuptool over ssh. All of them come through here, because a second
// place that issues keys is a second answer to "what is the current key".

// KeyPrefix marks a cbx key in logs and pasted strings, so one is recognisable
// when it turns up somewhere it should not be.
const KeyPrefix = "cbx_live_"

// DefaultKeyPath is where the key lives. State, not config: it is generated
// data cbx can reissue, the same reasoning as sessions.db.
func DefaultKeyPath() string {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "cbx", "api-key")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "cbx", "api-key")
	}
	return filepath.Join(home, ".local", "state", "cbx", "api-key")
}

// NewKey mints a key. 32 bytes from crypto/rand, which is not guessable and
// not worth anybody's time to search.
func NewKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// LoadOrCreateKey returns the key at path, minting one if there is none.
func LoadOrCreateKey(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		if key := strings.TrimSpace(string(raw)); key != "" {
			return key, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read api key: %w", err)
	}
	return RotateKey(path)
}

// RotateKey writes a new key, replacing whatever was there. The old one stops
// working on the next request: there is no grace period, because the reason to
// rotate is usually that the old key should already have stopped working.
func RotateKey(path string) (string, error) {
	key, err := NewKey()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	// 0600 from the moment it exists. Writing then chmod-ing would leave a
	// window where it is world-readable, which is the whole thing this avoids.
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write api key: %w", err)
	}
	return key, nil
}

// presentedKey pulls the key out of a request's Authorization header.
func presentedKey(header string) string {
	const bearer = "Bearer "
	if len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		return strings.TrimSpace(header[len(bearer):])
	}
	return ""
}

// keyMatches compares in constant time.
//
// A byte-at-a-time comparison returns faster the earlier it finds a
// difference, which hands the key's prefix to anyone willing to time the
// responses — one byte at a time, until they have all of it.
func keyMatches(presented, actual string) bool {
	return subtle.ConstantTimeCompare([]byte(presented), []byte(actual)) == 1
}
