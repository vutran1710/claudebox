package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Jobs are queries that outran the window their caller was willing to wait.
//
// They are rows rather than a map in the server, because a map dies with the
// process that holds it: a crash would lose every pending expiry and leave
// answers nothing would ever collect. A date on a row is swept by the janitor
// whatever state a restart found.

// Job statuses.
const (
	Running = "running"
	Done    = "done"
	Failed  = "failed"
	// Cancelled is a client asking for the query to stop.
	Cancelled = "cancelled"
	// Interrupted is the server having restarted underneath one. It is not
	// Failed: Claude did not fail, and a client should read it differently.
	Interrupted = "interrupted"
)

type Job struct {
	ID          string
	SessionName string
	Status      string
	Prompt      string
	Answer      string
	Error       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// migrate brings an existing database up to the current schema.
//
// Columns are added one at a time and "duplicate column name" is not an
// error, which is how this stays idempotent without a version table. Existing
// rows default to interactive, which is correct — every session recorded
// before the API existed was a tmux session.
func migrate(db *sql.DB) error {
	for _, col := range []string{
		`kind TEXT NOT NULL DEFAULT 'interactive'`,
		`claude_session_id TEXT NOT NULL DEFAULT ''`,
		`system_prompt TEXT NOT NULL DEFAULT ''`,
		`permission_mode TEXT NOT NULL DEFAULT ''`,
		`model TEXT NOT NULL DEFAULT ''`,
		`effort TEXT NOT NULL DEFAULT ''`,
		`turns INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(`ALTER TABLE sessions ADD COLUMN ` + col); err != nil &&
			!strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate sessions: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS jobs (
		id           TEXT PRIMARY KEY,
		session_name TEXT    NOT NULL,
		status       TEXT    NOT NULL,
		prompt       TEXT    NOT NULL DEFAULT '',
		answer       TEXT    NOT NULL DEFAULT '',
		error        TEXT    NOT NULL DEFAULT '',
		created_at   INTEGER NOT NULL,
		expires_at   INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create jobs: %w", err)
	}
	// The database enforces one query at a time per session. A mutex would be
	// forgotten on restart, and combined with an orphaned child that is how a
	// second --resume starts on a conversation already being written to.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS one_running_per_session
		ON jobs(session_name) WHERE status = 'running'`); err != nil {
		return fmt.Errorf("create jobs index: %w", err)
	}
	return nil
}

// ErrSessionBusy is returned when a session already has a query in flight.
var ErrSessionBusy = fmt.Errorf("session already has a query running")

// CreateJob records a query that has started. It fails with ErrSessionBusy if
// one is already running for that session — the unique index is what decides,
// so two callers racing cannot both win.
func (s *Store) CreateJob(j Job) error {
	created, expires := j.CreatedAt, j.ExpiresAt
	if created.IsZero() {
		created = s.now()
	}
	_, err := s.db.Exec(
		`INSERT INTO jobs (id, session_name, status, prompt, answer, error, created_at, expires_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		j.ID, j.SessionName, j.Status, j.Prompt, j.Answer, j.Error, created.Unix(), expires.Unix())
	if err != nil {
		if strings.Contains(err.Error(), "one_running_per_session") || strings.Contains(err.Error(), "UNIQUE constraint") {
			return ErrSessionBusy
		}
		return fmt.Errorf("record job %q: %w", j.ID, err)
	}
	return nil
}

// JobByID returns a job. Absent is (nil, nil) — callers routinely ask about an
// id that has since been swept.
func (s *Store) JobByID(id string) (*Job, error) {
	var j Job
	var created, expires int64
	err := s.db.QueryRow(
		`SELECT id, session_name, status, prompt, answer, error, created_at, expires_at
		 FROM jobs WHERE id = ?`, id).
		Scan(&j.ID, &j.SessionName, &j.Status, &j.Prompt, &j.Answer, &j.Error, &created, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read job %q: %w", id, err)
	}
	j.CreatedAt, j.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	// An expired row that the janitor has not reached yet is already gone as
	// far as a caller is concerned, so the sweep interval never shows through.
	if s.now().After(j.ExpiresAt) && j.Status != Running {
		return nil, nil
	}
	return &j, nil
}

// FinishJob records the outcome of a query and frees the session.
func (s *Store) FinishJob(id, status, answer, errMsg string) error {
	res, err := s.db.Exec(`UPDATE jobs SET status = ?, answer = ?, error = ? WHERE id = ?`,
		status, answer, errMsg, id)
	if err != nil {
		return fmt.Errorf("finish job %q: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no job %q", id)
	}
	return nil
}

// DeleteJob forgets a job. Deleting one already swept is not an error: the
// caller's intent is that it be gone.
func (s *Store) DeleteJob(id string) error {
	if _, err := s.db.Exec(`DELETE FROM jobs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete job %q: %w", id, err)
	}
	return nil
}

// RunningJob returns the query in flight for a session, if there is one.
func (s *Store) RunningJob(name string) (*Job, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM jobs WHERE session_name = ? AND status = ?`, name, Running).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read running job for %q: %w", name, err)
	}
	return s.JobByID(id)
}

// Sweep deletes expired jobs and reports how many went.
//
// A running job outlives its own expiry: a query legitimately still going must
// keep its row, or it would be pulled out from under the client waiting on it.
func (s *Store) Sweep() (int, error) {
	res, err := s.db.Exec(`DELETE FROM jobs WHERE status != ? AND expires_at <= ?`, Running, s.now().Unix())
	if err != nil {
		return 0, fmt.Errorf("sweep jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RecoverRunningJobs marks every running job interrupted, and is called once
// at startup.
//
// Job rows outlive the `claude -p` children that produce them. Without this a
// client polling after a restart would see "running" for ever — a ghost, and a
// lie, since nothing is working on it. It also clears the unique index, so no
// session stays wedged by a job that died with the last process.
func (s *Store) RecoverRunningJobs() (int, error) {
	res, err := s.db.Exec(`UPDATE jobs SET status = ?, error = ? WHERE status = ?`,
		Interrupted, "the server restarted while this query was running", Running)
	if err != nil {
		return 0, fmt.Errorf("recover running jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
