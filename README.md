<p align="center">
  <img src="logo.svg" width="200" />
</p>

# ClaudeBox

A cloud dev server for Claude Code. Deploy, SSH in, start coding.

## Quick Start

```bash
# 1. Deploy via GitHub Actions (DigitalOcean)
# 2. Run setup
ssh -t root@<host> "cbx setup"

# 3. Install Chrome extension + login to apps via VNC
# 4. Activate message pollers
ssh -t root@<host> "cbx activate"

# 5. Start coding
ssh claude@<host>
cd /workspace && claude
```

## What Is This

ClaudeBox puts your dev environment on a server in the cloud. SSH in from anywhere — iPad, Chromebook, phone. Claude writes the code, agent-browser tests it, Cloudflare tunnels share it.

All tools come pre-installed. A TUI CLI (`cbx`) handles the entire setup — tool installation, Claude authentication, VNC desktop, and message polling.

## Deploy

### GitHub Actions (recommended)

1. Fork/push this repo to GitHub
2. Add secrets (Settings > Secrets > Actions):

| Secret | Required | Purpose |
|--------|----------|---------|
| `DIGITALOCEAN_ACCESS_TOKEN` | Yes | DigitalOcean API token |
| `SSH_PUBLIC_KEY` | Yes | Your SSH public key |
| `GH_TOKEN` | No | GitHub PAT for private repos |

3. Go to **Actions** > **Deploy to DigitalOcean** > **Run workflow**
4. Choose region and size, click **Run**
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
- Installs all tools (Rust, Node, Go, Python, Docker, Claude CLI, etc.)
- Creates the `claude` user with autonomy mode
- Authenticates Claude Code via OAuth (prints URL, you paste the code)
- Starts VNC desktop + Chrome + Cloudflare tunnel

Output:

```
  ClaudeBox Setup

  ✓ System dependencies
  ✓ Rust 1.84
  ✓ Python 3.13 + uv
  ✓ Node.js 22
  ...
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
    2. Install Claude-in-Chrome extension
    3. Log into Discord, Gmail, Zalo
    4. Run: cbx activate
```

### Step 2: Browser setup (manual)

Open the VNC URL, enter the password. In Chrome:
1. Install the Claude-in-Chrome extension
2. Log into Discord, Gmail (personal + work), Zalo

### Step 3: `cbx activate`

```bash
ssh -t root@<host> "cbx activate"
```

Starts am-server (message aggregation) with a Cloudflare tunnel and activates cron-based message pollers.

Output:

```
  ClaudeBox Activate

  ✓ Start am-server
  ✓ Start Cloudflare tunnel
  ✓ Activate pollers

  AM Server
    URL: https://abc.trycloudflare.com
    Key: 005ca208ad3a411abb1b3ec04c0bc57f

  Usage:
    curl $URL/healthz
    curl -H "X-API-Key: $KEY" $URL/api/messages
    curl -H "X-API-Key: $KEY" "$URL/api/messages?q=meeting"
    curl -H "X-API-Key: $KEY" $URL/api/stats

  Pollers:
    ✓ discord        every 5m
    ✓ gmail-personal every 5m
    ✓ gmail-work     every 5m
    ✓ zalo           every 5m
```

### Check status

```bash
ssh root@<host> "cbx status"
```

Shows health of all services:

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

  Pollers
    discord        ✓ every 5m (last: 2m ago, ok)
    gmail-personal ✓ every 5m (last: 3m ago, ok)
    gmail-work     ✓ every 5m (last: 4m ago, ok)
    zalo           ✓ every 5m (last: 1m ago, ok)
```

## Message Polling

ClaudeBox periodically reads unread messages from messaging apps via Chrome browser automation and forwards them to am-server.

| Poller | App | Interval |
|--------|-----|----------|
| `check-discord-messages.md` | Discord DMs | Every 5 min |
| `check-gmail-personal.md` | Gmail (personal) | Every 5 min |
| `check-gmail-work.md` | Gmail (work) | Every 5 min |
| `check-zalo-messages.md` | Zalo | Every 5 min |

Pollers use Claude Code CLI (`claude -p`) to execute prompt files against the Chrome browser. Messages are forwarded to am-server's `/ingest` endpoint.

## Tools

| Tool | Version | Purpose |
|------|---------|---------|
| Claude Code CLI | latest | AI coding assistant |
| Chromium | 147+ | Desktop browser (VNC) + headless |
| Agent Browser | latest | Headless browser automation CLI |
| Playwright | latest | E2E testing framework |
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
You (SSH) ──> ClaudeBox (cloud server)
               ├── cbx CLI (setup, activate, status)
               ├── claude user (autonomy mode)
               ├── /workspace (projects)
               ├── VNC desktop (Chrome + Fluxbox)
               │    └── Cloudflare tunnel -> public URL
               ├── am-server (message aggregation)
               │    └── Cloudflare tunnel -> public URL
               └── Pollers (cron, every 5m)
                    ├── Discord
                    ├── Gmail (personal + work)
                    └── Zalo
```

## Security

- SSH: key-based auth only (passwords disabled)
- VNC: password-protected, only accessible via Cloudflare tunnel
- am-server: API key required, only accessible via Cloudflare tunnel
- DigitalOcean firewall: only port 22 open
- All other ports (5900, 6080, 8090) blocked from external access

## Ports

| Port | Service | Access |
|------|---------|--------|
| 22 | SSH | Direct (key auth) |
| 5900 | VNC | localhost only |
| 6080 | noVNC | Via Cloudflare tunnel |
| 8090 | am-server | Via Cloudflare tunnel |
