package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectIn_Found(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "my-app"), 0755)

	result, err := ResolveProjectIn(root, "my-app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "found" {
		t.Errorf("expected 'found', got %q", result.Status)
	}
	if result.Dir != filepath.Join(root, "my-app") {
		t.Errorf("unexpected dir: %s", result.Dir)
	}
}

func TestResolveProjectIn_NotFound(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveProjectIn(root, "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveGitHubIn_AlreadyCloned(t *testing.T) {
	root := t.TempDir()
	// Create a fake git repo
	os.MkdirAll(filepath.Join(root, "my-repo", ".git"), 0755)

	result, err := ResolveGitHubIn(root, "owner/my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "found" {
		t.Errorf("expected 'found', got %q", result.Status)
	}
	if result.Dir != filepath.Join(root, "my-repo") {
		t.Errorf("unexpected dir: %s", result.Dir)
	}
}

func TestResolveGitHubIn_ExtractsRepoName(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "claudebox", ".git"), 0755)

	result, err := ResolveGitHubIn(root, "vutran1710/claudebox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(result.Dir) != "claudebox" {
		t.Errorf("expected dir name 'claudebox', got %q", filepath.Base(result.Dir))
	}
}

func TestIsGitRepo(t *testing.T) {
	root := t.TempDir()

	// Not a git repo
	if isGitRepo(root) {
		t.Error("empty dir should not be a git repo")
	}

	// Make it a git repo
	os.MkdirAll(filepath.Join(root, ".git"), 0755)
	if !isGitRepo(root) {
		t.Error("dir with .git should be a git repo")
	}
}
