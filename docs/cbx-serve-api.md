# cbx serve — Session Management API

## Overview

`cbx serve` runs as a daemon, exposing an HTTP API for managing Claude Code sessions. Protected by API key. Accessible remotely via Cloudflare tunnel.

## Endpoints

### GET /health

Public. No auth required.

```
→ { "status": "ok" }
```

### POST /sessions

Create a Claude Code session.

**Request:**

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Session name (used as tmux session name) |
| `github` | No | GitHub repo (owner/repo) — clones if not found in /workspace |
| `project` | No | Existing directory name in /workspace |

```json
// Clone a GitHub repo and start session
{ "name": "my-app", "github": "owner/repo" }

// Open existing project
{ "name": "my-app", "project": "existing-dir" }

// Blank session in /workspace
{ "name": "scratch" }
```

**Response:**

```json
{
  "name": "my-app",
  "dir": "/workspace/repo",
  "status": "cloned",
  "remote_control": "https://claude.ai/code/session_..."
}
```

Status values:
- `cloned` — GitHub repo was cloned
- `found` — existing project directory was found
- `created` — new directory was created
- `running` — session started in /workspace (no project)

**Errors:**

```json
// Missing name
{ "error": "name is required" }

// Project not found
{ "error": "project not found: /workspace/foo" }

// Clone failed
{ "error": "clone failed: ..." }
```

### GET /sessions

List all active sessions.

```json
[
  {
    "name": "my-app",
    "dir": "/workspace/repo",
    "status": "running",
    "remote_control": ""
  },
  {
    "name": "scratch",
    "dir": "/workspace",
    "status": "running",
    "remote_control": ""
  }
]
```

Note: `remote_control` URL is only available at creation time. After that, the session is accessible via the Claude app's session list.

### DELETE /sessions/{name}

Kill a session.

```json
{ "deleted": "my-app" }
```

## Authentication

All endpoints except `/health` require an API key.

Pass via header or query parameter:
```
X-API-Key: <key>
# or
?key=<key>
```

Key is generated on first `cbx serve` run. View with:
```bash
cbx show api-key
```

## Running

```bash
# Foreground
cbx serve

# Background daemon
cbx serve -d

# Custom port
cbx serve -P 9000

# Stop daemon
cbx serve --stop
```

Default port: 8091.
PID file: `/tmp/cbx-serve.pid`
API key file: `/tmp/cbx-serve.key`

## Setup Integration

`cbx setup` starts the serve daemon automatically and creates a Cloudflare tunnel. The setup output includes the public API URL and instructions for the user.

## Usage from Claude App

User pastes the instruction block from `cbx setup` output into a Claude chat. Claude can then call the API to manage sessions:

```
Claude: "Start a session for my-app repo"
→ POST https://cbx-tunnel-url/sessions
  { "name": "my-app", "github": "owner/my-app" }
→ { "name": "my-app", "dir": "/workspace/my-app", "status": "cloned" }
→ Claude: "Session 'my-app' created in /workspace/my-app"
```
