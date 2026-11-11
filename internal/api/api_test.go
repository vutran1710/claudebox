package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/store"
	"github.com/vutran1710/claudebox/internal/tmux"
)

// These drive the real router against a real SQLite file, with only Claude
// faked — the unique index that enforces one query per session is load-bearing
// behaviour, and a mock store would assert nothing about it.

const testKey = "cbx_live_test-key"

// testConfig is the policy every harness runs under: one role that can do
// everything, so a test asserting something else is asserting it on purpose.
const testConfig = `
version: 1
roles:
  tester:
    deny: []
    session_env: [TENANT_ID, API_URL]
    query_env: [USER_KEY]
  restricted:
    deny: ["Write(//**)"]
commands:
  - name: /clear
    effect: rotate-session
  - name: /context
    effect: forward
`

type harness struct {
	*Server
	t   *testing.T
	dir string
}

// answering builds a server whose Claude returns whatever run says.
func answering(t *testing.T, run claude.Runner) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	home := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "cbx.yaml")
	if err := os.WriteFile(configPath, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	// The key is a row, not a field: what a caller may do is looked up, so a
	// test cannot accidentally grant itself something by constructing it.
	if _, err := st.AdoptKey("tester", testKey, "tester"); err != nil {
		t.Fatalf("register the test key: %v", err)
	}

	var n atomic.Int64
	s := &Server{
		Store:      st,
		Claude:     claude.New().WithRunner(run),
		Tmux:       tmux.New().WithRunner(func(string) (string, error) { return "", fmt.Errorf("no tmux") }),
		ConfigPath: configPath,
		Home:       home,
		JobTTL:     time.Hour,
		Version:    "test",
		now:        time.Now,
		newID:      func() string { return fmt.Sprintf("j_%d", n.Add(1)) },
		cancels:    map[string]context.CancelFunc{},
	}
	return &harness{Server: s, t: t, dir: home}
}

// install puts a skill where priming looks for it. Priming refuses a skill
// that is not on the box, so a test that primes has to put one there.
func (h *harness) install(names ...string) {
	h.t.Helper()
	for _, n := range names {
		dir := filepath.Join(h.dir, ".claude", "skills", n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+n), 0o600); err != nil {
			h.t.Fatal(err)
		}
	}
}

// result is a captured Claude Code 2.1.236 payload with fields substituted.
func result(sessionID, answer string, turns int) string {
	b, _ := json.Marshal(map[string]any{
		"is_error": false, "subtype": "success", "type": "result",
		"num_turns": turns, "duration_ms": 12, "duration_api_ms": 10,
		"session_id": sessionID, "result": answer,
	})
	return string(b)
}

func (h *harness) do(method, path string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else if s, ok := body.(string); ok {
		r = httptest.NewRequest(method, path, strings.NewReader(s))
	} else {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
	}
	r.Header.Set("Authorization", "Bearer "+testKey)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	return w
}

func (h *harness) json(w *httptest.ResponseRecorder) map[string]any {
	h.t.Helper()
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		h.t.Fatalf("response is not JSON (%d): %s", w.Code, w.Body.String())
	}
	return v
}

// headlessSession records one directly, so tests do not depend on the create
// endpoint working.
func (h *harness) headlessSession(name, sessionID string, turns int) store.Session {
	h.t.Helper()
	dir := filepath.Join(h.t.TempDir(), name)
	os.MkdirAll(dir, 0o755)
	s := store.Session{
		Name: name, Dir: dir, Kind: store.Headless,
		ClaudeSessionID: sessionID, Turns: turns,
	}
	if err := h.Store.Put(s); err != nil {
		h.t.Fatal(err)
	}
	return s
}

func answers(sessionID, answer string) claude.Runner {
	return func(context.Context, string, []string, []string) ([]byte, error) {
		return []byte(result(sessionID, answer, 1)), nil

	}
}

// ---------- auth ----------

func TestHealthzNeedsNoKey(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	r := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
}

func TestHealthzSaysNothingAboutTheKey(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	r := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), testKey) {
		t.Error("the health check leaked the API key")
	}
}

