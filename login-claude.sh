#!/bin/bash
# Login to Claude Code on the devbox and start remote-control session
# Run this on the remote server as root: ./login-claude.sh
set -e

export PATH="/root/.local/bin:/root/.npm-global/bin:/root/.cargo/bin:/usr/local/go/bin:$PATH"

# Wait for setup to finish if cloud-init is still running (DigitalOcean)
if command -v cloud-init &>/dev/null && [ ! -f /root/.devbox-ready ]; then
    echo "Waiting for devbox setup to complete..."
    while [ ! -f /root/.devbox-ready ]; do
        sleep 5
    done
    echo "Setup complete."
fi

# Ensure claude user exists
if ! id claude &>/dev/null; then
    useradd -m -s /bin/bash claude
    # Allow traversal (o+x) on tool directories so symlinks resolve
    chmod o+x /root
    chmod o+x /root/.local /root/.local/bin 2>/dev/null || true
    chmod o+x /root/.npm-global /root/.npm-global/bin 2>/dev/null || true
    chmod o+x /root/.cargo /root/.cargo/bin 2>/dev/null || true
    mkdir -p /usr/local/share/devbox-tools/bin
    for dir in /root/.local/bin /root/.npm-global/bin /root/.cargo/bin; do
        if [ -d "$dir" ]; then
            for f in "$dir"/*; do
                [ -x "$f" ] && ln -sf "$f" /usr/local/share/devbox-tools/bin/ 2>/dev/null || true
            done
        fi
    done
    mkdir -p /home/claude/.ssh /home/claude/.claude
    cp /root/.ssh/authorized_keys /home/claude/.ssh/ 2>/dev/null || true
    chmod 700 /home/claude/.ssh
    chmod 600 /home/claude/.ssh/authorized_keys 2>/dev/null || true
    cp -r /root/.claude/skills /home/claude/.claude/skills 2>/dev/null || true
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
    mkdir -p /workspace
    chown -R claude:claude /home/claude /workspace
    echo "Created claude user."
fi

HOST_IP=$(curl -sf https://ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')
TMUX_SESSION="claude-login"

# Helper: send keys to tmux session as claude user
send_keys() {
    su - claude -c "tmux send-keys -t $TMUX_SESSION $*"
}

# Helper: capture tmux pane content
capture_pane() {
    su - claude -c "tmux capture-pane -t $TMUX_SESSION -p -S -30"
}

# Helper: wait for text to appear in tmux pane
wait_for() {
    local pattern="$1"
    local max_attempts="${2:-20}"
    for i in $(seq 1 "$max_attempts"); do
        if capture_pane 2>/dev/null | grep -q "$pattern"; then
            return 0
        fi
        sleep 2
    done
    return 1
}

echo "================================================"
echo "  ClaudeBox — Login"
echo "================================================"
echo ""

# Check if claude user is already logged in
if su - claude -c "PATH=/root/.local/bin:\$PATH claude auth status --json" 2>/dev/null | grep -q '"loggedIn": true'; then
    echo "Already logged in!"
    su - claude -c "PATH=/root/.local/bin:\$PATH claude auth status"
    exit 0
fi

# Step 1: Launch claude as 'claude' user in /workspace
echo "[1/3] Launching Claude..."
su - claude -c "tmux kill-session -t $TMUX_SESSION" 2>/dev/null || true
su - claude -c "cd /workspace && tmux new-session -d -s $TMUX_SESSION '/root/.local/bin/claude --dangerously-skip-permissions'"
sleep 5

# Step 2: Navigate onboarding prompts until OAuth URL appears
echo "[2/3] Navigating setup..."
for step in $(seq 1 15); do
    PANE=$(capture_pane 2>/dev/null || true)

    if echo "$PANE" | grep -q "oauth/authorize"; then
        break
    elif echo "$PANE" | grep -q "Not logged in"; then
        echo "  Running /login..."
        send_keys "'/login' Enter"
        sleep 3
    elif echo "$PANE" | grep -q "Choose the text style"; then
        echo "  Accepting theme..."
        send_keys Enter
        sleep 3
    elif echo "$PANE" | grep -q "Bypass Permissions"; then
        echo "  Accepting bypass permissions..."
        send_keys Down
        sleep 1
        send_keys Enter
        sleep 3
    elif echo "$PANE" | grep -q "Select login method"; then
        echo "  Selecting Claude account..."
        send_keys Enter
        sleep 5
    elif echo "$PANE" | grep -q "Security notes"; then
        echo "  Accepting security notes..."
        send_keys Enter
        sleep 3
    elif echo "$PANE" | grep -q "trust this folder"; then
        echo "  Trusting workspace..."
        send_keys Enter
        sleep 3
    else
        sleep 3
    fi
done

# Step 3: Capture and display OAuth URL
echo "[3/3] Waiting for OAuth URL..."
if ! wait_for "oauth/authorize" 5; then
    echo "ERROR: OAuth URL not found."
    echo ""
    echo "Debug — tmux pane content:"
    capture_pane
    echo ""
    echo "Attach manually: su - claude -c 'tmux attach -t $TMUX_SESSION'"
    exit 1
fi

OAUTH_URL=$(capture_pane | tr -d '\n' | tr -d ' ' | grep -o 'https://claude\.com/cai/oauth/authorize[^"]*' | sed 's/Pastecodehereifprompted.*//' | head -1)

