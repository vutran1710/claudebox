package activate

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vutran1710/claudebox/internal/ui"
)

type activatePhase int

const (
	phaseAMServer activatePhase = iota
	phasePollers
	phaseDone
)

type activateModel struct {
	phase   activatePhase
	spinner spinner.Model
	steps   []ui.Step
	amInfo  *AMServerInfo
	pollers []ui.PollerInfo
	err     error
}

func Run() error {
	steps := []ui.Step{
		{Name: "Start am-server", State: ui.StepRunning},
		{Name: "Start Cloudflare tunnel", State: ui.StepPending},
		{Name: "Activate pollers", State: ui.StepPending},
	}
	m := activateModel{
		phase:   phaseAMServer,
		spinner: ui.NewSpinner(),
		steps:   steps,
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
		m.steps[1].State = ui.StepDone
		m.steps[2].State = ui.StepRunning
		m.amInfo = msg.info
		m.phase = phasePollers
		return m, doInstallPollers(msg.info.APIKey)
	case pollersReadyMsg:
		m.steps[2].State = ui.StepDone
		m.pollers = msg.pollers
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
	if m.phase == phaseDone && m.amInfo != nil {
		items := []ui.KV{
			{Key: "URL", Value: m.amInfo.TunnelURL},
			{Key: "Key", Value: m.amInfo.APIKey},
		}
		b.WriteString("\n" + ui.RenderSummaryBox("AM Server", items))
		b.WriteString("\n\n  " + ui.StyleBold.Render("Usage") + "\n")
		b.WriteString(fmt.Sprintf("    curl %s/healthz\n", m.amInfo.TunnelURL))
		b.WriteString(fmt.Sprintf("    curl -H \"X-API-Key: %s\" %s/api/messages\n", m.amInfo.APIKey, m.amInfo.TunnelURL))
		b.WriteString(fmt.Sprintf("    curl -H \"X-API-Key: %s\" \"%s/api/messages?q=meeting\"\n", m.amInfo.APIKey, m.amInfo.TunnelURL))
		b.WriteString(fmt.Sprintf("    curl -H \"X-API-Key: %s\" %s/api/stats\n", m.amInfo.APIKey, m.amInfo.TunnelURL))
		b.WriteString("\n  " + ui.StyleBold.Render("Pollers") + "\n")
		for _, p := range m.pollers {
			b.WriteString(fmt.Sprintf("    %s %s %s\n",
				ui.StyleCheck.Render(), ui.StyleLabel.Render(p.Name), ui.StyleDim.Render(p.Schedule)))
		}
	}
	if m.err != nil {
		b.WriteString(fmt.Sprintf("\n  %s %s\n", ui.StyleCross.Render(), m.err.Error()))
	}
	return b.String() + "\n"
}

type amServerReadyMsg struct{ info *AMServerInfo }
type pollersReadyMsg struct{ pollers []ui.PollerInfo }

func doStartAMServer() tea.Cmd {
	return func() tea.Msg {
		info, err := StartAMServer()
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return amServerReadyMsg{info: info}
	}
}

func doInstallPollers(apiKey string) tea.Cmd {
	return func() tea.Msg {
		pollers, err := InstallPollers(apiKey)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return pollersReadyMsg{pollers: pollers}
	}
}
