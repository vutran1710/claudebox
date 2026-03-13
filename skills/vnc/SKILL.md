---
name: vnc
description: Use start-vnc to run a virtual desktop with noVNC — share your screen over the internet so the user can watch browser activity in real time.
---

# VNC Screen Sharing

start-vnc runs a virtual desktop (Xvfb + Fluxbox + x11vnc + noVNC) that the user can view in their browser. Use it when the user wants to see what a browser or GUI app is doing in real time.

## Quick Reference

```bash
# Start the virtual desktop (default 1280x800)
start-vnc

# Start with custom resolution
start-vnc 1920x1080

# Check if running
start-vnc

# Stop everything
start-vnc --stop
```

## Sharing with the User

After starting VNC, expose it so the user can view from any device:

```bash
# Share over internet
wormhole http 6080

# Tell the user to open: <wormhole-url>/vnc.html
```

The user opens the URL in their browser and sees a live desktop — like screen sharing.

## Running a Browser on the Virtual Desktop

Apps must target display `:99` to appear on the virtual desktop:

```bash
# Launch Chromium
DISPLAY=:99 chromium --no-sandbox http://localhost:3000 &

# Launch Chromium with specific window size
DISPLAY=:99 chromium --no-sandbox --window-size=1280,800 http://localhost:3000 &

# Launch Firefox (if installed)
DISPLAY=:99 firefox http://localhost:3000 &

# Open xterm for debugging
DISPLAY=:99 xterm &
```

## Full Workflow: Let User Watch App Development

```bash
# 1. Start the dev server
cd /workspace/my-app
npm run dev &

# 2. Start the virtual desktop
start-vnc

# 3. Open the app in a browser on the virtual desktop
DISPLAY=:99 chromium --no-sandbox http://localhost:3000 &

# 4. Share with the user
wormhole http 6080
# → Give the user the URL + /vnc.html
```

Now the user sees the app live in their browser. Any changes (hot reload, navigation, interactions) are visible in real time.

## Full Workflow: Let User Watch Playwright Tests

```bash
# 1. Start VNC
start-vnc

# 2. Run Playwright tests with headed browser on the virtual display
DISPLAY=:99 npx playwright test --headed

# 3. User watches tests run via noVNC
```

## Ports

| Service | Port | Protocol |
|---------|------|----------|
| noVNC (web viewer) | 6080 | HTTP |
| VNC server | 5900 | VNC |

## Tips

- The virtual display is `:99` — always use `DISPLAY=:99` when launching GUI apps
- noVNC runs on port 6080 — tunnel with `wormhole http 6080` for internet access
- No password is set — access is gated by SSH/firewall, not VNC auth
- Fluxbox is the window manager — right-click the desktop for a menu
- Resolution can be changed by stopping and restarting: `start-vnc --stop && start-vnc 1920x1080`
- xterm is available for quick terminal access on the virtual desktop
- Multiple GUI apps can run simultaneously on the same virtual display
