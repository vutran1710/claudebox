#!/bin/bash
# poll-runner.sh — Runs a polling prompt via Claude Code CLI
# Usage: poll-runner.sh <prompt-file.md> [--profile <chrome-profile>]
#
# Requires:
#   - Claude Code CLI authenticated
#   - Chrome running on DISPLAY=:99 with Claude-in-Chrome extension
#   - AM_API_KEY set in environment
set -euo pipefail

PROMPT_FILE="$1"
CHROME_PROFILE="${3:-}"  # optional: --profile <name>

if [ ! -f "$PROMPT_FILE" ]; then
    echo "Error: prompt file not found: $PROMPT_FILE" >&2
    exit 1
fi

if [ -z "${AM_API_KEY:-}" ]; then
    echo "Error: AM_API_KEY not set" >&2
    exit 1
fi

PROMPT=$(cat "$PROMPT_FILE")
LOG_FILE="/tmp/poll-$(basename "$PROMPT_FILE" .md).log"

# Run as claude user (--dangerously-skip-permissions refuses root)
if [ "$(id -u)" = "0" ]; then
    su - claude -c "export DISPLAY=:99 AM_API_KEY='$AM_API_KEY' PATH=/usr/local/share/devbox-tools/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin && claude --dangerously-skip-permissions -p '$(echo "$PROMPT" | sed "s/'/'\\\\''/g")' --output-format json" \
        2>"$LOG_FILE" || true
else
    claude --dangerously-skip-permissions -p "$PROMPT" \
        --output-format json \
        2>"$LOG_FILE" || true
fi
