package api

// A session's env: set once at creation from the names the caller's role permits, carried
// to every turn's claude process, and shown by name only. Storage is store_test.go's.

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func capturingEnv(seen *[][]string) func(context.Context, string, []string, []string) ([]byte, error) {
	return func(_ context.Context, _ string, _ []string, env []string) ([]byte, error) {
		*seen = append(*seen, env)
		return []byte(result("", "ok", 1)), nil
	}
}

func TestASessionsEnvReachesEveryTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var seen [][]string
	h := answering(t, capturingEnv(&seen))
	h.do("POST", "/sessions", map[string]any{
		"name": "env-turns", "env": map[string]string{"KOIDRA_ORG_ID": "12", "KOIDRA_API_URL": "https://beta-api.koidra.ai"},
	})

	for range 2 {
		h.do("POST", "/sessions/env-turns/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
	}

	want := []string{"KOIDRA_API_URL=https://beta-api.koidra.ai", "KOIDRA_ORG_ID=12"}
	if len(seen) != 2 || !slices.Equal(seen[0], want) || !slices.Equal(seen[1], want) {
		t.Errorf("turns ran with %q, want %q twice", seen, want)
	}
}

func TestCreateSessionRefusesEnv(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"a name the role does not permit", map[string]string{"KOIDRA_SECRET": "x"}},
		{"a lowercase or dashed name", map[string]string{"koidra-org-id": "12"}},
		{"a value holding a newline or NUL", map[string]string{"KOIDRA_ORG_ID": "12\nPATH=/tmp"}},
		{"a name the box runs on", map[string]string{"PATH": "/tmp", "IS_SANDBOX": "0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			h := answering(t, answers("", "ok"))

			w := h.do("POST", "/sessions", map[string]any{"name": "env-refused", "env": c.env})

			if w.Code != http.StatusBadRequest {
				t.Fatalf("got %d %s, want 400", w.Code, w.Body)
			}
			if sess, _ := h.Store.Get("env-refused"); sess != nil {
				t.Errorf("a refused session was recorded")
			}
		})
	}
}

func TestASessionShowsItsEnvNamesNeverValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := answering(t, answers("", "ok"))
	h.do("POST", "/sessions", map[string]any{"name": "env-shown", "env": map[string]string{"KOIDRA_ORG_ID": "4242"}})

	w := h.do("GET", "/sessions/env-shown", nil)

	if !strings.Contains(w.Body.String(), `"env":["KOIDRA_ORG_ID"]`) || strings.Contains(w.Body.String(), "4242") {
		t.Errorf("GET /sessions/env-shown = %s, want the name and not the value", w.Body)
	}
}

func TestAQueryCannotChangeTheEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var seen [][]string
	h := answering(t, capturingEnv(&seen))
	h.do("POST", "/sessions", map[string]any{"name": "env-fixed", "env": map[string]string{"KOIDRA_ORG_ID": "12"}})

	w := h.do("POST", "/sessions/env-fixed/query", map[string]any{
		"prompt": "hi", "respond_within": "10s", "env": map[string]string{"KOIDRA_ORG_ID": "13"},
	})

	if w.Code != http.StatusBadRequest || len(seen) != 0 {
		t.Errorf("got %d with %d turns run, want 400 and none", w.Code, len(seen))
	}
}
