package cbx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vutran1710/claudebox/internal/store"
	"github.com/vutran1710/claudebox/internal/tmux"
)

// These drive real tmux and a real database. cbx has no remote dependency, so
// its behaviour can be verified in full on a laptop.

func app(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	if _, err := tmux.Shell("command -v tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	var out, errb bytes.Buffer
	return &App{
		Tmux:  tmux.New().WithCommand("sleep 300"),
		Store: st,
		Root:  t.TempDir(),
		Out:   &out, Err: &errb,
		// Nothing in these tests runs Claude, so no URL will ever appear.
		// Keep the wait short so the suite stays fast.
		Timeout: 1 * time.Second,
	}, &out, &errb
}

func name(t *testing.T) string {
	return fmt.Sprintf("cbxu-%d-%s", os.Getpid(), strings.ReplaceAll(t.Name(), "/", "-"))
}

func TestNewStartsASessionAndRecordsIt(t *testing.T) {
	a, out, _ := app(t)
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })

	if err := a.New(n, ""); err != nil {
		t.Fatalf("New: %v", err)
	}
	if !a.Tmux.Has(n) {
		t.Error("session is not running after New")
	}
	rec, _ := a.Store.Get(n)
	if rec == nil {
		t.Fatal("session was not recorded")
	}
	if rec.Dir != filepath.Join(a.Root, n) {
		t.Errorf("recorded dir = %q, want %q", rec.Dir, filepath.Join(a.Root, n))
	}
	if !strings.Contains(out.String(), "name\t"+n) {
		t.Errorf("output missing the name line:\n%s", out.String())
	}
}

// A session with no URL is still usable, so New must not fail or roll back
// when Remote Control does not answer.
func TestNewSucceedsWithoutARemoteControlURL(t *testing.T) {
	a, _, errb := app(t)
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })

	if err := a.New(n, ""); err != nil {
		t.Fatalf("New returned an error when only the URL was unavailable: %v", err)
	}
	if !a.Tmux.Has(n) {
		t.Error("the session was rolled back")
	}
	if !strings.Contains(errb.String(), "no Remote Control URL") {
		t.Errorf("the missing URL was not reported on stderr:\n%s", errb.String())
	}
}

func TestNewRefusesADuplicateName(t *testing.T) {
	a, _, _ := app(t)
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })

	if err := a.New(n, ""); err != nil {
		t.Fatalf("first New: %v", err)
	}
	err := a.New(n, "")
	if err == nil {
		t.Fatal("second New succeeded — a running session was replaced")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want it to say the session is already running", err)
	}
}

// The store outliving the process is the reason it exists.
func TestListReportsAKilledSessionAsStopped(t *testing.T) {
	a, out, _ := app(t)
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })

	a.New(n, "")
	a.Tmux.Kill(n) // kill behind cbx's back, as a crash would
	out.Reset()

	if err := a.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	line := out.String()
	if !strings.Contains(line, n) {
		t.Fatalf("List dropped the session entirely:\n%s", line)
	}
	if !strings.Contains(line, "stopped") {
		t.Errorf("List reported %q, want it marked stopped:\n%s", n, line)
	}
}

func TestKillStopsAndForgets(t *testing.T) {
	a, _, _ := app(t)
	n := name(t)
	a.New(n, "")

	if err := a.Kill(n); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if a.Tmux.Has(n) {
		t.Error("still running after Kill")
	}
	if rec, _ := a.Store.Get(n); rec != nil {
		t.Error("still recorded after Kill")
	}
}

func TestResumeOnAStoppedSessionSaysHowToRestart(t *testing.T) {
	a, _, _ := app(t)
	n := name(t)
	a.New(n, "")
	a.Tmux.Kill(n)

	err := a.Resume(n)
	if err == nil {
		t.Fatal("Resume succeeded for a session that is not running")
	}
	if !strings.Contains(err.Error(), "cbx new") {
		t.Errorf("error = %q, want it to name the command that fixes this", err)
	}
}

func TestResumePrintsTheAttachCommand(t *testing.T) {
	a, out, _ := app(t)
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })
	a.New(n, "")
	out.Reset()

	if err := a.Resume(n); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !strings.Contains(out.String(), "tmux attach -t "+n) {
		t.Errorf("output missing the attach command:\n%s", out.String())
	}
}

// Nothing in cbx may block on input: its caller has no terminal.
func TestNoCommandReadsStdin(t *testing.T) {
	a, _, _ := app(t)
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })

	r, w, _ := os.Pipe()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old; w.Close() }()
	w.Close() // any read returns EOF immediately rather than hanging

	done := make(chan struct{})
	go func() {
		a.New(n, "")
		a.List()
		a.Resume(n)
		a.Export("skills")
		a.Kill(n)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("a command blocked — cbx must never read stdin")
	}
}

func TestExportRejectsAnUnknownTarget(t *testing.T) {
	a, _, _ := app(t)
	err := a.Export("nonsense")
	if err == nil || !strings.Contains(err.Error(), "skills") {
		t.Errorf("err = %v, want it to list the valid targets", err)
	}
}

func TestExportDBHasAHeaderAndTheSession(t *testing.T) {
	a, out, _ := app(t)
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })
	a.New(n, "")
	out.Reset()

	if err := a.Export("db"); err != nil {
		t.Fatalf("Export: %v", err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "name\tdir\trepo\turl\tcreated\n") {
		t.Errorf("missing header row:\n%s", got)
	}
	if !strings.Contains(got, n) {
		t.Errorf("session absent from the export:\n%s", got)
	}
}

// The end-to-end guard: a hostile repo value must not execute, and must not
// leave a half-made directory behind. The rejection itself is tested in
// internal/workspace, which owns it; this asserts New refuses at the seam.
func TestNewWithAHostileRepoNeitherExecutesNorLittersr(t *testing.T) {
	a, _, _ := app(t)
	canary := filepath.Join(t.TempDir(), "pwned")
	n := name(t)
	t.Cleanup(func() { a.Tmux.Kill(n) })

	err := a.New(n, "--upload-pack=touch "+canary)
	if err == nil {
		t.Fatal("New accepted a repo value git would read as an option")
	}
	if _, statErr := os.Stat(canary); statErr == nil {
		t.Fatal("the payload executed")
	}
	if _, statErr := os.Stat(filepath.Join(a.Root, n)); statErr == nil {
		t.Error("a project directory was left behind after the failure")
	}
}
