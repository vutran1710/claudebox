#!/bin/bash
set -e

echo "================================================"
echo "  ClaudeBox"
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

# ── Create non-root 'claude' user (--dangerously-skip-permissions requires non-root) ──
useradd -m -s /bin/bash claude 2>/dev/null || true
# Share tools via symlinks in /usr/local/share instead of opening /root to world
mkdir -p /usr/local/share/devbox-tools/bin
for dir in /root/.local/bin /root/.npm-global/bin /root/.cargo/bin; do
    if [ -d "$dir" ]; then
        for f in "$dir"/*; do
            [ -x "$f" ] && ln -sf "$f" /usr/local/share/devbox-tools/bin/ 2>/dev/null || true
        done
    fi
done

mkdir -p /home/claude/.ssh
cp /root/.ssh/authorized_keys /home/claude/.ssh/
chmod 700 /home/claude/.ssh
chmod 600 /home/claude/.ssh/authorized_keys

cp -r /root/.claude /home/claude/.claude 2>/dev/null || true
cp /root/CLAUDE.md /home/claude/CLAUDE.md 2>/dev/null || true

cat > /home/claude/.bashrc <<'BASHRC'
export PATH="/usr/local/share/devbox-tools/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
export HOME=/home/claude
claude() {
  if [ "$1" = "auth" ] || [ "$1" = "config" ]; then
    /usr/local/share/devbox-tools/bin/claude "$@"
  else
    /usr/local/share/devbox-tools/bin/claude --dangerously-skip-permissions "$@"
  fi
}
BASHRC

chown -R claude:claude /home/claude /workspace

claude config set --global autoUpdaterStatus disabled 2>/dev/null || true

# ── GitHub CLI auth ──
if [ -n "$GITHUB_TOKEN" ]; then
    GH_TOKEN_VAL="$GITHUB_TOKEN"
    unset GITHUB_TOKEN
    echo "$GH_TOKEN_VAL" | gh auth login --with-token || true
    # Use a temp file instead of shell quoting to avoid injection if token contains special chars
    TOKEN_FILE=$(mktemp)
    printf '%s' "$GH_TOKEN_VAL" > "$TOKEN_FILE"
    chmod 600 "$TOKEN_FILE"
    su - claude -c "gh auth login --with-token < '$TOKEN_FILE'" 2>/dev/null || true
    rm -f "$TOKEN_FILE"
    echo "GitHub CLI authenticated."
fi

echo "Non-root 'claude' user created. SSH as 'claude' for full autonomy mode."
echo ""

# Keep container alive
exec tail -f /dev/null
