# cbx Code Structure

## Current Problems

```
internal/
├── activate/       ← does 3 things: am-server, MCP config, session spawning
├── auth/           ← does 3 things: OAuth, tmux navigation, user creation
├── code/           ← duplicates session logic from serve/ and session/
├── serve/          ← duplicates GitHub resolution from code/
├── session/        ← mixed: tmux management + remote control + user switching
├── setup/          ← fine, but coupled to auth/ and vnc/
├── shell/          ← fine
├── status/         ← imports everything, knows all internals
├── ui/             ← fine
└── vnc/            ← fine
```

Issues:
1. **Duplicate logic** — GitHub repo resolution in `code/`, `serve/`, and future places
2. **Mixed responsibilities** — `auth/` does OAuth + user creation + tmux navigation
3. **No interfaces** — everything calls concrete implementations directly
4. **`activate/` is confused** — part service management, part config, part session
5. **`status/` knows everything** — imports every package, fragile

## Proposed Structure

```
cmd/cbx/
  main.go              ← cobra commands only, thin wrappers

internal/
  ├── cli/             ← command implementations (TUI flows)
  │   ├── setup.go     ← setup TUI
  │   ├── code.go      ← code TUI (thin: calls session manager)
  │   └── status.go    ← status display
  │
  ├── session/         ← session management (single responsibility)
  │   ├── manager.go   ← interface + implementation
  │   └── tmux.go      ← tmux operations (create, kill, list, capture)
  │
  ├── service/         ← long-running service management
  │   ├── manager.go   ← interface: Start, Stop, Status
  │   ├── amserver.go  ← am-server service
  │   ├── vnc.go       ← VNC + Chrome service
  │   └── tunnel.go    ← Cloudflare tunnel management
  │
  ├── api/             ← HTTP API (cbx serve)
  │   ├── server.go    ← HTTP server + routes + auth
  │   ├── handlers.go  ← request handlers
  │   └── daemon.go    ← PID file, start/stop daemon
  │
  ├── auth/            ← authentication only
  │   ├── oauth.go     ← Claude OAuth flow
  │   └── apikey.go    ← API key generation/storage
  │
  ├── provision/       ← one-time server setup
  │   ├── tools.go     ← tool installation
  │   └── user.go      ← claude user creation + config
  │
  ├── workspace/       ← project/repo resolution
  │   └── resolve.go   ← find/clone GitHub repos, locate projects
  │
  ├── shell/           ← shell execution (unchanged)
  │   └── exec.go
  │
  └── ui/              ← TUI components (unchanged)
      ├── components.go
      └── styles.go
```

## Interfaces

### SessionManager

```go
// session/manager.go
type Session struct {
    Name     string
    Dir      string
    Status   string   // running, stopped
    RCURL    string   // remote control URL (only at creation)
}

type Manager interface {
    Create(name string, workDir string) (*Session, error)
    List() ([]Session, error)
    Kill(name string) error
    IsRunning(name string) bool
}
```

Single implementation: `TmuxManager`. All session operations go through this.
Used by: `api/handlers.go`, `cli/code.go`, `cli/setup.go`

### ServiceManager

```go
// service/manager.go
type ServiceStatus struct {
    Name      string
    Running   bool
    Port      int
    TunnelURL string
    Extra     map[string]string // service-specific info (password, API key, etc.)
}

type Service interface {
    Start() error
    Stop() error
    Status() ServiceStatus
}
```

Implementations: `AMServer`, `VNC`, `Tunnel`
Used by: `cli/setup.go`, `cli/status.go`

### Workspace

```go
// workspace/resolve.go
type ResolveResult struct {
    Dir    string   // absolute path
    Status string   // cloned, found, created
}

func ResolveGitHub(repo string) (*ResolveResult, error)
func ResolveProject(name string) (*ResolveResult, error)
func Resolve(github, project string) (*ResolveResult, error)
```

Pure functions. No state. Used by: `api/handlers.go`, `cli/code.go`

## Data Flow

### Setup
```
cli/setup.go
  → provision/tools.go      install tools
  → provision/user.go       create claude user
  → auth/oauth.go           authenticate Claude
  → service/vnc.go          start VNC + Chrome
  → service/amserver.go     start am-server
  → api/daemon.go           start cbx serve daemon
  → service/tunnel.go       start tunnels
  → print output
```

### Create Session (API)
```
api/handlers.go (POST /sessions)
  → workspace/resolve.go    find/clone/create workdir
  → session/manager.go      create tmux session
  → return { name, dir, status, rcURL }
```

### Create Session (CLI)
```
cli/code.go
  → workspace/resolve.go    find/clone/create workdir
  → session/manager.go      create tmux session
  → print result
```

### Status
```
cli/status.go
  → service/*.Status()      check each service
  → session/manager.List()  list sessions
  → print
```

## Key Principles

1. **workspace/ is the only place that resolves repos/projects** — no duplication
2. **session/manager.go is the only place that touches tmux** — no duplication
3. **service/ wraps each external service** — unified Start/Stop/Status
4. **api/ only does HTTP** — delegates to session/ and workspace/
5. **cli/ only does TUI** — delegates to everything else
6. **auth/ only does authentication** — OAuth and API keys
7. **provision/ only runs during setup** — tools and user creation

## Migration Path

1. Extract `workspace/resolve.go` from `code/` and `serve/`
2. Clean `session/` — remove user switching (that's provision's job)
3. Split `auth/` — move user creation to `provision/user.go`
4. Split `activate/` — am-server to `service/amserver.go`, MCP config to `provision/user.go`
5. Rewrite `serve/` as `api/` using session/ and workspace/
6. Rewrite `code/` as `cli/code.go` using session/ and workspace/
7. Rewrite `status/` as `cli/status.go` using service/ and session/
8. Delete `activate/`, old `code/`, old `serve/`
