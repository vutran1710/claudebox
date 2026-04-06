# ClaudeBox TODO

## Phase 2: cbx serve as control plane (feat/plugin-architecture branch)

### 1. Update cbx serve API
- [ ] `name` field required in POST /sessions
- [ ] Response includes `dir` and `status` (cloned/found/created/running)
- [ ] Handle `project` param: locate existing dir or create new one
- [ ] Handle `github` param: clone if not found, report status

### 2. Cloudflare tunnel for cbx serve
- [ ] `cbx serve` starts Cloudflare tunnel alongside HTTP server
- [ ] Tunnel URL stored in state file for retrieval
- [ ] `cbx status` shows serve tunnel URL

### 3. cbx setup — single command flow
- [ ] Remove master session spawning from setup
- [ ] Start cbx serve daemon at the end of setup
- [ ] Start Cloudflare tunnel for serve daemon
- [ ] Print VNC URL + password (for web app login)
- [ ] Print instruction block with CLI + API + MCP info
- [ ] User pastes instruction block into Claude app to get started

### 4. Simplify commands
- [ ] `cbx activate` removed or merged into setup
- [ ] `cbx code` calls serve API internally (thin wrapper)
- [ ] `cbx show api-key` shows serve API key

### 5. CLAUDE.md update
- [ ] Remove master session references
- [ ] Document cbx serve API endpoints
- [ ] Document Chrome Lite MCP plugin commands
- [ ] Sessions are created on-demand, not pre-spawned

## Done

### ClaudeBox core (main branch)
- [x] `cbx setup` — install tools, OAuth, VNC, master session
- [x] `cbx activate` — am-server, Chrome Lite MCP config, Claude session
- [x] `cbx code` — spawn sessions with -g (GitHub) and -p (project)
- [x] `cbx code --headless` — non-interactive mode for master session
- [x] `cbx status` — show all services + sessions
- [x] `cbx show api-key` — view serve API key
- [x] `cbx serve` — HTTP API daemon with auth (POST/GET/DELETE sessions)
- [x] Cobra CLI, version injection from git tag
- [x] Cloud-init wait, dpkg lock handling, unattended-upgrades
- [x] Deploy/undeploy via GitHub Actions (DO + Railway, default Singapore)
- [x] Cloudflare tunnels for VNC + am-server

### Chrome Lite MCP
- [x] Plugin system: loader, scheduler, API (plugins/tools/get/post/create_job)
- [x] Plugin lifecycle: init → awaiting_login → confirm → ready
- [x] Extension side panel UI for plugin status
- [x] Gmail plugin: list, read, get_unread, select, mark_read, delete, archive
- [x] Discord, Zalo, Messenger, Slack plugin stubs
- [x] Background jobs with webhook delivery
- [x] Typed results: { type, data, metadata }
- [x] 35 unit tests

### am-server
- [x] /webhook/chrome-lite-mcp endpoint
- [x] Array expansion into individual messages
- [x] Field extraction (sender, subject, preview)

## Future

### Plugins to develop
- [ ] GitHub plugin (notifications, PRs, issues)
- [ ] Google Calendar plugin
- [ ] Telegram plugin
- [ ] Custom webhook receiver plugin

### Features
- [ ] Plugin hot-reload (watch plugins/ dir)
- [ ] Job persistence (survive server restart)
- [ ] Plugin config (per-plugin settings file)
- [ ] Rate limiting on serve API
- [ ] Session auto-cleanup (kill idle sessions)
- [ ] TypeScript type definitions for plugin interface
