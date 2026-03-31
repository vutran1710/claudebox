#!/bin/bash
set -e

echo "================================================"
echo "  ClaudeBox"
echo "================================================"
echo ""

# ── SSH setup ──
if [ -z "$SSH_PUBLIC_KEY" ]; then
    echo "ERROR: SSH_PUBLIC_KEY env var is required."
    exit 1
fi

mkdir -p /root/.ssh
echo "$SSH_PUBLIC_KEY" > /root/.ssh/authorized_keys
chmod 700 /root/.ssh && chmod 600 /root/.ssh/authorized_keys

/usr/sbin/sshd
echo "SSH server started on port 22."

# ── Tool versions ──
echo ""
echo "Installed tools:"
echo "  Node.js    : $(node --version 2>/dev/null)"
echo "  Go         : $(go version 2>/dev/null | awk '{print $3}')"
echo "  Claude     : $(claude --version 2>/dev/null || echo 'installed')"
echo "  Chromium   : $(chromium --version 2>/dev/null || echo 'installed')"
echo "  Wormhole   : $(wormhole version 2>/dev/null || echo 'installed')"
echo ""

# ── Create non-root 'claude' user ──
useradd -m -s /bin/bash claude 2>/dev/null || true

# Let claude user traverse to tool binaries (execute only, not list)
chmod o+x /root
chmod o+x /root/.local /root/.local/bin 2>/dev/null || true
chmod o+x /root/.npm-global /root/.npm-global/bin 2>/dev/null || true
chmod o+x /root/.cargo /root/.cargo/bin 2>/dev/null || true

# Symlink all tools into a shared bin
mkdir -p /usr/local/share/devbox-tools/bin
for dir in /root/.local/bin /root/.npm-global/bin /root/.cargo/bin; do
    [ -d "$dir" ] && for f in "$dir"/*; do
        [ -x "$f" ] && ln -sf "$f" /usr/local/share/devbox-tools/bin/ 2>/dev/null || true
    done
done

# SSH + claude config for claude user
mkdir -p /home/claude/.ssh
cp /root/.ssh/authorized_keys /home/claude/.ssh/
chmod 700 /home/claude/.ssh && chmod 600 /home/claude/.ssh/authorized_keys
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
    TOKEN_FILE=$(mktemp) && printf '%s' "$GH_TOKEN_VAL" > "$TOKEN_FILE" && chmod 600 "$TOKEN_FILE"
    su - claude -c "gh auth login --with-token < '$TOKEN_FILE'" 2>/dev/null || true
    rm -f "$TOKEN_FILE"
    echo "GitHub CLI authenticated."
fi

# ── Auto-start VNC if requested ──
if [ "${ENABLE_VNC:-false}" = "true" ]; then
    /start-vnc.sh "${VNC_RESOLUTION:-1280x800}"
fi

echo ""
echo "Ready. SSH as 'claude' for autonomy mode, 'root' for admin."
echo "  start-vnc           — launch VNC desktop"
echo "  /login-claude.sh    — authenticate Claude CLI"
echo ""

exec tail -f /dev/null
