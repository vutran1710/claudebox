package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	s := New(0)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected ok, got %s", body["status"])
	}
}

func TestListSessionsEmpty(t *testing.T) {
	s := New(0)
	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	s.handleListSessions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCreateSessionBadJSON(t *testing.T) {
	s := New(0)
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	s.handleCreateSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestIsRunningWhenNotRunning(t *testing.T) {
	if IsRunning() {
		t.Error("expected not running")
	}
}

func TestSplitLines(t *testing.T) {
	lines := splitLines("a\nb\nc")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestIndexOf(t *testing.T) {
	if indexOf("hello:world", ':') != 5 {
		t.Error("expected 5")
	}
	if indexOf("hello", ':') != -1 {
		t.Error("expected -1")
	}
}
