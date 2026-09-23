package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vutran1710/claudebox/internal/store"
)

// A session writes its work into its own directory, and this is how it comes
// back out. The interesting cases are all about staying inside that directory.

func withFiles(t *testing.T, h *harness, name string, files map[string]string) store.Session {
	t.Helper()
	sess := h.headlessSession(name, "uuid-"+name, 1)
	for rel, body := range files {
		p := filepath.Join(sess.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return sess
}

func TestListFilesReportsWhatTheSessionProduced(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	withFiles(t, h, "rep", map[string]string{
		"report.html":       "<h1>Q3</h1>",
		"data/summary.md":   "# summary",
		".git/config":       "should not appear",
		"node_modules/x.js": "should not appear",
	})

	got := h.json(h.do("GET", "/sessions/rep/files", nil))
	files, _ := got["files"].([]any)
	var paths []string
	for _, f := range files {
		paths = append(paths, f.(map[string]any)["path"].(string))
	}
	want := "data/summary.md,report.html"
	if strings.Join(paths, ",") != want {
		t.Errorf("paths = %v, want %s — machinery directories must not be listed", paths, want)
	}
}

func TestFetchAFileByPath(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	withFiles(t, h, "fetch", map[string]string{"report.html": "<h1>Q3</h1>"})

	w := h.do("GET", "/sessions/fetch/files/report.html", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	if w.Body.String() != "<h1>Q3</h1>" {
		t.Errorf("body = %q", w.Body.String())
	}
	// A backend saving this wants to know what it is.
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "report.html") {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestFetchANestedFile(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	withFiles(t, h, "nested", map[string]string{"out/2026/q3.md": "# q3"})
	w := h.do("GET", "/sessions/nested/files/out/2026/q3.md", nil)
	if w.Code != http.StatusOK || w.Body.String() != "# q3" {
		t.Fatalf("code = %d body = %q", w.Code, w.Body.String())
	}
}

// The path is checked by resolving it, not by inspecting the string: a path
// can escape without containing ".." at all.
func TestAPathCannotEscapeTheSessionDirectory(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	sess := withFiles(t, h, "escape", map[string]string{"ok.txt": "fine"})

	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the session pointing outside it reads as an ordinary
	// relative path right up until it is resolved.
	if err := os.Symlink(secret, filepath.Join(sess.Dir, "sneaky.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, attempt := range []string{
		"../../../etc/passwd",
		"..%2f..%2fetc%2fpasswd",
		"sneaky.txt",
		"/etc/passwd",
	} {
		w := h.do("GET", "/sessions/escape/files/"+attempt, nil)
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "PRIVATE") {
			t.Errorf("%q escaped the session directory and returned the file", attempt)
		}
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "root:") {
			t.Errorf("%q read /etc/passwd", attempt)
		}
	}
}

func TestAMissingFileIs404(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	withFiles(t, h, "missing", map[string]string{"a.txt": "a"})
	if w := h.do("GET", "/sessions/missing/files/nope.txt", nil); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

func TestADirectoryIsNotAFile(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	withFiles(t, h, "dir", map[string]string{"out/a.txt": "a"})
	if w := h.do("GET", "/sessions/dir/files/out", nil); w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", w.Code)
	}
}

// Reading is allowed on an interactive session: the read-only rule is about
// writing, and nothing here competes with the phone for a turn.
func TestFilesAreReadableOnAnInteractiveSession(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# notes"), 0o644)
	h.Store.Put(store.Session{Name: "phone", Dir: dir, Kind: store.Interactive})

	if w := h.do("GET", "/sessions/phone/files", nil); w.Code != http.StatusOK {
		t.Errorf("list on an interactive session: %d", w.Code)
	}
	w := h.do("GET", "/sessions/phone/files/notes.md", nil)
	if w.Code != http.StatusOK || w.Body.String() != "# notes" {
		t.Errorf("fetch on an interactive session: %d %q", w.Code, w.Body.String())
	}
}

func TestFilesNeedTheKey(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	withFiles(t, h, "guarded", map[string]string{"a.txt": "a"})
	for _, path := range []string{"/sessions/guarded/files", "/sessions/guarded/files/a.txt"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d, want 401", path, w.Code)
		}
	}
}

func TestAnUnknownSessionIs404(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	if w := h.do("GET", "/sessions/never/files", nil); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}
