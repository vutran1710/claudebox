// Package store records what sessions exist on this machine.
//
// tmux is the only registry cbx had before, so everything it knew about a
// session died with the tmux server. That is not hypothetical: restarting the
// server to pick up a new group took the master session's Remote Control URL
// with it, and nothing could recover it.
//
// SQLite rather than a JSON file because two callers overlap — the master
// Claude session and a person over SSH can both run cbx at once, and a
// read-modify-write over JSON is a lost update.
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Session kinds. An interactive session is a tmux process a human drives from
// a phone; a headless one is a conversation id the API drives with `claude -p`.
// The kind records which driver owns it, and one conversation never has two.
const (
	Interactive = "interactive"
	Headless    = "headless"
)

// Session is one Claude Code session cbx started.
type Session struct {
	Name      string
	Dir       string
	Repo      string
	RCURL     string
	CreatedAt time.Time

	Kind string
	// ClaudeSessionID is the conversation `claude -p` resumes. Empty for an
	// interactive session, whose conversation belongs to its tmux process.
	ClaudeSessionID string
	SystemPrompt    string
	// PermissionMode is fixed when the session is created. Per session rather
	// than per query: a caller that could raise its own permissions per
	// request would make the setting meaningless.
	PermissionMode string
	// Model and Effort are fixed at creation for the same reason as
	// PermissionMode: Claude Code treats both as properties of a session.
	Model  string
	Effort string
	// Turns selects the flag. At 0 the conversation does not exist yet and the
	// first query must create it with --session-id.
	Turns int

	// Running is not stored — it is reconciled against tmux at read time,
	// because a row can outlive the process it describes.
	Running bool
}

type Store struct {
	db *sql.DB
	// now is injected so job expiry can be tested without a test that sleeps.
	now func() time.Time
}

// WithClock replaces the clock. Tests drive expiry with it; nothing in
// production does.
func (s *Store) WithClock(now func() time.Time) *Store { s.now = now; return s }

// DefaultPath is where the database lives for the current user. State, not
// config: this is generated data cbx can rebuild, so it follows XDG_STATE_HOME.
func DefaultPath() string {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "cbx", "sessions.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "cbx", "sessions.db")
	}
	return filepath.Join(home, ".local", "state", "cbx", "sessions.db")
}

// Open creates the database if it does not exist.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	// The pragmas go in the DSN, not an Exec. database/sql pools connections
	// and a PRAGMA set via Exec applies only to whichever connection served
	// it — the others still fail instantly on contention. busy_timeout makes a
	// blocked writer wait instead of returning SQLITE_BUSY; WAL lets readers
	// proceed during a write.
	// Built through net/url rather than concatenated. The path goes into a
	// URI, so a '#' in it would start a fragment and silently truncate the
	// filename — two different databases resolving to the same file, which a
	// test found by opening one under a directory named "...90#01".
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		name       TEXT PRIMARY KEY,
		dir        TEXT NOT NULL,
		repo       TEXT NOT NULL DEFAULT '',
		rc_url     TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Put records a session, replacing any earlier row with the same name. A name
// is reused when a session is killed and recreated, and the new row is the
// truth.
func (s *Store) Put(sess Session) error {
	if sess.Name == "" {
		return fmt.Errorf("session name is required")
	}
	created := sess.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	kind := sess.Kind
	if kind == "" {
		// cbx new does not name a kind, and everything it creates is a tmux
		// session. Defaulting here keeps that caller unchanged.
		kind = Interactive
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (name, dir, repo, rc_url, created_at,
		   kind, claude_session_id, system_prompt, permission_mode, model, effort, turns)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET dir=excluded.dir, repo=excluded.repo,
		   rc_url=excluded.rc_url, created_at=excluded.created_at, kind=excluded.kind,
		   claude_session_id=excluded.claude_session_id, system_prompt=excluded.system_prompt,
		   permission_mode=excluded.permission_mode, model=excluded.model,
		   effort=excluded.effort, turns=excluded.turns`,
		sess.Name, sess.Dir, sess.Repo, sess.RCURL, created.Unix(),
		kind, sess.ClaudeSessionID, sess.SystemPrompt, sess.PermissionMode,
		sess.Model, sess.Effort, sess.Turns)
	if err != nil {
		return fmt.Errorf("record session %q: %w", sess.Name, err)
	}
	return nil
}

// Get returns a session by name. Absent is (nil, nil), not an error — callers
// routinely ask about a name that may not exist.
func (s *Store) Get(name string) (*Session, error) {
	var sess Session
	var created int64
	err := s.db.QueryRow(
		`SELECT name, dir, repo, rc_url, created_at, kind, claude_session_id, system_prompt, permission_mode, model, effort, turns FROM sessions WHERE name = ?`, name).
		Scan(&sess.Name, &sess.Dir, &sess.Repo, &sess.RCURL, &created, &sess.Kind, &sess.ClaudeSessionID, &sess.SystemPrompt, &sess.PermissionMode,
			&sess.Model, &sess.Effort, &sess.Turns)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session %q: %w", name, err)
	}
	sess.CreatedAt = time.Unix(created, 0)
	return &sess, nil
}

// List returns every recorded session, oldest first.
func (s *Store) List() ([]Session, error) {
	rows, err := s.db.Query(`SELECT name, dir, repo, rc_url, created_at, kind, claude_session_id, system_prompt, permission_mode, model, effort, turns FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var sess Session
		var created int64
		if err := rows.Scan(&sess.Name, &sess.Dir, &sess.Repo, &sess.RCURL, &created, &sess.Kind, &sess.ClaudeSessionID, &sess.SystemPrompt, &sess.PermissionMode,
			&sess.Model, &sess.Effort, &sess.Turns); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.CreatedAt = time.Unix(created, 0)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// Delete forgets a session. Deleting one that was never recorded is not an
// error: the caller's intent is that it be gone.
func (s *Store) Delete(name string) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE name = ?`, name); err != nil {
		return fmt.Errorf("delete session %q: %w", name, err)
	}
	return nil
}
