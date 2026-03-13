#!/bin/bash
# Start VNC desktop sharing (Xvfb + x11vnc + noVNC + Fluxbox)
# Access via browser at http://<host>:6080/vnc.html
#
# Usage:
#   start-vnc               # start with defaults (1280x800)
#   start-vnc 1920x1080     # custom resolution
#   start-vnc --stop        # stop all VNC services

set -e

RESOLUTION="${1:-1280x800}"
DISPLAY_NUM=99
export DISPLAY=":${DISPLAY_NUM}"
VNC_PORT=5900
NOVNC_PORT=6080

if [ "$1" = "--stop" ]; then
    echo "Stopping VNC services..."
    pkill -f "Xvfb :${DISPLAY_NUM}" 2>/dev/null || true
    pkill -f "x11vnc.*:${DISPLAY_NUM}" 2>/dev/null || true
    pkill -f "fluxbox" 2>/dev/null || true
    pkill -f "websockify.*${NOVNC_PORT}" 2>/dev/null || true
    echo "VNC stopped."
    exit 0
fi

# Check if already running
if pgrep -f "Xvfb :${DISPLAY_NUM}" > /dev/null 2>&1; then
    echo "VNC is already running on display :${DISPLAY_NUM}"
    echo "  noVNC: http://localhost:${NOVNC_PORT}/vnc.html"
    echo "  VNC:   localhost:${VNC_PORT}"
    echo "Use 'start-vnc --stop' to stop, or tunnel with 'wormhole http ${NOVNC_PORT}'"
    exit 0
fi

echo "Starting VNC desktop (${RESOLUTION})..."

# Start virtual framebuffer
Xvfb ":${DISPLAY_NUM}" -screen 0 "${RESOLUTION}x24" -ac &
sleep 1

# Start window manager
fluxbox -display ":${DISPLAY_NUM}" &
sleep 1

# Start VNC server (no password — access is already gated by SSH/firewall)
x11vnc -display ":${DISPLAY_NUM}" -rfbport "${VNC_PORT}" -forever -shared -nopw &
sleep 1

# Start noVNC web proxy
websockify --web /usr/share/novnc "${NOVNC_PORT}" "localhost:${VNC_PORT}" &
sleep 1

echo ""
echo "================================================"
echo "  VNC Desktop Ready"
echo "================================================"
echo "  noVNC (browser): http://localhost:${NOVNC_PORT}/vnc.html"
echo "  VNC client:      localhost:${VNC_PORT}"
echo ""
echo "  To share over internet:"
echo "    wormhole http ${NOVNC_PORT}"
echo ""
echo "  To open a browser on the virtual desktop:"
echo "    DISPLAY=:${DISPLAY_NUM} chromium --no-sandbox &"
echo ""
echo "  To stop: start-vnc --stop"
echo "================================================"
