package code

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vutran1710/claudebox/internal/session"
	"github.com/vutran1710/claudebox/internal/shell"
	"github.com/vutran1710/claudebox/internal/ui"
)

const workspace = "/workspace"

// ResolveName determines the session name from the inputs.
func ResolveName(name, repo, project string) string {
	if name != "" {
		return name
	}
	if repo != "" {
		parts := strings.Split(repo, "/")
		return parts[len(parts)-1]
	}
	if project != "" {
		return project
	}
	return fmt.Sprintf("claude-%d", time.Now().Unix())
}

// ResolveWorkDir finds or clones the working directory.
// Returns (dir, status, error).
func ResolveWorkDir(repo, project string) (string, string, error) {
	if repo != "" {
		return resolveRepoDir(repo)
	}
	if project != "" {
		return resolveProjectDir(project)
	}
	return "", "", nil
}

func resolveRepoDir(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	repoName := parts[len(parts)-1]

	// Check if repo already exists
	for _, dir := range []string{
		filepath.Join(workspace, repoName),
		filepath.Join(workspace, repo),
	} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				return dir, fmt.Sprintf("Found repo at %s", dir), nil
			}
		}
	}

	// Clone it
	cloneDir := filepath.Join(workspace, repoName)
	cloneURL := fmt.Sprintf("https://github.com/%s.git", repo)
	_, err := shell.RunShellTimeout(2*time.Minute,
		fmt.Sprintf(`git clone %s %s`, cloneURL, cloneDir))
	if err != nil {
		return "", "", fmt.Errorf("failed to clone %s: %w", repo, err)
	}
	return cloneDir, fmt.Sprintf("Cloned %s", repo), nil
}

func resolveProjectDir(project string) (string, string, error) {
	dir := filepath.Join(workspace, project)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, fmt.Sprintf("Found project at %s", dir), nil
	}
	return "", "", fmt.Errorf("project not found: %s", dir)
}

// RunHeadless runs without TUI — suitable for non-interactive use (e.g., from another Claude session).
func RunHeadless(name, repo, project string) error {
	name = ResolveName(name, repo, project)

	workDir, status, err := ResolveWorkDir(repo, project)
	if err != nil {
		return err
	}
	if status != "" {
		fmt.Println(status)
	}

	fmt.Printf("Starting session '%s'...\n", name)
	rcURL, err := session.StartClaudeSession(name, workDir)
	if err != nil {
		return err
	}

	fmt.Printf("Session '%s' started\n", name)
	if workDir != "" {
		fmt.Printf("Working dir: %s\n", workDir)
	}
	if rcURL != "" {
		fmt.Printf("Remote Control: %s\n", rcURL)
	}
	fmt.Printf("Attach: tmux attach -t %s\n", name)
	return nil
}

// Run starts the TUI version.
func Run(name, repo, project string, headless bool) error {
	if headless {
		return RunHeadless(name, repo, project)
	}

	name = ResolveName(name, repo, project)
	m := model{
		spinner: ui.NewSpinner(),
		name:    name,
		repo:    repo,
		project: project,
	}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// TUI model

type model struct {
	spinner spinner.Model
	name    string
	repo    string
	project string
	workDir string
	rcURL   string
	status  string
	done    bool
	err     error
}

func (m model) Init() tea.Cmd {
	if m.repo != "" || m.project != "" {
		return tea.Batch(m.spinner.Tick, resolveDir(m.repo, m.project))
	}
	return tea.Batch(m.spinner.Tick, startSession(m.name, ""))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case dirResolvedMsg:
		m.workDir = msg.dir
		m.status = msg.status
		return m, startSession(m.name, m.workDir)
	case sessionReadyMsg:
		m.rcURL = msg.rcURL
		m.done = true
		return m, tea.Quit
	case ui.ErrMsg:
		m.err = msg.Err
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	if !m.done {
		if m.status != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.StyleCheck.Render(), m.status))
		}
		b.WriteString(fmt.Sprintf("  %s Starting Claude Code session '%s'...\n",
			ui.StyleSpin.Render(m.spinner.View()), m.name))
	} else {
		if m.status != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.StyleCheck.Render(), m.status))
		}
		b.WriteString(fmt.Sprintf("  %s Session '%s' started\n", ui.StyleCheck.Render(), m.name))
		if m.workDir != "" {
			b.WriteString(fmt.Sprintf("  %s Working dir: %s\n", ui.StyleCheck.Render(), m.workDir))
		}
		if m.rcURL != "" {
			b.WriteString(fmt.Sprintf("\n  Remote Control: %s\n", ui.StyleValue.Render(m.rcURL)))
		}
		b.WriteString(fmt.Sprintf("\n  Attach: tmux attach -t %s\n", m.name))
	}
	if m.err != nil {
		b.WriteString(fmt.Sprintf("\n  %s %s\n", ui.StyleCross.Render(), m.err.Error()))
	}
	return b.String() + "\n"
}

// Tea messages

type dirResolvedMsg struct {
	dir    string
	status string
}
type sessionReadyMsg struct{ rcURL string }

func resolveDir(repo, project string) tea.Cmd {
	return func() tea.Msg {
		dir, status, err := ResolveWorkDir(repo, project)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return dirResolvedMsg{dir: dir, status: status}
	}
}

func startSession(name, workDir string) tea.Cmd {
	return func() tea.Msg {
		rcURL, err := session.StartClaudeSession(name, workDir)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return sessionReadyMsg{rcURL: rcURL}
	}
}
