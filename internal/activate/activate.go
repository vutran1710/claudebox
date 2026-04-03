package activate

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vutran1710/claudebox/internal/session"
	"github.com/vutran1710/claudebox/internal/ui"
)

type activatePhase int

const (
	phaseAMServer activatePhase = iota
	phaseChromeMCP
	phaseClaudeSession
	phasePollerSession
	phaseDone
)

type activateModel struct {
	phase          activatePhase
	spinner        spinner.Model
	steps          []ui.Step
	amInfo         *AMServerInfo
	rcURL          string
	pollerEnabled  bool
	err            error
}

func Run(withPoller bool) error {
	steps := []ui.Step{
		{Name: "Start am-server + tunnel", State: ui.StepRunning},
		{Name: "Configure Chrome Lite MCP", State: ui.StepPending},
		{Name: "Start Claude Code session", State: ui.StepPending},
	}
	if withPoller {
		steps = append(steps, ui.Step{Name: "Start message poller session", State: ui.StepPending})
	}
	m := activateModel{
		phase:         phaseAMServer,
		spinner:       ui.NewSpinner(),
		steps:         steps,
		pollerEnabled: withPoller,
	}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func (m activateModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, doStartAMServer())
}

func (m activateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case amServerReadyMsg:
		m.steps[0].State = ui.StepDone
		m.steps[1].State = ui.StepRunning
		m.amInfo = msg.info
		m.phase = phaseChromeMCP
		return m, doConfigureChromeMCP()
	case chromeMCPReadyMsg:
		m.steps[1].State = ui.StepDone
		m.steps[2].State = ui.StepRunning
		m.phase = phaseClaudeSession
		return m, doStartClaudeSession()
	case claudeSessionReadyMsg:
		m.steps[2].State = ui.StepDone
		m.rcURL = msg.rcURL
		if m.pollerEnabled {
			m.steps[3].State = ui.StepRunning
			m.phase = phasePollerSession
			return m, doStartPollerSession()
		}
		m.phase = phaseDone
		return m, tea.Quit
	case pollerSessionReadyMsg:
		m.steps[3].State = ui.StepDone
		m.phase = phaseDone
		return m, tea.Quit
	case ui.ErrMsg:
		m.err = msg.Err
		return m, tea.Quit
	}
	return m, nil
}

func (m activateModel) View() string {
	var b strings.Builder
	b.WriteString(ui.StyleBold.Render("  ClaudeBox Activate") + "\n\n")
	b.WriteString(ui.RenderStepList(m.steps, m.spinner))
	if m.phase == phaseDone {
		if m.amInfo != nil {
			items := []ui.KV{
				{Key: "URL", Value: m.amInfo.TunnelURL},
				{Key: "Key", Value: m.amInfo.APIKey},
			}
			b.WriteString("\n" + ui.RenderSummaryBox("AM Server", items))
		}
		if m.rcURL != "" {
			b.WriteString("\n" + ui.RenderSummaryBox("Claude Code", []ui.KV{
				{Key: "Remote Control", Value: m.rcURL},
				{Key: "Session", Value: "claude-main"},
			}))
		}
		b.WriteString("\n  " + ui.StyleBold.Render("Attach to session:") + "\n")
		b.WriteString("    tmux attach -t claude-main\n")
		if m.pollerEnabled {
			b.WriteString("    tmux attach -t claude-poller\n")
		}
	}
	if m.err != nil {
		b.WriteString(fmt.Sprintf("\n  %s %s\n", ui.StyleCross.Render(), m.err.Error()))
	}
	return b.String() + "\n"
}

type amServerReadyMsg struct{ info *AMServerInfo }
type chromeMCPReadyMsg struct{}
type claudeSessionReadyMsg struct{ rcURL string }
type pollerSessionReadyMsg struct{}

func doStartAMServer() tea.Cmd {
	return func() tea.Msg {
		info, err := StartAMServer()
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return amServerReadyMsg{info: info}
	}
}

func doConfigureChromeMCP() tea.Cmd {
	return func() tea.Msg {
		RemoveOldPollers()
		if err := ConfigureChromeMCP(); err != nil {
			return ui.ErrMsg{Err: err}
		}
		return chromeMCPReadyMsg{}
	}
}

func doStartClaudeSession() tea.Cmd {
	return func() tea.Msg {
		rcURL, err := session.StartClaudeSession("claude-main")
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return claudeSessionReadyMsg{rcURL: rcURL}
	}
}

func doStartPollerSession() tea.Cmd {
	return func() tea.Msg {
		// Start a second Claude session for message polling
		_, err := session.StartClaudeSession("claude-poller")
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return pollerSessionReadyMsg{}
	}
}