func TestEveryOtherRouteRejectsAMissingKey(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	routes := []struct{ method, path string }{
		{"POST", "/auth/rotate"},
		{"POST", "/sessions"},
		{"GET", "/sessions"},
		{"GET", "/sessions/x"},
		{"DELETE", "/sessions/x"},
		{"POST", "/sessions/x/query"},
		{"POST", "/sessions/x/command"},
		{"PUT", "/sessions/x/skills/y"},
		{"GET", "/jobs/j_1"},
		{"DELETE", "/jobs/j_1"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			r := httptest.NewRequest(rt.method, rt.path, strings.NewReader("{}"))
			w := httptest.NewRecorder()
			h.Handler().ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("code = %d, want 401", w.Code)
			}
		})
	}
}

func TestAWrongKeyIsRejected(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	for _, k := range []string{"", "Bearer ", "Bearer wrong", "cbx_live_wrong", "Basic " + testKey} {
		r := httptest.NewRequest("GET", "/sessions", nil)
		r.Header.Set("Authorization", k)
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("header %q: code = %d, want 401", k, w.Code)
		}
	}
}

func TestRotateReturnsAKeyThatWorksAndKillsTheOld(t *testing.T) {
	h := answering(t, answers("s", "hi"))

	w := h.do("POST", "/auth/rotate", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", w.Code, w.Body)
	}
	newKey, _ := h.json(w)["api_key"].(string)
	if newKey == "" || newKey == testKey {
		t.Fatalf("rotate returned %q", newKey)
	}

	// The old key dies immediately: the reason to rotate is usually that it
	// should already have stopped working.
	old := httptest.NewRequest("GET", "/sessions", nil)
	old.Header.Set("Authorization", "Bearer "+testKey)
	ow := httptest.NewRecorder()
	h.Handler().ServeHTTP(ow, old)
	if ow.Code != http.StatusUnauthorized {
		t.Errorf("the old key still works: %d", ow.Code)
	}

	fresh := httptest.NewRequest("GET", "/sessions", nil)
	fresh.Header.Set("Authorization", "Bearer "+newKey)
	fw := httptest.NewRecorder()
	h.Handler().ServeHTTP(fw, fresh)
	if fw.Code != http.StatusOK {
		t.Errorf("the new key does not work: %d", fw.Code)
	}
}

// A key naming a role nobody defined is refused rather than defaulted. The
// fallback would be exactly the boundary somebody meant to tighten.
func TestAKeyWithAnUndefinedRoleIsRefused(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	value, err := h.Store.AddKey("stray", "no-such-role")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/sessions", nil)
	r.Header.Set("Authorization", "Bearer "+value)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no-such-role") {
		t.Errorf("the error does not name the missing role: %s", w.Body)
	}
}

// The role decides the session's permission mode, and the caller cannot name
// one: a caller able to choose could choose the one that ignores every rule.
func TestTheRoleDecidesTheSessionsPermissionMode(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.do("POST", "/sessions", map[string]any{
		"name": "moded", "permission_mode": "bypassPermissions",
	})
	got, _ := h.Store.Get("moded")
	if got == nil || got.PermissionMode != claude.AcceptEdits {
		t.Fatalf("permission_mode = %v, want the role's %q", got, claude.AcceptEdits)
	}
}

// ---------- sessions ----------

func TestCreateMakesAHeadlessSession(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	w := h.do("POST", "/sessions", map[string]any{"name": "api-work"})
	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	got := h.json(w)
	if got["kind"] != store.Headless {
		t.Errorf("kind = %v", got["kind"])
	}
	if got["session_id"] == "" || got["session_id"] == nil {
		t.Error("no conversation id was allocated")
	}
	if got["turns"] != float64(0) {
		t.Errorf("turns = %v, want 0 — the conversation is created by the first query", got["turns"])
	}
}

func TestCreateStoresThePermissionMode(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.do("POST", "/sessions", map[string]any{"name": "pm", "permission_mode": claude.AcceptEdits})
	got, _ := h.Store.Get("pm")
	if got == nil || got.PermissionMode != claude.AcceptEdits {
		t.Fatalf("stored = %v", got)
	}
}

