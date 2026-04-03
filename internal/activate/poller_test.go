package activate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureChromeMCP(t *testing.T) {
	// Create temp home dir
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")

	// Run configure
	err := ConfigureChromeMCP()
	if err != nil {
		t.Fatalf("ConfigureChromeMCP failed: %v", err)
	}

	// Check config file was written
	configPath := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	if !strings.Contains(string(data), "chrome-lite-mcp") {
		t.Error("config should contain chrome-lite-mcp")
	}
	if !strings.Contains(string(data), "node") {
		t.Error("config should contain node command")
	}
}

func TestConfigureChromeMCPIdempotent(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")

	// Run twice
	ConfigureChromeMCP()
	ConfigureChromeMCP()

	configPath := filepath.Join(home, ".claude.json")
	data, _ := os.ReadFile(configPath)

	// Should only have one instance
	count := strings.Count(string(data), "chrome-lite-mcp")
	if count != 1 {
		t.Errorf("expected 1 occurrence of chrome-lite-mcp, got %d", count)
	}
}

func TestIsChromeMCPConfigured(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")

	// Not configured yet
	if IsChromeMCPConfigured() {
		t.Error("should not be configured yet")
	}

	// Configure
	ConfigureChromeMCP()

	// Now configured
	if !IsChromeMCPConfigured() {
		t.Error("should be configured after ConfigureChromeMCP")
	}
}
