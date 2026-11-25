// Package api serves the HTTP control plane on the box.
//
// It exists for one capability the rest of cbx does not have: send a prompt to
// a session and get the answer back in one response. Neither SSH nor Remote
// Control offers that programmatically, and the CRUD around it is here only
// because a query needs something to talk to.
//
// Sessions it creates are headless — a conversation id driven by `claude -p`.
// tmux sessions are listed but never driven from here, because the phone and a
// query writing to one transcript would interleave turns.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/vutran1710/claudebox/internal/boxconfig"
	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/store"
	"github.com/vutran1710/claudebox/internal/tmux"
)

// MaxRespondWithin caps how long a caller may hold a connection open.
//
// The server's own WriteTimeout must exceed this, or the transport closes
// connections this value explicitly permits.
const MaxRespondWithin = 15 * time.Minute

// DefaultJobTTL is how long a finished job stays readable. Long enough that a
// dropped response can be retried, short enough that unread answers do not
// accumulate.
const DefaultJobTTL = time.Hour

// SweepInterval is how often the janitor collects expired jobs.
const SweepInterval = 10 * time.Minute

type Server struct {
	Store  *store.Store
	Claude *claude.Client
	Tmux   *tmux.Client

	ConfigPath string
	// EnvarsPath is the box's env map, which env_from reads and /envs writes.
	EnvarsPath string
	Home       string
	JobTTL     time.Duration
	// ArtifactTTL is how long a declared output stays fetchable; zero means
	// DefaultArtifactTTL.
	ArtifactTTL time.Duration
	Version     string

	// now is injected for tests; nothing in production replaces it.
	now func() time.Time
	// newID names jobs. Injected so a test can predict one.
	newID func() string

	// cancels reaches the query behind a running job. Runtime control rather
	// than state: a restart loses these, and RecoverRunningJobs is what
	// reconciles the rows they would have finished.
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// New builds a server with the real dependencies.
//
// No key is passed in. Who may call this box, and what their sessions may do,
// is read from the config file on every request — so revoking a key is an
// edit over ssh rather than a restart.
func New(st *store.Store) *Server {
	home, _ := os.UserHomeDir()
	return &Server{
		Store:      st,
		Claude:     claude.New(),
		Tmux:       tmux.New(),
		ConfigPath: boxconfig.DefaultPath(),
		EnvarsPath: boxconfig.DefaultEnvarsPath(),
		Home:       home,
		JobTTL:     DefaultJobTTL,
		Version:    "dev",
		now:        time.Now,
		newID:      newJobID,
		cancels:    map[string]context.CancelFunc{},
	}
}

// route is one endpoint.
//
// Declared in a table rather than registered inline so the OpenAPI document
// can be checked against it. A published description that has drifted from the
// server is worse than none: it is believed.
type route struct {
	Pattern string
	Handler http.HandlerFunc
	// Public endpoints skip the bearer key. Only one does.
	Public bool
}

func (s *Server) routes() []route {
	return []route{
		// A health check is for a load balancer, and one that needed a
		// credential could not do its job.
		{Pattern: "GET /healthz", Handler: s.health, Public: true},

		{Pattern: "GET /openapi.yaml", Handler: s.openAPIYAML},
		{Pattern: "GET /openapi.json", Handler: s.openAPIJSON},

		{Pattern: "POST /auth/rotate", Handler: s.rotateKey},

		{Pattern: "GET /commands", Handler: s.getCommands},
		{Pattern: "PUT /commands", Handler: s.putCommands},

		{Pattern: "GET /envs", Handler: s.getEnvars},
		{Pattern: "PUT /envs/{name}", Handler: s.putEnvar},
		{Pattern: "DELETE /envs/{name}", Handler: s.deleteEnvar},

		{Pattern: "POST /sessions", Handler: s.createSession},
		{Pattern: "GET /sessions", Handler: s.listSessions},
		{Pattern: "GET /sessions/{name}", Handler: s.getSession},
		{Pattern: "DELETE /sessions/{name}", Handler: s.deleteSession},

		{Pattern: "POST /sessions/{name}/query", Handler: s.query},
		{Pattern: "POST /sessions/{name}/command", Handler: s.command},
		{Pattern: "PUT /sessions/{name}/skills/{skill}", Handler: s.putSkill},

		{Pattern: "GET /sessions/{name}/artifacts", Handler: s.listArtifacts},
		{Pattern: "GET /sessions/{name}/artifacts/{path...}", Handler: s.getArtifact},

		{Pattern: "GET /jobs/{id}", Handler: s.getJob},
		{Pattern: "DELETE /jobs/{id}", Handler: s.deleteJob},
	}
}

// Handler builds the routes.
//
// Guarded endpoints go on their own mux behind authenticate, so an endpoint
// cannot be added without a key check by forgetting to wrap it.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	guarded := http.NewServeMux()
	for _, r := range s.routes() {
		if r.Public {
			mux.HandleFunc(r.Pattern, r.Handler)
			continue
		}
		guarded.HandleFunc(r.Pattern, r.Handler)
	}
	mux.Handle("/", s.authenticate(guarded))
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	// Deliberately says nothing about whether a key is configured: a health
	// check is for a load balancer, not a way to probe the box's state.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
}

// rotateKey issues a new value for the calling key's own entry.
//
// Its own and no other: a caller may replace the credential it already holds,
// which is what rotation is, and may not touch anybody else's or grant itself
// different permissions. Those live in the config file and change over ssh.
func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	key := caller(r)
	if key == nil {
		fail(w, http.StatusUnauthorized, "unknown key")
		return
	}
	fresh, err := s.Store.RotateKey(key.Label)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"label": key.Label, "api_key": fresh})
}

// Janitor sweeps expired jobs until ctx is done. Started with the server and
// stopped with it.
func (s *Server) Janitor(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Store.Sweep()
			s.sweepArtifacts()
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// fail writes an error a caller can act on. Every message here names what to
// do next, because the caller is usually an agent with no way to ask.
func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}
