package api

// A session's env: set once at creation from the names the caller's role permits, carried
// to every turn's claude process, and shown by name only; a query adds its role's per-turn
// names for that turn alone, on top of the box's own env from its config. Storage is store_test.go's.

import (
	"context"
	"net/http"
	"os"
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

func TestAQuerySetsOnlyItsRolesPerTurnNames(t *testing.T) {
	cases := []struct {
		name     string
		turn     map[string]string
		wantCode int
		wantEnv  []string
	}{
		{"a name the role lists under turn_env", map[string]string{"KOIDRA_RUN_KEY": "rk_1"}, http.StatusOK, []string{"KOIDRA_ORG_ID=12", "KOIDRA_RUN_KEY=rk_1"}},
		{"a name fixed when the session was created", map[string]string{"KOIDRA_ORG_ID": "13"}, http.StatusBadRequest, nil},
		{"a name not under turn_env", map[string]string{"KOIDRA_SECRET": "x"}, http.StatusBadRequest, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var seen [][]string
			h := answering(t, capturingEnv(&seen))
			h.do("POST", "/sessions", map[string]any{"name": "env-turn", "env": map[string]string{"KOIDRA_ORG_ID": "12"}})

			w := h.do("POST", "/sessions/env-turn/query", map[string]any{"prompt": "hi", "respond_within": "10s", "env": c.turn})

			sess, _ := h.Store.Get("env-turn")
			if w.Code != c.wantCode || (c.wantEnv != nil && (len(seen) != 1 || !slices.Equal(seen[0], c.wantEnv))) || (c.wantEnv == nil && len(seen) != 0) {
				t.Errorf("got %d with turns %q, want %d with %q", w.Code, seen, c.wantCode, c.wantEnv)
			}
			if sess == nil || len(sess.Env) != 1 || sess.Env["KOIDRA_ORG_ID"] != "12" {
				t.Errorf("the session's env became %v", sess)
			}
		})
	}
}

func TestAPerTurnValueDoesNotOutliveItsTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var seen [][]string
	h := answering(t, capturingEnv(&seen))
	h.do("POST", "/sessions", map[string]any{"name": "env-once", "env": map[string]string{"KOIDRA_ORG_ID": "12"}})
	h.do("POST", "/sessions/env-once/query", map[string]any{"prompt": "hi", "respond_within": "10s", "env": map[string]string{"KOIDRA_RUN_KEY": "rk_1"}})

	h.do("POST", "/sessions/env-once/query", map[string]any{"prompt": "again", "respond_within": "10s"})

	if len(seen) != 2 || !slices.Equal(seen[1], []string{"KOIDRA_ORG_ID=12"}) {
		t.Errorf("turns ran with %q, want the second without KOIDRA_RUN_KEY", seen)
	}
}

func withBoxEnv(t *testing.T, h *harness, env string) {
	t.Helper()
	if err := os.WriteFile(h.ConfigPath, []byte(testConfig+"env:\n"+env), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestATurnsEnvLayersTheBoxTheSessionAndTheTurn(t *testing.T) {
	cases := []struct {
		name    string
		box     string
		session map[string]string
		turn    map[string]string
		want    []string
	}{
		{"only the box sets it", "  KOIDRA_API_URL: http://127.0.0.1:8017\n", map[string]string{"KOIDRA_ORG_ID": "2"}, nil,
			[]string{"KOIDRA_API_URL=http://127.0.0.1:8017", "KOIDRA_ORG_ID=2"}},
		{"the session's value wins over the box's", "  KOIDRA_ORG_ID: \"9\"\n", map[string]string{"KOIDRA_ORG_ID": "2"}, nil,
			[]string{"KOIDRA_ORG_ID=2"}},
		{"a per-turn name is added on top of both", "  KOIDRA_API_URL: http://127.0.0.1:8017\n", map[string]string{"KOIDRA_ORG_ID": "2"}, map[string]string{"KOIDRA_RUN_KEY": "rk_1"},
			[]string{"KOIDRA_API_URL=http://127.0.0.1:8017", "KOIDRA_ORG_ID=2", "KOIDRA_RUN_KEY=rk_1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var seen [][]string
			h := answering(t, capturingEnv(&seen))
			withBoxEnv(t, h, c.box)
			h.do("POST", "/sessions", map[string]any{"name": "env-layers", "env": c.session})

			h.do("POST", "/sessions/env-layers/query", map[string]any{"prompt": "hi", "respond_within": "10s", "env": c.turn})

			if len(seen) != 1 || !slices.Equal(seen[0], c.want) {
				t.Errorf("turn ran with %q, want %q", seen, c.want)
			}
		})
	}
}

func TestTheBoxsEnvIsReadEachTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var seen [][]string
	h := answering(t, capturingEnv(&seen))
	withBoxEnv(t, h, "  KOIDRA_API_URL: http://first\n")
	h.do("POST", "/sessions", map[string]any{"name": "env-reread"})
	h.do("POST", "/sessions/env-reread/query", map[string]any{"prompt": "hi", "respond_within": "10s"})

	withBoxEnv(t, h, "  KOIDRA_API_URL: http://second\n")
	h.do("POST", "/sessions/env-reread/query", map[string]any{"prompt": "again", "respond_within": "10s"})

	if len(seen) != 2 || !slices.Equal(seen[1], []string{"KOIDRA_API_URL=http://second"}) {
		t.Errorf("turns ran with %q, want the second to read the edited config", seen)
	}
}
