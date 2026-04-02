<p align="center">
  <img src="logo.svg" width="200" />
</p>

# ClaudeBox

Your personal Claude Code agent, always on, accessible from anywhere.

Deploy Claude Code to a cloud server and access it from your phone, tablet, or any device via the official Claude app. No static workstation needed — your dev environment lives in the cloud, ready when you are.

## Why

- Work from anywhere — phone, iPad, coffee shop laptop
- Claude Code runs 24/7 with full autonomy, pre-authenticated
- Browser automation reads your Gmail, Discord, Zalo, Messenger via Chrome Lite MCP
- Messages aggregated to am-server, queryable anytime
- VNC desktop for visual tasks, tunneled via Cloudflare

## Quick Start

```bash
# 1. Deploy via GitHub Actions (DigitalOcean or Railway)
# 2. Run setup
ssh -t root@<host> "cbx setup"

# 3. Log into messaging apps via VNC
# 4. Activate services
ssh -t root@<host> "cbx activate"

# 5. Access Claude Code from the official Claude app or SSH
ssh claude@<host>
cd /workspace && claude
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

3. Go to **Actions** > **Deploy to DigitalOcean** > **Run workflow**
4. Choose region and size, click **Run** (defaults to Singapore)
5. SSH command appears in the workflow summary

### Docker (local)

```bash
docker build -t claudebox .
docker run -d -p 2222:22 -p 6080:6080 \
  -e SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)" \
  claudebox
```

## Setup

### Step 1: `cbx setup`

```bash
ssh -t root@<host> "cbx setup"
```

This runs a TUI that:
- Installs all tools (Rust, Node, Go, Python, Docker, Claude CLI, Chrome Lite MCP, etc.)
- Creates the `claude` user with autonomy mode
- Authenticates Claude Code via OAuth (prints URL, you paste the code)
- Starts VNC desktop + Chrome (with MCP extension) + Cloudflare tunnel

Output:

```
  ClaudeBox Setup

  ✓ System dependencies
  ✓ Rust 1.84
  ✓ Python 3.13 + uv
  ✓ Node.js 22
  ...
  ✓ Chrome Lite MCP
  ✓ am-server

  Open this URL to sign in:
  https://claude.com/cai/oauth/authorize?...

  Auth code: <paste here>

  ✓ Login successful
  ✓ VNC + Chrome started

  VNC:      https://xyz.trycloudflare.com/vnc.html
  Password: tUDvpVIHtS5s

  Next steps:
    1. Open VNC URL in your browser
    2. Log into Discord, Gmail, Zalo
    3. Run: cbx activate
```

### Step 2: Browser login (manual, one-time)

Open the VNC URL, enter the password. In Chrome, log into:
- Gmail (personal + work)
- Discord
- Zalo (QR code)
- Messenger (if needed)

### Step 3: `cbx activate`

```bash
ssh -t root@<host> "cbx activate"
```

Starts am-server (message aggregation) with a Cloudflare tunnel and configures Chrome Lite MCP for Claude Code.

Output:

```
  ClaudeBox Activate

  ✓ Start am-server
  ✓ Start Cloudflare tunnel
  ✓ Configure Chrome Lite MCP

  AM Server
    URL: https://abc.trycloudflare.com
    Key: 005ca208ad3a411abb1b3ec04c0bc57f

  Usage:
    curl $URL/healthz
    curl -H "X-API-Key: $KEY" $URL/api/messages
    curl -H "X-API-Key: $KEY" $URL/api/stats

  Chrome Lite MCP
    Chrome Lite MCP configured in Claude Code
    Claude can now read Gmail, Discord, Zalo, Messenger, Slack
    via Chrome browser automation
```

### Check status

```bash
ssh root@<host> "cbx status"
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
```

## How Messages Work

Claude Code reads messages from your apps using Chrome Lite MCP — a browser automation MCP server that controls Chrome via extension + DevTools Protocol.

```
Claude Code  <-MCP->  Chrome Lite MCP  <-WebSocket->  Chrome Extension  <-Chrome API->  Gmail/Discord/Zalo/...
                                                                                              |
                                                                                              v
                                                                                         am-server (SQLite)
                                                                                              |
                                                                                              v
                                                                                    GET /api/messages (query anytime)
```

- **Read**: Claude navigates to each app, runs JS snippets to extract messages
- **Store**: Structured messages are pushed to am-server for persistent storage
- **Reply**: Claude can navigate back and send replies when you ask
- **Query**: am-server is queryable anytime via REST API, even when Claude isn't running

Supported apps: Gmail, Discord, Zalo, Messenger (non-E2EE), Slack

## Tools

| Tool | Version | Purpose |
|------|---------|---------|
| Claude Code CLI | latest | AI coding assistant |
| Chrome Lite MCP | latest | Browser automation via MCP |
| Chromium | 147+ | Desktop browser (VNC) |
| Agent Browser | latest | Headless browser automation CLI |
| Playwright | latest | E2E testing framework |
| am-server | latest | Message aggregation + search |
| Node.js | 22 | JavaScript runtime |
| Python | 3.13 (via uv) | Python dev |
| Rust + Cargo | 1.84 | Systems programming |
| Go | 1.24 | Go tooling |
| Docker | latest | Container management |
| GitHub CLI | latest | Git operations |
| Vercel CLI | latest | Frontend deployments |
| Supabase CLI | latest | Backend/database |
| Cloudflared | latest | Cloudflare tunneling |

## Architecture

```
You (phone/tablet/laptop)
  |
  ├── Claude App (mobile) ──> Claude Code (remote, always on)
  └── SSH ──> ClaudeBox (cloud server)
               ├── cbx CLI (setup, activate, status)
               ├── claude user (autonomy mode)
               ├── /workspace (projects)
               ├── VNC desktop (Chrome + Fluxbox)
               │    ├── Chrome Lite MCP extension
               │    └── Cloudflare tunnel -> public URL
               └── am-server (message store)
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
