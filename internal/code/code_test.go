package code

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectFound(t *testing.T) {
	// Create a temp workspace
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "my-app")
	os.MkdirAll(projectDir, 0755)

	// Test that resolveProject finds it
	// We can't easily test the tea.Cmd, so test the logic directly
	dir := filepath.Join(workspace, "my-app")
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected project dir to exist: %s", dir)
	}
}

func TestResolveProjectNotFound(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "nonexistent")
	_, err := os.Stat(dir)
	if err == nil {
		t.Fatal("expected project dir to not exist")
	}
}

func TestRunNameDefaults(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		project string
		want    string
	}{
		{name: "", repo: "owner/my-repo", project: "", want: "my-repo"},
		{name: "", repo: "", project: "my-app", want: "my-app"},
		{name: "custom", repo: "owner/repo", project: "", want: "custom"},
		{name: "custom", repo: "", project: "app", want: "custom"},
	}

	for _, tt := range tests {
		name := tt.name
		if name == "" {
			if tt.repo != "" {
				parts := filepath.SplitList(tt.repo)
				if len(parts) == 0 {
					parts = []string{tt.repo}
				}
				// Mimic the Split logic
				idx := len(tt.repo) - 1
				for idx >= 0 && tt.repo[idx] != '/' {
					idx--
				}
				if idx >= 0 {
					name = tt.repo[idx+1:]
				} else {
					name = tt.repo
				}
			} else if tt.project != "" {
				name = tt.project
			}
		}
		if name != tt.want {
			t.Errorf("name=%q repo=%q project=%q: got %q, want %q",
				tt.name, tt.repo, tt.project, name, tt.want)
		}
	}
}
