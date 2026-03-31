<p align="center">
  <img src="logo.svg" width="200" />
</p>

# ClaudeBox

A cloud dev server for Claude Code. Deploy, SSH in, start coding.

## Quick Start

```bash
# 1. Deploy (GitHub Actions or manual)
# 2. SSH in and authenticate Claude
ssh root@<host>
./login-claude.sh

# 3. Start coding
ssh claude@<host>
cd /workspace && claude
```

## What Is This

ClaudeBox puts your dev environment on a server in the cloud. SSH in from anywhere — iPad, Chromebook, phone. Claude writes the code, agent-browser tests it, Cloudflare tunnels share it. Your job is to think and type directions.

All tools come pre-installed: Claude Code CLI, Node.js 22, Python 3.13, Rust, Go, Docker, GitHub CLI, Vercel CLI, Supabase CLI, Playwright, Chromium, and more.

## Deploy

### Option A: GitHub Actions (recommended)

1. Fork/push this repo to GitHub
2. Add secrets (Settings → Secrets → Actions):

| Provider | Secrets |
|----------|---------|
| **DigitalOcean** | `DIGITALOCEAN_ACCESS_TOKEN`, `SSH_PUBLIC_KEY` |
| **Railway** | `RAILWAY_TOKEN`, `SSH_PUBLIC_KEY` |

Optional: add `GH_TOKEN` (GitHub PAT) to auto-authenticate `gh` CLI on the server.

3. Go to **Actions** → pick a deploy workflow → **Run workflow**
4. SSH access info appears in the workflow summary

### Option B: Manual (DigitalOcean)

```bash
doctl compute droplet create claudebox \
  --image ubuntu-24-04-x64 \
  --size s-4vcpu-8gb \
  --region nyc1 \
  --ssh-keys <your-key-id> \
  --user-data-file cloud-init.yml \
  --wait
```

### Option C: Docker (local or any host)

```bash
docker build -t claudebox .
docker run -d -p 2222:22 -p 6080:6080 \
  -e SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)" \
  claudebox

ssh -p 2222 root@localhost
```

## Setup After Deploy

### 1. Wait for setup to finish (DigitalOcean only)

Setup runs in background via cloud-init. Monitor with:

```bash
ssh root@<host> 'tail -f /var/log/devbox-setup.log'
```

Done when you see `ClaudeBox setup complete!`

### 2. Authenticate Claude

```bash
ssh root@<host>
./login-claude.sh
```

The script navigates the OAuth flow, prints a URL — open it in your browser, sign in, paste the auth code back. Done.

### 3. Start coding

```bash
ssh claude@<host>         # non-root user with --dangerously-skip-permissions
cd /workspace
git clone https://github.com/your/project.git
cd project
claude
```

Use `claude` user for coding (autonomy mode). Use `root` for admin tasks.

## VNC Desktop

One command starts a password-protected VNC desktop with Chromium, tunneled to the internet via Cloudflare:

```bash
ssh root@<host> 'start-vnc'
```

Output:

```
================================================
  VNC Desktop Ready
================================================
  URL:      https://random-words.trycloudflare.com/vnc.html
  Password: xK9mQ7vLp3nR

  Stop:     start-vnc --stop
================================================
```

Open the URL in your browser, enter the password. You'll see a full desktop with Chromium running.

### What it does

- Starts Xvfb (virtual display) + Fluxbox (window manager)
- Starts x11vnc (VNC server, password-protected) + noVNC (web proxy)
- Launches Chromium on the virtual desktop
- Opens a Cloudflare tunnel to expose noVNC publicly
- Auto-generates a strong password if `VNC_PASSWORD` is not set

### Custom resolution

```bash
start-vnc 1920x1080
```

### Run apps on the desktop

```bash
DISPLAY=:99 chromium --no-sandbox http://localhost:3000 &
```

### Stop

```bash
start-vnc --stop
```

## Tunneling

### Cloudflare (default for VNC)

VNC uses Cloudflare tunnels automatically via `start-vnc`. For other services:

```bash
cloudflared tunnel --url http://localhost:3000
```

### Wormhole (alternative)

```bash
wormhole http 3000                          # random subdomain
wormhole http 3000 --subdomain my-preview   # custom (requires login)
```

## tmux

Keep Claude running across SSH disconnects:

```bash
tmux new -s claude        # new session
tmux attach -t claude     # reattach later
# Ctrl+B then D           # detach (keeps running)
```

## Tools

| Tool | Version | Purpose |
|------|---------|---------|
| Claude Code CLI | latest | AI coding assistant |
| Chromium | 147+ | Desktop browser (on VNC) + headless |
| Agent Browser | latest | Headless browser automation CLI |
| Playwright | latest | E2E testing framework |
| Node.js | 22 | JavaScript runtime |
| Python | 3.13 (via uv) | Python dev |
| Rust + Cargo | 1.84 | Systems programming |
| Go | 1.24 | Go tooling |
| Docker | latest | Container management |
| GitHub CLI | latest | Git operations, PRs, issues |
| Vercel CLI | latest | Frontend deployments |
| Supabase CLI | latest | Backend/database |
| Wormhole | latest | HTTPS tunneling |
| Cloudflared | latest | Cloudflare tunneling |

## Architecture

```
You (SSH) ──→ ClaudeBox (cloud server)
               ├── claude user (autonomy mode, --dangerously-skip-permissions)
               ├── root user (admin, tool management)
               ├── /workspace (project directory)
               ├── VNC desktop (Chromium + Fluxbox)
               │    └── Cloudflare tunnel → public URL
               └── Dev servers → Cloudflare/Wormhole tunnel → public URL
```

## Ports

| Port | Service | Access |
|------|---------|--------|
| 22 | SSH | Direct (key-based auth only) |
| 6080 | noVNC | Via Cloudflare tunnel (password-protected) |
| 5900 | VNC | localhost only |
