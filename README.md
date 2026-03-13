<p align="center">
  <img src="logo.svg" width="200" />
</p>

# Claude DevBox

A dedicated remote development server deployed on Railway with Claude Code CLI and essential dev tools pre-installed.

## Why

I stopped writing code. I stopped reading code. Claude does both now.

So why am I still carrying a Macbook around like I'm the one who needs the compute?

Claude DevBox puts the dev environment where it belongs — on a server, in the cloud, accessible from anywhere. SSH in from an iPad, a Chromebook, your phone, whatever. Claude writes the code, agent-browser tests it, wormhole shares it. Your job is to think and type directions.

The Macbook stays home. You don't.

## What's Included

| Tool | Purpose |
|------|---------|
| Claude Code CLI | AI coding assistant |
| Agent Browser | Headless browser automation for testing web apps |
| GitHub CLI | Git operations, PRs, issues |
| Vercel CLI | Frontend deployments |
| Supabase CLI | Backend/database management |
| Docker | Container management |
| Wormhole | Tunnel localhost to internet (HTTPS) |
| Rust + Cargo | Systems programming |
| Python 3.13 + uv | Python dev with fast package management |
| Node.js 22 | JavaScript runtime |
| Go | For Go-based tooling |

## Deploy to Railway

### Via GitHub Actions (recommended)

1. Push this repo to GitHub
2. Create a Railway project and link it (or get a project token from Railway dashboard)
3. Add these **GitHub repo secrets** (Settings → Secrets → Actions):
   - `RAILWAY_TOKEN` — your Railway project token
   - `SSH_PUBLIC_KEY` — your public key (`cat ~/.ssh/id_ed25519.pub`)
4. Go to Actions → "Deploy to Railway" → Run workflow
5. In Railway, expose port 22 (TCP) in networking settings for SSH access
6. SSH in and attach to the tmux session:

```bash
ssh root@<railway-host> -p <port>
tmux attach -t claude
# Complete the OAuth login, then you're ready
```

### Manual deploy

```bash
# Install Railway CLI
curl -fsSL https://railway.com/install.sh | sh

# Link to your project
railway link

# Set required env var
railway variables set SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)"

# Deploy
railway up --detach
```

Then expose port 22 in Railway networking settings and SSH in.

## tmux Basics

```bash
tmux attach -t claude     # Attach to the claude session
# Ctrl+B then D           # Detach (keeps running)
tmux new -s work          # Create another session
tmux ls                   # List sessions
```

## Usage

SSH into the server, then:

```bash
# Start coding with Claude
cd /workspace
git clone https://github.com/your/project.git
cd project
claude

# Expose dev server to internet
wormhole http 3000

# Test the app with headless browser
agent-browser open "http://localhost:3000"
agent-browser screenshot /tmp/page.png
```

## Ports

Only port 22 (SSH) is exposed by default. All other ports are dynamic — expose whatever you need through Railway's networking settings or tunnel them via wormhole.
