#!/bin/bash
# Start password-protected VNC desktop with Cloudflare tunnel.
# Returns public URL + password.
#
# Usage:
#   start-vnc               # 1280x800, auto password, cloudflare tunnel
#   start-vnc 1920x1080     # custom resolution
#   start-vnc --stop        # stop all VNC + tunnel services
set -e

DISPLAY_NUM=99
export DISPLAY=":${DISPLAY_NUM}"
VNC_PORT=5900
NOVNC_PORT=6080
VNC_PASSWD_FILE="/root/.vnc_passwd"
TUNNEL_LOG="/tmp/cloudflared-vnc.log"

# ── Stop ──
if [ "$1" = "--stop" ]; then
    pkill -f "Xvfb :${DISPLAY_NUM}" 2>/dev/null || true
    pkill -f "x11vnc" 2>/dev/null || true
    pkill -f "fluxbox" 2>/dev/null || true
    pkill -f "websockify.*${NOVNC_PORT}" 2>/dev/null || true
    pkill -f "cloudflared.*${NOVNC_PORT}" 2>/dev/null || true
    echo "VNC stopped."
    exit 0
fi

RESOLUTION="${1:-1280x800}"

# ── Already running? ──
if pgrep -f "Xvfb :${DISPLAY_NUM}" > /dev/null 2>&1; then
    TUNNEL_URL=$(grep -o 'https://[^ ]*\.trycloudflare\.com' "$TUNNEL_LOG" 2>/dev/null | tail -1)
    echo "VNC already running"
    echo "  Local:  http://localhost:${NOVNC_PORT}/vnc.html"
    [ -n "$TUNNEL_URL" ] && echo "  Public: ${TUNNEL_URL}/vnc.html"
    exit 0
fi

# ── Password (always required, auto-generate if needed) ──
if [ -z "${VNC_PASSWORD:-}" ]; then
    if [ -f "$VNC_PASSWD_FILE" ]; then
        echo "(Using saved VNC password)"
    else
        VNC_PASSWORD=$(head -c 16 /dev/urandom | base64 | tr -d '/+=\n' | head -c 12)
    fi
fi

if [ -n "${VNC_PASSWORD:-}" ]; then
    x11vnc -storepasswd "$VNC_PASSWORD" "$VNC_PASSWD_FILE" 2>/dev/null
fi

if [ ! -f "$VNC_PASSWD_FILE" ]; then
    echo "ERROR: No VNC password. Set VNC_PASSWORD env var."
    exit 1
fi

# ── Start VNC services ──
Xvfb ":${DISPLAY_NUM}" -screen 0 "${RESOLUTION}x24" -ac > /dev/null 2>&1 &
sleep 1
fluxbox -display ":${DISPLAY_NUM}" > /dev/null 2>&1 &
sleep 1
x11vnc -display ":${DISPLAY_NUM}" -rfbport "${VNC_PORT}" -forever -shared -rfbauth "$VNC_PASSWD_FILE" > /dev/null 2>&1 &
sleep 1
websockify --web /usr/share/novnc "${NOVNC_PORT}" "localhost:${VNC_PORT}" > /dev/null 2>&1 &
sleep 1

# ── Launch Chromium on the desktop ──
DISPLAY=":${DISPLAY_NUM}" chromium --no-sandbox --disable-gpu --no-first-run \
    --disable-dev-shm-usage --window-size=1280,800 > /dev/null 2>&1 &

# ── Start Cloudflare tunnel ──
TUNNEL_URL=""
if command -v cloudflared > /dev/null 2>&1; then
    cloudflared tunnel --url "http://localhost:${NOVNC_PORT}" > "$TUNNEL_LOG" 2>&1 &
    # Wait for tunnel URL (up to 15s)
    for i in $(seq 1 15); do
        TUNNEL_URL=$(grep -o 'https://[^ ]*\.trycloudflare\.com' "$TUNNEL_LOG" 2>/dev/null | tail -1)
        [ -n "$TUNNEL_URL" ] && break
        sleep 1
    done
fi

# ── Output ──
echo ""
echo "================================================"
echo "  VNC Desktop Ready"
echo "================================================"
if [ -n "$TUNNEL_URL" ]; then
    echo "  URL:      ${TUNNEL_URL}/vnc.html"
else
    echo "  URL:      http://localhost:${NOVNC_PORT}/vnc.html"
    echo "  (cloudflared not available — use SSH tunnel)"
fi
if [ -n "${VNC_PASSWORD:-}" ]; then
    echo "  Password: ${VNC_PASSWORD}"
fi
echo ""
echo "  Stop:     start-vnc --stop"
echo "================================================"
