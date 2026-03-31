#!/bin/bash
# Start password-protected VNC desktop. Returns noVNC URL + password.
#
# Usage:
#   start-vnc               # 1280x800, auto-generated password
#   start-vnc 1920x1080     # custom resolution
#   start-vnc --stop        # stop all VNC services
set -e

DISPLAY_NUM=99
export DISPLAY=":${DISPLAY_NUM}"
VNC_PORT=5900
NOVNC_PORT=6080
VNC_PASSWD_FILE="/root/.vnc_passwd"

# ── Stop ──
if [ "$1" = "--stop" ]; then
    pkill -f "Xvfb :${DISPLAY_NUM}" 2>/dev/null || true
    pkill -f "x11vnc" 2>/dev/null || true
    pkill -f "fluxbox" 2>/dev/null || true
    pkill -f "websockify.*${NOVNC_PORT}" 2>/dev/null || true
    echo "VNC stopped."
    exit 0
fi

RESOLUTION="${1:-1280x800}"

# ── Already running? ──
if pgrep -f "Xvfb :${DISPLAY_NUM}" > /dev/null 2>&1; then
    echo "VNC already running — http://localhost:${NOVNC_PORT}/vnc.html"
    exit 0
fi

# ── Password (always required) ──
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

# ── Start services ──
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

# ── Output ──
echo ""
echo "================================================"
echo "  VNC Desktop Ready"
echo "================================================"
echo "  URL:      http://localhost:${NOVNC_PORT}/vnc.html"
if [ -n "${VNC_PASSWORD:-}" ]; then
    echo "  Password: ${VNC_PASSWORD}"
fi
echo ""
echo "  Share:    wormhole http ${NOVNC_PORT}"
echo "  Browser:  DISPLAY=:${DISPLAY_NUM} chromium --no-sandbox <url>"
echo "  Stop:     start-vnc --stop"
echo "================================================"
