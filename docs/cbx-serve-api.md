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
| `name` | Yes | Session name — maps to `/workspace/{name}` |
| `repo` | No | Git repo to clone (owner/repo shorthand or full URL) |

```json
// Find or create /workspace/my-app
{ "name": "my-app" }

// Clone GitHub repo to /workspace/my-app
{ "name": "my-app", "repo": "owner/repo" }

// Clone any git URL
{ "name": "my-app", "repo": "https://github.com/owner/repo.git" }
```

**Response:**

```json
{
  "name": "my-app",
  "dir": "/workspace/my-app",
  "status": "cloned",
  "remote_control": "https://claude.ai/code/session_..."
}
```

If session already exists:
```json
{
  "name": "my-app",
  "status": "already running"
}
```

Status values:
- `cloned` — repo was cloned
- `found` — existing directory found
- `created` — new directory created with git init
- `already running` — session with this name exists

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
