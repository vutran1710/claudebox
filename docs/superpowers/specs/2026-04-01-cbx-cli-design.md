# cbx — ClaudeBox CLI

## Overview

`cbx` is a Go CLI tool that replaces all bash setup scripts in ClaudeBox with a single binary featuring a Bubble Tea TUI. It orchestrates three phases of deployment: tool installation, Claude authentication + VNC, and poller activation.

## User Flow

### Phase 0: Deploy (GitHub Actions)

User clicks "Run workflow" in GitHub Actions. The workflow:

1. Creates a DigitalOcean droplet with minimal cloud-init (SSH key + download `cbx` binary)
2. Prints the SSH command:

```
ssh root@<IP> "cbx setup"
```

### Phase 1: `cbx setup`

User pastes the command. The TUI shows a progress view:

1. **Install tools** — each tool shows a spinner while installing, checkmark when done. No verbose logs. Steps:
   - System dependencies (apt)
   - Rust
   - Python 3.13 + uv
   - Node.js 22
   - Go 1.24
   - Docker CLI
   - GitHub CLI
   - Claude Code CLI
   - Agent Browser + Chromium
   - Vercel CLI, Playwright, Supabase CLI
   - Cloudflare Tunnel
   - VNC stack (Xvfb, x11vnc, noVNC, Fluxbox)
   - Download am-server binary from GitHub release

2. **Create claude user** — non-root user with tool symlinks and autonomy-mode wrapper.

3. **Claude OAuth** — launches Claude Code in tmux, navigates onboarding prompts, displays the OAuth URL. Waits for user to paste auth code. Completes login.

4. **Start VNC + Chrome** — starts virtual desktop, launches Chromium, opens Cloudflare tunnel.

5. **Print summary** — VNC URL, password, and next steps (install extension, login to apps, run `cbx activate`).

### Phase 2: Manual Browser Setup

User opens VNC URL, installs Claude-in-Chrome extension, logs into Discord, Gmail (both profiles), and Zalo. No CLI involvement.

### Phase 3: `cbx activate`

User runs `cbx activate` after browser setup is complete:

1. **Start am-server** — creates systemd service if not exists, starts it.
2. **Start am-server tunnel** — creates cloudflared tunnel systemd service, starts it, captures URL.
3. **Activate pollers** — installs cron jobs (4 pollers, every 5 min, staggered by 1 min).
4. **Print summary** — am-server URL, API key, usage examples in markdown, poller schedule.

### Bonus: `cbx status`

Health check command showing the state of all services:

```
$ cbx status

  Claude Code      ✓ authenticated
  VNC              ✓ running
                     https://xyz.trycloudflare.com/vnc.html
                     password: tUDvpVIHtS5s
  Chrome           ✓ running (PID 1234)
  am-server        ✓ running (port 8090)
                     https://abc.trycloudflare.com
                     key: 005ca208ad3a411abb1b3ec04c0bc57f

  Pollers
    discord        ✓ every 5m (last: 2m ago, ok)
    gmail-personal ✓ every 5m (last: 3m ago, ok)
    gmail-work     ✓ every 5m (last: 4m ago, ok)
    zalo           ✓ every 5m (last: 1m ago, ok)
```

Note: `cbx` runs server-side. Access requires SSH (`ssh root@<IP> "cbx status"`). SSH key auth is the security boundary — no additional auth needed for cbx commands.

## Project Structure

```
claudebox/
├── cmd/cbx/
│   └── main.go               # Entry point, subcommand dispatch
├── internal/
│   ├── setup/
│   │   ├── setup.go          # setup command orchestration
│   │   └── tools.go          # tool installation definitions
│   ├── auth/
│   │   └── auth.go           # Claude OAuth via tmux
│   ├── vnc/
│   │   └── vnc.go            # VNC + Chrome + tunnel launch
│   ├── activate/
│   │   ├── activate.go       # activate command orchestration
│   │   ├── amserver.go       # am-server systemd setup
│   │   └── poller.go         # cron job installation
│   ├── status/
│   │   └── status.go         # health check command
│   └── ui/
│       ├── model.go          # Bubble Tea model
│       ├── styles.go         # lipgloss styles
│       └── components.go     # reusable UI components (spinner, step list, box)
├── polling/                   # Prompt .md files (unchanged)
│   ├── check-discord-messages.md
│   ├── check-gmail-personal.md
│   ├── check-gmail-work.md
│   ├── check-zalo-messages.md
│   └── poll-runner.sh         # Stays as bash (cron calls it)
├── cloud-providers/
│   └── digitalocean/
│       └── cloud-init.yml     # Minimal: SSH key + download cbx
├── .github/workflows/
│   ├── deploy-digitalocean.yml  # Updated: simplified cloud-init
│   └── release.yml              # Builds cbx binary for all platforms
├── Dockerfile                   # Updated: uses cbx instead of bash scripts
├── CLAUDE.md
└── go.mod
```

## Component Design

### cmd/cbx/main.go

Minimal entry point. Uses standard library `os.Args` or a lightweight CLI parser (no heavy framework — Bubble Tea handles the UI). Routes to:
- `cbx setup` → `setup.Run()`
- `cbx activate` → `activate.Run()`
- `cbx status` → `status.Run()`

No flags needed for v1. Each subcommand owns its own Bubble Tea program.

### internal/setup/tools.go

Defines each tool as a struct:

