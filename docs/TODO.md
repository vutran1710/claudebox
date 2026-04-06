# ClaudeBox TODO

## Phase 2: Refactor & Control Plane (feat/plugin-architecture branch)

**Full plan:** [docs/refactor-plan.md](refactor-plan.md)

### Refactor (Steps 1-6)
- [ ] Step 1: Shell utilities (foundation)
- [ ] Step 2: Extract `workspace/` — repo & project resolution
- [ ] Step 3: Extract `session/` — clean Manager interface
- [ ] Step 4: Extract `service/` — unified Service interface (VNC, am-server, tunnel)
- [ ] Step 5: Clean `auth/` — OAuth + API keys only
- [ ] Step 6: Create `provision/` — tools + user creation, testable

### New Implementation (Steps 7-10)
- [ ] Step 7: Rewrite `api/` — HTTP server with interfaces
- [ ] Step 8: Rewrite `cli/` — TUI separated from logic
- [ ] Step 9: Wire everything in `cmd/cbx/main.go`
- [ ] Step 10: Integration & E2E tests

### Feature Requirements
- [ ] `name` required in POST /sessions
- [ ] Response includes `dir` and `status` (cloned/found/created/running)
- [ ] Cloudflare tunnel for cbx serve
- [ ] Setup: one command, prints VNC URL + instruction block
- [ ] No master session — sessions created on-demand
- [ ] `cbx activate` removed or merged into setup

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
