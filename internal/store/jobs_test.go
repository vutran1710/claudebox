package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Jobs are rows rather than a map in the server so that expiry survives the
// process that scheduled it. These drive a real SQLite file with an injected
// clock, so nothing here sleeps.

func jobStore(t *testing.T, now func() time.Time) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s.WithClock(now)
}

func at(ts string) time.Time {
	v, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return v
}

func running(id, session string, expires time.Time) Job {
	return Job{ID: id, SessionName: session, Status: Running, Prompt: "hello", ExpiresAt: expires}
}

func TestJobRoundTrips(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	want := running("j1", "alpha", now.Add(time.Hour))
	if err := s.CreateJob(want); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	got, err := s.JobByID("j1")
	if err != nil || got == nil {
		t.Fatalf("JobByID: %v %v", got, err)
	}
	if got.SessionName != "alpha" || got.Status != Running || got.Prompt != "hello" {
		t.Errorf("round trip lost fields: %+v", got)
	}
}

func TestOnlyOneRunningJobPerSession(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	if err := s.CreateJob(running("j1", "alpha", now.Add(time.Hour))); err != nil {
		t.Fatalf("first job: %v", err)
	}
	err := s.CreateJob(running("j2", "alpha", now.Add(time.Hour)))
	if err != ErrSessionBusy {
		t.Fatalf("second concurrent job on one session: got %v, want ErrSessionBusy — two --resume processes would interleave turns", err)
	}
}

func TestAFinishedJobFreesTheSession(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	s.CreateJob(running("j1", "alpha", now.Add(time.Hour)))
	if err := s.FinishJob("j1", Done, "the answer", ""); err != nil {
		t.Fatalf("FinishJob: %v", err)
	}
	if err := s.CreateJob(running("j2", "alpha", now.Add(time.Hour))); err != nil {
		t.Errorf("session still locked after its job finished: %v", err)
	}
}

func TestADifferentSessionIsNotBlocked(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	s.CreateJob(running("j1", "alpha", now.Add(time.Hour)))
	if err := s.CreateJob(running("j2", "beta", now.Add(time.Hour))); err != nil {
		t.Errorf("a second session was blocked by the first: %v", err)
	}
}

func TestSweepDeletesExpiredFinishedJobs(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	clock := now
	s := jobStore(t, func() time.Time { return clock })

	s.CreateJob(running("j1", "alpha", now.Add(time.Hour)))
	s.FinishJob("j1", Done, "answer", "")

	clock = now.Add(2 * time.Hour)
	n, err := s.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("swept %d jobs, want 1", n)
	}
	if got, _ := s.JobByID("j1"); got != nil {
		t.Error("an expired job survived the sweep")
	}
}

func TestSweepSparesARunningJobPastItsExpiry(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	clock := now
	s := jobStore(t, func() time.Time { return clock })

	s.CreateJob(running("j1", "alpha", now.Add(time.Minute)))

	clock = now.Add(time.Hour)
	if _, err := s.Sweep(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// A query still going must keep its row, or it is pulled out from under
	// the client waiting on it.
	if got, _ := s.JobByID("j1"); got == nil {
		t.Fatal("a still-running job was swept — its client is waiting on that row")
	}
}

func TestAnExpiredJobReadsAsGoneBeforeTheSweepReachesIt(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	clock := now
	s := jobStore(t, func() time.Time { return clock })

	s.CreateJob(running("j1", "alpha", now.Add(time.Minute)))
	s.FinishJob("j1", Done, "answer", "")

	clock = now.Add(time.Hour)
	// The janitor runs every ten minutes; the interval must not show through
	// to a caller as an answer that is alive again.
	if got, _ := s.JobByID("j1"); got != nil {
		t.Error("an expired job was readable because the sweep had not run yet")
	}
}

func TestBootRecoveryMarksRunningJobsInterrupted(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	s.CreateJob(running("j1", "alpha", now.Add(time.Hour)))
	n, err := s.RecoverRunningJobs()
	if err != nil {
		t.Fatalf("RecoverRunningJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered %d, want 1", n)
	}
	got, _ := s.JobByID("j1")
	if got == nil || got.Status != Interrupted {
		t.Fatalf("status = %v, want %q — a client would otherwise poll a ghost for ever", got, Interrupted)
	}
}

func TestBootRecoveryUnwedgesTheSession(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	s.CreateJob(running("j1", "alpha", now.Add(time.Hour)))
	s.RecoverRunningJobs()
	// The unique index is cleared by the same statement, so a session is not
	// left permanently busy by a job that died with the last process.
	if err := s.CreateJob(running("j2", "alpha", now.Add(time.Hour))); err != nil {
		t.Errorf("session still wedged after recovery: %v", err)
	}
}

func TestRunningJobFindsTheQueryInFlight(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	s.CreateJob(running("j1", "alpha", now.Add(time.Hour)))
	got, err := s.RunningJob("alpha")
	if err != nil || got == nil || got.ID != "j1" {
		t.Fatalf("RunningJob = %v, %v; want j1", got, err)
	}
	if other, _ := s.RunningJob("beta"); other != nil {
		t.Error("found a running job for a session that has none")
	}
}

func TestDeleteJobIsIdempotent(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	if err := s.DeleteJob("never-existed"); err != nil {
		t.Errorf("deleting an absent job: %v", err)
	}
}

func TestExistingSessionsMigrateToInteractive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// A database written before the API existed: Put goes through the same
	// migration, so the assertion is that the default is the right one.
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Session{Name: "legacy", Dir: "/workspace/legacy"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	got, _ := reopened.Get("legacy")
	if got == nil || got.Kind != Interactive {
		t.Fatalf("kind = %v, want %q — every session recorded before this feature was a tmux session", got, Interactive)
	}
}

func TestHeadlessSessionFieldsRoundTrip(t *testing.T) {
	now := at("2026-01-01T12:00:00Z")
	s := jobStore(t, func() time.Time { return now })

	want := Session{
		Name: "api-work", Dir: "/workspace/api-work", Kind: Headless,
		ClaudeSessionID: "11111111-1111-1111-1111-111111111111",
		SystemPrompt:    "be terse", PermissionMode: "bypassPermissions", Turns: 3,
	}
	if err := s.Put(want); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("api-work")
	if got == nil {
		t.Fatal("session vanished")
	}
	if got.Kind != Headless || got.ClaudeSessionID != want.ClaudeSessionID ||
		got.SystemPrompt != want.SystemPrompt || got.PermissionMode != want.PermissionMode || got.Turns != 3 {
		t.Errorf("round trip lost fields: %+v", got)
	}
}

// The database path goes into a URI, so characters with meaning there must
// survive it. A directory named "...90#01" silently truncated the filename at
// the '#' and resolved two databases to one file.
func TestAPathWithURIMetacharactersOpensItsOwnDatabase(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"plain", "has#hash", "has?question", "has space"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(base, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			s, err := Open(filepath.Join(dir, "s.db"))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()
			if err := s.Put(Session{Name: "only-here", Dir: dir}); err != nil {
				t.Fatal(err)
			}
			all, err := s.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != 1 {
				t.Fatalf("found %d sessions, want 1 — this database is shared with another path", len(all))
			}
		})
	}
}
