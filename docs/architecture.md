# ClaudeBox Architecture

## Overview

ClaudeBox is a remote Claude Code server accessible from any device. It runs 24/7 in the cloud, manages Claude Code sessions, and optionally polls messaging apps.

## Components

```
Phone/Tablet/Laptop
  |
  ├── Claude App → Remote Control URL → Claude Code session
  ├── HTTP API  → cbx serve (port 8091) → session management
  └── SSH       → direct access
        |
        ▼
ClaudeBox (cloud server)
  |
  ├── cbx serve (daemon, port 8091)          ← control plane
  │   ├── POST /sessions                     ← create Claude Code session
  │   ├── GET  /sessions                     ← list sessions
  │   ├── DELETE /sessions/{name}            ← kill session
  │   └── Cloudflare tunnel → public URL
  │
  ├── Claude Code sessions (tmux)
  │   ├── claude-master                      ← primary session
  │   └── claude-{project}                   ← project sessions
  │
  ├── Chrome Lite MCP (port 7331)            ← browser automation
  │   ├── Plugin system (gmail, discord, zalo, messenger, slack)
  │   ├── Background jobs (scheduler)
  │   └── Chrome extension (side panel UI)
  │
  ├── am-server (port 8090)                  ← message store
  │   ├── POST /ingest
  │   ├── POST /webhook/chrome-lite-mcp
  │   ├── GET  /api/messages
  │   └── Cloudflare tunnel → public URL
  │
  └── VNC desktop (port 6080)                ← visual access
      ├── Chrome + MCP extension
      └── Cloudflare tunnel → public URL
```

## Setup Flow

```
1. Deploy via GitHub Actions (DigitalOcean)
2. ssh -t root@host 'cbx setup'
   ├── Wait for cloud-init
   ├── Install all tools (15+)
   ├── Create claude user
   ├── OAuth authenticate
   ├── Start VNC + Chrome
   ├── Start cbx serve daemon + Cloudflare tunnel
   ├── Start master Claude session
   └── Print: Remote Control URL, VNC URL, API URL + key
3. Done — access from phone via Remote Control URL
```

## Session Management

All sessions go through `cbx serve`:

```
# Via API (from phone, another service, or Claude master session)
POST http://cbx-serve-url/sessions
  { "name": "my-project", "github": "owner/repo" }
  → { "name": "my-project", "remote_control": "https://claude.ai/code/...", "status": "running" }

# Via CLI (from SSH)
cbx code -g owner/repo
cbx code -p existing-project
```

## Data Flow (Message Polling)

```
1. Claude or scheduler calls: create_job("gmail", { tool: "get_unread", ... })
2. Scheduler runs get_unread every N minutes
3. Gmail plugin opens Chrome tab, runs JS, extracts emails
4. Results POSTed to webhook: POST am-server/webhook/chrome-lite-mcp
5. am-server expands array into individual messages
6. Queryable: GET am-server/api/messages?source=gmail
```

## Ports

| Port | Service | Access |
|------|---------|--------|
| 22   | SSH | Direct (key auth) |
| 5900 | VNC | localhost → Cloudflare tunnel |
| 6080 | noVNC | localhost → Cloudflare tunnel |
| 7331 | Chrome Lite MCP | localhost only |
| 8090 | am-server | localhost → Cloudflare tunnel |
| 8091 | cbx serve | localhost → Cloudflare tunnel |

## Repos

| Repo | Purpose |
|------|---------|
| [claudebox](https://github.com/vutran1710/claudebox) | Server setup, CLI, session management |
| [chrome-lite-mcp](https://github.com/vutran1710/chrome-lite-mcp) | Browser automation MCP with plugin system |
| [am](https://github.com/vutran1710/am) | Message aggregation server |
