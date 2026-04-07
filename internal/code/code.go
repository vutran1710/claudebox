package code

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vutran1710/claudebox/internal/session"
	"github.com/vutran1710/claudebox/internal/ui"
	"github.com/vutran1710/claudebox/internal/workspace"
)

// RunHeadless runs without TUI.
func RunHeadless(name, repo string) error {
	result, err := workspace.Resolve(name, repo)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", result.Status, result.Dir)

	fmt.Printf("Starting session '%s'...\n", name)
	sess, err := session.NewTmuxManager().Create(name, result.Dir)
	if err != nil {
		return err
	}

	fmt.Printf("Session '%s' started\n", name)
	fmt.Printf("Working dir: %s\n", sess.Dir)
	if sess.RCURL != "" {
		fmt.Printf("Remote Control: %s\n", sess.RCURL)
	}
	fmt.Printf("Attach: tmux attach -t %s\n", name)
	return nil
}

// Run starts the TUI or headless mode.
func Run(name, repo string, headless bool) error {
	if headless {
		return RunHeadless(name, repo)
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

// TUI model

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

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, resolveDir(m.name, m.repo))
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
			fmt.Fprintf(&b, "  %s %s\n", ui.StyleCheck.Render(), m.status)
		}
		fmt.Fprintf(&b, "  %s Starting session '%s'...\n",
			ui.StyleSpin.Render(m.spinner.View()), m.name)
	} else {
		if m.status != "" {
			fmt.Fprintf(&b, "  %s %s\n", ui.StyleCheck.Render(), m.status)
		}
		fmt.Fprintf(&b, "  %s Session '%s' started\n", ui.StyleCheck.Render(), m.name)
		fmt.Fprintf(&b, "  %s Working dir: %s\n", ui.StyleCheck.Render(), m.workDir)
		if m.rcURL != "" {
			fmt.Fprintf(&b, "\n  Remote Control: %s\n", ui.StyleValue.Render(m.rcURL))
		}
		fmt.Fprintf(&b, "\n  Attach: tmux attach -t %s\n", m.name)
	}
	if m.err != nil {
		fmt.Fprintf(&b, "\n  %s %s\n", ui.StyleCross.Render(), m.err.Error())
	}
	return b.String() + "\n"
}

// Tea messages

type dirResolvedMsg struct {
	dir    string
	status string
}
type sessionReadyMsg struct{ rcURL string }

func resolveDir(name, repo string) tea.Cmd {
	return func() tea.Msg {
		result, err := workspace.Resolve(name, repo)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return dirResolvedMsg{dir: result.Dir, status: fmt.Sprintf("%s: %s", result.Status, result.Dir)}
	}
}

func startSession(name, workDir string) tea.Cmd {
	return func() tea.Msg {
		sess, err := session.NewTmuxManager().Create(name, workDir)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return sessionReadyMsg{rcURL: sess.RCURL}
	}
}
