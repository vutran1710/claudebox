package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/vutran1710/claudebox/internal/auth"
	"github.com/vutran1710/claudebox/internal/session"
	"github.com/vutran1710/claudebox/internal/workspace"
)

const (
	DefaultPort = 8091
	PIDFile     = "/tmp/cbx-serve.pid"
	APIKeyFile  = "/tmp/cbx-serve.key"
)

type Server struct {
	port     int
	apiKey   string
	sessions session.Manager
	logger   *slog.Logger
}

func New(port int) *Server {
	if port == 0 {
		port = DefaultPort
	}
	return &Server{
		port:     port,
		apiKey:   auth.LoadOrCreateKey(APIKeyFile),
		sessions: session.NewTmuxManager(),
		logger:   slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	}
}

// NewWithManager creates a server with a custom session manager (for testing).
func NewWithManager(port int, apiKey string, mgr session.Manager) *Server {
	return &Server{
		port:     port,
		apiKey:   apiKey,
		sessions: mgr,
		logger:   slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	}
}

func (s *Server) Start() error {
	if IsRunning() {
		return fmt.Errorf("cbx serve already running (PID file: %s)", PIDFile)
	}

	os.WriteFile(PIDFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(PIDFile)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.Handler(),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		s.logger.Info("cbx serve starting", "port", s.port, "api_key", s.apiKey)
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	s.logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return httpServer.Shutdown(shutdownCtx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /sessions", s.auth(s.handleCreateSession))
	mux.HandleFunc("GET /sessions", s.auth(s.handleListSessions))
	mux.HandleFunc("DELETE /sessions/{name}", s.auth(s.handleDeleteSession))
	return mux
}

// --- Auth ---

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		if key != s.apiKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// --- Handlers ---

type createRequest struct {
	Name    string `json:"name"`
	GitHub  string `json:"github"`
	Project string `json:"project"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	// Resolve working directory
	var workDir, dirStatus string
	if req.GitHub != "" {
		result, err := workspace.ResolveGitHub(req.GitHub)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		workDir = result.Dir
		dirStatus = result.Status
	} else if req.Project != "" {
		result, err := workspace.ResolveProject(req.Project)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		workDir = result.Dir
		dirStatus = result.Status
	}

	s.logger.Info("creating session", "name", req.Name, "dir", workDir)
	sess, err := s.sessions.Create(req.Name, workDir)
	if err != nil {
		s.logger.Error("create session failed", "err", err)
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if dirStatus != "" {
		sess.Status = dirStatus
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sessions.List()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []session.Session{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}
	if !s.sessions.IsRunning(name) {
		jsonError(w, "session not found", http.StatusNotFound)
		return
	}
	if err := s.sessions.Kill(name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.logger.Info("session deleted", "name", name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"deleted": name})
}

// --- Utilities ---

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func GetAPIKey() string {
	return auth.GetKey(APIKeyFile)
}

func GetPort() int {
	return DefaultPort
}

func IsRunning() bool {
	data, err := os.ReadFile(PIDFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func Stop() error {
	data, err := os.ReadFile(PIDFile)
	if err != nil {
		return fmt.Errorf("not running (no PID file)")
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid PID file")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %d", pid)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		os.Remove(PIDFile)
		return fmt.Errorf("failed to stop: %w", err)
	}
	os.Remove(PIDFile)
	return nil
}
