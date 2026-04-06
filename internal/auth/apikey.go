package auth

import (
	"crypto/rand"
	"encoding/hex"
	"os"
)

// LoadOrCreateKey loads an API key from a file, or generates and saves a new one.
func LoadOrCreateKey(path string) string {
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		return string(data)
	}
	b := make([]byte, 32)
	rand.Read(b)
	key := hex.EncodeToString(b)
	os.WriteFile(path, []byte(key), 0600)
	return key
}

// GetKey reads an API key from a file. Returns empty if not found.
func GetKey(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}
