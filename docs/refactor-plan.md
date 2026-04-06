# cbx Refactor & Implementation Plan

## Goal

Refactor cbx into clean, testable modules with interfaces, then implement the phase 2 features (serve as control plane, simplified setup).

## Principles

- Every package has one job
- Dependencies are interfaces, not concrete types
- Business logic is separated from TUI/presentation
- Each step is independently testable before moving to the next
- No breaking changes to CLI surface until the end

---

## Part 1: Refactor (Steps 1-6)

### Step 1: Extract `shell/` utilities

**Current:** `shell/exec.go` is fine but missing some helpers used elsewhere.

**Changes:**
- Add `shell.RunShellContext(ctx, script)` if missing
- Ensure all shell operations go through this package
- No other package should call `os/exec` directly

**Tests:**
- Unit: `TestRunShellTimeout`, `TestWhich`, `TestFileExists`, `TestProcessRunning`
- Already partially tested via other packages

**Verify:** `go test ./internal/shell/`

---

### Step 2: Extract `workspace/` — repo & project resolution

**Current:** Duplicated in `code/code.go`, `serve/serve.go`

**Changes:**
- Create `internal/workspace/resolve.go`
- Move `resolveRepoDir()`, `resolveProjectDir()` from `code/`
- Public API:

```go
type ResolveResult struct {
    Dir    string // absolute path
    Status string // "cloned", "found", "created"
}

func ResolveGitHub(repo string) (*ResolveResult, error)
func ResolveProject(name string) (*ResolveResult, error)
```

**Tests:**
- Unit: `TestResolveProject_Found` — create temp dir, resolve it
- Unit: `TestResolveProject_NotFound` — resolve nonexistent
- Unit: `TestResolveGitHub_AlreadyCloned` — create dir with `.git`, resolve
- Unit: `TestResolveGitHub_NeedsClone` — mock or skip (requires network)
- All tests use `t.TempDir()` with a configurable workspace root

**Verify:** `go test ./internal/workspace/`

---

### Step 3: Extract `session/` — clean interface

**Current:** `session/session.go` mixes tmux ops, remote control setup, and user switching.

**Changes:**
- Define interface in `session/manager.go`
- Implementation in `session/tmux.go`

```go
type Session struct {
    Name   string
    Dir    string
    Status string
    RCURL  string
}

type Manager interface {
    Create(name, workDir string) (*Session, error)
    List() ([]Session, error)
    Kill(name string) error
    IsRunning(name string) bool
}

type TmuxManager struct{}
```

- Remove `StartMasterSession` (no longer needed)
- Remove user switching logic (that belongs in provision)
- `Create()` returns `Session` with RCURL

**Tests:**
- Unit: `TestTmuxManager_IsRunning` — mock shell, check tmux command
- Unit: `TestTmuxManager_List` — mock shell, parse tmux ls output
- Unit: `TestTmuxManager_Kill` — mock shell, verify command
- Integration: `TestTmuxManager_CreateAndKill` — real tmux (skip in CI)

**Verify:** `go test ./internal/session/`

---

### Step 4: Extract `service/` — unified service management

**Current:** `vnc/vnc.go` and `activate/amserver.go` manage services differently.

**Changes:**
- Define interface in `service/service.go`
- Move VNC logic to `service/vnc.go`
- Move am-server logic to `service/amserver.go`
- Add `service/tunnel.go` for Cloudflare tunnel management

```go
type Status struct {
    Name      string
    Running   bool
    Port      int
    TunnelURL string
    Extra     map[string]string
}

type Service interface {
    Name() string
    Start() error
    Stop() error
    Status() Status
}
```

**Tests:**
- Unit: `TestVNCService_Status` — mock shell, check process detection
- Unit: `TestAMServerService_Status` — mock shell
- Unit: `TestTunnelService_Start` — mock shell, verify cloudflared command
- Each service tested independently via mocked shell

**Verify:** `go test ./internal/service/`

---

### Step 5: Clean `auth/` — authentication only

**Current:** `auth/` does OAuth + tmux navigation + user creation.

