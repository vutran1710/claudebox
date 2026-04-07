package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vutran1710/claudebox/internal/session"
)

// mockManager implements session.Manager for testing.
type mockManager struct {
	sessions map[string]*session.Session
}

func (m *mockManager) Create(name, workDir string) (*session.Session, error) {
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	s := &session.Session{Name: name, Dir: workDir, Status: "running"}
	m.sessions[name] = s
	return s, nil
}

func (m *mockManager) List() ([]session.Session, error) {
	var result []session.Session
	for _, s := range m.sessions {
		result = append(result, *s)
	}
	return result, nil
}

func (m *mockManager) Kill(name string) error {
	delete(m.sessions, name)
	return nil
}

func (m *mockManager) IsRunning(name string) bool {
	_, ok := m.sessions[name]
	return ok
}

func newTestServer(t *testing.T) *Server {
	return NewWithManager(0, "test-key", &mockManager{sessions: map[string]*session.Session{}}, t.TempDir())
}

func TestHealth(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCreateSession_Valid(t *testing.T) {
	s := newTestServer(t)
	body := `{"name":"test-session"}`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sess session.Session
	json.NewDecoder(w.Body).Decode(&sess)
	if sess.Name != "test-session" {
		t.Errorf("expected name 'test-session', got %q", sess.Name)
	}
}

func TestCreateSession_MissingName(t *testing.T) {
	s := newTestServer(t)
	body := `{"github":"owner/repo"}`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateSession_BadJSON(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader("not json"))
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListSessions(t *testing.T) {
	s := newTestServer(t)

	// Create a session first
	s.sessions.Create("test", "")

	req := httptest.NewRequest("GET", "/sessions", nil)
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var sessions []session.Session
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}

func TestDeleteSession(t *testing.T) {
	s := newTestServer(t)
	s.sessions.Create("to-delete", "")

	req := httptest.NewRequest("DELETE", "/sessions/to-delete", nil)
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if s.sessions.IsRunning("to-delete") {
		t.Error("session should be deleted")
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/sessions/nonexistent", nil)
	req.Header.Set("X-API-Key", "test-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestAuth_MissingKey(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuth_WrongKey(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/sessions", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuth_QueryParam(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/sessions?key=test-key", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIsRunning_NotRunning(t *testing.T) {
	if IsRunning() {
		t.Error("expected not running")
	}
}
