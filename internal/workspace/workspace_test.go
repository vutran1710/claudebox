package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// Quoting is not argv-safety. `git clone` was once built as a shell string
// with both arguments shell-quoted, which is shell-safe and still wrong:
// quoting makes a value one argv element, and that element can still be an
// option. `git clone --upload-pack=<cmd>` executes <cmd>.
//
// These moved here with the code. They used to live in internal/cbx, which no
// longer owns repo resolution — internal/api needed the same thing, and a
// second copy is how the earlier design shipped a command injection in one
// package that the others did not have.

func TestRepoURLRejectsWhatGitWouldReadAsAnOption(t *testing.T) {
	for _, bad := range []string{
		"--upload-pack=touch /tmp/pwned",
		"-u/tmp/x",
		"--help",
		"owner/repo; touch /tmp/pwned",
		"owner repo",
		"not-a-repo",
		"a/b/c",
		"",
	} {
		if got, err := RepoURL(bad); err == nil {
			t.Errorf("RepoURL(%q) = %q, want an error", bad, got)
		}
	}
}

func TestRepoURLAcceptsShorthandAndFullURLs(t *testing.T) {
	for in, want := range map[string]string{
		"owner/repo":                        "https://github.com/owner/repo.git",
		"my-org/my.repo_v2":                 "https://github.com/my-org/my.repo_v2.git",
		"https://github.com/o/r.git":        "https://github.com/o/r.git",
		"git@github.com:o/r.git":            "git@github.com:o/r.git",
		"https://gitlab.com/group/proj.git": "https://gitlab.com/group/proj.git",
	} {
		got, err := RepoURL(in)
		if err != nil {
			t.Errorf("RepoURL(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("RepoURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrepareRejectsAHostileRepoAndLeavesNothingBehind(t *testing.T) {
	canary := filepath.Join(t.TempDir(), "pwned")
	dir := filepath.Join(t.TempDir(), "proj")

	if err := Prepare(dir, "--upload-pack=touch "+canary); err == nil {
		t.Fatal("Prepare accepted a repo value git would read as an option")
	}
	if _, err := os.Stat(canary); err == nil {
		t.Fatal("the payload executed")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("a project directory was left behind after the failure")
	}
}

func TestPrepareCreatesAnEmptyDirectoryWithoutARepo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if err := Prepare(dir, ""); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestPrepareAcceptsAnExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := Prepare(dir, ""); err != nil {
		t.Errorf("Prepare on an existing directory: %v", err)
	}
}

func TestPrepareRefusesToCloneOverExistingWork(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(dir, "owner/repo"); err == nil {
		t.Error("cloned into a non-empty directory — that would bury someone's work")
	}
}
