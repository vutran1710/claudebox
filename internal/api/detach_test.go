package api

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Detaching is the one part of this that cannot be asserted from a value: the
// behaviours that matter are what survives a parent exiting and what a kernel
// does with a lock when a process dies. These drive real processes.

func TestTheLockIsHeldWhileRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.lock")
	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer lock.Release()

	if _, err := AcquireLock(path); err != ErrAlreadyRunning {
		t.Fatalf("second AcquireLock = %v, want ErrAlreadyRunning", err)
	}
}

func TestTheLockIsFreeAgainAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.lock")
	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	lock.Release()

	again, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	again.Release()
}

func TestTheLockRecordsThePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.lock")
	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	pid, running := RunningPID(path)
	if !running {
		t.Fatal("RunningPID says nothing is running while the lock is held")
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestRunningPIDIsFalseWhenNothingHoldsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.lock")
	if _, running := RunningPID(path); running {
		t.Error("reported a running server with no lock file at all")
	}
	lock, _ := AcquireLock(path)
	lock.Release()
	if _, running := RunningPID(path); running {
		t.Error("reported a running server from a released lock file")
	}
}

// The helper is this test binary. Generating a separate one does not work:
// a file outside the module cannot import an internal/ package, and the tests
// that matter most here were silently skipping because of it.
//
// TestMain re-enters as a lock holder when CBX_TEST_HOLD_LOCK names a path.

func TestMain(m *testing.M) {
	if path := os.Getenv("CBX_TEST_HOLD_LOCK"); path != "" {
		lock, err := AcquireLock(path)
		if err != nil {
			os.Stderr.WriteString("lock: " + err.Error() + "\n")
			os.Exit(1)
		}
		defer lock.Release()
		os.Stdout.WriteString("held\n")
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// detachedHolder starts a detached process holding the lock at path, through
// the real Detach code path, and returns its pid.
func detachedHolder(t *testing.T, path, logPath string) int {
	t.Helper()
	t.Setenv("CBX_TEST_HOLD_LOCK", path)
	pid, err := Detach(nil, logPath)
	if err != nil {
		t.Fatalf("Detach: %v", err)
	}
	t.Cleanup(func() { syscall.Kill(-pid, syscall.SIGKILL) })
	waitFor(t, 10*time.Second, func() bool { _, running := RunningPID(path); return running })
	if _, running := RunningPID(path); !running {
		raw, _ := os.ReadFile(logPath)
		t.Fatalf("the detached process never took the lock; log: %s", raw)
	}
	return pid
}

// This is the reason it is a lock file rather than a PID file. A PID outlives
// its process and gets recycled, so a stale PID file reports a running server
// — and a stop signal aimed at it hits whatever inherited the number. A flock
// is released by the kernel however the process died.
func TestTheLockIsReleasedWhenTheProcessDies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.lock")
	pid := detachedHolder(t, path, filepath.Join(dir, "serve.log"))

	// SIGKILL: no chance to clean up, no deferred Release, nothing but the
	// kernel to drop the lock.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, func() bool { _, running := RunningPID(path); return !running })
	if _, running := RunningPID(path); running {
		t.Fatal("the lock survived SIGKILL — a PID file would lie here, and this must not")
	}
}

func TestDetachRedirectsStdioToTheLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "serve.log")
	detachedHolder(t, filepath.Join(dir, "serve.lock"), logPath)

	// Borrowing the caller's stdio was the bug in the previous attempt at this
	// flag, and a daemon holding a terminal also stops an ssh session closing.
	waitFor(t, 10*time.Second, func() bool {
		raw, _ := os.ReadFile(logPath)
		return strings.Contains(string(raw), "held")
	})
	raw, _ := os.ReadFile(logPath)
	if !strings.Contains(string(raw), "held") {
		t.Fatalf("the detached process's output did not reach the log: %q", raw)
	}
}

func TestDetachedProcessIsNotOurChild(t *testing.T) {
	dir := t.TempDir()
	pid := detachedHolder(t, filepath.Join(dir, "serve.lock"), filepath.Join(dir, "serve.log"))

	// Setsid puts it in a new session, so it survives this process and its
	// terminal. Nothing here waits on it, which is the point.
	var status syscall.WaitStatus
	got, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if err == nil && got == pid {
		t.Error("the detached process is still a child of this one — it would die with its parent")
	}
}

func TestStopSignalsTheProcessGroup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.lock")
	detachedHolder(t, path, filepath.Join(dir, "serve.log"))

	if err := StopDetached(path, 10*time.Second); err != nil {
		t.Fatalf("StopDetached: %v", err)
	}
	if _, running := RunningPID(path); running {
		t.Error("the server is still running after --stop")
	}
}

func TestStopOnNothingSucceeds(t *testing.T) {
	// Already stopped is the outcome the caller asked for, the same reading
	// cbx kill has always taken.
	if err := StopDetached(filepath.Join(t.TempDir(), "absent.lock"), time.Second); err != nil {
		t.Errorf("StopDetached on nothing: %v", err)
	}
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}
