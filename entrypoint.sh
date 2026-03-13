#!/bin/bash
set -e

echo "================================================"
echo "  Claude DevBox"
echo "================================================"
echo ""

# ── SSH setup (required) ──
if [ -z "$SSH_PUBLIC_KEY" ]; then
    echo "ERROR: SSH_PUBLIC_KEY env var is required but not set."
    echo "Set SSH_PUBLIC_KEY as an environment variable (e.g. ssh-ed25519 AAAA... you@machine)"
    exit 1
fi

mkdir -p /root/.ssh
echo "$SSH_PUBLIC_KEY" > /root/.ssh/authorized_keys
chmod 700 /root/.ssh
chmod 600 /root/.ssh/authorized_keys
echo "SSH public key installed."

# Start SSH daemon
/usr/sbin/sshd
echo "SSH server started on port 22."
echo ""

# Print installed tool versions
echo "Installed tools:"
echo "  Node.js    : $(node --version)"
echo "  npm        : $(npm --version)"
echo "  Go         : $(go version | awk '{print $3}')"
echo "  Docker     : $(docker --version 2>/dev/null | awk '{print $3}' || echo 'CLI only')"
echo "  GitHub CLI : $(gh --version | head -1 | awk '{print $3}')"
echo "  Claude     : $(claude --version 2>/dev/null || echo 'installed')"
echo "  Vercel     : $(vercel --version 2>/dev/null | tail -1)"
echo "  Supabase   : $(supabase --version 2>/dev/null | awk '{print $2}')"
echo "  Rust       : $(rustc --version | awk '{print $2}')"
echo "  Cargo      : $(cargo --version | awk '{print $2}')"
echo "  Python     : $(uv python list --installed 2>/dev/null | head -1 | awk '{print $1}' || echo 'installed')"
echo "  uv         : $(uv --version 2>/dev/null)"
echo "  Wormhole   : $(wormhole version 2>/dev/null || echo 'installed')"
echo "  AgentBrowser: $(agent-browser --version 2>/dev/null || echo 'installed')"
echo "  tmux       : $(tmux -V)"
echo "  noVNC      : $(dpkg -s novnc 2>/dev/null | grep Version | awk '{print $2}' || echo 'installed')"
echo ""

# ── Start VNC if ENABLE_VNC is set ──
if [ "${ENABLE_VNC:-false}" = "true" ]; then
    /start-vnc.sh "${VNC_RESOLUTION:-1280x800}"
fi

# ── Start Claude auth in a tmux session ──
echo "Starting tmux session 'claude' with auth login..."
echo ""
echo "After SSH-ing in, run:"
echo "  tmux attach -t claude"
echo ""

# Enable fully autonomous mode (skip all permission prompts)
claude config set --global autoUpdaterStatus disabled 2>/dev/null || true
echo 'alias claude="claude --dangerously-skip-permissions"' >> /root/.bashrc

tmux new-session -d -s claude "claude auth login && echo 'Auth complete. Claude is ready with --dangerously-skip-permissions.'; exec bash"

# Keep container alive
exec tail -f /dev/null
