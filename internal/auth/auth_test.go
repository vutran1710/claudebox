package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKey_Creates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.key")

	key := LoadOrCreateKey(path)
	if len(key) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("expected 64 char key, got %d", len(key))
	}

	// File should exist
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if string(data) != key {
		t.Error("file content doesn't match returned key")
	}
}

func TestLoadOrCreateKey_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.key")

	// Create first
	key1 := LoadOrCreateKey(path)
	// Load again — should be same
	key2 := LoadOrCreateKey(path)

	if key1 != key2 {
		t.Error("expected same key on second load")
	}
}

func TestLoadOrCreateKey_Random(t *testing.T) {
	dir := t.TempDir()
	key1 := LoadOrCreateKey(filepath.Join(dir, "k1"))
	key2 := LoadOrCreateKey(filepath.Join(dir, "k2"))

	if key1 == key2 {
		t.Error("keys should be different")
	}
}

func TestGetKey_NotExists(t *testing.T) {
	key := GetKey("/tmp/nonexistent-key-file-xyz")
	if key != "" {
		t.Errorf("expected empty, got %q", key)
	}
}

func TestGetKey_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.key")
	os.WriteFile(path, []byte("my-secret-key"), 0600)

	key := GetKey(path)
	if key != "my-secret-key" {
		t.Errorf("expected 'my-secret-key', got %q", key)
	}
}
