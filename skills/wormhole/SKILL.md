---
name: wormhole
description: Use wormhole CLI to expose local dev servers to the internet via HTTPS tunnel — for sharing previews, testing webhooks, and mobile testing.
---

# Wormhole

Wormhole is a tunneling CLI that exposes localhost to the internet with automatic HTTPS. Use it to share dev server previews, test webhooks, or access the app from a phone.

## Quick Reference

```bash
# Expose a local port (e.g., Next.js dev server on 3000)
wormhole http 3000

# Use a custom subdomain (requires `wormhole login` first)
wormhole http 3000 --subdomain my-preview

# Headless mode (no TUI, plain log output — best for background use)
wormhole http 3000 --headless

# Disable traffic inspector
wormhole http 3000 --no-inspect

# Custom inspector address
wormhole http 3000 --inspect 0.0.0.0:4040
```

## Auth (for custom subdomains)

```bash
wormhole login     # GitHub OAuth — gives 3 custom subdomains
wormhole status    # Check auth status
wormhole logout    # Clear credentials
```

## How It Works

1. Wormhole opens a WebSocket to the nearest Cloudflare edge
2. A unique `*.wormhole.bar` HTTPS URL is assigned
3. Incoming requests route through Cloudflare → WebSocket → your local port
4. Responses reverse the path

Latency: ~5-20ms to nearest Cloudflare edge + local processing.

## Traffic Inspector

By default, wormhole runs a traffic inspector at `http://localhost:4040`:
- View all request/response pairs
- Replay requests
- Export HAR files

## Common Use Cases

### Share a dev preview
```bash
# Start your dev server
npm run dev &

# Expose it
wormhole http 3000
# → https://abc123.wormhole.bar
```

### Test webhooks (e.g., SePay, Stripe)
```bash
wormhole http 3000 --subdomain claw-webhook
# Configure webhook URL: https://claw-webhook.wormhole.bar/api/webhook
```

### Mobile testing
```bash
wormhole http 3000
# Open the wormhole URL on your phone
```

### Background tunnel
```bash
# Run in background with headless mode
nohup wormhole http 3000 --headless > /tmp/wormhole.log 2>&1 &

# Check the log for the public URL
cat /tmp/wormhole.log
```

## Security

Wormhole tunnels are **publicly accessible** — anyone with the URL can reach the exposed service. Before tunneling:

- **Never tunnel VNC (port 6080) without a VNC password** — set `VNC_PASSWORD` env var first
- **Never tunnel services with sensitive data** unless the service has its own authentication
- **Prefer short-lived tunnels** — stop the tunnel when done testing
- **Use `--subdomain`** for predictable URLs that are easier to audit and revoke

## Tips

- The public URL changes each time unless you use `--subdomain` (requires login)
- Free plan: random subdomains unlimited, custom subdomains need GitHub auth (3 max)
- WebSocket connections are supported
- Auto-reconnects with exponential backoff if the connection drops
- Inspector at localhost:4040 is useful for debugging webhook payloads
