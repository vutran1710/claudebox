#!/bin/bash
# setup-cron.sh — Install cron jobs for message polling
# Run this once after ClaudeBox is set up and Chrome profiles are logged in.
set -euo pipefail

POLLING_DIR="/opt/claudebox/polling"
POLL_RUNNER="$POLLING_DIR/poll-runner.sh"

# Load AM_API_KEY from config
AM_API_KEY=$(grep api_key /root/.agent-mesh/config.toml 2>/dev/null | awk -F'"' '{print $2}')
if [ -z "$AM_API_KEY" ]; then
    echo "Error: could not read AM_API_KEY from ~/.agent-mesh/config.toml" >&2
    exit 1
fi

# Cron environment
CRON_ENV="DISPLAY=:99 AM_API_KEY=$AM_API_KEY PATH=/usr/local/share/devbox-tools/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin HOME=/root"

# Build crontab
# All pollers run every 5 minutes, staggered by 1 min to avoid overlap
CRON_JOBS=$(cat <<EOF
# ClaudeBox message polling
$CRON_ENV

*/5 * * * * $POLL_RUNNER $POLLING_DIR/check-discord-messages.md >> /var/log/poll-discord.log 2>&1
1,6,11,16,21,26,31,36,41,46,51,56 * * * * $POLL_RUNNER $POLLING_DIR/check-gmail-personal.md >> /var/log/poll-gmail-personal.log 2>&1
2,7,12,17,22,27,32,37,42,47,52,57 * * * * $POLL_RUNNER $POLLING_DIR/check-gmail-work.md >> /var/log/poll-gmail-work.log 2>&1
3,8,13,18,23,28,33,38,43,48,53,58 * * * * $POLL_RUNNER $POLLING_DIR/check-zalo-messages.md >> /var/log/poll-zalo.log 2>&1
EOF
)

# Install crontab (preserves existing non-claudebox entries)
(crontab -l 2>/dev/null | grep -v "ClaudeBox message polling" | grep -v "poll-runner.sh" || true; echo "$CRON_JOBS") | crontab -

echo "Cron jobs installed:"
crontab -l | grep poll-runner
echo ""
echo "Logs at /var/log/poll-*.log"
echo "Remove with: crontab -l | grep -v poll-runner | crontab -"