**Changes:**
- Keep `auth/oauth.go` — OAuth flow (uses tmux for interactive login)
- Keep `auth/apikey.go` — API key generation/storage (move from `serve/`)
- Move `auth/user.go` → `provision/user.go`
- Move `auth/tmux.go` logic into `auth/oauth.go` (it's only used there)

```go
// auth/oauth.go
type OAuthResult struct {
    RemoteControlURL string
}
func Authenticate() (*OAuthResult, error)
func IsLoggedIn() bool

// auth/apikey.go
func LoadOrCreateKey(path string) string
func GetKey(path string) string
```

**Tests:**
- Unit: `TestLoadOrCreateKey` — create in temp dir, verify random, verify idempotent
- Unit: `TestGetKey_NotExists` — returns empty
- Unit: `TestIsLoggedIn` — mock shell

**Verify:** `go test ./internal/auth/`

---

### Step 6: Create `provision/` — one-time setup logic

**Current:** `setup/tools.go` + `auth/user.go`

**Changes:**
- Move `setup/tools.go` → `provision/tools.go` (unchanged)
- Move `auth/user.go` → `provision/user.go`
- Create `provision/provision.go` — orchestrates the full setup

```go
type Provisioner struct {
    Tools    []Tool
    Services []service.Service
    Auth     func() (*auth.OAuthResult, error)
}

type Result struct {
    VNCURL     string
    VNCPass    string
    ServeURL   string
    ServeKey   string
}

func (p *Provisioner) Run(ctx context.Context) (*Result, error)
```

**Tests:**
- Unit: `TestProvisioner_AllToolsInstalled` — tools that return `Check() = true`, skip install
- Unit: `TestProvisioner_ToolFails` — critical tool fails, stops
- Unit: `TestProvisioner_UserCreation` — mock shell, verify useradd called
- Mock services that return `Status{Running: true}` immediately

**Verify:** `go test ./internal/provision/`

---

## Part 2: New Implementation (Steps 7-10)

### Step 7: Rewrite `api/` — HTTP server

**Current:** `serve/serve.go` — mixed concerns

**Changes:**
- `api/server.go` — HTTP server, routes, middleware (auth, CORS)
- `api/handlers.go` — handlers that call `session.Manager` and `workspace.Resolve`
- `api/daemon.go` — PID file management, start/stop
- Server takes interfaces as dependencies:

```go
type Server struct {
    sessions  session.Manager
    port      int
    apiKey    string
    tunnelURL string
}
```

**Tests:**
- Unit: `TestCreateSession_Valid` — mock session manager, verify Create called
- Unit: `TestCreateSession_MissingName` — returns 400
- Unit: `TestCreateSession_GitHubClone` — mock workspace, verify resolve
- Unit: `TestListSessions` — mock manager, verify response format
- Unit: `TestDeleteSession` — mock manager, verify Kill called
- Unit: `TestAuth_MissingKey` — returns 401
- Unit: `TestAuth_ValidKey` — passes through
- Unit: `TestHealth` — returns 200
- All use `httptest.NewRecorder()` — no real HTTP server needed

**Verify:** `go test ./internal/api/`

---

### Step 8: Rewrite `cli/` — presentation layer

**Current:** `setup/setup.go`, `code/code.go`, `status/status.go` — TUI mixed with logic

**Changes:**
- `cli/setup.go` — Bubble Tea TUI that calls `provision.Provisioner`
- `cli/code.go` — Bubble Tea TUI that calls `workspace.Resolve` + `session.Manager`
- `cli/status.go` — prints status from `service.Service` + `session.Manager`
- Each CLI function takes interfaces, not concrete types

**Tests:**
- Unit: `TestStatusOutput` — mock services/sessions, verify output format
- CLI tests are mostly visual — test the logic (provision, session, workspace) not the TUI

**Verify:** `go test ./internal/cli/`

---

### Step 9: Wire everything in `cmd/cbx/main.go`

**Changes:**
- Cobra commands create concrete implementations and pass to cli/ functions
- `cbx setup` → `cli.Setup(provision.New(...), service.NewVNC(), ...)`
- `cbx code` → `cli.Code(session.NewTmuxManager(), workspace.Resolve)`
- `cbx serve` → `api.NewServer(session.NewTmuxManager(), ...)`
- `cbx status` → `cli.Status(services, sessions)`
- `cbx show api-key` → `auth.GetKey(path)`

**Tests:**
- Unit: `TestVersion` — already exists
- Integration: build binary, run `cbx --help`, verify output

**Verify:** `go test ./cmd/cbx/`

---

### Step 10: Integration & E2E tests

#### Integration tests (no external services)

Located in `tests/integration/`

```go
// TestSessionLifecycle — real tmux, create + list + kill
func TestSessionLifecycle(t *testing.T) {
    if os.Getenv("CI") != "" { t.Skip("needs tmux") }
    mgr := session.NewTmuxManager()
    s, err := mgr.Create("test-session", t.TempDir())
    require.NoError(t, err)
    assert.Equal(t, "test-session", s.Name)

    sessions, _ := mgr.List()
    assert.True(t, containsSession(sessions, "test-session"))

    mgr.Kill("test-session")
    assert.False(t, mgr.IsRunning("test-session"))
}

// TestAPIWithMockSession — real HTTP, mock session manager
func TestAPIWithMockSession(t *testing.T) {
    mock := &mockManager{sessions: map[string]*Session{}}
    srv := api.NewServer(mock, 0, "test-key")
    ts := httptest.NewServer(srv.Handler())
    defer ts.Close()

    // Create
    resp := post(ts.URL+"/sessions", `{"name":"test"}`, "test-key")
    assert.Equal(t, 200, resp.StatusCode)

    // List
    resp = get(ts.URL+"/sessions", "test-key")
    var sessions []Session
    json.NewDecoder(resp.Body).Decode(&sessions)
    assert.Len(t, sessions, 1)

    // Delete
    resp = delete(ts.URL+"/sessions/test", "test-key")
    assert.Equal(t, 200, resp.StatusCode)
}

// TestWorkspaceResolve — real filesystem
func TestWorkspaceResolve(t *testing.T) {
    root := t.TempDir()
    os.MkdirAll(filepath.Join(root, "my-project", ".git"), 0755)

    result, err := workspace.ResolveProjectIn(root, "my-project")
    require.NoError(t, err)
    assert.Equal(t, "found", result.Status)
}
```

**Run:** `go test ./tests/integration/ -tags integration`

#### E2E tests (full stack, real server)

Located in `tests/e2e/`

These run against a real deployed ClaudeBox. Triggered manually or in CI with a live server.

```bash
#!/bin/bash
# tests/e2e/test_e2e.sh
set -e

HOST=${1:?"Usage: test_e2e.sh <host>"}
API_KEY=${2:?"Usage: test_e2e.sh <host> <api-key>"}
API="https://$HOST"

echo "=== Health ==="
curl -sf "$API/health" | jq .

echo "=== Create Session ==="
curl -sf -X POST "$API/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "e2e-test"}' | jq .

echo "=== List Sessions ==="
curl -sf "$API/sessions" \
  -H "X-API-Key: $API_KEY" | jq .

echo "=== Delete Session ==="
curl -sf -X DELETE "$API/sessions/e2e-test" \
  -H "X-API-Key: $API_KEY" | jq .

echo "=== Status ==="
ssh claude@"$HOST" 'cbx status'

echo "PASS"
```

**Run:** `./tests/e2e/test_e2e.sh <tunnel-url> <api-key>`

---

## Test Summary

| Layer | What | How | Count |
|-------|------|-----|-------|
| Unit: shell | exec, which, fileExists | mock-free | ~5 |
| Unit: workspace | resolve project, resolve github | t.TempDir() | ~5 |
| Unit: session | manager interface | mock shell | ~5 |
| Unit: service | vnc, amserver, tunnel | mock shell | ~6 |
| Unit: auth | apikey, isLoggedIn | t.TempDir(), mock shell | ~4 |
| Unit: provision | tool install, user creation | mock shell, mock services | ~5 |
| Unit: api | all endpoints, auth middleware | httptest, mock manager | ~8 |
| Unit: cli | status output | mock services/sessions | ~3 |
| **Total unit** | | | **~41** |
| Integration | session lifecycle, API+mock, workspace | real tmux, real HTTP, real FS | ~5 |
| E2E | full stack against live server | curl + ssh | ~5 checks |

---

## Execution Order

```
Step 1:  shell/          (foundation, no deps)
Step 2:  workspace/      (depends on shell/)
Step 3:  session/        (depends on shell/)
Step 4:  service/        (depends on shell/)
Step 5:  auth/           (depends on shell/)
Step 6:  provision/      (depends on shell/, service/, auth/)
Step 7:  api/            (depends on session/, workspace/, auth/)
Step 8:  cli/            (depends on provision/, session/, workspace/, service/)
Step 9:  cmd/cbx/        (wires everything)
Step 10: tests/          (integration + e2e)
```

Steps 2-5 can be done in parallel — they only depend on shell/.
Step 6 depends on 4+5.
Step 7 depends on 2+3+5.
Step 8 depends on 2+3+4+5+6.

```
         shell/
        / | | \
workspace session service auth
       \    |      |    /
        \   |  provision
         \  |  /
          api    cli
           \    /
           cmd/cbx
              |
           tests/
```
