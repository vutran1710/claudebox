package setup

import (
	"context"
	"fmt"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

type Tool struct {
	Name    string
	Check   func() bool
	Install func(ctx context.Context) error
}

func AllTools() []Tool {
	return []Tool{
		{
			Name:  "System dependencies",
			Check: func() bool {
				return shell.Which("tmux") && shell.Which("jq") && shell.Which("x11vnc") && shell.Which("websockify") && shell.Which("fluxbox")
			},
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `
while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 2; done
apt-get update && apt-get install -y \
					curl wget git unzip jq build-essential \
					ca-certificates gnupg lsb-release sudo tmux locales \
					libnss3 libatk1.0-0t64 libatk-bridge2.0-0t64 libcups2t64 \
					libdrm2 libxkbcommon0 libxcomposite1 libxdamage1 libxrandr2 \
					libgbm1 libpango-1.0-0 libcairo2 libasound2t64 libxshmfence1 \
					libgtk-3-0t64 libx11-xcb1 fonts-liberation xdg-utils \
					pkg-config libssl-dev \
					xvfb x11vnc novnc websockify fluxbox xterm \
					&& locale-gen en_US.UTF-8 \
					&& rm -rf /var/lib/apt/lists/*`)
				return err
			},
		},
		{
			Name:  "Cloudflare Tunnel",
			Check: func() bool { return shell.Which("cloudflared") },
			Install: func(ctx context.Context) error {
				// Install via apt repo — shares the lock with system deps so no contention
				_, err := shell.RunShell(ctx, `
while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1 || fuser /var/lib/dpkg/lock >/dev/null 2>&1; do sleep 3; done
mkdir -p --mode=0755 /usr/share/keyrings
curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared $(lsb_release -cs) main" | tee /etc/apt/sources.list.d/cloudflared.list
apt-get update -o Dir::Etc::sourcelist="sources.list.d/cloudflared.list" -o Dir::Etc::sourceparts="-" -o APT::Get::List-Cleanup="0"
apt-get install -y cloudflared`)
				return err
			},
		},
		{
			Name:  "Rust 1.84",
			Check: func() bool { return shell.FileExists("/root/.cargo/bin/rustc") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain 1.84.0`)
				return err
			},
		},
		{
			Name:  "Python 3.13 + uv",
			Check: func() bool { return shell.FileExists("/root/.local/bin/uv") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `curl -LsSf "https://astral.sh/uv/0.6.6/install.sh" | sh && /root/.local/bin/uv python install 3.13`)
				return err
			},
		},
		{
			Name: "Node.js 22",
			Check: func() bool {
				res, _ := shell.RunShellTimeout(5*time.Second, `node --version 2>/dev/null | grep -q "^v2[2-9]\|^v[3-9]" && which npm >/dev/null 2>&1`)
				return res.ExitCode == 0
			},
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 2; done && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y nodejs && npm config set prefix "/root/.npm-global" && rm -rf /var/lib/apt/lists/*`)
				return err
			},
		},
		{
			Name:  "Go 1.24",
			Check: func() bool { return shell.FileExists("/usr/local/go/bin/go") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `curl -fsSL "https://go.dev/dl/go1.24.1.linux-amd64.tar.gz" -o /tmp/go.tar.gz && echo "cb2396bae64183cdccf81a9a6df0aea3bce9511fc21469fb89a0c00470088073  /tmp/go.tar.gz" | sha256sum -c - && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz`)
				return err
			},
		},
		{
			Name:  "Docker CLI",
			Check: func() bool { return shell.Which("docker") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `curl -fsSL https://get.docker.com | sh`)
				return err
			},
		},
		{
			Name:  "GitHub CLI",
			Check: func() bool { return shell.Which("gh") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 2; done && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null && apt-get update && apt-get install -y gh && rm -rf /var/lib/apt/lists/*`)
				return err
			},
		},
		{
			Name:  "Claude Code CLI",
			Check: func() bool { return shell.FileExists("/root/.local/bin/claude") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `curl -fsSL https://claude.ai/install.sh | bash`)
				return err
			},
		},
		{
			Name:  "Agent Browser + Chromium",
			Check: func() bool { return shell.Which("agent-browser") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `npm install -g agent-browser && agent-browser install && ln -sf /root/.agent-browser/browsers/chrome-*/chrome /usr/local/bin/chromium`)
				return err
			},
		},
		{
			Name:  "Vercel CLI",
			Check: func() bool { return shell.Which("vercel") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `npm install -g vercel`)
				return err
			},
		},
		{
			Name:  "Playwright",
			Check: func() bool { return shell.Which("playwright") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `npm install -g playwright && playwright install --with-deps chromium`)
				return err
			},
		},
		{
			Name:  "Supabase CLI",
			Check: func() bool { return shell.Which("supabase") },
			Install: func(ctx context.Context) error {
				_, err := shell.RunShell(ctx, `curl -fsSL https://raw.githubusercontent.com/supabase/cli/main/install.sh | bash`)
				return err
			},
		},
		{
			Name:  "am-server",
			Check: func() bool { return shell.FileExists("/usr/local/bin/am-server") },
			Install: func(ctx context.Context) error {
				script := `
AM_REPO="vutran1710/am"
DOWNLOAD_URL=$(curl -sL "https://api.github.com/repos/${AM_REPO}/releases/latest" | grep -o 'https://[^"]*am-server-linux-amd64[^"]*' | head -1)
if [ -n "$DOWNLOAD_URL" ]; then
    curl -sL "$DOWNLOAD_URL" -o /usr/local/bin/am-server
    chmod +x /usr/local/bin/am-server
else
    echo "Failed to find am-server release" >&2
    exit 1
fi`
				_, err := shell.RunShell(ctx, script)
				return err
			},
		},
		{
			Name: "Chrome Lite MCP",
			Check: func() bool {
				return shell.FileExists("/opt/chrome-lite-mcp/server/index.js") &&
					shell.FileExists("/opt/chrome-lite-mcp/extension/background.js")
			},
			Install: func(ctx context.Context) error {
				script := `
REPO="vutran1710/chrome-lite-mcp"
DOWNLOAD_URL=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep -o 'https://[^"]*chrome-lite-mcp-.*\.tar\.gz[^"]*' | head -1)
if [ -n "$DOWNLOAD_URL" ]; then
    mkdir -p /opt/chrome-lite-mcp
    curl -sL "$DOWNLOAD_URL" | tar -xz -C /opt/chrome-lite-mcp
else
    echo "Failed to find chrome-lite-mcp release" >&2
    exit 1
fi`
				_, err := shell.RunShell(ctx, script)
				return err
			},
		},
	}
}

func DefaultTimeout() time.Duration {
	return 10 * time.Minute
}

func ToolCount() int {
	return len(AllTools())
}

func ToolName(index int) string {
	tools := AllTools()
	if index < len(tools) {
		return tools[index].Name
	}
	return fmt.Sprintf("Tool %d", index)
}
