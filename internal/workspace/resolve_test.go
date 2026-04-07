package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveIn_FindExisting(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "my-app"), 0755)

	result, err := ResolveIn(root, "my-app", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "found" {
		t.Errorf("expected 'found', got %q", result.Status)
	}
}

func TestResolveIn_CreateNew(t *testing.T) {
	root := t.TempDir()

	result, err := ResolveIn(root, "new-project", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "created" {
		t.Errorf("expected 'created', got %q", result.Status)
	}
	if _, err := os.Stat(result.Dir); err != nil {
		t.Error("directory should exist")
	}
}

func TestResolveIn_FindClonedRepo(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "my-repo", ".git"), 0755)

	result, err := ResolveIn(root, "my-repo", "owner/my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "found" {
		t.Errorf("expected 'found', got %q", result.Status)
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"owner/repo", "https://github.com/owner/repo.git"},
		{"https://github.com/a/b.git", "https://github.com/a/b.git"},
		{"git@github.com:a/b.git", "git@github.com:a/b.git"},
	}
	for _, tt := range tests {
		got := normalizeRepoURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsGitRepo(t *testing.T) {
	root := t.TempDir()
	if isGitRepo(root) {
		t.Error("empty dir should not be a git repo")
	}
	os.MkdirAll(filepath.Join(root, ".git"), 0755)
	if !isGitRepo(root) {
		t.Error("dir with .git should be a git repo")
	}
}
