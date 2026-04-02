# ClaudeBox

This is a dedicated remote development server. Supports deployment to Railway or DigitalOcean.

## Installed Tools

- **Claude Code CLI** — AI coding assistant (runs with `--dangerously-skip-permissions` for full autonomy)
- **Chrome Lite MCP** — browser automation via Chrome extension + MCP server (port 7331)
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
- **noVNC** (`start-vnc`) — browser-based remote desktop for viewing GUI apps

## Workspace

All projects should be cloned into `/workspace`.

## Web App Development Workflow

1. Clone a repo into `/workspace`
2. Install dependencies and start the dev server
3. Use `agent-browser` to navigate, interact, and screenshot the app
4. Use `wormhole` to share the dev server URL for external testing
5. Use `vercel` for deployments, `supabase` for backend, `gh` for GitHub operations

## Agent Browser

Headless browser automation CLI. Use it to navigate, interact with, and verify web apps.

```bash
agent-browser open "http://localhost:3000"   # Open URL
agent-browser snapshot                        # Page structure (accessibility tree with refs)
agent-browser screenshot /tmp/page.png        # Screenshot
agent-browser click @e5                       # Click element by ref
agent-browser fill @e3 "test@example.com"     # Fill form field
agent-browser type @e3 "hello"                # Type keystrokes
agent-browser select @e4 "option-value"       # Select dropdown
agent-browser press Enter                     # Press key
agent-browser hover @e2                       # Hover
agent-browser scroll down                     # Scroll
agent-browser get text @e1                    # Get element text
agent-browser get title                       # Get page title
agent-browser get url                         # Get page URL
agent-browser set viewport 1280 720           # Set viewport
agent-browser set device "iPhone 15"          # Mobile emulation
agent-browser network --filter "api/"         # Monitor network
agent-browser wait @e5                        # Wait for element
agent-browser wait --url "**/dashboard"       # Wait for navigation
agent-browser diff before.png after.png       # Visual regression
agent-browser close                           # Close browser
```

Tips:
- Always `snapshot` after navigation to get fresh element refs (`@e1`, `@e2`, etc.)
- Element refs change after navigation or major DOM updates — re-snapshot
- Use `screenshot` then read the image with the Read tool to visually verify
- `close` when done to free resources

## Wormhole

Tunneling CLI that exposes localhost to the internet with automatic HTTPS.

```bash
wormhole http 3000                            # Expose port 3000
wormhole http 3000 --subdomain my-preview     # Custom subdomain (needs login)
wormhole http 3000 --headless                 # No TUI, plain log output (background use)
wormhole http 3000 --no-inspect               # Disable traffic inspector
```

Background tunnel:
```bash
nohup wormhole http 3000 --headless > /tmp/wormhole.log 2>&1 &
cat /tmp/wormhole.log    # Get the public URL
```

Tips:
- URL changes each time unless you use `--subdomain` (requires `wormhole login`)
- Traffic inspector at `http://localhost:4040` — useful for debugging webhooks
- WebSocket connections are supported

## Screen Sharing (VNC)

Virtual desktop viewable in the user's browser — like desktop screen sharing.

```bash
start-vnc                    # Start virtual desktop (1280x800)
start-vnc 1920x1080          # Custom resolution
start-vnc --stop             # Stop all VNC services
```

Sharing with the user:
```bash
wormhole http 6080           # Share over internet
# Tell the user to open: <wormhole-url>/vnc.html
```

Running apps on the virtual desktop (must use `DISPLAY=:99`):
```bash
DISPLAY=:99 chromium --no-sandbox http://localhost:3000 &
DISPLAY=:99 chromium --no-sandbox --window-size=1280,800 http://localhost:3000 &
DISPLAY=:99 xterm &
```

Watching Playwright tests:
```bash
start-vnc
DISPLAY=:99 npx playwright test --headed
```

Ports: noVNC web viewer on `6080`, VNC server on `5900`.
Set `ENABLE_VNC=true` env var to auto-start on boot.

## Chrome Lite MCP (Message Polling)

Chrome Lite MCP provides browser automation via an MCP server connected to the Chrome extension.
Use it to read and interact with web apps: Gmail, Discord, Zalo, Messenger, Slack.

Skills reference: `/home/claude/.claude/chrome-lite-mcp-skills.md`

### Available tools

| Tool | Description |
|------|-------------|
| `tabs_list` | List all open Chrome tabs |
| `tab_create` | Open a new tab with URL |
| `tab_navigate` | Navigate a tab to a URL |
| `tab_close` | Close a tab |
| `tab_switch` | Focus/activate a tab |
| `page_read` | Read page content (modes: text, interactive, accessibility) |
| `page_click` | Click element by CSS selector or coordinates |
| `page_type` | Type text into an element |
| `page_screenshot` | Capture screenshot (base64 PNG) |
| `page_eval` | Execute JS via DevTools Protocol (bypasses CSP) |

### Message checking workflow

1. Navigate to each app (Gmail, Discord, Zalo, etc.)
2. Use `page_eval` with the JS snippets from the skills reference to extract messages
3. Push structured messages to am-server:

```bash
curl -X POST http://localhost:8090/ingest \
  -H "X-API-Key: $AM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '[{"source":"gmail","sender":"Name","subject":"Subject","preview":"Message..."}]'
```

### Key patterns

- Gmail toolbar buttons need `realClick()` (mousedown + mouseup + click)
- Discord hashes class names — use `[class*="message_"]` partial matches
- Zalo conversation items are `.conv-item` class
- Messenger E2EE chats cannot be read (content is encrypted in DOM)
- Use `page_read mode: "accessibility"` as fallback when selectors break

### Replying to messages

When asked to reply to a message:
1. Navigate to the correct app and conversation
2. Find and click the message input
3. Use `document.execCommand('insertText', false, 'message')` via `page_eval` for contenteditable editors
4. Click send or press Enter

## Conventions

- When developing a web app, proactively start VNC and share the noVNC URL so the user can watch in real time
- Take screenshots after UI changes and read them to verify visually
- Use `agent-browser snapshot` to inspect page structure before interacting
- Use `wormhole http <port> --headless` when running tunnels in background
- Always close agent-browser sessions when done (`agent-browser close`)
