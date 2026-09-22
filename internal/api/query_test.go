package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/commandspec"
	"github.com/vutran1710/claudebox/internal/store"
)

// respond_within is tested against a runner that actually stalls, not a fake
// clock. The behaviour under test is a race between a query and a deadline,
// and a mocked clock would assert the mock rather than the race.

// stalling returns a runner that blocks until release is closed, then answers.
func stalling(sessionID, answer string, release <-chan struct{}) claude.Runner {
	return func(ctx context.Context, _ string, _ []string) ([]byte, error) {
		select {
		case <-release:
			return []byte(result(sessionID, answer, 1)), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func TestQueryRequiresRespondWithin(t *testing.T) {
	h := answering(t, answers("uuid-q", "hello"))
	h.headlessSession("q", "uuid-q", 1)

	w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 — there is no safe default", w.Code)
	}
	if !strings.Contains(w.Body.String(), "respond_within") {
		t.Errorf("the error does not name the field: %s", w.Body)
	}
}

func TestRespondWithinAcceptsSecondsAndDurations(t *testing.T) {
	for _, given := range []any{90, "90", "90s", "1m30s", 90.0} {
		t.Run(fmt.Sprint(given), func(t *testing.T) {
			h := answering(t, answers("uuid-q", "hello"))
			h.headlessSession("q", "uuid-q", 1)
			w := h.do("POST", "/sessions/q/query", map[string]any{
				"prompt": "hi", "respond_within": given,
			})
			if w.Code != http.StatusOK {
				t.Fatalf("code = %d: %s", w.Code, w.Body)
			}
		})
	}
}

func TestRespondWithinAboveTheCeilingIsRefused(t *testing.T) {
	h := answering(t, answers("uuid-q", "hello"))
	h.headlessSession("q", "uuid-q", 1)

	w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": "20m"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 — a silent clamp does something other than what was asked", w.Code)
	}
	if !strings.Contains(w.Body.String(), MaxRespondWithin.String()) {
		t.Errorf("the error does not name the maximum: %s", w.Body)
	}
	// And nothing ran.
	if job, _ := h.Store.RunningJob("q"); job != nil {
		t.Error("a refused request still started a query")
	}
}

func TestRespondWithinRejectsNonsense(t *testing.T) {
	h := answering(t, answers("uuid-q", "hello"))
	h.headlessSession("q", "uuid-q", 1)
	for _, bad := range []any{"soon", "-5s", true} {
		w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": bad})
		if w.Code != http.StatusBadRequest {
			t.Errorf("respond_within %v: code = %d, want 400", bad, w.Code)
		}
	}
}

func TestAnAnswerInsideTheWindowReturns200(t *testing.T) {
	h := answering(t, answers("uuid-q", "the answer"))
	h.headlessSession("q", "uuid-q", 1)

	w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	got := h.json(w)
	if got["answer"] != "the answer" {
		t.Errorf("answer = %v", got["answer"])
	}
	if got["session_id"] != "uuid-q" {
		t.Errorf("session_id = %v", got["session_id"])
	}
}

func TestZeroRespondWithinReturnsAJobImmediately(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := answering(t, stalling("uuid-q", "late", release))
	h.headlessSession("q", "uuid-q", 1)

	start := time.Now()
	w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": 0})
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202: %s", w.Code, w.Body)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waited %s for a zero window", elapsed)
	}
	if h.json(w)["job"] == nil {
		t.Error("no job id returned")
	}
}

func TestAnAnswerOutsideTheWindowReturns202AndKeepsRunning(t *testing.T) {
	release := make(chan struct{})
	h := answering(t, stalling("uuid-q", "eventually", release))
	h.headlessSession("q", "uuid-q", 1)

	w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": "100ms"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202: %s", w.Code, w.Body)
	}
	id, _ := h.json(w)["job"].(string)

	// The window closing does not stop the work. That is the whole reason the
	// field is not called "timeout".
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, _ := h.Store.JobByID(id)
		if job != nil && job.Status == store.Done {
			if job.Answer != "eventually" {
				t.Errorf("answer = %q", job.Answer)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the query did not finish after its window closed — the answer was lost")
}

func TestASecondQueryOnABusySessionIsRefused(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := answering(t, stalling("uuid-q", "x", release))
	h.headlessSession("q", "uuid-q", 1)

	if w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "one", "respond_within": 0}); w.Code != http.StatusAccepted {
		t.Fatalf("first query: %d %s", w.Code, w.Body)
	}
	w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "two", "respond_within": 0})
	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409 — two --resume processes would interleave turns", w.Code)
	}
}

