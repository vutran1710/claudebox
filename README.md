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

# 3. Open the Claude app — your session appears automatically
```

## Commands

```bash
# Setup (as root, one-time)
cbx setup                              # install tools, authenticate, start VNC

# Sessions (as claude user)
cbx code                               # new session in /workspace
cbx code -g owner/repo                 # clone/find repo, start session
cbx code -p my-project                 # open existing project
cbx code --headless -g owner/repo      # non-interactive mode

# API daemon
cbx serve                              # start API server (foreground)
cbx serve -d                           # start as daemon
cbx serve --stop                       # stop daemon

# Utilities
cbx status                             # show all services + sessions
cbx show api-key                       # show API key for cbx serve
cbx help                               # full help
```

## Session Management API

When `cbx serve` is running (port 8091):

```
POST   /sessions     Create session { name, github?, project? }
GET    /sessions     List active sessions
DELETE /sessions/:n  Kill session
GET    /health       Health check
```

Auth: `X-API-Key` header or `?key=` query param.

See [docs/cbx-serve-api.md](docs/cbx-serve-api.md) for full spec.

## Deploy

### GitHub Actions (recommended)

1. Fork/push this repo to GitHub
2. Add secrets (Settings > Secrets > Actions):

| Secret | Required | Purpose |
|--------|----------|---------|
| `DIGITALOCEAN_ACCESS_TOKEN` | For DigitalOcean | DigitalOcean API token |
| `RAILWAY_TOKEN` | For Railway | Railway API token |
| `SSH_PUBLIC_KEY` | Yes | Your SSH public key |
| `GH_TOKEN` | No | GitHub PAT for `gh` CLI auth on the server |

3. Go to **Actions** > **Deploy to DigitalOcean** > **Run workflow**
4. Choose region and size, click **Run** (defaults to Singapore)

### Docker (local)

```bash
docker build -t claudebox .
docker run -d -p 2222:22 -p 6080:6080 \
  -e SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)" \
  claudebox
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
  +-- cbx serve (API daemon)
  +-- Chrome Lite MCP (browser automation + plugins)
  +-- am-server (message store)
  +-- VNC desktop (Chrome + Fluxbox)
```

See [docs/architecture.md](docs/architecture.md) for full diagram.

## Code Structure

```
cmd/cbx/          Cobra CLI
internal/
  auth/           OAuth + API key management
  provision/      Tool installation + user creation
  workspace/      GitHub repo + project resolution
  session/        Manager interface + TmuxManager
  service/        Service interface (VNC, AMServer)
  serve/          HTTP API server
  shell/          Shell execution utilities
  setup/          TUI setup flow
  code/           TUI session creation
  status/         Status display
  ui/             TUI components
tests/            Integration tests + E2E script
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
| 5900 | VNC | localhost only |
| 6080 | noVNC | Cloudflare tunnel |
| 7331 | Chrome Lite MCP | localhost only |
| 8090 | am-server | Cloudflare tunnel |
| 8091 | cbx serve | Cloudflare tunnel |

## Security

- SSH: key-based auth only
- VNC: password-protected, Cloudflare tunnel
- am-server: API key, Cloudflare tunnel
- cbx serve: API key, Cloudflare tunnel
- DigitalOcean firewall: only port 22 open
