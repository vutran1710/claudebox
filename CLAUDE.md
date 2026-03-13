# Claude DevBox

This is a dedicated remote development server. Supports deployment to Railway or DigitalOcean.

## Installed Tools

- **Claude Code CLI** — AI coding assistant (runs with `--dangerously-skip-permissions` for full autonomy)
- **Agent Browser** — headless browser automation CLI for testing web apps
- **Playwright** — browser automation and end-to-end testing framework
- **GitHub CLI** (`gh`) — GitHub operations
- **Vercel CLI** (`vercel`) — deploy and manage Vercel projects
- **Supabase CLI** (`supabase`) — local Supabase dev and edge functions
- **Docker** — container management
- **Wormhole** (`wormhole`) — expose local ports to internet via HTTPS tunnel
- **Rust + Cargo** — systems programming
- **Python 3.13 + uv** — Python dev with fast package management
- **Node.js 22** + npm
- **Go** — for building Go-based tools

## Skills

- `/agent-browser` — reference for using agent-browser CLI to interact with web pages
- `/wormhole` — reference for using wormhole CLI to tunnel local ports

## Workspace

All projects should be cloned into `/workspace`.

## Web App Development Workflow

1. Clone a repo into `/workspace`
2. Install dependencies and start the dev server
3. Use `agent-browser` to navigate, interact, and screenshot the app
4. Use `wormhole` to share the dev server URL for external testing
5. Use `vercel` for deployments, `supabase` for backend, `gh` for GitHub operations

## Conventions

- Take screenshots after UI changes and read them to verify visually
- Use `agent-browser snapshot` to inspect page structure before interacting
- Use `wormhole http <port> --headless` when running tunnels in background
- Always close agent-browser sessions when done (`agent-browser close`)
