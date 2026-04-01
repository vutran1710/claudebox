package auth

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	TmuxSession = "claude-login"
	ClaudeUser  = "claude"
	ClaudeBin   = "/root/.local/bin/claude"
)

func sendKeys(keys string) error {
	_, err := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`su - %s -c "tmux send-keys -t %s %s"`, ClaudeUser, TmuxSession, keys))
	return err
}

func capturePane() (string, error) {
	res, err := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`su - %s -c "tmux capture-pane -t %s -p -S -50"`, ClaudeUser, TmuxSession))
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

func waitForPattern(pattern string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	for time.Now().Before(deadline) {
		pane, err := capturePane()
		if err == nil && re.MatchString(pane) {
			return true, nil
		}
		time.Sleep(2 * time.Second)
	}
	return false, nil
}

func killSession() {
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`su - %s -c "tmux kill-session -t %s" 2>/dev/null || true`, ClaudeUser, TmuxSession))
}

func startClaudeInTmux() error {
	killSession()
	_, err := shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`su - %s -c "cd /workspace && tmux new-session -d -s %s '%s --dangerously-skip-permissions'"`,
			ClaudeUser, TmuxSession, ClaudeBin))
	return err
}

func renameSession(newName string) {
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`su - %s -c "tmux rename-session -t %s %s" 2>/dev/null || true`,
			ClaudeUser, TmuxSession, newName))
}

func extractOAuthURL() (string, error) {
	pane, err := capturePane()
	if err != nil {
		return "", err
	}
	stripped := strings.Join(strings.Fields(pane), "")
	re := regexp.MustCompile(`https://claude\.com/cai/oauth/authorize[^"]*`)
	match := re.FindString(stripped)
	if match == "" {
		return "", fmt.Errorf("OAuth URL not found in pane")
	}
	if idx := strings.Index(match, "Pastecodehereifprompted"); idx > 0 {
		match = match[:idx]
	}
	return match, nil
}

func extractRemoteControlURL() string {
	pane, _ := capturePane()
	re := regexp.MustCompile(`https://claude\.(com|ai)/code/\S+`)
	return re.FindString(pane)
}

func IsAlreadyLoggedIn() bool {
	res, _ := shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`su - %s -c "PATH=/root/.local/bin:$PATH claude auth status --json" 2>/dev/null`, ClaudeUser))
	return strings.Contains(res.Stdout, `"loggedIn": true`) || strings.Contains(res.Stdout, `"loggedIn":true`)
}
