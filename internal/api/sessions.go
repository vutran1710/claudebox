package api

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/store"
	"github.com/vutran1710/claudebox/internal/workspace"
)

type sessionView struct {
	Name           string   `json:"name"`
	Dir            string   `json:"dir"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Repo           string   `json:"repo,omitempty"`
	RCURL          string   `json:"rc_url,omitempty"`
	SessionID      string   `json:"session_id,omitempty"`
	SystemPrompt   string   `json:"system_prompt,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`
	Model          string   `json:"model,omitempty"`
	Effort         string   `json:"effort,omitempty"`
	Context        []string `json:"context,omitempty"`
	// Env lists the names a session was given, never their values.
	Env   []string `json:"env,omitempty"`
	Turns int      `json:"turns"`
	// Priming is present only when a session was created with skills.
	Priming *primingView `json:"priming,omitempty"`
}

func view(s store.Session, running bool) sessionView {
	status := "stopped"
	if s.Kind == store.Headless {
		// A headless session is a uuid between queries, not a process. There
		// is nothing to be stopped, so "ready" is the honest word.
		status = "ready"
	} else if running {
		status = "running"
	}
	return sessionView{
		Name: s.Name, Dir: s.Dir, Kind: s.Kind, Status: status, Repo: s.Repo, RCURL: s.RCURL,
		SessionID: s.ClaudeSessionID, SystemPrompt: s.SystemPrompt,
		PermissionMode: s.PermissionMode, Model: s.Model, Effort: s.Effort,
		Context: s.Context, Env: slices.Sorted(maps.Keys(s.Env)), Turns: s.Turns,
	}
}

type createSessionRequest struct {
	Name         string `json:"name"`
	Repo         string `json:"repo"`
	SystemPrompt string `json:"system_prompt"`
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	// Context names knowledge files under the box's ~/.claude to give this
	// session before its first turn.
	Context []string `json:"context"`
	// Env is set on every turn's process. Each name must be one the caller's
	// role permits, so a key can never set what changes how the box runs.
	Env map[string]string `json:"env"`
	// Skills are invoked as turns once the session exists, in the order
	// given. Each costs a real turn, so respond_within is required with them.
	Skills        []string       `json:"skills"`
	RespondWithin *respondWithin `json:"respond_within"`
}

var sessionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var envName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// refusedEnv answers 400 and reports true when env names a variable outside
// permitted, or a value holding a newline or NUL.
func refusedEnv(w http.ResponseWriter, env map[string]string, permitted []string) bool {
	for name, value := range env {
		if !envName.MatchString(name) || !slices.Contains(permitted, name) || strings.ContainsAny(value, "\x00\n\r") {
			fail(w, http.StatusBadRequest, fmt.Sprintf(
				"env %q is not one this key's role may set here, or its value holds a newline or NUL", name))
			return true
		}
	}
	return false
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if !sessionName.MatchString(req.Name) {
		fail(w, http.StatusBadRequest, "name must be 1-64 characters of letters, digits, dot, dash or underscore")
		return
	}
	if !claude.ValidEffort(req.Effort) {
		fail(w, http.StatusBadRequest, fmt.Sprintf(
			"effort %q is not one of %s, %s, %s, %s, %s",
			req.Effort, claude.Low, claude.Medium, claude.High, claude.XHigh, claude.Max))
		return
	}
	// --model takes its value before the "--" that ends option parsing, so a
	// name beginning with a dash would be read as another flag.
	if !claude.ValidModel(req.Model) {
		fail(w, http.StatusBadRequest, fmt.Sprintf(
			"model %q is not a usable model name — an alias like \"opus\", a full name like \"claude-fable-5\", or a variant like \"opus[1m]\"", req.Model))
		return
	}
	// Resolved before the session exists: a context file that is absent or
	// outside the knowledge directory should cost a round trip, not a session
	// created without the knowledge it was asked for.
	context, err := s.resolveContext(req.Context)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if refusedEnv(w, req.Env, caller(r).role().SessionEnv) {
		return
	}
	for _, name := range req.Skills {
		if !ValidSkillRef(name) {
			fail(w, http.StatusBadRequest, fmt.Sprintf(
				"skill %q is not a usable name — lowercase letters, digits, dashes, and a colon for a plugin skill", name))
			return
		}
	}
	// Priming runs turns, so it needs the same deadline a query does. Without
	// skills there is nothing slow to wait for and the field is not asked for.
	var wait time.Duration
	if len(req.Skills) > 0 {
		w2, werr := window(req.RespondWithin)
		if werr != nil {
			fail(w, http.StatusBadRequest, werr.Error())
			return
		}
		wait = w2
	}
	if existing, err := s.Store.Get(req.Name); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	} else if existing != nil {
		fail(w, http.StatusConflict, fmt.Sprintf("session %q already exists", req.Name))
		return
	}

	root, err := workspace.Ensure()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir := filepath.Join(root, req.Name)
	if err := workspace.Prepare(dir, req.Repo); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// Written before the session is recorded: a session that exists without
	// the knowledge it named would be worse than one that was never created.
	if err := s.writeContext(dir, context); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	sess := store.Session{
		Name: req.Name, Dir: dir, Repo: req.Repo, Kind: store.Headless,
		// The conversation is named now and created by the first query.
		ClaudeSessionID: uuid.NewString(),
		SystemPrompt:    req.SystemPrompt,
		// From the key, never from the request: a caller able to name its own
		// permission mode could name the one that ignores every deny rule.
		PermissionMode: caller(r).Mode(),
		Model:          req.Model,
		Effort:         req.Effort,
		Context:        context,
		Env:            req.Env,
	}
	if err := s.Store.Put(sess); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	created := view(sess, false)
	if len(req.Skills) > 0 {
		p := s.prime(&sess, req.Skills, wait)
		created.Priming = &p
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	recorded, err := s.Store.List()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	live := map[string]bool{}
	if names, err := s.Tmux.List(); err == nil {
		for _, n := range names {
			live[n] = true
		}
	}
	out := make([]sessionView, 0, len(recorded))
	for _, sess := range recorded {
		out = append(out, view(sess, live[sess.Name]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// headless loads a session and refuses anything the API must not drive.
//
// An interactive session is someone's phone session: a query or a command from
// here would be a second writer on a transcript a human is already using, and
// turns would interleave.
func (s *Server) headless(w http.ResponseWriter, name string) *store.Session {
	sess, err := s.Store.Get(name)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	if sess == nil {
		fail(w, http.StatusNotFound, fmt.Sprintf("no session named %q", name))
		return nil
	}
	if sess.Kind != store.Headless {
		fail(w, http.StatusConflict, fmt.Sprintf(
			"session %q is driven from Remote Control — use the phone, or `cbx kill %s`", name, name))
		return nil
	}
	return sess
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sess, err := s.Store.Get(name)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess == nil {
		fail(w, http.StatusNotFound, fmt.Sprintf("no session named %q", name))
		return
	}
	// Readable whatever its kind: "what is on this box" is one question.
	writeJSON(w, http.StatusOK, view(*sess, s.Tmux.Has(name)))
}

// deleteSession forgets a session and its history.
//
// The project directory survives. cbx kill has always refused to delete
// someone's work, and an HTTP verb is not a reason to change that — the
// transcript is the session, the directory is the work.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sess := s.headless(w, name)
	if sess == nil {
		return
	}
	// A query in flight is cancelled rather than refused: cbx kill succeeds on
	// a session that is already gone because the intent is that it be gone,
	// and the same reading applies when the obstacle is a running query.
	if job, _ := s.Store.RunningJob(name); job != nil {
		s.cancel(job.ID)
		s.Store.FinishJob(job.ID, store.Cancelled, "", "the session was deleted")
	}
	if sess.ClaudeSessionID != "" && s.Home != "" {
		os.Remove(claude.TranscriptPath(s.Home, sess.Dir, sess.ClaudeSessionID))
	}
	// The register goes with the session, so a name reused later does not
	// inherit what the last one could hand back.
	s.Store.DeleteArtifacts(name)
	if err := s.Store.Delete(name); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// There is no endpoint for changing a session's system prompt.
//
// Claude Code snapshots the system prompt on a conversation's first request
// and reuses the record verbatim on every later request and resume. A prompt
// set after that is accepted, stored, passed on every query — and ignored.
// Measured against 2.1.236: a conversation seeded with one codeword and
// resumed with another still answered with the first.
//
// An endpoint that reports success having done nothing is the failure this
// project keeps finding in other people's tools. Everything that shapes a
// session is fixed when it is created.

// skillName is validated, never sanitised. This is a filesystem write driven
// by a network request, and quoting defends a shell while doing nothing at all
// about "..".
var skillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func (s *Server) putSkill(w http.ResponseWriter, r *http.Request) {
	sess := s.headless(w, r.PathValue("name"))
	if sess == nil {
		return
	}
	skill := r.PathValue("skill")
	if !skillName.MatchString(skill) {
		fail(w, http.StatusBadRequest, "skill name must be lowercase letters, digits and dashes")
		return
	}
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	content, err := io.ReadAll(body)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	dir := filepath.Join(sess.Dir, ".claude", "skills", skill)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"skill": skill, "path": path})
}
