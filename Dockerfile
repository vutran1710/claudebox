FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV HOME=/root
ENV PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.cargo/bin:/usr/local/go/bin:$PATH"

# System deps + Chromium shared libs + VNC
RUN apt-get update && apt-get install -y \
    curl wget git unzip jq build-essential \
    ca-certificates gnupg lsb-release sudo \
    openssh-client openssh-server tmux \
    libnss3 libatk1.0-0t64 libatk-bridge2.0-0t64 libcups2t64 \
    libdrm2 libxkbcommon0 libxcomposite1 libxdamage1 libxrandr2 \
    libgbm1 libpango-1.0-0 libcairo2 libasound2t64 libxshmfence1 \
    libgtk-3-0t64 libx11-xcb1 fonts-liberation xdg-utils \
    pkg-config libssl-dev \
    xvfb x11vnc novnc websockify fluxbox xterm \
    && rm -rf /var/lib/apt/lists/*

# Rust
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain 1.84.0

# Python (uv)
ARG UV_VERSION=0.6.6
RUN curl -LsSf "https://astral.sh/uv/${UV_VERSION}/install.sh" | sh \
    && $HOME/.local/bin/uv python install 3.13

# Node.js 22
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs \
    && npm config set prefix "$HOME/.npm-global" \
    && rm -rf /var/lib/apt/lists/*

# Go
ARG GO_VERSION=1.24.1
ARG GO_SHA256=cb2396bae64183cdccf81a9a6df0aea3bce9511fc21469fb89a0c00470088073
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz \
    && echo "${GO_SHA256}  /tmp/go.tar.gz" | sha256sum -c - \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz

# Docker CLI
RUN curl -fsSL https://get.docker.com | sh

# GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Claude Code CLI
RUN curl -fsSL https://claude.ai/install.sh | bash

# Agent Browser + system chromium symlink
RUN npm install -g agent-browser \
    && agent-browser install \
    && ln -sf /root/.agent-browser/browsers/chrome-*/chrome /usr/local/bin/chromium

# Vercel CLI
RUN npm install -g vercel

# Playwright
RUN npm install -g playwright \
    && playwright install --with-deps chromium

# Supabase CLI
RUN curl -fsSL https://raw.githubusercontent.com/supabase/cli/main/install.sh | bash

# Wormhole CLI
RUN curl -fsSL https://wormhole.bar/install.sh | sh

# Cloudflared (for tunneling VNC/services to internet)
RUN curl -fsSL https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb -o /tmp/cloudflared.deb \
    && dpkg -i /tmp/cloudflared.deb \
    && rm /tmp/cloudflared.deb

# SSH server
RUN mkdir -p /run/sshd \
    && sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config \
    && sed -i 's/#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config \
    && sed -i 's/#PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config

# Claude skills
RUN mkdir -p /root/.claude/skills/agent-browser \
    && mkdir -p /root/.claude/skills/wormhole \
    && mkdir -p /root/.claude/skills/vnc

COPY skills/agent-browser/SKILL.md /root/.claude/skills/agent-browser/SKILL.md
COPY skills/wormhole/SKILL.md /root/.claude/skills/wormhole/SKILL.md
COPY skills/vnc/SKILL.md /root/.claude/skills/vnc/SKILL.md
COPY CLAUDE.md /root/CLAUDE.md

RUN mkdir -p /workspace
WORKDIR /workspace

COPY entrypoint.sh /entrypoint.sh
COPY start-vnc.sh /start-vnc.sh
COPY login-claude.sh /login-claude.sh
RUN chmod +x /entrypoint.sh /start-vnc.sh /login-claude.sh \
    && ln -sf /start-vnc.sh /usr/local/bin/start-vnc

# Locale (suppress setlocale warnings)
RUN apt-get update && apt-get install -y locales \
    && locale-gen en_US.UTF-8 \
    && rm -rf /var/lib/apt/lists/*
ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

EXPOSE 22 6080

ENTRYPOINT ["/entrypoint.sh"]
