package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vutran1710/claudebox/internal/api"
	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/store"
)

// End-to-end coverage of the API against a real Claude Code.
//
// The unit tests fake the runner, which is right for asserting argv and
// status codes and wrong for the two things this project keeps being caught
// by: that a command can report success having done nothing, and that a
// killed turn leaves a conversation somebody still has to resume. Both are
// only true or false against the real binary.
//
// Skipped where Claude Code is absent or signed out, which is most CI.

const e2eKey = "cbx_live_e2e"

func signedIn(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude is not installed")
	}
	out, err := exec.Command("claude", "auth", "status", "--json").CombinedOutput()
	if err != nil || !strings.Contains(string(out), `"loggedIn"`) ||
		strings.Contains(string(out), `"loggedIn": false`) || strings.Contains(string(out), `"loggedIn":false`) {
		t.Skip("claude is not signed in")
	}
}

// liveAPI starts a real server backed by a temp database and a temp workspace.
func liveAPI(t *testing.T) (*httptest.Server, *store.Store, string) {
	t.Helper()
	signedIn(t)

	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(st, e2eKey)
	srv.Home = home
	// An empty spec path falls back to the shipped default, which is what a
	// fresh box runs.
	srv.SpecPath = filepath.Join(t.TempDir(), "commands.yaml")

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st, t.TempDir()
}

func call(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var r *http.Request
	var err error
	if body == nil {
		r, err = http.NewRequest(method, ts.URL+path, nil)
	} else {
		b, _ := json.Marshal(body)
		r, err = http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+e2eKey)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

// session records a headless session pointing at dir, bypassing the create
// endpoint so the workspace stays inside the test's temp directory.
func session(t *testing.T, st *store.Store, name, dir string) store.Session {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := store.Session{
		Name: name, Dir: dir, Kind: store.Headless,
		ClaudeSessionID: newUUID(t),
	}
	if err := st.Put(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if home, err := os.UserHomeDir(); err == nil {
			os.Remove(claude.TranscriptPath(home, dir, s.ClaudeSessionID))
		}
	})
	return s
}

func newUUID(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("uuidgen").Output()
	if err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1e12)
	}
	return strings.ToLower(strings.TrimSpace(string(out)))
}

func TestCreateQueryAnswerLifecycle(t *testing.T) {
	ts, st, root := liveAPI(t)
	session(t, st, "e2e-query", filepath.Join(root, "e2e-query"))

	code, got := call(t, ts, "POST", "/sessions/e2e-query/query", map[string]any{
		"prompt":         "Reply with exactly the word READY and nothing else.",
		"respond_within": "5m",
	})
	if code != http.StatusOK {
		t.Fatalf("code = %d: %v", code, got)
	}
	if !strings.Contains(strings.ToUpper(fmt.Sprint(got["answer"])), "READY") {
		t.Errorf("answer = %v", got["answer"])
	}
	// The turn reached the model, so the session can be resumed next time.
	after, _ := st.Get("e2e-query")
	if after == nil || after.Turns < 1 {
		t.Errorf("turns = %v, want at least 1", after)
	}
}

// The measured trap, as a regression test. Forwarded to claude -p, /clear
// reports success, forks a new conversation id and leaves the transcript
// intact — the codeword comes back. cbx rotates the id instead.
func TestClearActuallyForgetsTheCodeword(t *testing.T) {
	ts, st, root := liveAPI(t)
	before := session(t, st, "e2e-clear", filepath.Join(root, "e2e-clear"))

	if code, got := call(t, ts, "POST", "/sessions/e2e-clear/query", map[string]any{
		"prompt":         "Remember the codeword ZEPHYR. Reply with just: OK",
		"respond_within": "5m",
	}); code != http.StatusOK {
		t.Fatalf("seed: %d %v", code, got)
	}
	_, recalled := call(t, ts, "POST", "/sessions/e2e-clear/query", map[string]any{
		"prompt": "What is the codeword? One word.", "respond_within": "5m",
	})
	if !strings.Contains(strings.ToUpper(fmt.Sprint(recalled["answer"])), "ZEPHYR") {
		t.Fatalf("the conversation did not remember before clearing: %v", recalled["answer"])
	}

	code, cleared := call(t, ts, "POST", "/sessions/e2e-clear/command", map[string]any{"command": "/clear"})
	if code != http.StatusOK {
		t.Fatalf("/clear: %d %v", code, cleared)
	}
	after, _ := st.Get("e2e-clear")
	if after == nil || after.ClaudeSessionID == before.ClaudeSessionID {
		t.Fatalf("the conversation id was not rotated: %v", after)
	}
	t.Cleanup(func() {
		if home, err := os.UserHomeDir(); err == nil {
			os.Remove(claude.TranscriptPath(home, after.Dir, after.ClaudeSessionID))
		}
	})

	_, forgotten := call(t, ts, "POST", "/sessions/e2e-clear/query", map[string]any{
		"prompt":         "What is the codeword? One word. If you do not know, say UNKNOWN.",
		"respond_within": "5m",
	})
	if strings.Contains(strings.ToUpper(fmt.Sprint(forgotten["answer"])), "ZEPHYR") {
		t.Errorf("the codeword survived /clear: %v — this is exactly what forwarding the command does", forgotten["answer"])
	}
}

// Cancelling costs the turn in flight and nothing else. If a killed claude -p
// left a transcript --resume could not continue, DELETE /jobs/{id} would
// quietly break the session it was used on.
func TestCancelledQueryLeavesAResumableSession(t *testing.T) {
	ts, st, root := liveAPI(t)
	session(t, st, "e2e-cancel", filepath.Join(root, "e2e-cancel"))

	// Seed a turn so the conversation exists to be resumed afterwards.
	if code, got := call(t, ts, "POST", "/sessions/e2e-cancel/query", map[string]any{
		"prompt": "Reply with just: OK", "respond_within": "5m",
	}); code != http.StatusOK {
		t.Fatalf("seed: %d %v", code, got)
	}

	code, started := call(t, ts, "POST", "/sessions/e2e-cancel/query", map[string]any{
		"prompt":         "Write a detailed 2000-word essay on the history of the bicycle.",
		"respond_within": 0,
	})
	if code != http.StatusAccepted {
		t.Fatalf("expected a job: %d %v", code, started)
	}
	id, _ := started["job"].(string)

	time.Sleep(3 * time.Second)
	if code, got := call(t, ts, "DELETE", "/jobs/"+id, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d %v", code, got)
	}
	job, _ := st.JobByID(id)
	if job == nil || job.Status != store.Cancelled {
		t.Fatalf("status = %v, want cancelled", job)
	}

	// The session still works, which is what makes cancellation safe to offer.
	code, after := call(t, ts, "POST", "/sessions/e2e-cancel/query", map[string]any{
		"prompt": "Reply with just: STILL HERE", "respond_within": "5m",
	})
	if code != http.StatusOK {
		t.Fatalf("the session was unusable after a cancelled turn: %d %v", code, after)
	}
	if !strings.Contains(strings.ToUpper(fmt.Sprint(after["answer"])), "STILL HERE") {
		t.Errorf("answer = %v", after["answer"])
	}
}