func TestCreateRejectsABadName(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	for _, n := range []string{"", "../escape", "has space", "-leading", strings.Repeat("x", 65)} {
		w := h.do("POST", "/sessions", map[string]any{"name": n})
		if w.Code != http.StatusBadRequest {
			t.Errorf("name %q: code = %d, want 400", n, w.Code)
		}
	}
}

func TestCreateRefusesADuplicate(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.do("POST", "/sessions", map[string]any{"name": "dup"})
	if w := h.do("POST", "/sessions", map[string]any{"name": "dup"}); w.Code != http.StatusConflict {
		t.Errorf("code = %d, want 409", w.Code)
	}
}

func TestListReportsBothKinds(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.headlessSession("headless-one", "uuid-1", 0)
	h.Store.Put(store.Session{Name: "phone", Dir: "/workspace/phone", Kind: store.Interactive})

	got := h.json(h.do("GET", "/sessions", nil))
	list, _ := got["sessions"].([]any)
	if len(list) != 2 {
		t.Fatalf("listed %d sessions, want 2: %s", len(list), h.do("GET", "/sessions", nil).Body)
	}
	kinds := map[string]bool{}
	for _, s := range list {
		kinds[s.(map[string]any)["kind"].(string)] = true
	}
	if !kinds[store.Headless] || !kinds[store.Interactive] {
		t.Errorf("kinds = %v — what is on this box is one question", kinds)
	}
}

func TestMutatingAnInteractiveSessionIsRefused(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.Store.Put(store.Session{Name: "phone", Dir: "/workspace/phone", Kind: store.Interactive})

	cases := []struct {
		method, path string
		body         any
	}{
		{"POST", "/sessions/phone/query", map[string]any{"prompt": "hi", "respond_within": "1s"}},
		{"POST", "/sessions/phone/command", map[string]any{"command": "/clear"}},
		{"DELETE", "/sessions/phone", nil},
	}
	for _, c := range cases {
		w := h.do(c.method, c.path, c.body)
		if w.Code != http.StatusConflict {
			t.Errorf("%s %s: code = %d, want 409", c.method, c.path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "Remote Control") {
			t.Errorf("%s %s: error does not say where to do it: %s", c.method, c.path, w.Body)
		}
	}
	// Reading it is still fine.
	if w := h.do("GET", "/sessions/phone", nil); w.Code != http.StatusOK {
		t.Errorf("GET on an interactive session: %d", w.Code)
	}
}

func TestDeleteRemovesTheRowAndTheTranscript(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	sess := h.headlessSession("gone", "uuid-gone", 1)

	transcript := claude.TranscriptPath(h.Home, sess.Dir, sess.ClaudeSessionID)
	os.MkdirAll(filepath.Dir(transcript), 0o755)
	os.WriteFile(transcript, []byte("{}\n"), 0o644)

	if w := h.do("DELETE", "/sessions/gone", nil); w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	if got, _ := h.Store.Get("gone"); got != nil {
		t.Error("the row survived")
	}
	if _, err := os.Stat(transcript); err == nil {
		t.Error("the transcript survived — closing a session should not leave its history readable")
	}
}

func TestDeleteLeavesTheProjectDirectory(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	sess := h.headlessSession("keep-work", "uuid-keep", 0)
	os.WriteFile(filepath.Join(sess.Dir, "work.txt"), []byte("mine"), 0o644)

	h.do("DELETE", "/sessions/keep-work", nil)
	if _, err := os.Stat(filepath.Join(sess.Dir, "work.txt")); err != nil {
		t.Error("deleting a session deleted the work — the transcript is the session, the directory is the work")
	}
}

// The system prompt is set when the session is created and never after —
// Claude Code snapshots it on the conversation's first request, so there is
// nothing to change it with. See the note in sessions.go.
func TestSystemPromptIsStoredAtCreation(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.do("POST", "/sessions", map[string]any{"name": "sp", "system_prompt": "be terse"})
	got, _ := h.Store.Get("sp")
	if got == nil || got.SystemPrompt != "be terse" {
		t.Errorf("stored = %v", got)
	}
}

func TestTheSystemPromptReachesEveryQuery(t *testing.T) {
	var seen string
	h := answering(t, func(_ context.Context, _ string, args []string, _ []string) ([]byte, error) {
		seen = strings.Join(args, " ")
		id := ""
		for i, a := range args {
			if a == "--session-id" || a == "--resume" {
				id = args[i+1]
			}
		}
		return []byte(result(id, "ok", 1)), nil

	})
	h.do("POST", "/sessions", map[string]any{"name": "spq", "system_prompt": "be terse"})
	h.do("POST", "/sessions/spq/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	if !strings.Contains(seen, "--append-system-prompt be terse") {
		t.Errorf("argv %q does not carry the session's system prompt", seen)
	}
}

// ---------- skills ----------

func TestSkillIsWrittenUnderTheSessionDirectory(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	sess := h.headlessSession("sk", "uuid-sk", 0)

	w := h.do("PUT", "/sessions/sk/skills/my-skill", "---\nname: my-skill\n---\nbody\n")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	path := filepath.Join(sess.Dir, ".claude", "skills", "my-skill", "SKILL.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skill not written: %v", err)
	}
	if !strings.Contains(string(got), "body") {
		t.Errorf("content = %q", got)
	}
}

