package session

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	ClaudeBin = "/usr/local/share/devbox-tools/bin/claude"
	WorkDir   = "/workspace"
)

// TmuxManager manages Claude Code sessions via tmux.
type TmuxManager struct{}

func NewTmuxManager() *TmuxManager {
	return &TmuxManager{}
}

func (m *TmuxManager) Create(name, workDir string) (*Session, error) {
	if name == "" {
		return nil, fmt.Errorf("session name is required")
	}
	if workDir == "" {
		workDir = WorkDir
	}

	// Kill existing session with this name
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux kill-session -t %s 2>/dev/null || true`, name))

	// Spawn claude in a new tmux session
	_, err := shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`cd %s && tmux new-session -d -s %s '%s --dangerously-skip-permissions'`,
			workDir, name, ClaudeBin))
	if err != nil {
		return nil, fmt.Errorf("failed to start session: %w", err)
	}

	time.Sleep(5 * time.Second)

	// Navigate startup prompts
	for i := 0; i < 10; i++ {
		pane := capturPane(name)
		switch {
		case strings.Contains(pane, "Choose the text style"):
			sendKeys(name, "Enter")
		case strings.Contains(pane, "trust this folder"):
			sendKeys(name, "Enter")
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
	rcURL := enableRemoteControl(name)

	return &Session{
		Name:   name,
		Dir:    workDir,
		Status: "running",
		RCURL:  rcURL,
	}, nil
}

func (m *TmuxManager) List() ([]Session, error) {
	res, _ := shell.RunShellTimeout(5*time.Second, `tmux ls 2>/dev/null`)
	output := strings.TrimSpace(res.Stdout)
	if output == "" {
		return nil, nil
	}

	var sessions []Session
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		// Format: "name: N windows (created ...)"
		name := line
		if idx := strings.Index(line, ":"); idx > 0 {
			name = line[:idx]
		}
		sessions = append(sessions, Session{
			Name:   name,
			Status: "running",
		})
	}
	return sessions, nil
}

func (m *TmuxManager) Kill(name string) error {
	_, err := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux kill-session -t %s`, name))
	return err
}

func (m *TmuxManager) IsRunning(name string) bool {
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux has-session -t %s 2>/dev/null`, name))
	return res.ExitCode == 0
}

// --- internal helpers ---

func sendKeys(session, keys string) {
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux send-keys -t %s %s`, session, keys))
}

func capturPane(session string) string {
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`tmux capture-pane -t %s -p -S -50`, session))
	return res.Stdout
}

func enableRemoteControl(session string) string {
	sendKeys(session, "'/remote-control' Enter")
	time.Sleep(3 * time.Second)

	pane := capturPane(session)
	if strings.Contains(pane, "Enable Remote Control") {
		sendKeys(session, "Enter")
		time.Sleep(5 * time.Second)
	}

	// Extract URL
	for i := 0; i < 10; i++ {
		pane = capturPane(session)
		re := regexp.MustCompile(`https://claude\.(com|ai)/code/\S+`)
		if url := re.FindString(pane); url != "" {
			return url
		}
		time.Sleep(2 * time.Second)
	}
	return ""
}
