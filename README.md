<p align="center">
  <img src="logo.svg" width="200" />
</p>

# ClaudeBox

Your personal Claude Code agent, always on, accessible from anywhere.

Deploy Claude Code to a cloud server and access it from your phone, tablet, or any device via the official Claude app. No static workstation needed — your dev environment lives in the cloud, ready when you are.

## Why

- Work from anywhere — phone, iPad, coffee shop laptop
- Claude Code runs 24/7 with full autonomy, pre-authenticated
- Session management via HTTP API — create, list, kill sessions remotely
- Browser automation for Gmail, Discord, Zalo, Messenger via Chrome Lite MCP (optional)
- Messages aggregated to am-server, queryable anytime (optional)

## Quick Start

```bash
# 1. Deploy via GitHub Actions (DigitalOcean or Railway)

# 2. Setup — install tools, authenticate, start services
ssh -t root@<host> 'cbx setup'

# 3. Save the printed skill to ~/.claude/skills/claudebox/SKILL.md
#    or paste it into a Claude Project at claude.ai/projects

# 4. Start working from the Claude app
```

Setup prints a ready-to-use skill file with your server's URLs and API keys baked in. Save it and Claude knows how to manage your server.

## Commands

```bash
# Setup (as root, one-time)
cbx setup                                  # install tools, authenticate, start services

# Sessions
cbx code hello-world                       # find or create /workspace/hello-world
cbx code my-app --repo owner/repo          # clone GitHub repo
cbx code my-app --repo https://...         # clone any git URL
cbx code my-app --headless                 # non-interactive mode

# API daemon
cbx serve                                  # start API server (foreground)
cbx serve -d                               # start as daemon
cbx serve --stop                           # stop daemon

# Utilities
cbx status                                 # show all services + sessions
cbx show api-key                           # show API key for cbx serve
```

## Session Management API

When `cbx serve` is running (port 8091, Cloudflare tunneled):

```
POST   /sessions     { name, repo? }    Create session
GET    /sessions                         List active sessions
DELETE /sessions/{name}                  Kill a session
GET    /health                           Health check
```

- `name` is required — maps to `/workspace/{name}`
- `repo` is optional — GitHub shorthand (`owner/repo`) or full git URL
- If session already exists, returns `"already running"`
- If no repo, finds existing dir or creates new with `git init`

Auth: `X-API-Key` header. See [docs/cbx-serve-api.md](docs/cbx-serve-api.md).

## Deploy

### GitHub Actions (recommended)

1. Fork/push this repo to GitHub
2. Add secrets (Settings > Secrets > Actions):

| Secret | Required | Purpose |
|--------|----------|---------|
| `DIGITALOCEAN_ACCESS_TOKEN` | For DigitalOcean | DigitalOcean API token |
| `RAILWAY_TOKEN` | For Railway | Railway API token |
| `SSH_PUBLIC_KEY` | Yes | Your SSH public key |
| `GH_TOKEN` | No | GitHub PAT for `gh` CLI auth |

3. Go to **Actions** > **Deploy to DigitalOcean** > **Run workflow**
4. Choose region and size, click **Run** (defaults to Singapore)

### Docker (local)

```bash
docker build -t claudebox .
docker run -d -p 2222:22 -p 6080:6080 \
  -e SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)" \
  claudebox
```

## Setup Flow

```
cbx setup
  ├── Wait for cloud-init
  ├── Install 15+ tools
  ├── Create claude user + gh auth
  ├── OAuth authenticate Claude Code
  ├── Start VNC + Chrome (with MCP extension)
  ├── Start am-server + Cloudflare tunnel
  ├── Start cbx serve + Cloudflare tunnel
  └── Print skill file with URLs and API keys
```

## Architecture

```
Phone/Tablet/Laptop
  |
  +-- Claude App --> Claude Code sessions (tmux)
  +-- HTTP API  --> cbx serve (port 8091) --> session management
  +-- SSH       --> direct access
        |
ClaudeBox (cloud server)
  +-- cbx serve (API daemon, Cloudflare tunneled)
  +-- Chrome Lite MCP (browser automation + plugins)
  +-- am-server (message store, Cloudflare tunneled)
  +-- VNC desktop (Chrome, Cloudflare tunneled)
```

See [docs/architecture.md](docs/architecture.md).

## Code Structure

```
cmd/cbx/            Cobra CLI
internal/
  auth/             OAuth + API key management
  provision/        Tool installation + user creation
  workspace/        Smart project resolution (find/clone/create)
  session/          Manager interface + TmuxManager
  service/          Service interface (VNC, AMServer)
  serve/            HTTP API server with auth
  setup/            TUI setup flow + skill template
  shell/            Shell execution utilities
  code/             Session creation TUI
  status/           Status display
  ui/               TUI components
tests/              Integration tests + E2E script
```

49 tests across 10 packages. See [docs/cbx-structure.md](docs/cbx-structure.md).

## Related Repos

| Repo | Purpose |
|------|---------|
| [chrome-lite-mcp](https://github.com/vutran1710/chrome-lite-mcp) | Browser automation MCP with plugin system |
| [am](https://github.com/vutran1710/am) | Message aggregation server |

## Ports

| Port | Service | Access |
|------|---------|--------|
| 22   | SSH | Direct (key auth) |
| 6080 | noVNC | Cloudflare tunnel |
| 7331 | Chrome Lite MCP | localhost only |
| 8090 | am-server | Cloudflare tunnel |
| 8091 | cbx serve | Cloudflare tunnel |
