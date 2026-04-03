package code

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vutran1710/claudebox/internal/session"
	"github.com/vutran1710/claudebox/internal/ui"
)

type model struct {
	spinner spinner.Model
	name    string
	rcURL   string
	done    bool
	err     error
}

func Run(name string) error {
	if name == "" {
		name = fmt.Sprintf("claude-%d", time.Now().Unix())
	}
	m := model{
		spinner: ui.NewSpinner(),
		name:    name,
	}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, startSession(m.name))
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
		b.WriteString(fmt.Sprintf("  %s Starting Claude Code session '%s'...\n",
			ui.StyleSpin.Render(m.spinner.View()), m.name))
	} else {
		b.WriteString(fmt.Sprintf("  %s Session '%s' started\n\n", ui.StyleCheck.Render(), m.name))
		if m.rcURL != "" {
			b.WriteString(fmt.Sprintf("  Remote Control: %s\n", ui.StyleValue.Render(m.rcURL)))
		}
		b.WriteString(fmt.Sprintf("\n  Attach: tmux attach -t %s\n", m.name))
	}
	if m.err != nil {
		b.WriteString(fmt.Sprintf("\n  %s %s\n", ui.StyleCross.Render(), m.err.Error()))
	}
	return b.String() + "\n"
}

type sessionReadyMsg struct{ rcURL string }

func startSession(name string) tea.Cmd {
	return func() tea.Msg {
		rcURL, err := session.StartClaudeSession(name)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return sessionReadyMsg{rcURL: rcURL}
	}
}
