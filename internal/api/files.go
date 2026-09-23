package api

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Reading what a session produced.
//
// A session writes its work into its own directory — a report, a diff, a
// rendered page — and until now the API could put files in and never take any
// out. This is the other half: list what is there, and fetch one.
//
// Reads are allowed on an interactive session too. The rule is that interactive
// sessions are read-only through the API, and a read is a read; nothing here
// writes, and nothing here competes with the phone for a turn.

// maxFileBytes caps a single fetch. Large enough for a rendered report,
// small enough that one request cannot exhaust the box's memory.
const maxFileBytes = 25 << 20

// maxListed caps a listing. A session directory with a node_modules in it
// would otherwise produce a response nobody wants.
const maxListed = 2000

// skipDirs are never walked. They are either enormous, or machinery rather
// than output.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".claude": true,
	"vendor": true, "__pycache__": true, ".venv": true, "target": true,
}

type fileEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	sess := s.anySession(w, r.PathValue("name"))
	if sess == nil {
		return
	}
	root, err := filepath.EvalSymlinks(sess.Dir)
	if err != nil {
		fail(w, http.StatusNotFound, fmt.Sprintf("the session's directory is gone: %v", err))
		return
	}

	var out []fileEntry
	truncated := false
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // an unreadable corner is not a reason to fail the listing
		}
		if info.IsDir() {
			if skipDirs[info.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) >= maxListed {
			truncated = true
			return filepath.SkipAll
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, fileEntry{
			Path:     filepath.ToSlash(rel),
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	body := map[string]any{"dir": sess.Dir, "files": out}
	if truncated {
		// Said out loud. A silently truncated listing reads as "this is
		// everything", which is the failure this project keeps finding.
		body["truncated"] = fmt.Sprintf("more than %d files; only the first %d are listed", maxListed, maxListed)
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	sess := s.anySession(w, r.PathValue("name"))
	if sess == nil {
		return
	}
	full, err := resolveInside(sess.Dir, r.PathValue("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if os.IsNotExist(err) {
		fail(w, http.StatusNotFound, fmt.Sprintf("no file %q in this session", r.PathValue("path")))
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if info.IsDir() {
		fail(w, http.StatusBadRequest, "that is a directory — list the session's files instead")
		return
	}
	if info.Size() > maxFileBytes {
		fail(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"file is %d bytes; the limit is %d — fetch it over ssh instead", info.Size(), maxFileBytes))
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
	// Named so a browser or a backend saving it gets the file's own name
	// rather than the last path segment of a URL.
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(full)))
	io.Copy(w, f)
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
		// Not there yet — check the cleaned path instead, so a missing file
		// still gets a 404 rather than leaking whether a parent exists.
		resolved = filepath.Clean(full)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes the session directory")
	}
	return resolved, nil
}

// anySession loads a session of either kind. Reading is allowed on both:
// "interactive sessions are read-only through the API" is a rule about
// writing, and this only reads.
func (s *Server) anySession(w http.ResponseWriter, name string) *sessionRef {
	sess, err := s.Store.Get(name)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	if sess == nil {
		fail(w, http.StatusNotFound, fmt.Sprintf("no session named %q", name))
		return nil
	}
	return &sessionRef{Dir: sess.Dir}
}

type sessionRef struct{ Dir string }
