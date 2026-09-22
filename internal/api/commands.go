package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/vutran1710/claudebox/internal/commandspec"
)

// Reading and replacing the command allowlist over HTTP, so a box's policy can
// be managed without an ssh session.
//
// This does not widen what a caller can do. Anyone holding the bearer key can
// already create a session and send it any prompt; the allowlist governs slash
// commands, which are the narrower power. What it protects against is a
// *mistake* — a command that silently does nothing — not an attacker.

type commandsView struct {
	Path     string                `json:"path"`
	Version  int                   `json:"version"`
	Commands []commandspec.Command `json:"commands"`
}

func (s *Server) getCommands(w http.ResponseWriter, _ *http.Request) {
	spec, err := commandspec.Load(s.SpecPath)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, commandsView{
		Path: s.SpecPath, Version: spec.Version, Commands: spec.Commands,
	})
}

// putCommands replaces the spec.
//
// The body may be YAML or JSON — JSON is a subset of YAML, so one parser reads
// both. It is validated before anything is written: a spec that does not parse
// would take the command endpoint down with it, and the file already on disk is
// a working one.
//
// Comments do not survive a replacement. The shipped default carries its
// reasoning in comments, so editing the file on the box keeps more than this
// does.
func (s *Server) putCommands(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	spec, err := commandspec.Parse(raw)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// Written back from the parsed form, so what lands on disk is what was
	// accepted rather than whatever shape the body happened to be in.
	out, err := yaml.Marshal(spec)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.SpecPath), 0o755); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(s.SpecPath, out, 0o644); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, commandsView{
		Path: s.SpecPath, Version: spec.Version, Commands: spec.Commands,
	})
}
