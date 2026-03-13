<p align="center">
  <img src="logo.svg" width="200" />
</p>

# Claude DevBox

A dedicated remote development server with Claude Code CLI and essential dev tools pre-installed. Deploy to **Railway** or **DigitalOcean** — your choice.

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

## Deploy

Pick a cloud provider and run the corresponding GitHub Actions workflow.

| Provider | Deploy | Undeploy | Secrets Needed |
|----------|--------|----------|----------------|
| **Railway** | "Deploy to Railway" | "Undeploy from Railway" | `RAILWAY_TOKEN`, `SSH_PUBLIC_KEY` |
| **DigitalOcean** | "Deploy to DigitalOcean" | "Undeploy from DigitalOcean" | `DIGITALOCEAN_ACCESS_TOKEN`, `SSH_PUBLIC_KEY` |

### Quick Start

1. Push this repo to GitHub
2. Add the required **secrets** for your chosen provider (Settings → Secrets → Actions)
3. Go to **Actions** → pick your deploy workflow → **Run workflow**
4. SSH in and attach to the tmux session:

```bash
ssh root@<host>
tmux attach -t claude
# Complete the OAuth login, then you're ready
```

See [`cloud-providers/railway/`](cloud-providers/railway/) or [`cloud-providers/digitalocean/`](cloud-providers/digitalocean/) for provider-specific details and manual deploy instructions.

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

Port 22 (SSH) is exposed by default. Other ports depend on your provider — expose them through your provider's networking settings or tunnel them via wormhole.
