package setuptool

import (
	"os"
	"path/filepath"
	"testing"
)

// What travels is decided by two things: the directory a caller points at, and
// the filter naming what inside it is worth shipping. Neither is fixed.
//
// These assert the selection, which is pure. Uploading needs a box.

func claudeDir(t *testing.T, entries map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range entries {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// counts what Plan selected, walking each directory the way the upload does.
func counts(t *testing.T, dir string, only []string) map[string]int {
	t.Helper()
	plan, _, err := Plan(dir, only)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got := map[string]int{}
	for _, e := range plan {
		if !e.IsDir {
			got[e.Rel] = 1
			continue
		}
		n := 0
		filepath.Walk(e.Local, func(p string, i os.FileInfo, err error) error {
			if err != nil || i.IsDir() {
				return err
			}
			if target, err := os.Stat(p); err == nil && target.Mode().IsRegular() {
				n++
			}
			return nil
		})
		got[e.Rel] = n
	}
	return got
}

func dropReasons(t *testing.T, dir string, only []string) map[string]string {
	t.Helper()
	_, dropped, err := Plan(dir, only)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got := map[string]string{}
	for _, d := range dropped {
		got[d.Path] = d.Reason
	}
	return got
}

func TestTheDefaultFilterCarriesWhatShapesASession(t *testing.T) {
	// rules/ was absent from an earlier default, so a box got skills but none
	// of the instructions that govern how they are used.
	for _, want := range []string{"skills", "agents", "rules", "settings.json"} {
		found := false
		for _, have := range DefaultFilter {
			if have == want {
				found = true
			}
		}
		if !found {
			t.Errorf("DefaultFilter does not carry %q", want)
		}
	}
}

func TestTheDefaultFilterLeavesOutWhatIsLargeOrMeaningless(t *testing.T) {
	// Caches and transcripts are either enormous or about this machine only.
	// Plugin bundles travel as their manifest instead.
	for _, unwanted := range []string{"projects", "cache", "history.jsonl", "shell-snapshots", "plugins"} {
		for _, have := range DefaultFilter {
			if have == unwanted {
				t.Errorf("DefaultFilter carries %q", unwanted)
			}
		}
	}
}

func TestAFilterCopiesOnlyWhatItNames(t *testing.T) {
	dir := claudeDir(t, map[string]string{
		"skills/a/SKILL.md": "a",
		"rules/one.md":      "1",
		"agents/x.md":       "x",
		"projects/huge.log": "noise",
	})
	got := counts(t, dir, []string{"skills", "rules"})
	if _, ok := got["agents"]; ok {
		t.Error("copied something the filter did not name")
	}
	if _, ok := got["projects"]; ok {
		t.Error("copied a directory the filter did not name")
	}
	if got["skills"] != 1 || got["rules"] != 1 {
		t.Errorf("counts = %v", got)
	}
}

func TestAnAbsentFilterEntryIsReportedNotInvented(t *testing.T) {
	dir := claudeDir(t, map[string]string{"skills/a/SKILL.md": "a"})
	got := dropReasons(t, dir, []string{"skills", "does-not-exist"})
	if _, ok := got["does-not-exist"]; !ok {
		t.Errorf("an absent filter entry was silently ignored: %v", got)
	}
}

func TestSymlinkedEntriesAreCopiedAsTheirTarget(t *testing.T) {
	// The case that made this worth changing: a configuration directory whose
	// entries link into a dotfiles repository arrived empty and was reported
	// as copied.
	real := claudeDir(t, map[string]string{"one.md": "1", "two.md": "2"})
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.md", "two.md"} {
		if err := os.Symlink(filepath.Join(real, name), filepath.Join(dir, "rules", name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	if got := counts(t, dir, []string{"rules"}); got["rules"] != 2 {
		t.Errorf("counted %d files, want 2 — symlinked config did not travel", got["rules"])
	}
}

func TestAFilterCannotEscapeTheDirectory(t *testing.T) {
	dir := claudeDir(t, map[string]string{"skills/a/SKILL.md": "a"})
	for _, escape := range []string{"../../etc/passwd", "..", "skills/../../outside"} {
		got := dropReasons(t, dir, []string{escape})
		if _, ok := got[escape]; !ok {
			t.Errorf("filter entry %q was not refused: %v", escape, got)
		}
	}
}

func TestPlanRefusesADirectoryThatIsNotThere(t *testing.T) {
	if _, _, err := Plan(filepath.Join(t.TempDir(), "nope"), nil); err == nil {
		t.Error("planning from a missing directory reported success")
	}
}

func TestPlanRefusesAFileWhereADirectoryWasExpected(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Plan(f, nil); err == nil {
		t.Error("planning from a file reported success")
	}
}

func TestAnEmptyFilterUsesTheDefault(t *testing.T) {
	dir := claudeDir(t, map[string]string{
		"skills/a/SKILL.md": "a", "rules/one.md": "1", "projects/huge.log": "noise",
	})
	got := counts(t, dir, nil)
	if got["skills"] != 1 || got["rules"] != 1 {
		t.Errorf("default filter missed something: %v", got)
	}
	if _, ok := got["projects"]; ok {
		t.Error("the default filter carried projects/")
	}
}
