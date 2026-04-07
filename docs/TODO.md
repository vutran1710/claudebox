# ClaudeBox TODO

## Phase 2: Refactor & Control Plane (feat/plugin-architecture branch)

**Full plan:** [docs/refactor-plan.md](refactor-plan.md)

### Refactor (Steps 1-6)
- [x] Step 1: Shell utilities (foundation) — 6 tests
- [x] Step 2: Extract `workspace/` — repo & project resolution — 5 tests
- [x] Step 3: Extract `session/` — clean Manager interface — 5 tests
- [x] Step 4: Extract `service/` — unified Service interface — 5 tests
- [x] Step 5: Clean `auth/` — OAuth + API keys only — 5 tests
- [x] Step 6: Create `provision/` — tools + user creation

### New Implementation (Steps 7-10)
- [x] Step 7: Rewrite `api/` — HTTP server with interfaces — 11 tests
- [x] Step 8: CLI uses new packages
- [x] Step 9: Wire everything in `cmd/cbx/main.go`
- [x] Step 10: Integration tests — 4 tests + E2E script

### Feature Requirements
- [x] `name` required in POST /sessions
- [x] Response includes `dir` and `status` (cloned/found/created/already running)
- [x] Cloudflare tunnel for cbx serve
- [x] Setup: one command, prints VNC URL + skill file
- [x] No master session — sessions created on-demand
- [x] Unified `cbx code <name> [--repo]` — smart resolution
- [x] Duplicate session check
- [x] gh auth via token file (cloud-init → setup)
- [x] am-server started with tunnel during setup
- [x] Skill-formatted output ready to save as SKILL.md

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