echo ""
echo "================================================"
echo "  Open this URL in your browser to sign in:"
echo ""
echo "  $OAUTH_URL"
echo ""
echo "  After signing in, copy the auth code"
echo "  and paste it below."
echo "================================================"
echo ""
read -r -p "Auth code: " AUTH_CODE

if [ -z "$AUTH_CODE" ]; then
    echo "No code entered. Aborting."
    su - claude -c "tmux kill-session -t $TMUX_SESSION" 2>/dev/null || true
    exit 1
fi

# Send code to claude
send_keys "'$AUTH_CODE' Enter"
sleep 3

# Navigate remaining post-login prompts until main prompt
echo ""
echo "Completing setup..."
for step in $(seq 1 10); do
    PANE=$(capture_pane 2>/dev/null || true)

    if echo "$PANE" | grep -q "bypass permissions on"; then
        break
    elif echo "$PANE" | grep -q "Login successful"; then
        echo "Login successful!"
        send_keys Enter
        sleep 3
    elif echo "$PANE" | grep -q "Security notes"; then
        send_keys Enter
        sleep 3
    elif echo "$PANE" | grep -q "trust this folder"; then
        send_keys Enter
        sleep 3
    elif echo "$PANE" | grep -q "Bypass Permissions"; then
        send_keys Down
        sleep 1
        send_keys Enter
        sleep 5
    else
        sleep 3
    fi
done

# Start remote-control
echo "Starting remote-control..."
send_keys "'/remote-control' Enter"
sleep 3

if wait_for "Enable Remote Control" 5; then
    send_keys Enter
    sleep 5
fi

# Capture remote-control URL
RC_URL=""
if wait_for "claude.com/code\|claude.ai/code" 10; then
    RC_URL=$(capture_pane | grep -oP 'https://claude\.(com|ai)/code/[^\s]*' | head -1 || true)
fi

# Rename tmux session to 'claude' for persistence
su - claude -c "tmux rename-session -t $TMUX_SESSION claude" 2>/dev/null || true

# ── Auto-start VNC + Chrome ──
echo ""
echo "[4/5] Starting VNC + Chrome..."
if [ -x /start-vnc.sh ]; then
    VNC_OUTPUT=$(/start-vnc.sh 2>&1)
    VNC_URL=$(echo "$VNC_OUTPUT" | grep -o 'https://[^ ]*\.trycloudflare\.com[^ ]*' | head -1)
    VNC_PASS=$(echo "$VNC_OUTPUT" | grep -oP 'Password:\s*\K\S+' | head -1)
else
    VNC_URL=""
    VNC_PASS=""
    echo "  WARNING: start-vnc.sh not found, skipping VNC"
fi

# Launch Chromium on the virtual desktop
sleep 2
DISPLAY=:99 chromium --no-sandbox --window-size=1280,800 &>/dev/null &

# ── Auto-activate pollers ──
echo "[5/5] Activating message pollers..."
AM_KEY=$(grep api_key /root/.agent-mesh/config.toml 2>/dev/null | awk -F'"' '{print $2}')
if [ -n "$AM_KEY" ] && [ -x /opt/claudebox/polling/setup-cron.sh ]; then
    export AM_API_KEY="$AM_KEY"
    bash /opt/claudebox/polling/setup-cron.sh 2>&1 | head -5
else
    echo "  WARNING: am-server not configured or setup-cron.sh not found, skipping pollers"
fi

# ── Summary ──
echo ""
echo "================================================"
echo "  ClaudeBox — Ready!"
echo ""
if [ -n "$RC_URL" ]; then
    echo "  Remote Control:"
    echo "    $RC_URL"
    echo ""
fi
if [ -n "$VNC_URL" ]; then
    echo "  VNC (Chrome):"
    echo "    URL:      ${VNC_URL}/vnc.html"
    echo "    Password: ${VNC_PASS}"
    echo ""
fi
echo "  SSH access:"
echo "    ssh claude@${HOST_IP}"
echo ""
echo "  Next steps:"
echo "    1. Open VNC URL above in your browser"
echo "    2. Install Claude-in-Chrome extension in Chromium"
echo "    3. Log into Discord, Gmail, Zalo in Chromium"
echo "    4. Pollers are already active — they'll start"
echo "       forwarding messages once apps are logged in"
echo "================================================"
