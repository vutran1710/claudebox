package api

// The box's env map over HTTP: entries are written by whoever deploys a caller (any key may),
// and a session takes one whole with env_from, subject to its role's session_env as its own
// env is. Read back by names only, as a session's env is.

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/vutran1710/claudebox/internal/boxconfig"
)

type envarsView struct {
	Path string              `json:"path"`
	Envs map[string][]string `json:"envs"`
}

func (s *Server) getEnvars(w http.ResponseWriter, _ *http.Request) {
	envs, err := boxconfig.LoadEnvars(s.EnvarsPath)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := map[string][]string{}
	for entry, vars := range envs {
		names[entry] = slices.Sorted(maps.Keys(vars))
	}
	writeJSON(w, http.StatusOK, envarsView{Path: s.EnvarsPath, Envs: names})
}

func (s *Server) putEnvar(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !sessionName.MatchString(name) {
		fail(w, http.StatusBadRequest, "an entry name must be 1-64 characters of letters, digits, dot, dash or underscore")
		return
	}
	var vars map[string]string
	if err := decode(r, &vars); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(vars) == 0 {
		fail(w, http.StatusBadRequest, "an entry sets at least one variable")
		return
	}
	for v, value := range vars {
		if !envName.MatchString(v) || strings.ContainsAny(value, "\x00\n\r") {
			fail(w, http.StatusBadRequest, fmt.Sprintf(
				"%q is not an upper-case variable name, or its value holds a newline or NUL", v))
			return
		}
	}
	if err := boxconfig.PutEnvar(s.EnvarsPath, name, vars); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "env": slices.Sorted(maps.Keys(vars))})
}

func (s *Server) deleteEnvar(w http.ResponseWriter, r *http.Request) {
	found, err := boxconfig.DeleteEnvar(s.EnvarsPath, r.PathValue("name"))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		fail(w, http.StatusNotFound, fmt.Sprintf("no entry %q", r.PathValue("name")))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
