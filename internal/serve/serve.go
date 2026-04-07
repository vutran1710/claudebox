package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
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
	StateFile   = "/tmp/cbx-serve.state"
	TunnelLog   = "/tmp/cbx-serve-tunnel.log"
)

type Server struct {
	port      int
	apiKey    string
	sessions  session.Manager
	workspace string // root dir for projects
	logger    *slog.Logger
}

func New(port int) *Server {
	if port == 0 {
		port = DefaultPort
	}
	return &Server{
		port:      port,
		apiKey:    auth.LoadOrCreateKey(APIKeyFile),
		sessions:  session.NewTmuxManager(),
		workspace: workspace.DefaultRoot,
		logger:    slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	}
}

// NewWithManager creates a server with custom dependencies (for testing).
func NewWithManager(port int, apiKey string, mgr session.Manager, wsRoot string) *Server {
	if wsRoot == "" {
		wsRoot = workspace.DefaultRoot
	}
	return &Server{
		port:      port,
		apiKey:    apiKey,
		sessions:  mgr,
		workspace: wsRoot,
		logger:    slog.New(slog.NewJSONHandler(os.Stderr, nil)),
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

	// Start Cloudflare tunnel
	tunnelURL := s.startTunnel()
	if tunnelURL != "" {
		s.logger.Info("tunnel ready", "url", tunnelURL)
		saveState(tunnelURL)
	}

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
	Name string `json:"name"`
	Repo string `json:"repo"`
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

	// Check if session already exists
	if s.sessions.IsRunning(req.Name) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session.Session{
			Name:   req.Name,
			Status: "already running",
		})
		return
	}

	// Resolve working directory
	result, err := workspace.ResolveIn(s.workspace, req.Name, req.Repo)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	workDir := result.Dir
	dirStatus := result.Status

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

func (s *Server) startTunnel() string {
	logFile, err := os.OpenFile(TunnelLog, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		s.logger.Error("failed to create tunnel log", "err", err)
		return ""
	}

	cmd := exec.Command("cloudflared", "tunnel", "--url", fmt.Sprintf("http://localhost:%d", s.port))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		s.logger.Error("failed to start tunnel", "err", err)
		return ""
	}

	// Wait for tunnel URL
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		data, _ := os.ReadFile(TunnelLog)
		for _, line := range strings.Split(string(data), "\n") {
			if idx := strings.Index(line, "https://"); idx >= 0 {
				url := line[idx:]
				if strings.Contains(url, ".trycloudflare.com") {
					if end := strings.IndexByte(url, ' '); end > 0 {
						url = url[:end]
					}
					return strings.TrimSpace(url)
				}
			}
		}
	}
	return ""
}

func saveState(tunnelURL string) {
	os.WriteFile(StateFile, []byte(fmt.Sprintf("tunnel_url=%s\n", tunnelURL)), 0644)
}

func GetTunnelURL() string {
	data, _ := os.ReadFile(StateFile)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "tunnel_url=") {
			return strings.TrimPrefix(line, "tunnel_url=")
		}
	}
	return ""
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
