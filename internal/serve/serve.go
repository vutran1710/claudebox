package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/vutran1710/claudebox/internal/session"
)

const (
	DefaultPort = 8091
	PIDFile     = "/tmp/cbx-serve.pid"
)

const APIKeyFile = "/tmp/cbx-serve.key"

type Server struct {
	port   int
	apiKey string
	logger *slog.Logger
}

func New(port int) *Server {
	if port == 0 {
		port = DefaultPort
	}
	// Load or generate API key
	apiKey := loadOrCreateKey()
	return &Server{
		port:   port,
		apiKey: apiKey,
		logger: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	}
}

func loadOrCreateKey() string {
	data, err := os.ReadFile(APIKeyFile)
	if err == nil && len(data) > 0 {
		return string(data)
	}
	b := make([]byte, 32)
	rand.Read(b)
	key := hex.EncodeToString(b)
	os.WriteFile(APIKeyFile, []byte(key), 0600)
	return key
}

// GetAPIKey returns the current API key (for clients to read).
func GetAPIKey() string {
	data, _ := os.ReadFile(APIKeyFile)
	return string(data)
}

func (s *Server) Start() error {
	// Check if already running
	if IsRunning() {
		return fmt.Errorf("cbx serve already running (PID file: %s)", PIDFile)
	}

	// Write PID file
	os.WriteFile(PIDFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(PIDFile)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /sessions", s.auth(s.handleCreateSession))
	mux.HandleFunc("GET /sessions", s.auth(s.handleListSessions))
	mux.HandleFunc("DELETE /sessions/{name}", s.auth(s.handleDeleteSession))

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
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
	// Check if process is alive
	return proc.Signal(syscall.Signal(0)) == nil
}

func GetPort() int {
	return DefaultPort
}

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

type sessionInfo struct {
	Name   string `json:"name"`
	RC     string `json:"remote_control,omitempty"`
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
		return
	}

	// Resolve name
	name := req.Name
	if name == "" {
		if req.GitHub != "" {
			parts := filepath.Base(req.GitHub)
			name = parts
		} else if req.Project != "" {
			name = req.Project
		} else {
			name = fmt.Sprintf("claude-%d", time.Now().Unix())
		}
	}

	// Resolve work dir
	workDir := ""
	if req.GitHub != "" {
		dir, err := resolveGitHub(req.GitHub)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
			return
		}
		workDir = dir
	} else if req.Project != "" {
		dir := filepath.Join("/workspace", req.Project)
		if _, err := os.Stat(dir); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"project not found: %s"}`, dir), http.StatusBadRequest)
			return
		}
		workDir = dir
	}

	s.logger.Info("creating session", "name", name, "workDir", workDir)
	rcURL, err := session.StartClaudeSession(name, workDir)
	if err != nil {
		s.logger.Error("create session failed", "err", err)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessionInfo{
		Name:   name,
		RC:     rcURL,
		Status: "running",
	})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := session.ListSessions()
	w.Header().Set("Content-Type", "application/json")
	if sessions == "" {
		json.NewEncoder(w).Encode([]sessionInfo{})
		return
	}
	// Parse tmux ls output
	var result []sessionInfo
	for _, line := range splitLines(sessions) {
		if line == "" {
			continue
		}
		// Format: "name: N windows (created ...)"
		name := line
		if idx := indexOf(line, ':'); idx > 0 {
			name = line[:idx]
		}
		status := "running"
		if !session.IsSessionRunning(name) {
			status = "stopped"
		}
		result = append(result, sessionInfo{Name: name, Status: status})
	}
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}

	if !session.IsSessionRunning(name) {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	// Kill tmux session
	_, err := shellExec(fmt.Sprintf(`tmux kill-session -t %s 2>/dev/null`, name))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	s.logger.Info("session deleted", "name", name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"deleted": name})
}

// --- Helpers ---

func resolveGitHub(repo string) (string, error) {
	repoName := filepath.Base(repo)
	dir := filepath.Join("/workspace", repoName)

	// Check if already cloned
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
	}

	// Clone
	url := fmt.Sprintf("https://github.com/%s.git", repo)
	if _, err := shellExec(fmt.Sprintf(`git clone %s %s`, url, dir)); err != nil {
		return "", fmt.Errorf("clone failed: %w", err)
	}
	return dir, nil
}

func shellExec(cmd string) (string, error) {
	res, err := execShell(cmd)
	return res, err
}

func splitLines(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