func TestJobPollReturnsRunningThenDone(t *testing.T) {
	release := make(chan struct{})
	h := answering(t, stalling("uuid-q", "done now", release))
	h.headlessSession("q", "uuid-q", 1)

	id, _ := h.json(h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": 0}))["job"].(string)

	got := h.json(h.do("GET", "/jobs/"+id, nil))
	if got["status"] != store.Running {
		t.Fatalf("status = %v, want running", got["status"])
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got = h.json(h.do("GET", "/jobs/"+id, nil))
		if got["status"] == store.Done {
			if got["answer"] != "done now" {
				t.Errorf("answer = %v", got["answer"])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job never reached done: %v", got)
}

func TestJobLongPollWaitsForCompletion(t *testing.T) {
	release := make(chan struct{})
	h := answering(t, stalling("uuid-q", "waited for", release))
	h.headlessSession("q", "uuid-q", 1)

	id, _ := h.json(h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": 0}))["job"].(string)
	go func() {
		time.Sleep(300 * time.Millisecond)
		close(release)
	}()

	got := h.json(h.do("GET", "/jobs/"+id+"?respond_within=5s", nil))
	if got["status"] != store.Done {
		t.Fatalf("long poll returned %v before the answer existed", got["status"])
	}
	if got["answer"] != "waited for" {
		t.Errorf("answer = %v", got["answer"])
	}
}

func TestAnUnknownJobIs404(t *testing.T) {
	h := answering(t, answers("uuid-q", "x"))
	if w := h.do("GET", "/jobs/j_nope", nil); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

func TestDeletingARunningJobCancelsIt(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := answering(t, stalling("uuid-q", "never", release))
	h.headlessSession("q", "uuid-q", 1)

	id, _ := h.json(h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": 0}))["job"].(string)

	w := h.do("DELETE", "/jobs/"+id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	job, _ := h.Store.JobByID(id)
	if job == nil || job.Status != store.Cancelled {
		t.Fatalf("status = %v, want cancelled", job)
	}
	// Cancelling frees the session: a SIGKILLed claude -p leaves a transcript
	// --resume continues from, so there is nothing to quarantine.
	if w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "again", "respond_within": 0}); w.Code != http.StatusAccepted {
		t.Errorf("session still busy after cancelling: %d %s", w.Code, w.Body)
	}
}

func TestDeletingAFinishedJobDiscardsIt(t *testing.T) {
	h := answering(t, answers("uuid-q", "hello"))
	h.headlessSession("q", "uuid-q", 1)

	id, _ := h.json(h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": "10s"}))["job"].(string)
	if w := h.do("DELETE", "/jobs/"+id, nil); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w := h.do("GET", "/jobs/"+id, nil); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404 after discarding", w.Code)
	}
}

func TestDeletingAnAbsentJobSucceeds(t *testing.T) {
	h := answering(t, answers("uuid-q", "x"))
	if w := h.do("DELETE", "/jobs/j_never", nil); w.Code != http.StatusOK {
		t.Errorf("code = %d — already gone is the outcome the caller asked for", w.Code)
	}
}

func TestDeletingASessionCancelsItsQuery(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := answering(t, stalling("uuid-q", "never", release))
	h.headlessSession("q", "uuid-q", 1)

	id, _ := h.json(h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": 0}))["job"].(string)
	if w := h.do("DELETE", "/sessions/q", nil); w.Code != http.StatusOK {
		t.Fatalf("delete session: %d %s", w.Code, w.Body)
	}
	job, _ := h.Store.JobByID(id)
	if job == nil || job.Status != store.Cancelled {
		t.Errorf("status = %v, want cancelled — the intent is that the session be gone", job)
	}
}

// ---------- turns ----------

func TestTheFirstQueryCreatesTheConversation(t *testing.T) {
	var seen []string
	h := answering(t, func(_ context.Context, _ string, args []string) ([]byte, error) {
		seen = strings.Split(strings.Join(args, " "), " ")
		return []byte(result("uuid-new", "hi", 1)), nil
	})
	h.headlessSession("fresh", "uuid-new", 0)

	h.do("POST", "/sessions/fresh/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	joined := strings.Join(seen, " ")
	if !strings.Contains(joined, "--session-id uuid-new") {
		t.Errorf("args %q do not create the conversation at turns=0", joined)
	}
}

func TestATurnThatReachedTheModelAdvancesTurns(t *testing.T) {
	h := answering(t, answers("uuid-q", "hello"))
	h.headlessSession("q", "uuid-q", 0)

	h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	got, _ := h.Store.Get("q")
	if got == nil || got.Turns != 1 {
		t.Fatalf("turns = %v, want 1", got)
	}
}

func TestALocallyHandledTurnDoesNotAdvanceTurns(t *testing.T) {
	// turns 0 and an empty answer: handled locally, no conversation created.
	// Counting it would make the next query resume one that is not there.
	h := answering(t, func(context.Context, string, []string) ([]byte, error) {
		return []byte(result("uuid-q", "", 0)), nil
	})
	h.headlessSession("q", "uuid-q", 0)

	h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	got, _ := h.Store.Get("q")
	if got == nil || got.Turns != 0 {
		t.Fatalf("turns = %v, want 0", got)
	}
}

func TestAForkedSessionIdIsReportedNotSwallowed(t *testing.T) {
	// Measured: a locally-handled command reports success while forking a new
	// conversation id, having done nothing to the one asked for.
	h := answering(t, func(context.Context, string, []string) ([]byte, error) {
		return []byte(result("918dc472-somewhere-else", "", 0)), nil
	})
	h.headlessSession("q", "uuid-q", 1)

	w := h.do("POST", "/sessions/q/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	if w.Code == http.StatusOK {
		t.Fatalf("a query that ran against another conversation reported success: %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), "did not apply") {
		t.Errorf("the error does not say what happened: %s", w.Body)
	}
}

// ---------- commands ----------

func writeSpec(t *testing.T, h *harness, body string) {
	t.Helper()
	if err := os.WriteFile(h.SpecPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestACommandNotInTheSpecIsRefused(t *testing.T) {
	h := answering(t, answers("uuid-q", "x"))
	h.headlessSession("q", "uuid-q", 1)
	writeSpec(t, h, "version: 1\ncommands:\n  - name: /compact\n    effect: forward\n")

	w := h.do("POST", "/sessions/q/command", map[string]any{"command": "/definitely-not-allowed", "respond_within": "5s"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 — deny by default", w.Code)
	}
}

func TestRequiresArgsIsEnforcedOverHTTP(t *testing.T) {
	h := answering(t, answers("uuid-q", "set"))
	h.headlessSession("q", "uuid-q", 1)
	writeSpec(t, h, "version: 1\ncommands:\n  - name: /model\n    effect: forward\n    requires_args: true\n")

	if w := h.do("POST", "/sessions/q/command", map[string]any{"command": "/model", "respond_within": "5s"}); w.Code != http.StatusBadRequest {
		t.Errorf("bare /model: code = %d, want 400 — it only prints its usage", w.Code)
	}
	if w := h.do("POST", "/sessions/q/command", map[string]any{"command": "/model sonnet", "respond_within": "5s"}); w.Code != http.StatusOK {
		t.Errorf("/model sonnet: code = %d", w.Code)
	}
}

func TestClearRotatesTheConversationInsteadOfForwarding(t *testing.T) {
	ran := false
	h := answering(t, func(context.Context, string, []string) ([]byte, error) {
		ran = true
		return []byte(result("forked", "", 0)), nil
	})
	sess := h.headlessSession("q", "uuid-before", 4)
	writeSpec(t, h, "version: 1\ncommands:\n  - name: /clear\n    effect: rotate-session\n")

	w := h.do("POST", "/sessions/q/command", map[string]any{"command": "/clear"})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	if ran {
		t.Error("forwarded /clear to Claude — measured: that reports success and clears nothing")
	}
	got, _ := h.Store.Get("q")
	if got == nil || got.ClaudeSessionID == sess.ClaudeSessionID {
		t.Fatalf("conversation id unchanged: %v", got)
	}
	if got.Turns != 0 {
		t.Errorf("turns = %d, want 0 — the new conversation does not exist yet", got.Turns)
	}
}

func TestAMalformedSpecRefusesRatherThanFallingBack(t *testing.T) {
	h := answering(t, answers("uuid-q", "x"))
	h.headlessSession("q", "uuid-q", 1)
	writeSpec(t, h, "commands: [broken: [")

	w := h.do("POST", "/sessions/q/command", map[string]any{"command": "/clear"})
	if w.Code == http.StatusOK {
		t.Error("a malformed spec silently became the default — a policy nobody chose")
	}
}

func TestSpecIsRereadPerRequest(t *testing.T) {
	h := answering(t, answers("uuid-q", "ok"))
	h.headlessSession("q", "uuid-q", 1)
	writeSpec(t, h, "version: 1\ncommands:\n  - name: /compact\n    effect: forward\n")

	if w := h.do("POST", "/sessions/q/command", map[string]any{"command": "/context", "respond_within": "5s"}); w.Code != http.StatusBadRequest {
		t.Fatalf("expected /context to be denied first: %d", w.Code)
	}
	// Allowing a command is an edit, not a release.
	writeSpec(t, h, "version: 1\ncommands:\n  - name: /context\n    effect: forward\n")
	if w := h.do("POST", "/sessions/q/command", map[string]any{"command": "/context", "respond_within": "5s"}); w.Code != http.StatusOK {
		t.Errorf("the spec was not re-read: %d", w.Code)
	}
}

func TestTheShippedSpecAllowsClearByRotation(t *testing.T) {
	spec, err := commandspec.Parse(commandspec.Default())
	if err != nil {
		t.Fatal(err)
	}
	c, err := spec.Resolve("/clear")
	if err != nil || c.Effect != commandspec.RotateSession {
		t.Fatalf("/clear = %v, %v", c, err)
	}
}
