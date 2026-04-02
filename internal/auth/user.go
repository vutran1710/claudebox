package auth

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

func EnsureClaudeUser() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create user if not exists
	shell.RunShellTimeout(10*time.Second, `id claude >/dev/null 2>&1 || useradd -m -s /bin/bash claude`)
	// Verify user was created
	res, err := shell.RunShellTimeout(5*time.Second, `id claude`)
	if err != nil || res.ExitCode != 0 {
		return fmt.Errorf("failed to create claude user: %s", res.Stderr)
	}

	shell.RunShell(ctx, "chmod o+x /root")
	shell.RunShell(ctx, "chmod o+x /root/.local /root/.local/bin 2>/dev/null || true")
	shell.RunShell(ctx, "chmod o+x /root/.npm-global /root/.npm-global/bin 2>/dev/null || true")
	shell.RunShell(ctx, "chmod o+x /root/.cargo /root/.cargo/bin 2>/dev/null || true")

	shell.RunShell(ctx, `mkdir -p /usr/local/share/devbox-tools/bin
for dir in /root/.local/bin /root/.npm-global/bin /root/.cargo/bin; do
    [ -d "$dir" ] && for f in "$dir"/*; do
        [ -x "$f" ] && ln -sf "$f" /usr/local/share/devbox-tools/bin/ 2>/dev/null || true
    done
done`)

	shell.RunShell(ctx, `mkdir -p /home/claude/.ssh
cp /root/.ssh/authorized_keys /home/claude/.ssh/ 2>/dev/null || true
chmod 700 /home/claude/.ssh
chmod 600 /home/claude/.ssh/authorized_keys 2>/dev/null || true`)

	shell.RunShell(ctx, `cp -r /root/.claude /home/claude/.claude 2>/dev/null || true`)
	shell.RunShell(ctx, `mkdir -p /home/claude/.config && cp -r /root/.config/gh /home/claude/.config/gh 2>/dev/null || true`)

	bashrc := `export PATH="/usr/local/share/devbox-tools/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
export HOME=/home/claude
claude() {
  if [ "$1" = "auth" ] || [ "$1" = "config" ]; then
    /usr/local/share/devbox-tools/bin/claude "$@"
  else
    /usr/local/share/devbox-tools/bin/claude --dangerously-skip-permissions "$@"
  fi
}
`
	os.WriteFile("/home/claude/.bashrc", []byte(bashrc), 0644)

	shell.RunShell(ctx, "mkdir -p /workspace")
	shell.RunShell(ctx, "chown -R claude:claude /home/claude /workspace")
	shell.RunShell(ctx, "claude config set --global autoUpdaterStatus disabled 2>/dev/null || true")

	return nil
}

func AuthenticateGitHub(token string) error {
	if token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shell.RunShell(ctx, fmt.Sprintf(`echo "%s" | gh auth login --with-token || true`, token))
	shell.RunShell(ctx, fmt.Sprintf(
		`TMPFILE=$(mktemp) && printf '%%s' "%s" > "$TMPFILE" && chmod 600 "$TMPFILE" && su - claude -c "gh auth login --with-token < '$TMPFILE'" 2>/dev/null || true && rm -f "$TMPFILE"`,
		token))

	return nil
}
