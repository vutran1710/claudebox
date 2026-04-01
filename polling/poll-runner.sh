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

# Run Claude with the prompt, non-interactively
# --dangerously-skip-permissions: required for unattended execution
# --allowedTools: limit to browser + network tools
claude --dangerously-skip-permissions -p "$PROMPT" \
    --output-format json \
    2>/tmp/poll-$(basename "$PROMPT_FILE" .md).log || true
