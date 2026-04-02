package activate

import (
	"fmt"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	ChromeMCPConfig = `/home/claude/.claude.json`
	ChromeMCPEntry  = `{
  "mcpServers": {
    "chrome": {
      "command": "node",
      "args": ["/opt/chrome-mcp/server/index.js"],
      "env": {}
    }
  }
}`
)

// ConfigureChromeMCP sets up the Chrome MCP server in Claude Code's config.
// This replaces the old cron-based pollers — Claude Code now reads messages
// directly via Chrome MCP tools (page_eval, page_read, etc.) on demand.
func ConfigureChromeMCP() error {
	// Ensure .claude directory exists
	_, err := shell.RunShellTimeout(10*time.Second,
		`mkdir -p /home/claude/.claude && chown claude:claude /home/claude/.claude`)
	if err != nil {
		return fmt.Errorf("failed to create .claude dir: %w", err)
	}

	// Write MCP config if not already present
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`cat %s 2>/dev/null`, ChromeMCPConfig))
	if !strings.Contains(res.Stdout, "chrome-mcp") {
		_, err = shell.RunShellTimeout(10*time.Second,
			fmt.Sprintf(`echo '%s' > %s && chown claude:claude %s`,
				ChromeMCPEntry, ChromeMCPConfig, ChromeMCPConfig))
		if err != nil {
			return fmt.Errorf("failed to write MCP config: %w", err)
		}
	}

	// Copy skills.md to a location Claude Code can reference
	_, _ = shell.RunShellTimeout(10*time.Second,
		`cp /opt/chrome-mcp/docs/skills.md /home/claude/.claude/chrome-mcp-skills.md 2>/dev/null && chown claude:claude /home/claude/.claude/chrome-mcp-skills.md`)

	return nil
}

// IsChromeMCPConfigured checks if Chrome MCP is set up in Claude Code's config.
func IsChromeMCPConfigured() bool {
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`cat %s 2>/dev/null`, ChromeMCPConfig))
	return strings.Contains(res.Stdout, "chrome-mcp")
}

// RemoveOldPollers cleans up any legacy cron-based pollers.
func RemoveOldPollers() {
	shell.RunShellTimeout(10*time.Second,
		`(crontab -l 2>/dev/null | grep -v "ClaudeBox message polling" | grep -v "poll-runner.sh") | crontab - 2>/dev/null || true`)
}
