package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vutran1710/claudebox/internal/store"
)

// The register decides what a caller may fetch. The session directory holds a
// cloned repository, scratch files and whatever else — none of which is a
// caller's business — so these are mostly about what stays unreachable.

// writing returns a runner that creates files in the session directory, the
// way a real turn would.
func writing(files map[string]string) func(*testing.T, *harness, string) {
	return func(t *testing.T, h *harness, dir string) {
		t.Helper()
		for rel, body := range files {
			p := filepath.Join(dir, rel)
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// produced sets up a session whose next query writes files and succeeds.
func produced(t *testing.T, name string, files map[string]string) (*harness, store.Session) {
	t.Helper()
	var h *harness
	var dir string
	h = answering(t, func(_ context.Context, _ string, args []string) ([]byte, error) {
		writing(files)(t, h, dir)
		id := ""
		for i, a := range args {
			if a == "--session-id" || a == "--resume" {
				id = args[i+1]
			}
		}
		return []byte(result(id, "written", 1)), nil
	})
	sess := h.headlessSession(name, "uuid-"+name, 1)
	dir = sess.Dir
	return h, sess
}

func TestADeclaredArtifactBecomesFetchable(t *testing.T) {
	h, _ := produced(t, "rep", map[string]string{"report.html": "<h1>Q3</h1>"})

	w := h.do("POST", "/sessions/rep/query", map[string]any{
		"prompt": "write the report", "respond_within": "30s",
		"artifacts": []string{"report.html"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("query: %d %s", w.Code, w.Body)
	}

	got := h.do("GET", "/sessions/rep/artifacts/report.html", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("fetch: %d %s", got.Code, got.Body)
	}
	if got.Body.String() != "<h1>Q3</h1>" {
		t.Errorf("body = %q", got.Body.String())
	}
	if ct := got.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}

// The reason the register exists. A session directory is full of things a
// caller has no business reading.
func TestAnUndeclaredFileIsNotFetchable(t *testing.T) {
	h, sess := produced(t, "secrets", map[string]string{"report.html": "ok"})
	os.WriteFile(filepath.Join(sess.Dir, ".env"), []byte("API_KEY=hunter2"), 0o600)
	os.MkdirAll(filepath.Join(sess.Dir, "src"), 0o755)
	os.WriteFile(filepath.Join(sess.Dir, "src", "private.go"), []byte("package secret"), 0o644)

	h.do("POST", "/sessions/secrets/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})

	for _, path := range []string{".env", "src/private.go", "report.html.bak"} {
		w := h.do("GET", "/sessions/secrets/artifacts/"+path, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: code = %d, want 404", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "hunter2") {
			t.Errorf("%s leaked its contents", path)
		}
	}
}

func TestListingShowsOnlyRegisteredArtifacts(t *testing.T) {
	h, sess := produced(t, "listed", map[string]string{"report.html": "a", "scratch.tmp": "b"})
	os.WriteFile(filepath.Join(sess.Dir, "notes.txt"), []byte("c"), 0o644)

	h.do("POST", "/sessions/listed/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})

	got := h.json(h.do("GET", "/sessions/listed/artifacts", nil))
	list, _ := got["artifacts"].([]any)
	if len(list) != 1 {
		t.Fatalf("listed %d artifacts, want 1: %v", len(list), list)
	}
	only := list[0].(map[string]any)
	if only["path"] != "report.html" {
		t.Errorf("path = %v", only["path"])
	}
	// A digest lets a caller confirm what it fetched is what was produced.
	if only["sha256"] == nil || only["sha256"] == "" {
		t.Error("no digest recorded")
	}
	if only["job"] == nil || only["job"] == "" {
		t.Error("the producing job was not recorded")
	}
}

func TestADeclaredFileTheTurnNeverWroteIsNotRegistered(t *testing.T) {
	h, _ := produced(t, "missing", map[string]string{"report.html": "ok"})

	h.do("POST", "/sessions/missing/query", map[string]any{
		"prompt": "go", "respond_within": "30s",
		"artifacts": []string{"report.html", "appendix.html"},
	})

	got := h.json(h.do("GET", "/sessions/missing/artifacts", nil))
	list, _ := got["artifacts"].([]any)
	if len(list) != 1 {
		t.Fatalf("listed %d, want 1 — a declared file that was never written is not an artifact", len(list))
	}
	if w := h.do("GET", "/sessions/missing/artifacts/appendix.html", nil); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

// Rejected at request time, so a typo costs a round trip rather than a report.
func TestADeclaredPathCannotEscapeTheSession(t *testing.T) {
	h, _ := produced(t, "escape", map[string]string{"ok.txt": "fine"})
	for _, bad := range []string{"../../../etc/passwd", "/etc/passwd", ""} {
		w := h.do("POST", "/sessions/escape/query", map[string]any{
			"prompt": "go", "respond_within": "30s", "artifacts": []string{bad},
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("declared %q: code = %d, want 400", bad, w.Code)
		}
	}
}

// A registered path still has to be inside the session when it is fetched: a
// symlink planted afterwards must not turn a registration into a way out.
func TestARegisteredPathIsStillCheckedOnFetch(t *testing.T) {
	h, sess := produced(t, "swap", map[string]string{"report.html": "real"})
	h.do("POST", "/sessions/swap/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})

	secret := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(secret, []byte("PRIVATE"), 0o600)
	os.Remove(filepath.Join(sess.Dir, "report.html"))
	if err := os.Symlink(secret, filepath.Join(sess.Dir, "report.html")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	w := h.do("GET", "/sessions/swap/artifacts/report.html", nil)
	if strings.Contains(w.Body.String(), "PRIVATE") {
		t.Error("a symlink swapped in after registration escaped the session")
	}
}

func TestArtifactsExpire(t *testing.T) {
	h, _ := produced(t, "ttl", map[string]string{"report.html": "ok"})
	clock := time.Now()
	h.now = func() time.Time { return clock }
	h.Store.WithClock(func() time.Time { return clock })
	h.ArtifactTTL = time.Hour

	h.do("POST", "/sessions/ttl/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})
	if w := h.do("GET", "/sessions/ttl/artifacts/report.html", nil); w.Code != http.StatusOK {
		t.Fatalf("before expiry: %d %s", w.Code, w.Body)
	}

	clock = clock.Add(2 * time.Hour)
	if w := h.do("GET", "/sessions/ttl/artifacts/report.html", nil); w.Code != http.StatusNotFound {
		t.Errorf("after expiry: code = %d, want 404", w.Code)
	}
	got := h.json(h.do("GET", "/sessions/ttl/artifacts", nil))
	if list, _ := got["artifacts"].([]any); len(list) != 0 {
		t.Errorf("an expired artifact was still listed: %v", list)
	}
	expired, err := h.Store.SweepArtifacts()
	if err != nil || len(expired) != 1 {
		t.Errorf("sweep removed %d rows (%v), want 1", len(expired), err)
	}
}

// Expiry has to mean the file as well. Expiring only the register would leave
// every report a session ever produced on disk for ever, merely unfetchable —
// and these files hold customer data.
func TestTheJanitorDeletesExpiredArtifactFiles(t *testing.T) {
	h, sess := produced(t, "swept", map[string]string{"report.html": "confidential"})
	clock := time.Now()
	h.now = func() time.Time { return clock }
	h.Store.WithClock(func() time.Time { return clock })
	h.ArtifactTTL = time.Hour

	h.do("POST", "/sessions/swept/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})
	onDisk := filepath.Join(sess.Dir, "report.html")
	if _, err := os.Stat(onDisk); err != nil {
		t.Fatalf("the artifact was never written: %v", err)
	}

	clock = clock.Add(2 * time.Hour)
	h.sweepArtifacts()

	if _, err := os.Stat(onDisk); !os.IsNotExist(err) {
		t.Error("an expired artifact is still on disk — the TTL only hid it")
	}
}

// Only ever files that were registered. Everything else in the directory was
// never an artifact and the janitor has no business touching it.
func TestTheJanitorLeavesEverythingElseAlone(t *testing.T) {
	h, sess := produced(t, "spared", map[string]string{"report.html": "declared"})
	clock := time.Now()
	h.now = func() time.Time { return clock }
	h.Store.WithClock(func() time.Time { return clock })
	h.ArtifactTTL = time.Hour

	os.WriteFile(filepath.Join(sess.Dir, "notes.md"), []byte("someone's work"), 0o644)
	os.MkdirAll(filepath.Join(sess.Dir, "src"), 0o755)
	os.WriteFile(filepath.Join(sess.Dir, "src", "main.go"), []byte("package main"), 0o644)

	h.do("POST", "/sessions/spared/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})
	clock = clock.Add(2 * time.Hour)
	h.sweepArtifacts()

	for _, kept := range []string{"notes.md", "src/main.go"} {
		if _, err := os.Stat(filepath.Join(sess.Dir, kept)); err != nil {
			t.Errorf("the janitor deleted %s, which was never an artifact", kept)
		}
	}
	if _, err := os.Stat(sess.Dir); err != nil {
		t.Error("the janitor removed the working directory")
	}
}

// A session deleted before its artifacts expire takes its register with it, so
// the sweep has nothing to unlink and must not go looking.
func TestSweepingAfterTheSessionIsGoneIsHarmless(t *testing.T) {
	h, _ := produced(t, "vanished", map[string]string{"report.html": "ok"})
	h.do("POST", "/sessions/vanished/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})
	h.do("DELETE", "/sessions/vanished", nil)
	h.sweepArtifacts() // must not panic or error
}

func TestTheDefaultTTLIsFourHours(t *testing.T) {
	if DefaultArtifactTTL != 4*time.Hour {
		t.Errorf("DefaultArtifactTTL = %v, want 4h", DefaultArtifactTTL)
	}
}

func TestDeletingASessionClearsItsRegister(t *testing.T) {
	h, _ := produced(t, "gone", map[string]string{"report.html": "ok"})
	h.do("POST", "/sessions/gone/query", map[string]any{
		"prompt": "go", "respond_within": "30s", "artifacts": []string{"report.html"},
	})
	h.do("DELETE", "/sessions/gone", nil)

	// A name reused later must not inherit what the last session could hand back.
	if got, _ := h.Store.Artifacts("gone"); len(got) != 0 {
		t.Errorf("the register survived the session: %v", got)
	}
}

func TestArtifactsNeedTheKey(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.headlessSession("guarded", "uuid-g", 1)
	for _, path := range []string{"/sessions/guarded/artifacts", "/sessions/guarded/artifacts/a.txt"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d, want 401", path, w.Code)
		}
	}
}

func TestAnUnknownSessionHasNoArtifacts(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	if w := h.do("GET", "/sessions/never/artifacts", nil); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}
