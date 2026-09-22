package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Running the server in the background, for a box with nothing supervising it.
//
// Where a supervisor exists it should do this instead — systemd on a droplet,
// PID 1 in a container — because it also provides restart-on-failure,
// start-on-boot and log capture, none of which --detach can. A detached server
// that dies stays dead.
//
// Never a container's CMD: a PID 1 that forks and exits takes the container
// with it.

// DefaultLockPath is the lock a running server holds.
func DefaultLockPath() string { return filepath.Join(stateDir(), "serve.lock") }

// DefaultLogPath is where a detached server's output goes.
func DefaultLogPath() string { return filepath.Join(stateDir(), "serve.log") }

func stateDir() string {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "cbx")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "cbx")
	}
	return filepath.Join(home, ".local", "state", "cbx")
}

// Lock is held for as long as a server runs.
//
// A lock file rather than a PID file, because the kernel releases a flock
// however the process died. A PID outlives its process and gets recycled, so a
// stale PID file reports a running server — or worse, sends a stop signal to
// whatever unrelated process inherited the number. The pid is written inside
// for reporting; the lock is what is trusted.
type Lock struct {
	f    *os.File
	path string
}

// ErrAlreadyRunning is returned when another server holds the lock.
var ErrAlreadyRunning = fmt.Errorf("a cbx server is already running")

// AcquireLock takes the lock, or reports that someone else has it.
func AcquireLock(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrAlreadyRunning
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f, path: path}, nil
}

// Release drops the lock. The kernel would do it anyway when the process ends;
// this makes an orderly shutdown orderly.
func (l *Lock) Release() error {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// RunningPID reports the pid of a running server, if there is one.
//
// "Is it running" is answered by trying to take the lock, not by looking at
// the pid: the pid is only how to reach it once the lock says it exists.
func RunningPID(path string) (int, bool) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		// We took it, so nobody was holding it.
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, true
	}
	return pid, true
}

// Detach re-execs this binary in the background and returns the child's pid.
//
// Re-exec rather than fork: Go's runtime is multi-threaded and only the
// calling thread survives a fork, so the child would be missing the goroutines
// it needs.
func Detach(args []string, logPath string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("locate this binary: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return 0, err
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", logPath, err)
	}
	defer log.Close()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return 0, err
	}
	defer devnull.Close()

	cmd := exec.Command(exe, args...)
	// Stdio goes nowhere near the caller. Borrowing it was the bug in the
	// previous attempt at this flag, and a daemon holding a terminal open also
	// stops an ssh session from closing.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, log, log
	// Setsid is the part that matters: without a new session the child keeps
	// the caller's controlling terminal and dies of SIGHUP when ssh
	// disconnects — detached in appearance only.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start detached: %w", err)
	}
	// Not waited on deliberately: the point is that it outlives this process.
	return cmd.Process.Pid, nil
}

// StopDetached stops a running server and everything it started.
//
// The signal goes to the process group, not the process. Under systemd
// KillMode=control-group kills query children with the service; detached there
// is no cgroup, and killing only the server would leave `claude -p` children
// writing to transcripts nothing is tracking.
func StopDetached(lockPath string, grace time.Duration) error {
	pid, running := RunningPID(lockPath)
	if !running {
		// Already stopped is the outcome the caller asked for.
		return nil
	}
	if pid <= 0 {
		return fmt.Errorf("a server is running but %s does not name it — stop it by hand", lockPath)
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("signal process group %d: %w", pid, err)
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if _, still := RunningPID(lockPath); !still {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	// A child that ignores SIGTERM still goes.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("kill process group %d: %w", pid, err)
	}
	return nil
}
