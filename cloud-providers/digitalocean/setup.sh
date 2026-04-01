#!/bin/bash
# DigitalOcean droplet setup — runs via cloud-init
# Mirrors the Dockerfile but installs natively (no Docker)
set -e

export DEBIAN_FRONTEND=noninteractive
export HOME=/root
export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.cargo/bin:/usr/local/go/bin:$PATH"

REPO_DIR="/opt/claudebox"

echo "================================================"
echo "  ClaudeBox — DigitalOcean Setup"
echo "================================================"

# ── System deps + Chromium shared libs + VNC ──
apt-get update && apt-get install -y \
    curl wget git unzip jq build-essential \
    ca-certificates gnupg lsb-release sudo \
    tmux locales \
    libnss3 libatk1.0-0t64 libatk-bridge2.0-0t64 libcups2t64 \
    libdrm2 libxkbcommon0 libxcomposite1 libxdamage1 libxrandr2 \
    libgbm1 libpango-1.0-0 libcairo2 libasound2t64 libxshmfence1 \
    libgtk-3-0t64 libx11-xcb1 fonts-liberation xdg-utils \
    pkg-config libssl-dev \
    xvfb x11vnc novnc websockify fluxbox xterm \
    && locale-gen en_US.UTF-8 \
    && rm -rf /var/lib/apt/lists/*

export LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

# ── Rust ──
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain 1.84.0
source "$HOME/.cargo/env"

# ── Python (uv) ──
curl -LsSf "https://astral.sh/uv/0.6.6/install.sh" | sh
$HOME/.local/bin/uv python install 3.13

# ── Node.js 22 ──
curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs
npm config set prefix "$HOME/.npm-global"
rm -rf /var/lib/apt/lists/*

# ── Go ──
GO_VERSION=1.24.1
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
echo "cb2396bae64183cdccf81a9a6df0aea3bce9511fc21469fb89a0c00470088073  /tmp/go.tar.gz" | sha256sum -c -
tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz

# ── Docker ──
curl -fsSL https://get.docker.com | sh

# ── GitHub CLI ──
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null
apt-get update && apt-get install -y gh && rm -rf /var/lib/apt/lists/*

# ── Claude Code CLI ──
curl -fsSL https://claude.ai/install.sh | bash

# ── Agent Browser + system chromium ──
npm install -g agent-browser
agent-browser install
ln -sf /root/.agent-browser/browsers/chrome-*/chrome /usr/local/bin/chromium

# ── Vercel CLI ──
npm install -g vercel

# ── Playwright ──
npm install -g playwright
playwright install --with-deps chromium

# ── Supabase CLI ──
curl -fsSL https://raw.githubusercontent.com/supabase/cli/main/install.sh | bash

# ── Wormhole CLI ──
curl -fsSL https://wormhole.bar/install.sh | sh

# ── Cloudflared ──
curl -fsSL https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb -o /tmp/cloudflared.deb
dpkg -i /tmp/cloudflared.deb && rm /tmp/cloudflared.deb

# ── Agent Mesh server (build from source + systemd services) ──
if [ -d /opt/claudebox ] && command -v go > /dev/null 2>&1; then
    if [ -n "${GH_TOKEN_VAL:-}" ]; then
        git clone "https://${GH_TOKEN_VAL}@github.com/vutran1710/amesh.git" /opt/amesh 2>/dev/null || true
    elif [ -n "${GITHUB_TOKEN:-}" ]; then
        git clone "https://${GITHUB_TOKEN}@github.com/vutran1710/amesh.git" /opt/amesh 2>/dev/null || true
    else
        git clone https://github.com/vutran1710/amesh.git /opt/amesh 2>/dev/null || true
    fi
    if [ -d /opt/amesh/cmd/am-server ]; then
        cd /opt/amesh && go build -o /usr/local/bin/am-server ./cmd/am-server/
        cat > /etc/systemd/system/am-server.service <<'SVCEOF'
[Unit]
Description=Agent Mesh Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/am-server
Restart=always
RestartSec=5
Environment=DATA_DIR=/root/.agent-mesh

[Install]
WantedBy=multi-user.target
SVCEOF

        cat > /etc/systemd/system/am-tunnel.service <<'SVCEOF'
[Unit]
Description=Cloudflare tunnel for am-server
After=am-server.service

[Service]
Type=simple
ExecStart=/usr/local/bin/cloudflared tunnel --url http://localhost:8090 --no-tls-verify
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SVCEOF

        systemctl daemon-reload
        systemctl enable am-server am-tunnel
        systemctl start am-server
        sleep 2
        systemctl start am-tunnel
        sleep 5
        AM_URL=$(journalctl -u am-tunnel --no-pager -n 30 | grep -o 'https://[^ ]*\.trycloudflare\.com' | tail -1)
        AM_KEY=$(grep api_key /root/.agent-mesh/config.toml 2>/dev/null | awk -F'"' '{print $2}')
        echo "Agent Mesh server running."
        echo "  URL: ${AM_URL:-pending}"
        echo "  Key: ${AM_KEY:-check ~/.agent-mesh/config.toml}"
        cd /workspace
    fi
fi

# ── Copy project files ──
mkdir -p /root/.claude/skills/{agent-browser,wormhole,vnc}
cp "$REPO_DIR/skills/agent-browser/SKILL.md" /root/.claude/skills/agent-browser/SKILL.md
cp "$REPO_DIR/skills/wormhole/SKILL.md" /root/.claude/skills/wormhole/SKILL.md
cp "$REPO_DIR/skills/vnc/SKILL.md" /root/.claude/skills/vnc/SKILL.md
cp "$REPO_DIR/CLAUDE.md" /root/CLAUDE.md
cp "$REPO_DIR/start-vnc.sh" /start-vnc.sh && chmod +x /start-vnc.sh
ln -sf /start-vnc.sh /usr/local/bin/start-vnc
cp "$REPO_DIR/login-claude.sh" /root/login-claude.sh && chmod +x /root/login-claude.sh

# ── Polling prompts ──
cp -r "$REPO_DIR/polling" /opt/claudebox/polling
chmod +x /opt/claudebox/polling/poll-runner.sh /opt/claudebox/polling/setup-cron.sh

# ── Workspace ──
mkdir -p /workspace

# ── Create non-root 'claude' user ──
useradd -m -s /bin/bash claude 2>/dev/null || true

chmod o+x /root
chmod o+x /root/.local /root/.local/bin 2>/dev/null || true
chmod o+x /root/.npm-global /root/.npm-global/bin 2>/dev/null || true
chmod o+x /root/.cargo /root/.cargo/bin 2>/dev/null || true

mkdir -p /usr/local/share/devbox-tools/bin
for dir in /root/.local/bin /root/.npm-global/bin /root/.cargo/bin; do
    [ -d "$dir" ] && for f in "$dir"/*; do
        [ -x "$f" ] && ln -sf "$f" /usr/local/share/devbox-tools/bin/ 2>/dev/null || true
    done
done

mkdir -p /home/claude/.ssh
cp /root/.ssh/authorized_keys /home/claude/.ssh/
chmod 700 /home/claude/.ssh && chmod 600 /home/claude/.ssh/authorized_keys
cp -r /root/.claude /home/claude/.claude 2>/dev/null || true
cp "$REPO_DIR/CLAUDE.md" /home/claude/CLAUDE.md

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

# ── Done ──
touch /root/.devbox-ready
echo ""
echo "================================================"
echo "  ClaudeBox setup complete!"
echo "================================================"
