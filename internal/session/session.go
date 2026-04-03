package session

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	ClaudeBin         = "/usr/local/share/devbox-tools/bin/claude"
	WorkDir           = "/workspace"
	MasterSessionName = "claude-master"
	ClaudeUser        = "claude"
)

// StartClaudeSession spawns a new Claude Code tmux session in the given directory.
// If workDir is empty, defaults to /workspace.
// Returns the remote control URL.
func StartClaudeSession(name string, workDir string) (remoteControlURL string, err error) {
	if workDir == "" {
		workDir = WorkDir
	}

	// Kill existing session with this name
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux kill-session -t %s 2>/dev/null || true`, name))

	// Spawn claude in a new tmux session
	_, err = shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`cd %s && tmux new-session -d -s %s '%s --dangerously-skip-permissions'`,
			workDir, name, ClaudeBin))
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

// StartMasterSession spawns the master Claude session as the claude user.
// Called from setup (which runs as root), so uses su to switch user.
func StartMasterSession() (remoteControlURL string, err error) {
	// Kill any existing master session
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`su - %s -c "tmux kill-session -t %s 2>/dev/null || true"`, ClaudeUser, MasterSessionName))

	// Spawn as claude user
	_, err = shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`su - %s -c "cd %s && tmux new-session -d -s %s '%s --dangerously-skip-permissions'"`,
			ClaudeUser, WorkDir, MasterSessionName, ClaudeBin))
	if err != nil {
		return "", fmt.Errorf("failed to start master session: %w", err)
	}

	time.Sleep(5 * time.Second)

	// Navigate startup prompts
	for i := 0; i < 10; i++ {
		pane := capturePaneAsUser(ClaudeUser, MasterSessionName)

		switch {
		case strings.Contains(pane, "Choose the text style"):
			sendKeysAsUser(ClaudeUser, MasterSessionName, "Enter")
		case strings.Contains(pane, "trust this folder"):
			sendKeysAsUser(ClaudeUser, MasterSessionName, "Enter")
		case strings.Contains(pane, "bypass permissions on"):
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
	sendKeysAsUser(ClaudeUser, MasterSessionName, "'/remote-control' Enter")
	time.Sleep(3 * time.Second)

	pane := capturePaneAsUser(ClaudeUser, MasterSessionName)
	if strings.Contains(pane, "Enable Remote Control") {
		sendKeysAsUser(ClaudeUser, MasterSessionName, "Enter")
		time.Sleep(5 * time.Second)
	}

	// Extract remote control URL
	for i := 0; i < 10; i++ {
		pane = capturePaneAsUser(ClaudeUser, MasterSessionName)
		re := regexp.MustCompile(`https://claude\.(com|ai)/code/\S+`)
		if url := re.FindString(pane); url != "" {
			remoteControlURL = url
			break
		}
		time.Sleep(2 * time.Second)
	}

	return remoteControlURL, nil
}

func sendKeysAsUser(user, session, keys string) {
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`su - %s -c "tmux send-keys -t %s %s"`, user, session, keys))
}

func capturePaneAsUser(user, session string) string {
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`su - %s -c "tmux capture-pane -t %s -p -S -50"`, user, session))
	return res.Stdout
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
