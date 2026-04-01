package setup

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vutran1710/claudebox/internal/auth"
	"github.com/vutran1710/claudebox/internal/ui"
	"github.com/vutran1710/claudebox/internal/vnc"
)

type phase int

const (
	phaseTools phase = iota
	phaseUser
	phaseOAuth
	phaseAuthInput
	phaseAuthSubmit
	phaseVNC
	phaseDone
)

type model struct {
	phase      phase
	tools      []ui.Step
	spinner    spinner.Model
	textInput  textinput.Model
	oauthURL   string
	authResult *auth.OAuthResult
	vncInfo    *vnc.VNCInfo
	err        error
}

func Run() error {
	tools := AllTools()
	steps := make([]ui.Step, len(tools))
	for i, t := range tools {
		steps[i] = ui.Step{Name: t.Name, State: ui.StepPending}
	}
	steps[0].State = ui.StepRunning

	m := model{
		phase:     phaseTools,
		tools:     steps,
		spinner:   ui.NewSpinner(),
		textInput: ui.NewTextInput("Paste auth code here..."),
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, installNextTool(0))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.phase == phaseAuthInput && m.textInput.Value() != "" {
				m.phase = phaseAuthSubmit
				code := m.textInput.Value()
				return m, submitAuthCode(code)
			}
		}
		if m.phase == phaseAuthInput {
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case ui.ToolInstalledMsg:
		m.tools[msg.Index].State = ui.StepDone
		next := msg.Index + 1
		if next < len(m.tools) {
			m.tools[next].State = ui.StepRunning
			return m, installNextTool(next)
		}
		m.phase = phaseUser
		return m, createUser()

	case ui.ToolErrorMsg:
		m.tools[msg.Index].State = ui.StepError
		m.tools[msg.Index].Error = msg.Err.Error()
		next := msg.Index + 1
		if next < len(m.tools) {
			m.tools[next].State = ui.StepRunning
			return m, installNextTool(next)
		}
		m.phase = phaseUser
		return m, createUser()

	case userCreatedMsg:
		m.phase = phaseOAuth
		return m, startOAuth()

	case ui.OAuthURLMsg:
		m.oauthURL = msg.URL
		m.phase = phaseAuthInput
		m.textInput.Focus()
		// Print URL above the TUI viewport — persists and won't be truncated
		return m, tea.Batch(
			tea.Println("\n  Open this URL to sign in:\n"),
			tea.Println(msg.URL),
			tea.Println(""),
			textinput.Blink,
		)

	case ui.AuthCompleteMsg:
		m.authResult = &auth.OAuthResult{RemoteControlURL: msg.RemoteControlURL}
		m.phase = phaseVNC
		return m, startVNC()

	case ui.VNCReadyMsg:
		m.vncInfo = &vnc.VNCInfo{TunnelURL: msg.URL, Password: msg.Password}
		m.phase = phaseDone
		return m, tea.Quit

	case ui.ErrMsg:
		m.err = msg.Err
		return m, tea.Quit
	}

	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	b.WriteString(ui.StyleBold.Render("  ClaudeBox Setup") + "\n\n")

	b.WriteString(ui.RenderStepList(m.tools, m.spinner))

	switch m.phase {
	case phaseUser:
		b.WriteString(fmt.Sprintf("\n  %s Creating claude user...\n", ui.StyleSpin.Render(m.spinner.View())))
	case phaseOAuth:
		b.WriteString(fmt.Sprintf("\n  %s Waiting for OAuth URL...\n", ui.StyleSpin.Render(m.spinner.View())))
	case phaseAuthInput:
		// URL is printed above via tea.Println — just show the input here
		b.WriteString(fmt.Sprintf("\n  Auth code: %s\n", m.textInput.View()))
	case phaseAuthSubmit:
		b.WriteString(fmt.Sprintf("\n  %s Completing login...\n", ui.StyleSpin.Render(m.spinner.View())))
	case phaseVNC:
		b.WriteString(fmt.Sprintf("\n  %s %s\n", ui.StyleCheck.Render(), "Login successful"))
		b.WriteString(fmt.Sprintf("  %s Starting VNC + Chrome...\n", ui.StyleSpin.Render(m.spinner.View())))
	case phaseDone:
		b.WriteString(fmt.Sprintf("\n  %s Login successful\n", ui.StyleCheck.Render()))
		b.WriteString(fmt.Sprintf("  %s VNC + Chrome started\n\n", ui.StyleCheck.Render()))

		if m.authResult != nil && m.authResult.RemoteControlURL != "" {
			b.WriteString(fmt.Sprintf("  Remote Control: %s\n", ui.StyleValue.Render(m.authResult.RemoteControlURL)))
		}
		if m.vncInfo != nil && m.vncInfo.TunnelURL != "" {
			b.WriteString(fmt.Sprintf("  VNC:      %s\n", ui.StyleValue.Render(m.vncInfo.TunnelURL+"/vnc.html")))
			b.WriteString(fmt.Sprintf("  Password: %s\n", m.vncInfo.Password))
		}
		b.WriteString("\n  Next steps:\n")
		b.WriteString("    1. Open VNC URL in your browser\n")
		b.WriteString("    2. Install Claude-in-Chrome extension\n")
		b.WriteString("    3. Log into Discord, Gmail, Zalo\n")
		b.WriteString("    4. Run: cbx activate\n")
	}

	if m.err != nil {
		b.WriteString(fmt.Sprintf("\n  %s %s\n", ui.StyleCross.Render(), m.err.Error()))
	}

	return b.String()
}

// Commands

type userCreatedMsg struct{}

func installNextTool(index int) tea.Cmd {
	return func() tea.Msg {
		tools := AllTools()
		if index >= len(tools) {
			return nil
		}
		t := tools[index]
		if t.Check() {
			return ui.ToolInstalledMsg{Index: index}
		}
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
		defer cancel()
		if err := t.Install(ctx); err != nil {
			return ui.ToolErrorMsg{Index: index, Err: err}
		}
		return ui.ToolInstalledMsg{Index: index}
	}
}

func createUser() tea.Cmd {
	return func() tea.Msg {
		auth.EnsureClaudeUser()
		return userCreatedMsg{}
	}
}

func startOAuth() tea.Cmd {
	return func() tea.Msg {
		if auth.IsAlreadyLoggedIn() {
			return ui.AuthCompleteMsg{}
		}
		url, err := auth.NavigateToOAuth()
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return ui.OAuthURLMsg{URL: url}
	}
}

func submitAuthCode(code string) tea.Cmd {
	return func() tea.Msg {
		result, err := auth.SubmitAuthCode(code)
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		rcURL := ""
		if result != nil {
			rcURL = result.RemoteControlURL
		}
		return ui.AuthCompleteMsg{RemoteControlURL: rcURL}
	}
}

func startVNC() tea.Cmd {
	return func() tea.Msg {
		info, err := vnc.Start()
		if err != nil {
			return ui.ErrMsg{Err: err}
		}
		return ui.VNCReadyMsg{URL: info.TunnelURL, Password: info.Password}
	}
}
