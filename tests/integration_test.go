package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vutran1710/claudebox/internal/auth"
	"github.com/vutran1710/claudebox/internal/serve"
	"github.com/vutran1710/claudebox/internal/session"
	"github.com/vutran1710/claudebox/internal/workspace"
)

// --- Mock session manager for integration tests ---

type mockManager struct {
	sessions map[string]*session.Session
}

func newMockManager() *mockManager {
	return &mockManager{sessions: map[string]*session.Session{}}
}

func (m *mockManager) Create(name, workDir string) (*session.Session, error) {
	s := &session.Session{Name: name, Dir: workDir, Status: "running", RCURL: "https://claude.ai/code/test"}
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

// --- Integration: API with mock manager ---

func TestAPI_FullLifecycle(t *testing.T) {
	mgr := newMockManager()
	srv := serve.NewWithManager(0, "test-key", mgr)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Health
	resp := httpGet(t, ts.URL+"/health", "")
	assertStatus(t, resp, 200)

	// 2. Unauthorized without key
	resp = httpGet(t, ts.URL+"/sessions", "")
	assertStatus(t, resp, 401)

	// 3. Create session
	resp = httpPost(t, ts.URL+"/sessions", `{"name":"my-project"}`, "test-key")
	assertStatus(t, resp, 200)
	var sess session.Session
	json.NewDecoder(resp.Body).Decode(&sess)
	if sess.Name != "my-project" {
		t.Errorf("expected name 'my-project', got %q", sess.Name)
	}
	if sess.RCURL == "" {
		t.Error("expected remote control URL")
	}

	// 4. Create another
	resp = httpPost(t, ts.URL+"/sessions", `{"name":"another"}`, "test-key")
	assertStatus(t, resp, 200)

	// 5. List sessions
	resp = httpGet(t, ts.URL+"/sessions", "test-key")
	assertStatus(t, resp, 200)
	var sessions []session.Session
	json.NewDecoder(resp.Body).Decode(&sessions)
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}

	// 6. Delete session
	resp = httpDelete(t, ts.URL+"/sessions/my-project", "test-key")
	assertStatus(t, resp, 200)

	// 7. Verify deleted
	resp = httpGet(t, ts.URL+"/sessions", "test-key")
	json.NewDecoder(resp.Body).Decode(&sessions)
	if len(sessions) != 1 {
		t.Errorf("expected 1 session after delete, got %d", len(sessions))
	}

	// 8. Delete nonexistent
	resp = httpDelete(t, ts.URL+"/sessions/nope", "test-key")
	assertStatus(t, resp, 404)

	// 9. Create without name
	resp = httpPost(t, ts.URL+"/sessions", `{}`, "test-key")
	assertStatus(t, resp, 400)
}

func TestAPI_QueryParamAuth(t *testing.T) {
	mgr := newMockManager()
	srv := serve.NewWithManager(0, "secret", mgr)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := httpGet(t, ts.URL+"/sessions?key=secret", "")
	assertStatus(t, resp, 200)
}

// --- Integration: Workspace resolution ---

func TestWorkspace_FullResolution(t *testing.T) {
	root := t.TempDir()

	// Project not found
	_, err := workspace.ResolveProjectIn(root, "nope")
	if err == nil {
		t.Fatal("expected error for missing project")
	}

	// Create project
	os.MkdirAll(filepath.Join(root, "my-app"), 0755)
	result, err := workspace.ResolveProjectIn(root, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "found" {
		t.Errorf("expected 'found', got %q", result.Status)
	}

	// GitHub already cloned
	os.MkdirAll(filepath.Join(root, "repo", ".git"), 0755)
	result, err = workspace.ResolveGitHubIn(root, "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "found" {
		t.Errorf("expected 'found', got %q", result.Status)
	}
}

// --- Integration: Auth key management ---

func TestAuth_KeyLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	// No key exists
	if auth.GetKey(path) != "" {
		t.Error("expected no key initially")
	}

	// Create key
	key1 := auth.LoadOrCreateKey(path)
	if len(key1) != 64 {
		t.Errorf("expected 64-char hex key, got %d chars", len(key1))
	}

	// Same key on reload
	key2 := auth.LoadOrCreateKey(path)
	if key1 != key2 {
		t.Error("expected same key on reload")
	}

	// GetKey works
	if auth.GetKey(path) != key1 {
		t.Error("GetKey should return same key")
	}
}

// --- Helpers ---

func httpGet(t *testing.T, url, apiKey string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	return resp
}

func httpPost(t *testing.T, url, body, apiKey string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	return resp
}

func httpDelete(t *testing.T, url, apiKey string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("DELETE", url, nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s failed: %v", url, err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Errorf("expected status %d, got %d", expected, resp.StatusCode)
	}
}
