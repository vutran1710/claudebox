#!/bin/bash
# DigitalOcean droplet setup script — runs via cloud-init
# Installs all devbox tools natively (no Docker)
set -e

export DEBIAN_FRONTEND=noninteractive
export HOME=/root
export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.cargo/bin:/usr/local/go/bin:$PATH"

REPO_DIR="/opt/claudebox"

echo "================================================"
echo "  ClaudeBox — DigitalOcean Setup"
echo "================================================"

# ── System dependencies + Chromium deps ──
apt-get update && apt-get install -y \
    curl wget git unzip jq build-essential \
    ca-certificates gnupg lsb-release sudo \
    tmux \
    libnss3 libatk1.0-0t64 libatk-bridge2.0-0t64 libcups2t64 \
    libdrm2 libxkbcommon0 libxcomposite1 libxdamage1 libxrandr2 \
    libgbm1 libpango-1.0-0 libcairo2 libasound2t64 libxshmfence1 \
    libgtk-3-0t64 libx11-xcb1 fonts-liberation xdg-utils \
    pkg-config libssl-dev \
    xvfb x11vnc novnc websockify fluxbox xterm \
    && rm -rf /var/lib/apt/lists/*

# ── Rust + Cargo ──
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
source "$HOME/.cargo/env"

# ── Python (via uv) ──
curl -LsSf https://astral.sh/uv/install.sh | sh
$HOME/.local/bin/uv python install 3.13

# ── Node.js 22 ──
curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs
npm config set prefix "$HOME/.npm-global"
rm -rf /var/lib/apt/lists/*

# ── Go ──
curl -fsSL https://go.dev/dl/go1.24.1.linux-amd64.tar.gz | tar -C /usr/local -xzf -

# ── Docker ──
curl -fsSL https://get.docker.com | sh

# ── GitHub CLI ──
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null
apt-get update && apt-get install -y gh
rm -rf /var/lib/apt/lists/*

# ── Claude Code CLI ──
curl -fsSL https://claude.ai/install.sh | bash

# ── Agent Browser ──
npm install -g agent-browser
agent-browser install

# ── Vercel CLI ──
npm install -g vercel

# ── Playwright ──
npm install -g playwright
playwright install --with-deps chromium

# ── Supabase CLI ──
curl -fsSL https://raw.githubusercontent.com/supabase/cli/main/install.sh | bash

# ── Wormhole CLI ──
curl -fsSL https://wormhole.bar/install.sh | sh

# ── Copy project files ──
mkdir -p /root/.claude/skills/agent-browser
mkdir -p /root/.claude/skills/wormhole
mkdir -p /root/.claude/skills/vnc
cp "$REPO_DIR/skills/agent-browser/SKILL.md" /root/.claude/skills/agent-browser/SKILL.md
cp "$REPO_DIR/skills/wormhole/SKILL.md" /root/.claude/skills/wormhole/SKILL.md
cp "$REPO_DIR/skills/vnc/SKILL.md" /root/.claude/skills/vnc/SKILL.md
cp "$REPO_DIR/CLAUDE.md" /root/CLAUDE.md
cp "$REPO_DIR/start-vnc.sh" /start-vnc.sh
chmod +x /start-vnc.sh
cp "$REPO_DIR/login-claude.sh" /root/login-claude.sh
chmod +x /root/login-claude.sh

# ── Workspace ──
mkdir -p /workspace

# ── Create non-root 'claude' user (--dangerously-skip-permissions requires non-root) ──
useradd -m -s /bin/bash claude 2>/dev/null || true
# Allow traversal (o+x) on tool directories so symlinks resolve, but not listing (no o+r on /root)
chmod o+x /root
chmod o+x /root/.local /root/.local/bin 2>/dev/null || true
chmod o+x /root/.npm-global /root/.npm-global/bin 2>/dev/null || true
chmod o+x /root/.cargo /root/.cargo/bin 2>/dev/null || true
# Share tools via symlinks in a neutral path
mkdir -p /usr/local/share/devbox-tools/bin
for dir in /root/.local/bin /root/.npm-global/bin /root/.cargo/bin; do
    if [ -d "$dir" ]; then
        for f in "$dir"/*; do
            [ -x "$f" ] && ln -sf "$f" /usr/local/share/devbox-tools/bin/ 2>/dev/null || true
        done
    fi
done

# SSH access for claude user
mkdir -p /home/claude/.ssh
cp /root/.ssh/authorized_keys /home/claude/.ssh/
chmod 700 /home/claude/.ssh
chmod 600 /home/claude/.ssh/authorized_keys

# Copy Claude skills and config
cp -r /root/.claude /home/claude/.claude 2>/dev/null || true
cp "$REPO_DIR/CLAUDE.md" /home/claude/CLAUDE.md

# Bashrc for claude user
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

# ── Claude config ──
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

# ── Signal setup complete ──
touch /root/.devbox-ready
echo ""
echo "================================================"
echo "  ClaudeBox setup complete!"
echo "================================================"
