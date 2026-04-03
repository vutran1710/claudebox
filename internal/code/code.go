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

type model struct {
	spinner spinner.Model
	name    string
	repo    string
	workDir string
	rcURL   string
	status  string
	done    bool
	err     error
}

func Run(name string, repo string) error {
	if name == "" {
		if repo != "" {
			// Use repo name as session name (e.g., "vutran1710/claudebox" -> "claudebox")
			parts := strings.Split(repo, "/")
			name = parts[len(parts)-1]
		} else {
			name = fmt.Sprintf("claude-%d", time.Now().Unix())
		}
	}
	m := model{
		spinner: ui.NewSpinner(),
		name:    name,
		repo:    repo,
	}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func (m model) Init() tea.Cmd {
	if m.repo != "" {
		return tea.Batch(m.spinner.Tick, resolveRepo(m.repo))
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
	case repoResolvedMsg:
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

type repoResolvedMsg struct {
	dir    string
	status string
}
type sessionReadyMsg struct{ rcURL string }

func resolveRepo(repo string) tea.Cmd {
	return func() tea.Msg {
		// Extract repo name from "owner/repo"
		parts := strings.Split(repo, "/")
		repoName := parts[len(parts)-1]
		workspace := "/workspace"

		// Check if repo already exists in workspace
		candidates := []string{
			filepath.Join(workspace, repoName),
			filepath.Join(workspace, repo),
		}

		for _, dir := range candidates {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				// Check if it's a git repo
				if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
					return repoResolvedMsg{dir: dir, status: fmt.Sprintf("Found repo at %s", dir)}
				}
			}
		}

		// Not found — clone it
		cloneDir := filepath.Join(workspace, repoName)
		cloneURL := fmt.Sprintf("https://github.com/%s.git", repo)
		_, err := shell.RunShellTimeout(2*time.Minute,
			fmt.Sprintf(`git clone %s %s`, cloneURL, cloneDir))
		if err != nil {
			return ui.ErrMsg{Err: fmt.Errorf("failed to clone %s: %w", repo, err)}
		}

		return repoResolvedMsg{dir: cloneDir, status: fmt.Sprintf("Cloned %s", repo)}
	}
}

func startSession(name string, workDir string) tea.Cmd {
	return func() tea.Msg {
		rcURL, err := session.StartClaudeSession(name, workDir)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return sessionReadyMsg{rcURL: rcURL}
	}
}