func TestSkillNameIsValidatedNotSanitised(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.headlessSession("sk2", "uuid-sk2", 0)
	// A filesystem write driven by a network request. Quoting defends a shell
	// and does nothing about "..".
	for _, bad := range []string{"..", "..%2fx", "a_b", "UPPER", "-lead", ""} {
		w := h.do("PUT", "/sessions/sk2/skills/"+bad, "x")
		if w.Code == http.StatusOK {
			t.Errorf("skill name %q was accepted", bad)
		}
	}
}

func TestCreateStoresModelAndEffort(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	h.do("POST", "/sessions", map[string]any{
		"name": "me", "model": "opus[1m]", "effort": claude.XHigh,
	})
	got, _ := h.Store.Get("me")
	if got == nil || got.Model != "opus[1m]" || got.Effort != claude.XHigh {
		t.Fatalf("stored = %+v", got)
	}
}

func TestCreateRejectsABadEffort(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	cases := []struct {
		effort string
		want   int
	}{
		{claude.Low, http.StatusCreated}, {claude.Max, http.StatusCreated},
		{"", http.StatusCreated}, {"extreme", http.StatusBadRequest},
	}
	for i, c := range cases {
		w := h.do("POST", "/sessions", map[string]any{
			"name": fmt.Sprintf("ef%d", i), "effort": c.effort,
		})
		if w.Code != c.want {
			t.Errorf("effort %q: code = %d, want %d (%s)", c.effort, w.Code, c.want, w.Body)
		}
	}
}

func TestCreateRejectsAModelThatWouldBeReadAsAFlag(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	for i, bad := range []string{"--dangerously-skip-permissions", "-p", "opus --effort max"} {
		w := h.do("POST", "/sessions", map[string]any{
			"name": fmt.Sprintf("md%d", i), "model": bad,
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("model %q: code = %d, want 400", bad, w.Code)
		}
	}
}

func TestTheSessionsModelAndEffortReachTheQuery(t *testing.T) {
	var seen string
	h := answering(t, func(_ context.Context, _ string, args []string, _ []string) ([]byte, error) {
		seen = strings.Join(args, " ")
		return []byte(result("uuid-m", "ok", 1)), nil

	})
	sess := h.headlessSession("mq", "uuid-m", 1)
	sess.Model, sess.Effort = "sonnet", claude.High
	h.Store.Put(sess)

	h.do("POST", "/sessions/mq/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	for _, want := range []string{"--model sonnet", "--effort high"} {
		if !strings.Contains(seen, want) {
			t.Errorf("argv %q missing %q — the session's choice did not reach Claude", seen, want)
		}
	}
}

// recorderFor sends a request with no Authorization header.
func recorderFor(h *harness, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	return w
}
