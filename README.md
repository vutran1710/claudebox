<p align="center">
  <img src="logo.svg" width="200" />
</p>

# ClaudeBox

Your personal Claude Code agent, always on, accessible from anywhere.

Deploy Claude Code to a cloud server and access it from your phone, tablet, or any device via the official Claude app. No static workstation needed — your dev environment lives in the cloud, ready when you are.

## Why

- Work from anywhere — phone, iPad, coffee shop laptop
- Claude Code runs 24/7 with full autonomy, pre-authenticated
- Browser automation reads your Gmail, Discord, Zalo, Messenger via Chrome Lite MCP (optional)
- Messages aggregated to am-server, queryable anytime (optional)
- VNC desktop for visual tasks, tunneled via Cloudflare

## Quick Start

```bash
# 1. Deploy via GitHub Actions (DigitalOcean or Railway)

# 2. Setup — install tools + authenticate (as root)
ssh -t root@<host> 'cbx setup'

# 3. Activate — start Claude Code session (as claude user)
ssh -t claude@<host> 'cbx activate'

# 4. Access via Remote Control URL printed by activate
#    or attach directly:
ssh -t claude@<host> 'tmux attach -t claude-main'
```

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
5. Commands with real IP appear in the workflow summary

### Docker (local)

```bash
docker build -t claudebox .
docker run -d -p 2222:22 -p 6080:6080 \
  -e SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)" \
  claudebox
```

## Commands

### `cbx setup` (as root, required)

Installs all dev tools, authenticates Claude Code via OAuth, and starts VNC + Chrome.

```bash
ssh -t root@<host> 'cbx setup'
```

This runs a TUI that:
- Waits for cloud-init to finish
- Installs all tools (Cloudflare, Rust, Node, Go, Python, Docker, Claude CLI, Chrome Lite MCP, etc.)
- Creates the `claude` user with autonomy mode
- Authenticates Claude Code via OAuth (prints URL, you paste the code)
- Starts VNC desktop + Chrome (with MCP extension) + Cloudflare tunnel

After setup completes, you'll see the VNC URL and password.

### `cbx activate` (as claude user)

Starts services and spawns a Claude Code session.

```bash
# Basic — starts am-server + Claude Code session
ssh -t claude@<host> 'cbx activate'

# With message poller — also spawns a second session for polling
ssh -t claude@<host> 'cbx activate --poller'
```

What it does:
1. Starts am-server + Cloudflare tunnel
2. Configures Chrome Lite MCP for Claude Code
3. Spawns `claude-main` tmux session with remote-control + dangerously-skip-permissions
4. (with `--poller`) Spawns `claude-poller` session for message polling

Output includes:
- AM Server URL and API key
- **Remote Control URL** — open this on your phone/tablet to use Claude Code

### `cbx code [name]` (as claude user)

Spawn additional Claude Code sessions on demand.

```bash
ssh -t claude@<host> 'cbx code my-project'
```

Creates a new tmux session with remote-control enabled. Prints the Remote Control URL for mobile access.

### `cbx status`

Shows health of all services and active sessions.

```bash
ssh root@<host> 'cbx status'
```

```
  ClaudeBox Status

  Claude Code      ✓ authenticated
  VNC              ✓ running
                     https://xyz.trycloudflare.com/vnc.html
                     password: tUDvpVIHtS5s
  Chrome           ✓ running (PID 1234)
  am-server        ✓ running (port 8090)
                     https://abc.trycloudflare.com
                     key: 005ca208...
  Chrome Lite MCP  ✓ configured

  Sessions
    ✓ claude-main: 1 windows (created ...)
    ✓ claude-poller: 1 windows (created ...)
```

### `cbx help`

Shows detailed help with all commands and workflow.

## Message Polling (optional)

If you want Claude to read your messages from Gmail, Discord, Zalo, Messenger, or Slack:

1. After `cbx setup`, open the VNC URL and log into your apps in Chrome
2. Run `cbx activate --poller`
3. Claude can now read messages via Chrome Lite MCP and push them to am-server

```
Claude Code  <-MCP->  Chrome Lite MCP  <-WebSocket->  Chrome Extension  <->  Gmail/Discord/Zalo/...
                                                                                    |
                                                                                    v
                                                                               am-server (SQLite)
                                                                                    |
                                                                                    v
                                                                          GET /api/messages (query anytime)
```

This is entirely optional — if you just want a remote dev environment, `cbx setup` + `cbx activate` (without `--poller`) is all you need.

## Tools

| Tool | Purpose |
|------|---------|
| Claude Code CLI | AI coding assistant |
| Chrome Lite MCP | Browser automation via MCP |
| Chromium | Desktop browser (VNC) |
| Agent Browser | Headless browser automation CLI |
| Playwright | E2E testing framework |
| am-server | Message aggregation + search |
| Node.js 22 | JavaScript runtime |
| Python 3.13 (uv) | Python dev |
| Rust + Cargo 1.84 | Systems programming |
| Go 1.24 | Go tooling |
| Docker | Container management |
| GitHub CLI | Git operations |
| Vercel CLI | Frontend deployments |
| Supabase CLI | Backend/database |
| Cloudflared | Cloudflare tunneling |

## Architecture

```
You (phone/tablet/laptop)
  |
  ├── Claude App (mobile) ──> Remote Control URL ──> Claude Code session
  └── SSH ──> ClaudeBox (cloud server)
               ├── cbx CLI (setup, activate, code, status)
               ├── claude user (autonomy mode)
               ├── /workspace (projects)
               ├── tmux sessions
               │    ├── claude-main (primary Claude Code session)
               │    └── claude-poller (optional, message polling)
               ├── VNC desktop (Chrome + Fluxbox)
               │    ├── Chrome Lite MCP extension
               │    └── Cloudflare tunnel -> public URL
               └── am-server (message store, optional)
                    └── Cloudflare tunnel -> public URL
```

## Security

- SSH: key-based auth only (passwords disabled)
- VNC: password-protected, only accessible via Cloudflare tunnel
- am-server: API key required, only accessible via Cloudflare tunnel
- DigitalOcean firewall: only port 22 open
- All other ports (5900, 6080, 7331, 8090) blocked from external access

## Ports

| Port | Service | Access |
|------|---------|--------|
| 22 | SSH | Direct (key auth) |
| 5900 | VNC | localhost only |
| 6080 | noVNC | Via Cloudflare tunnel |
| 7331 | Chrome Lite MCP | localhost only |
| 8090 | am-server | Via Cloudflare tunnel |
