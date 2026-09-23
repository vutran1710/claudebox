package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/store"
)

// Handing work back out.
//
// A session writes its output into its own directory, and a caller needs to
// fetch it. What a caller must not get is everything else in there — a cloned
// repository, scratch files, whatever Claude happened to write — so the
// fetchable set is declared rather than discovered.
//
// A query names the files it expects to produce. When it finishes, those that
// exist are registered and become fetchable. Nothing else ever is, which is
// the same deny-by-default the command allowlist uses and for the same reason:
// the alternative is a boundary that only holds while everyone behaves.

// DefaultArtifactTTL is how long a declared output stays fetchable.
//
// The register expires, not the file. Deleting what a session wrote on a timer
// would contradict DELETE keeping the working directory, and the data is still
// reachable over ssh.
const DefaultArtifactTTL = 4 * time.Hour

// maxArtifactBytes caps a single fetch, so one request cannot exhaust the box.
const maxArtifactBytes = 25 << 20

type artifactView struct {
	Path      string `json:"path"`
	Job       string `json:"job,omitempty"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

func artifactOf(a store.Artifact) artifactView {
	return artifactView{
		Path: a.Path, Job: a.JobID, Size: a.Size, SHA256: a.SHA256,
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: a.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.session(w, name) == nil {
		return
	}
	found, err := s.Store.Artifacts(name)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]artifactView, 0, len(found))
	for _, a := range found {
		out = append(out, artifactOf(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": out})
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sess := s.session(w, name)
	if sess == nil {
		return
	}
	path := filepath.ToSlash(filepath.Clean(r.PathValue("path")))

	registered, err := s.Store.Artifact(name, path)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if registered == nil {
		// Deliberately the same answer whether it was never declared, has
		// expired, or is simply not there: a caller learns what it may fetch
		// from the register, not by probing the filesystem.
		fail(w, http.StatusNotFound, fmt.Sprintf(
			"%q is not a fetchable artifact of this session — declare it on the query that produces it", path))
		return
	}

	// Containment is still checked, even for a registered path. The register
	// says what may be fetched; this says it is still inside the session.
	full, err := resolveInside(sess.Dir, path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		fail(w, http.StatusNotFound, fmt.Sprintf("%q was registered but is no longer on disk", path))
		return
	}
	if info.Size() > maxArtifactBytes {
		fail(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"artifact is %d bytes; the limit is %d — fetch it over ssh instead", info.Size(), maxArtifactBytes))
		return
	}

	f, err := os.Open(full)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()

	ct := mime.TypeByExtension(filepath.Ext(full))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(full)))
	io.Copy(w, f)
}

// declaredArtifact checks a path a caller says a query will produce.
//
// Rejected at request time rather than after the turn has run, so a typo costs
// a round trip instead of a report.
func declaredArtifact(dir, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("an artifact path cannot be empty")
	}
	if _, err := resolveInside(dir, rel); err != nil {
		return "", fmt.Errorf("artifact %q: %w", rel, err)
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

// register records the declared outputs that actually exist once a turn has
// finished. A declared file the session never wrote is simply not registered —
// the caller finds out by its absence from the listing rather than by fetching
// something that is not there.
func (s *Server) register(sess *store.Session, jobID string, declared []string) {
	now := s.now()
	for _, rel := range declared {
		full, err := resolveInside(sess.Dir, rel)
		if err != nil {
			continue
		}
		info, err := os.Stat(full)
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			continue
		}
		s.Store.PutArtifact(store.Artifact{
			SessionName: sess.Name,
			Path:        filepath.ToSlash(filepath.Clean(rel)),
			JobID:       jobID,
			Size:        info.Size(),
			SHA256:      digest(full),
			CreatedAt:   now,
			ExpiresAt:   now.Add(s.artifactTTL()),
		})
	}
}

func (s *Server) artifactTTL() time.Duration {
	if s.ArtifactTTL > 0 {
		return s.ArtifactTTL
	}
	return DefaultArtifactTTL
}

// digest lets a caller confirm what it fetched is what was produced. Best
// effort: a file that cannot be read is registered without one rather than
// not registered at all.
func digest(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// resolveInside turns a request path into a real path inside dir, or refuses.
//
// Validated by resolving, not by inspecting the string. A path can escape a
// directory without containing ".." — a symlink inside the session pointing at
// /etc/passwd reads as an ordinary relative path until it is resolved.
func resolveInside(dir, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("no file named")
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("path must be relative to the session directory")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("the session's directory is gone")
	}
	full := filepath.Join(root, filepath.FromSlash(rel))

	// Resolve what exists of the path, so a symlink is followed before the
	// containment check rather than after it.
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		resolved = filepath.Clean(full)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes the session directory")
	}
	return resolved, nil
}

// session loads a session of either kind. Fetching an artifact is a read, and
// "interactive sessions are read-only through the API" is a rule about writing.
func (s *Server) session(w http.ResponseWriter, name string) *store.Session {
	sess, err := s.Store.Get(name)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	if sess == nil {
		fail(w, http.StatusNotFound, fmt.Sprintf("no session named %q", name))
		return nil
	}
	return sess
}
