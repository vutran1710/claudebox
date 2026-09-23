package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vutran1710/claudebox/internal/store"
)

// A skill costs a real turn — one measured at twenty seconds of API time — so
// priming is slow by nature, and the ways it can quietly not happen are what
// these cover.

func TestCreateWithSkillsInvokesThemInOrder(t *testing.T) {
	var got []string
	h := answering(t, func(_ context.Context, _ string, args []string) ([]byte, error) {
		prompt := args[len(args)-1]
		got = append(got, prompt)
		return []byte(result("uuid-p", "loaded "+prompt, 1)), nil
	})
	// The created session allocates its own uuid, so the fake must echo it.
	h.Claude = h.Claude.WithRunner(func(_ context.Context, _ string, args []string) ([]byte, error) {
		prompt := args[len(args)-1]
		got = append(got, prompt)
		id := ""
		for i, a := range args {
			if a == "--session-id" || a == "--resume" {
				id = args[i+1]
			}
		}
		return []byte(result(id, "loaded "+prompt, 1)), nil
	})

	w := h.do("POST", "/sessions", map[string]any{
		"name": "primed", "skills": []string{"inline", "testing"}, "respond_within": "30s",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	if strings.Join(got, ",") != "/inline,/testing" {
		t.Errorf("invoked %v, want /inline then /testing in order", got)
	}
	priming, _ := h.json(w)["priming"].(map[string]any)
	if priming == nil || priming["status"] != store.Done {
		t.Fatalf("priming = %v", priming)
	}
}

// The measured trap: an unknown skill answers "Unknown command:" with
// is_error false and turns 0. Nothing about the exit code says it failed.
func TestAnUnknownSkillIsReportedNotSwallowed(t *testing.T) {
	h := answering(t, func(_ context.Context, _ string, args []string) ([]byte, error) {
		id := ""
		for i, a := range args {
			if a == "--session-id" || a == "--resume" {
				id = args[i+1]
			}
		}
		return []byte(result(id, "Unknown command: /nope", 0)), nil
	})

	w := h.do("POST", "/sessions", map[string]any{
		"name": "bad-skill", "skills": []string{"nope"}, "respond_within": "30s",
	})
	// The session is created — it exists — but priming must not claim success.
	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	priming, _ := h.json(w)["priming"].(map[string]any)
	if priming == nil || priming["status"] != store.Failed {
		t.Fatalf("priming = %v, want failed — the skill does not exist", priming)
	}
	if !strings.Contains(fmt.Sprint(priming["error"]), "not installed") {
		t.Errorf("error does not say what happened: %v", priming["error"])
	}
}

func TestPrimingStopsAtTheFirstMissingSkill(t *testing.T) {
	var calls atomic.Int64
	h := answering(t, func(_ context.Context, _ string, args []string) ([]byte, error) {
		n := calls.Add(1)
		prompt := args[len(args)-1]
		id := ""
		for i, a := range args {
			if a == "--session-id" || a == "--resume" {
				id = args[i+1]
			}
		}
		if n == 2 {
			return []byte(result(id, "Unknown command: "+prompt, 0)), nil
		}
		return []byte(result(id, "loaded", 1)), nil
	})

	w := h.do("POST", "/sessions", map[string]any{
		"name": "partial", "skills": []string{"one", "missing", "three"}, "respond_within": "30s",
	})
	priming, _ := h.json(w)["priming"].(map[string]any)
	if priming["status"] != store.Failed {
		t.Fatalf("priming = %v", priming)
	}
	// A session primed with half of what was asked for is worse than one that
	// says which half failed.
	if calls.Load() != 2 {
		t.Errorf("ran %d skills, want 2 — it should stop at the missing one", calls.Load())
	}
	invoked, _ := priming["invoked"].([]any)
	if len(invoked) != 1 || invoked[0] != "one" {
		t.Errorf("invoked = %v, want just the one that worked", invoked)
	}
}

func TestSkillsRequireRespondWithin(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	w := h.do("POST", "/sessions", map[string]any{"name": "nowait", "skills": []string{"inline"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 — priming runs turns and there is no safe default", w.Code)
	}
	if !strings.Contains(w.Body.String(), "respond_within") {
		t.Errorf("the error does not name the field: %s", w.Body)
	}
}

func TestNoSkillsNeedsNoRespondWithin(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	if w := h.do("POST", "/sessions", map[string]any{"name": "plain"}); w.Code != http.StatusCreated {
		t.Errorf("code = %d — without skills there is nothing slow to wait for", w.Code)
	}
}

func TestPrimingThatOutrunsTheWindowReturnsAJob(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := answering(t, stalling("uuid-slow", "eventually", release))

	w := h.do("POST", "/sessions", map[string]any{
		"name": "slow", "skills": []string{"inline"}, "respond_within": "100ms",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	priming, _ := h.json(w)["priming"].(map[string]any)
	if priming["status"] != store.Running {
		t.Fatalf("priming = %v, want running", priming)
	}
	if priming["poll"] == nil || priming["job"] == nil {
		t.Errorf("no way to follow it up: %v", priming)
	}
}

func TestSkillNamesAreValidated(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	for i, bad := range []string{"../escape", "Has-Caps", "has space", "-lead", ""} {
		w := h.do("POST", "/sessions", map[string]any{
			"name": fmt.Sprintf("sv%d", i), "skills": []string{bad}, "respond_within": "10s",
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("skill %q: code = %d, want 400", bad, w.Code)
		}
	}
}

func TestPluginSkillsAreAccepted(t *testing.T) {
	// A skill from a plugin is addressed as plugin:skill.
	if !ValidSkillRef("my-plugin:some-skill") {
		t.Error("a plugin skill reference was rejected")
	}
}

func TestPrimingAdvancesTurnsSoTheNextQueryResumes(t *testing.T) {
	h := answering(t, func(_ context.Context, _ string, args []string) ([]byte, error) {
		id := ""
		for i, a := range args {
			if a == "--session-id" || a == "--resume" {
				id = args[i+1]
			}
		}
		return []byte(result(id, "loaded", 1)), nil
	})
	h.do("POST", "/sessions", map[string]any{
		"name": "turns", "skills": []string{"inline"}, "respond_within": "30s",
	})
	got, _ := h.Store.Get("turns")
	if got == nil || got.Turns != 1 {
		t.Fatalf("turns = %v, want 1 — the next query must resume, not recreate", got)
	}
}

// Priming holds the session's one running-query slot, so a query sent before
// it finishes is refused. Pinned here because it is a consequence of the
// design rather than an accident: creation returning a job means the session
// exists before it is usable, and a caller needs to know that is deliberate.
func TestAQueryDuringPrimingIsRefused(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := answering(t, stalling("uuid-slow", "later", release))

	w := h.do("POST", "/sessions", map[string]any{
		"name": "still-priming", "skills": []string{"inline"}, "respond_within": "50ms",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	priming, _ := h.json(w)["priming"].(map[string]any)
	if priming["status"] != store.Running {
		t.Fatalf("expected priming to still be running: %v", priming)
	}

	q := h.do("POST", "/sessions/still-priming/query", map[string]any{
		"prompt": "hi", "respond_within": "1s",
	})
	if q.Code != http.StatusConflict {
		t.Fatalf("query during priming = %d, want 409", q.Code)
	}
	// The message has to be usable, because this is the one place a caller
	// meets it without having started a query themselves.
	if !strings.Contains(q.Body.String(), "already has a query running") {
		t.Errorf("error does not explain the wait: %s", q.Body)
	}
}

// A session created with skills that fail is still a session. The 201 is
// honest about both halves: it exists, and priming did not happen.
func TestASessionSurvivesFailedPriming(t *testing.T) {
	h := answering(t, func(_ context.Context, _ string, args []string) ([]byte, error) {
		id := ""
		for i, a := range args {
			if a == "--session-id" || a == "--resume" {
				id = args[i+1]
			}
		}
		return []byte(result(id, "Unknown command: /nope", 0)), nil
	})
	h.do("POST", "/sessions", map[string]any{
		"name": "half", "skills": []string{"nope"}, "respond_within": "30s",
	})
	got, _ := h.Store.Get("half")
	if got == nil {
		t.Fatal("the session was rolled back — priming failing is not creation failing")
	}
	// And it is usable: the failed priming job is finished, so the slot is free.
	if w := h.do("POST", "/sessions/half/query", map[string]any{"prompt": "hi", "respond_within": "10s"}); w.Code == http.StatusConflict {
		t.Error("the session stayed locked after priming failed")
	}
}
