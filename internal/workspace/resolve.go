package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const DefaultRoot = "/workspace"

type ResolveResult struct {
	Dir    string `json:"dir"`
	Status string `json:"status"` // "cloned", "found", "created"
}

// ResolveGitHub finds or clones a GitHub repo in the workspace.
func ResolveGitHub(repo string) (*ResolveResult, error) {
	return ResolveGitHubIn(DefaultRoot, repo)
}

// ResolveGitHubIn finds or clones a GitHub repo in a given root.
func ResolveGitHubIn(root, repo string) (*ResolveResult, error) {
	repoName := filepath.Base(repo)
	dir := filepath.Join(root, repoName)

	// Already cloned?
	if isGitRepo(dir) {
		return &ResolveResult{Dir: dir, Status: "found"}, nil
	}

	// Clone
	url := fmt.Sprintf("https://github.com/%s.git", repo)
	_, err := shell.RunShellTimeout(2*time.Minute, fmt.Sprintf(`git clone %s %s`, url, dir))
	if err != nil {
		return nil, fmt.Errorf("clone failed: %w", err)
	}
	return &ResolveResult{Dir: dir, Status: "cloned"}, nil
}

// ResolveProject finds an existing project directory in the workspace.
func ResolveProject(name string) (*ResolveResult, error) {
	return ResolveProjectIn(DefaultRoot, name)
}

// ResolveProjectIn finds an existing project directory in a given root.
func ResolveProjectIn(root, name string) (*ResolveResult, error) {
	dir := filepath.Join(root, name)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("project not found: %s", dir)
	}
	return &ResolveResult{Dir: dir, Status: "found"}, nil
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}
