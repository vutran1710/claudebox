package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const DefaultRoot = "/workspace"

type ResolveResult struct {
	Dir    string `json:"dir"`
	Status string `json:"status"` // "cloned", "found", "created"
}

// Resolve finds or creates a project directory.
// If repo is provided, clones it. Otherwise finds or creates the dir with git init.
func Resolve(name, repo string) (*ResolveResult, error) {
	return ResolveIn(DefaultRoot, name, repo)
}

// ResolveIn finds or creates a project directory in a given root.
func ResolveIn(root, name, repo string) (*ResolveResult, error) {
	dir := filepath.Join(root, name)

	// If repo provided, find or clone
	if repo != "" {
		if isGitRepo(dir) {
			return &ResolveResult{Dir: dir, Status: "found"}, nil
		}
		url := normalizeRepoURL(repo)
		_, err := shell.RunShellTimeout(2*time.Minute, fmt.Sprintf(`git clone %s %s`, url, dir))
		if err != nil {
			return nil, fmt.Errorf("clone failed: %w", err)
		}
		return &ResolveResult{Dir: dir, Status: "cloned"}, nil
	}

	// No repo — find existing or create new
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return &ResolveResult{Dir: dir, Status: "found"}, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	shell.RunShellTimeout(10*time.Second, fmt.Sprintf(`cd %s && git init`, dir))
	return &ResolveResult{Dir: dir, Status: "created"}, nil
}

// normalizeRepoURL converts shorthand "owner/repo" to full URL.
// Passes through full URLs (https://, git@) as-is.
func normalizeRepoURL(repo string) string {
	if strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "git@") {
		return repo
	}
	return fmt.Sprintf("https://github.com/%s.git", repo)
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}
