# ClaudeBox TODO

## In Progress (feat/plugin-architecture branch)

### cbx serve improvements
- [ ] `cbx setup` starts serve daemon + Cloudflare tunnel at the end
- [ ] `cbx setup` prints API URL + key alongside VNC and Remote Control URLs
- [ ] `cbx serve` gets its own Cloudflare tunnel for remote API access
- [ ] Serve daemon auto-starts on boot (systemd service)

### Integration
- [ ] `cbx activate` calls serve API instead of managing sessions directly
- [ ] `cbx code` calls serve API instead of spawning tmux directly
- [ ] Master Claude session uses serve API via CLAUDE.md instructions

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