```go
type Tool struct {
    Name    string
    Check   func() bool          // already installed?
    Install func() error         // install command(s)
}
```

Each tool's `Install` runs shell commands via `os/exec`. The `Check` function tests if the tool is already present (idempotent). The setup orchestrator iterates tools in order, skipping already-installed ones.

Tool list (in order):
1. System deps (apt-get)
2. Rust (rustup)
3. Python + uv
4. Node.js 22
5. Go 1.24
6. Docker CLI
7. GitHub CLI
8. Claude Code CLI
9. Agent Browser + Chromium symlink
10. Vercel CLI
11. Playwright + Chromium
12. Supabase CLI
13. Cloudflare Tunnel
14. VNC stack (apt packages)
15. am-server (download from GitHub release)

### internal/auth/auth.go

Handles Claude Code OAuth. Logic ported from `login-claude.sh`:

1. Create `claude` user if not exists
2. Launch `claude` in tmux session
3. Poll tmux pane for prompts, send appropriate keys
4. Detect OAuth URL, return it to the TUI
5. Accept auth code from TUI input, send to tmux
6. Poll for login success
7. Start `/remote-control`, capture URL

The tmux interaction stays the same (pattern matching on pane content) — it's fragile but there's no better API. The Go wrapper adds proper timeouts and error reporting.

### internal/vnc/vnc.go

Starts the VNC desktop. Logic ported from `start-vnc.sh`:

1. Start Xvfb on :99
2. Start Fluxbox window manager
3. Start x11vnc with auto-generated password
4. Start noVNC websockify on :6080
5. Launch Chromium on DISPLAY=:99
6. Start cloudflared tunnel to :6080
7. Return tunnel URL and password

### internal/activate/amserver.go

Sets up am-server as a systemd service:

1. Check if am-server binary exists at `/usr/local/bin/am-server`
2. Write systemd unit file
3. Start service, wait for healthy (`/healthz`)
4. Start cloudflared tunnel to :8090
5. Capture tunnel URL
6. Read API key from `~/.agent-mesh/config.toml`

### internal/activate/poller.go

Installs cron jobs. Logic ported from `setup-cron.sh`:

1. Read AM_API_KEY from config
2. Verify VNC is running (DISPLAY=:99)
3. Write crontab entries (4 pollers, staggered)
4. Return schedule summary

### internal/ui/

Bubble Tea TUI components:

- **StepList** — ordered list of steps with spinner/checkmark/error states
- **InputPrompt** — text input for auth code
- **SummaryBox** — bordered box with key-value pairs (URLs, passwords)
- **StatusTable** — service name + status pairs for `cbx status`

Styles via lipgloss. Minimal color palette: green checkmarks, yellow spinners, red errors, cyan URLs.

## Cloud-Init (Simplified)

The cloud-init becomes trivial:

```yaml
#cloud-config
runcmd:
  - |
    # SSH setup
    mkdir -p /root/.ssh
    echo '<SSH_PUBLIC_KEY>' >> /root/.ssh/authorized_keys
    chmod 700 /root/.ssh && chmod 600 /root/.ssh/authorized_keys

    # Download cbx
    curl -sfL https://api.github.com/repos/vutran1710/claude-devbox/releases/latest \
      | grep -o 'https://[^"]*cbx-linux-amd64[^"]*' \
      | head -1 \
      | xargs curl -sfL -o /usr/local/bin/cbx
    chmod +x /usr/local/bin/cbx

    # Signal ready
    touch /root/.cbx-ready
```

GitHub Actions workflow prints:

```
ssh root@<IP> "cbx setup"
```

## Release Workflow

New `release.yml` in claudebox repo. Triggers on tag push (`v*`) or manual dispatch. Builds:

- `cbx-linux-amd64`
- `cbx-linux-arm64`
- `cbx-darwin-amd64`
- `cbx-darwin-arm64`

Same pattern as the am-server release workflow.

## What Gets Removed

After `cbx` is complete, these bash scripts are deleted:

| Script | Replaced By |
|--------|-------------|
| `cloud-providers/digitalocean/setup.sh` | `cbx setup` (internal/setup/) |
| `login-claude.sh` | `cbx setup` (internal/auth/) |
| `start-vnc.sh` | `cbx setup` (internal/vnc/) |
| `entrypoint.sh` | `cbx setup` (Docker mode) |
| `polling/setup-cron.sh` | `cbx activate` (internal/activate/) |

**Kept as-is:**
- `polling/poll-runner.sh` — thin cron wrapper, not worth porting
- `polling/*.md` — prompt files, used by poll-runner.sh

## Dependencies

```
github.com/charmbracelet/bubbletea    # TUI framework
github.com/charmbracelet/lipgloss     # Styling
github.com/charmbracelet/bubbles      # Spinner, text input components
github.com/BurntSushi/toml            # Read am-server config
```

## Error Handling

- Each tool installation step has a timeout (configurable, default 5 min per tool)
- If a step fails, the TUI shows the error and offers to retry or skip
- OAuth flow has a 10-minute timeout for user interaction
- All errors are logged to `/var/log/cbx.log` for debugging
- `cbx setup` is idempotent — re-running skips already-installed tools and picks up where it left off

## Future Considerations (Not in v1)

- `cbx teardown` — clean shutdown of all services
- `cbx logs` — tail polling logs
- `cbx poll <service>` — manual trigger of a single poller
- Configuration file for custom polling intervals
- Named Cloudflare tunnels for stable URLs
