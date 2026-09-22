// Package workspace resolves where a session's files live.
//
// It is the only place that turns a session name and an optional repo into a
// directory on disk. That is deliberate: repo resolution was duplicated across
// three packages in an earlier design, and the copies drifted — one of them
// built `git clone` as a shell string and shipped a command injection the
// others did not have.
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Root is where project directories live. /workspace on a box that has one,
// otherwise ~/workspace — a laptop has no writable /workspace, and cbx must be
// runnable there.
func Root() string {
	if info, err := os.Stat("/workspace"); err == nil && info.IsDir() {
		return "/workspace"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/workspace"
	}
	return filepath.Join(home, "workspace")
}

// Prepare makes a project directory ready: cloned, existing, or new.
func Prepare(dir, repo string) error {
	if _, err := os.Stat(dir); err == nil {
		if repo != "" {
			// Refuse rather than clone over someone's work.
			if entries, _ := os.ReadDir(dir); len(entries) > 0 {
				return fmt.Errorf("%s already exists and is not empty — remove it or omit the repo", dir)
			}
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if repo == "" {
		return nil
	}
	url, err := RepoURL(repo)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	// exec.Command, not a shell string. Shell-quoting makes a value safe for
	// the shell but not for argv: git reads a leading dash as an option, and
	// `git clone --upload-pack=...` runs an arbitrary command. "--" ends
	// option parsing, and going through exec directly means there is no shell
	// to quote for in the first place.
	out, err := exec.Command("git", "clone", "--", url, dir).CombinedOutput()
	if err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("clone %s: %w: %s", repo, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RepoURL expands owner/repo shorthand and rejects anything git would read as
// an option rather than a repository.
func RepoURL(repo string) (string, error) {
	if strings.HasPrefix(repo, "-") {
		return "", fmt.Errorf("invalid repo %q: leading dash would be read as a git option", repo)
	}
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return repo, nil
	}
	// Shorthand must look like owner/repo and nothing else.
	if !shorthand.MatchString(repo) {
		return "", fmt.Errorf("invalid repo %q: expected owner/repo or a full git URL", repo)
	}
	return "https://github.com/" + repo + ".git", nil
}

var shorthand = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
