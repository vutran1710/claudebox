package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Artifacts are the files a session is allowed to hand back.
//
// A register rather than a log. The session directory holds whatever a session
// happened to write — a cloned repository, scratch files, a stray .env — and
// none of that is something a caller should be able to ask for. A file becomes
// fetchable because somebody declared it as an output, not because it exists.

type Artifact struct {
	SessionName string
	Path        string
	// JobID is the turn that produced it, so "which query wrote this" has an
	// answer after the fact.
	JobID     string
	Size      int64
	SHA256    string
	CreatedAt time.Time
	// ExpiresAt is when the file stops being fetchable. The register expires,
	// not the file: deleting what a session wrote on a timer would contradict
	// the rule that DELETE keeps the working directory, and the data stays
	// reachable over ssh either way.
	ExpiresAt time.Time
}

// PutArtifact registers a file as fetchable, replacing any earlier entry for
// the same path — a later job overwriting a report is the new truth about it.
func (s *Store) PutArtifact(a Artifact) error {
	if a.SessionName == "" || a.Path == "" {
		return fmt.Errorf("artifact needs a session and a path")
	}
	created := a.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	_, err := s.db.Exec(
		`INSERT INTO artifacts (session_name, path, job_id, size, sha256, created_at, expires_at)
		 VALUES (?,?,?,?,?,?,?)
		 ON CONFLICT(session_name, path) DO UPDATE SET job_id=excluded.job_id,
		   size=excluded.size, sha256=excluded.sha256, created_at=excluded.created_at,
		   expires_at=excluded.expires_at`,
		a.SessionName, a.Path, a.JobID, a.Size, a.SHA256, created.Unix(), a.ExpiresAt.Unix())
	if err != nil {
		return fmt.Errorf("register artifact %q: %w", a.Path, err)
	}
	return nil
}

// Artifacts lists what a session may hand back, newest first.
func (s *Store) Artifacts(session string) ([]Artifact, error) {
	// Expired rows are already gone as far as a caller is concerned, so the
	// sweep interval never shows through as an artifact that came back.
	rows, err := s.db.Query(
		`SELECT session_name, path, job_id, size, sha256, created_at, expires_at
		 FROM artifacts WHERE session_name = ? AND expires_at > ?
		 ORDER BY created_at DESC, path`, session, s.now().Unix())
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	var out []Artifact
	for rows.Next() {
		var a Artifact
		var created int64
		var expires int64
		if err := rows.Scan(&a.SessionName, &a.Path, &a.JobID, &a.Size, &a.SHA256, &created, &expires); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		a.CreatedAt, a.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Artifact returns one by path. Absent is (nil, nil): a caller asking for
// something undeclared is the normal case this exists to refuse.
func (s *Store) Artifact(session, path string) (*Artifact, error) {
	var a Artifact
	var created, expires int64
	err := s.db.QueryRow(
		`SELECT session_name, path, job_id, size, sha256, created_at, expires_at
		 FROM artifacts WHERE session_name = ? AND path = ? AND expires_at > ?`,
		session, path, s.now().Unix()).
		Scan(&a.SessionName, &a.Path, &a.JobID, &a.Size, &a.SHA256, &created, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read artifact %q: %w", path, err)
	}
	a.CreatedAt, a.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	return &a, nil
}

// SweepArtifacts drops expired registrations and reports how many went.
func (s *Store) SweepArtifacts() (int, error) {
	res, err := s.db.Exec(`DELETE FROM artifacts WHERE expires_at <= ?`, s.now().Unix())
	if err != nil {
		return 0, fmt.Errorf("sweep artifacts: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteArtifacts forgets a session's register. Called when the session goes,
// so a name reused later does not inherit what the last one could hand back.
func (s *Store) DeleteArtifacts(session string) error {
	if _, err := s.db.Exec(`DELETE FROM artifacts WHERE session_name = ?`, session); err != nil {
		return fmt.Errorf("delete artifacts for %q: %w", session, err)
	}
	return nil
}
