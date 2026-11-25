package api

// The box's env map: named entries written through the API into envars.yaml, listed by
// their variable names only, and taken whole by a session created with env_from, its
// values copied into that session's env at creation. The file itself is boxconfig's.

import (
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestTheEnvMapIsEditedThroughTheAPI(t *testing.T) {
	cases := []struct {
		name  string
		edits func(h *harness)
		want  map[string]any
	}{
		{"an entry put is listed by its variable names, never values", func(h *harness) {
			h.do("PUT", "/envs/preview.1234", map[string]string{"API_URL": "https://api-1234.example.com", "TENANT_ID": "7"})
		}, map[string]any{"preview.1234": []any{"API_URL", "TENANT_ID"}}},
		{"putting it again replaces it", func(h *harness) {
			h.do("PUT", "/envs/stg", map[string]string{"API_URL": "https://old.example.com", "TENANT_ID": "7"})
			h.do("PUT", "/envs/stg", map[string]string{"API_URL": "https://stg.example.com"})
		}, map[string]any{"stg": []any{"API_URL"}}},
		{"a deleted entry is gone", func(h *harness) {
			h.do("PUT", "/envs/prod", map[string]string{"API_URL": "https://api.example.com"})
			h.do("PUT", "/envs/stg", map[string]string{"API_URL": "https://stg.example.com"})
			h.do("DELETE", "/envs/prod", nil)
		}, map[string]any{"stg": []any{"API_URL"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := answering(t, answers("", "ok"))
			config, _ := os.ReadFile(h.ConfigPath)

			c.edits(h)
			w := h.do("GET", "/envs", nil)

			if got := h.json(w)["envs"]; !reflect.DeepEqual(got, c.want) || strings.Contains(w.Body.String(), "example.com") {
				t.Errorf("GET /envs = %s, want envs %v and no values", w.Body, c.want)
			}
			if after, _ := os.ReadFile(h.ConfigPath); string(after) != string(config) {
				t.Errorf("cbx.yaml changed:\n%s", after)
			}
		})
	}
}

func TestWritingAnEntryRefuses(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   any
		status int
	}{
		{"an entry name outside the allowed characters", "PUT", "/envs/pre%20view", map[string]string{"API_URL": "x"}, http.StatusBadRequest},
		{"an entry name over 64 characters", "PUT", "/envs/" + strings.Repeat("p", 65), map[string]string{"API_URL": "x"}, http.StatusBadRequest},
		{"a lowercase or dashed variable name", "PUT", "/envs/stg", map[string]string{"api-url": "x"}, http.StatusBadRequest},
		{"a value holding a newline or NUL", "PUT", "/envs/stg", map[string]string{"API_URL": "x\nPATH=/tmp"}, http.StatusBadRequest},
		{"a body that is not an object of strings", "PUT", "/envs/stg", `{"API_URL": 7}`, http.StatusBadRequest},
		{"an entry with no variables", "PUT", "/envs/stg", map[string]string{}, http.StatusBadRequest},
		{"deleting an unknown entry", "DELETE", "/envs/nowhere", nil, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := answering(t, answers("", "ok"))
			h.do("PUT", "/envs/kept", map[string]string{"API_URL": "https://kept.example.com"})
			before, _ := os.ReadFile(h.EnvarsPath)

			w := h.do(c.method, c.path, c.body)

			if w.Code != c.status {
				t.Fatalf("got %d %s, want %d", w.Code, w.Body, c.status)
			}
			if after, _ := os.ReadFile(h.EnvarsPath); string(after) != string(before) {
				t.Errorf("envars.yaml changed:\n%s", after)
			}
		})
	}
}

func TestASessionFromAnEntryCarriesItsValuesToEveryTurn(t *testing.T) {
	entry := "API_URL=https://api-1234.example.com"
	cases := []struct {
		name  string
		own   map[string]string
		after func(h *harness)
		want  string
	}{
		{"its turns run with the entry's values", map[string]string{"TENANT_ID": "12"}, func(*harness) {}, entry},
		{"the session's own env overrides the entry's value",
			map[string]string{"TENANT_ID": "12", "API_URL": "https://mine.example.com"}, func(*harness) {},
			"API_URL=https://mine.example.com"},
		{"an edit to the entry after creation does not reach it", map[string]string{"TENANT_ID": "12"}, func(h *harness) {
			h.do("PUT", "/envs/preview.1234", map[string]string{"API_URL": "https://moved.example.com"})
		}, entry},
		{"a delete of the entry after creation does not reach it", map[string]string{"TENANT_ID": "12"}, func(h *harness) {
			h.do("DELETE", "/envs/preview.1234", nil)
		}, entry},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var seen [][]string
			h := answering(t, capturingEnv(&seen))
			h.do("PUT", "/envs/preview.1234", map[string]string{"API_URL": "https://api-1234.example.com"})
			made := h.do("POST", "/sessions", map[string]any{
				"name": "from-entry", "env_from": "preview.1234", "env": c.own,
			})

			c.after(h)
			h.do("POST", "/sessions/from-entry/query", map[string]any{"prompt": "hi", "respond_within": "10s"})
			shown := h.do("GET", "/sessions/from-entry", nil)

			want := []string{c.want, "TENANT_ID=12"}
			if made.Code != http.StatusCreated || len(seen) != 1 || !slices.Equal(seen[0], want) {
				t.Errorf("create %d %s; turns ran with %q, want %q", made.Code, made.Body, seen, want)
			}
			if !strings.Contains(shown.Body.String(), `"env":["API_URL","TENANT_ID"]`) {
				t.Errorf("GET /sessions/from-entry = %s, want env names API_URL and TENANT_ID", shown.Body)
			}
		})
	}
}

func TestCreateSessionFromAnEntryRefuses(t *testing.T) {
	cases := []struct {
		name    string
		envars  string
		request map[string]any
		status  int
		says    string
	}{
		{"an unknown entry", "", map[string]any{"env_from": "nowhere"}, http.StatusBadRequest, "nowhere"},
		{"an empty env_from", "", map[string]any{"env_from": ""}, http.StatusBadRequest, "env_from"},
		{"an entry naming a variable outside the role's session_env", "stg:\n  SECRET_TOKEN: x\n",
			map[string]any{"env_from": "stg"}, http.StatusBadRequest, "SECRET_TOKEN"},
		{"a malformed envars.yaml", "stg: [not, a, map\n", map[string]any{"env_from": "stg"},
			http.StatusInternalServerError, "envars.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			h := answering(t, answers("", "ok"))
			if c.envars != "" {
				if err := os.WriteFile(h.EnvarsPath, []byte(c.envars), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			c.request["name"] = "refused-entry"

			w := h.do("POST", "/sessions", c.request)

			if w.Code != c.status || !strings.Contains(w.Body.String(), c.says) {
				t.Fatalf("got %d %s, want %d naming %q", w.Code, w.Body, c.status, c.says)
			}
			if sess, _ := h.Store.Get("refused-entry"); sess != nil {
				t.Errorf("a session was made: %+v", sess)
			}
		})
	}
}
