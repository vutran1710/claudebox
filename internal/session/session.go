package session

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	ClaudeBin  = "/usr/local/share/devbox-tools/bin/claude"
	WorkDir    = "/workspace"
)

// StartClaudeSession spawns a new Claude Code tmux session.
// Returns the session name and remote control URL.
func StartClaudeSession(name string) (remoteControlURL string, err error) {
	// Kill existing session with this name
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux kill-session -t %s 2>/dev/null || true`, name))

	// Spawn claude in a new tmux session
	_, err = shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`cd %s && tmux new-session -d -s %s '%s --dangerously-skip-permissions'`,
			WorkDir, name, ClaudeBin))
	if err != nil {
		return "", fmt.Errorf("failed to start Claude session: %w", err)
	}

	// Wait for Claude to start and enable remote control
	time.Sleep(5 * time.Second)

	// Navigate through any startup prompts
	for i := 0; i < 10; i++ {
		pane := capturePane(name)

		switch {
		case strings.Contains(pane, "Choose the text style"):
			sendKeys(name, "Enter")
		case strings.Contains(pane, "trust this folder"):
			sendKeys(name, "Enter")
		case strings.Contains(pane, "bypass permissions on"):
			// Ready — enable remote control
			goto ready
		case strings.Contains(pane, "What can I help"):
			goto ready
		case strings.Contains(pane, ">"):
			goto ready
		}
		time.Sleep(3 * time.Second)
	}

ready:
	// Enable remote control
	sendKeys(name, "'/remote-control' Enter")
	time.Sleep(3 * time.Second)

	pane := capturePane(name)
	if strings.Contains(pane, "Enable Remote Control") {
		sendKeys(name, "Enter")
		time.Sleep(5 * time.Second)
	}

	// Extract remote control URL
	for i := 0; i < 10; i++ {
		pane = capturePane(name)
		re := regexp.MustCompile(`https://claude\.(com|ai)/code/\S+`)
		if url := re.FindString(pane); url != "" {
			remoteControlURL = url
			break
		}
		time.Sleep(2 * time.Second)
	}

	return remoteControlURL, nil
}

// IsSessionRunning checks if a tmux session exists.
func IsSessionRunning(name string) bool {
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux has-session -t %s 2>/dev/null`, name))
	return res.ExitCode == 0
}

// ListSessions returns all tmux sessions.
func ListSessions() string {
	res, _ := shell.RunShellTimeout(5*time.Second, `tmux ls 2>/dev/null`)
	return strings.TrimSpace(res.Stdout)
}

func sendKeys(session, keys string) {
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux send-keys -t %s %s`, session, keys))
}

func capturePane(session string) string {
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux capture-pane -t %s -p -S -50`, session))
	return res.Stdout
}
