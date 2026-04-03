package activate

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	ChromeMCPEntry = `{
  "mcpServers": {
    "chrome": {
      "command": "node",
      "args": ["/opt/chrome-lite-mcp/server/index.js"],
      "env": {}
    }
  }
}`
)

// ConfigureChromeMCP sets up the Chrome MCP server in Claude Code's config.
func ConfigureChromeMCP() error {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/claude"
	}
	configPath := home + "/.claude.json"

	// Ensure .claude directory exists
	os.MkdirAll(home+"/.claude", 0755)

	// Write MCP config if not already present
	existing, _ := os.ReadFile(configPath)
	if !strings.Contains(string(existing), "chrome-lite-mcp") {
		if err := os.WriteFile(configPath, []byte(ChromeMCPEntry), 0644); err != nil {
			return fmt.Errorf("failed to write MCP config: %w", err)
		}
	}

	// Copy skills reference
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`cp /opt/chrome-lite-mcp/docs/skills.md %s/.claude/chrome-lite-mcp-skills.md 2>/dev/null || true`, home))

	return nil
}

// IsChromeMCPConfigured checks if Chrome MCP is set up in Claude Code's config.
func IsChromeMCPConfigured() bool {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/claude"
	}
	data, _ := os.ReadFile(home + "/.claude.json")
	return strings.Contains(string(data), "chrome-lite-mcp")
}

// RemoveOldPollers cleans up any legacy cron-based pollers.
func RemoveOldPollers() {
	shell.RunShellTimeout(10*time.Second,
		`(crontab -l 2>/dev/null | grep -v "ClaudeBox message polling" | grep -v "poll-runner.sh") | crontab - 2>/dev/null || true`)
}
