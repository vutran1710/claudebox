package setup

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/vutran1710/claudebox/internal/auth"
	"github.com/vutran1710/claudebox/internal/provision"
	"github.com/vutran1710/claudebox/internal/serve"
	"github.com/vutran1710/claudebox/internal/service"
	"github.com/vutran1710/claudebox/internal/shell"
	"github.com/vutran1710/claudebox/internal/ui"
	"github.com/vutran1710/claudebox/internal/vnc"
)

type phase int

const (
	phaseCloudInit phase = iota
	phaseTools
	phaseUser
	phaseOAuth
	phaseAuthInput
	phaseAuthSubmit
	phaseVNC
	phaseAMServer
	phaseServe
	phaseDone
)

// Critical tools that must succeed for setup to continue
var criticalTools = map[string]bool{
	"System dependencies": true,
	"Claude Code CLI":     true,
	"Cloudflare Tunnel":   true,
}

type model struct {
	phase      phase
	tools      []ui.Step
	spinner    spinner.Model
	textInput  textinput.Model
	oauthURL   string
	authResult *auth.OAuthResult
	vncInfo   *vnc.VNCInfo
	amStatus  service.Status
	serveURL  string
	err       error
}

func Run() error {
	tools := provision.AllTools()
	steps := make([]ui.Step, len(tools))
	for i, t := range tools {
		steps[i] = ui.Step{Name: t.Name, State: ui.StepPending}
	}

	m := model{
		phase:     phaseCloudInit,
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
	return tea.Batch(m.spinner.Tick, waitForCloudInit())
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

	case cloudInitDoneMsg:
		m.phase = phaseTools
		m.tools[0].State = ui.StepRunning
		return m, installNextTool(0)

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
		toolName := provision.ToolName(msg.Index)
		if criticalTools[toolName] {
			m.err = fmt.Errorf("%s failed: %w", toolName, msg.Err)
			return m, tea.Quit
		}
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
		return m, tea.Batch(
			tea.Println("\n  Open this URL to sign in:"),
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
		m.phase = phaseAMServer
		return m, startAMServer()

	case amServerReadyMsg:
		m.amStatus = msg.status
		m.phase = phaseServe
		return m, startServe()

	case serveReadyMsg:
		m.serveURL = msg.tunnelURL
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
	check := ui.StyleCheck.Render()
	spin := ui.StyleSpin.Render(m.spinner.View())

	b.WriteString(ui.StyleBold.Render("  ClaudeBox Setup") + "\n\n")

	if m.phase == phaseCloudInit {
		fmt.Fprintf(&b, "  %s Waiting for cloud-init to finish...\n", spin)
		return b.String()
	}

	b.WriteString(ui.RenderStepList(m.tools, m.spinner))

	// Progress steps
	steps := []struct {
		label    string
		after    phase // show as done after this phase
		spinning phase // show spinner during this phase
	}{
		{"Login successful", phaseVNC, phaseOAuth},
		{"VNC + Chrome started", phaseAMServer, phaseVNC},
		{"am-server started", phaseServe, phaseAMServer},
		{"API daemon started", phaseDone, phaseServe},
	}

	switch m.phase {
	case phaseUser:
		fmt.Fprintf(&b, "\n  %s Creating claude user...\n", spin)
	case phaseOAuth:
		fmt.Fprintf(&b, "\n  %s Waiting for OAuth URL...\n", spin)
	case phaseAuthInput:
		fmt.Fprintf(&b, "\n  Auth code: %s\n", m.textInput.View())
	case phaseAuthSubmit:
		fmt.Fprintf(&b, "\n  %s Completing login...\n", spin)
	default:
		b.WriteString("\n")
		for _, s := range steps {
			if m.phase >= s.after {
				fmt.Fprintf(&b, "  %s %s\n", check, s.label)
			} else if m.phase == s.spinning {
				fmt.Fprintf(&b, "  %s %s...\n", spin, s.label)
				break
			}
		}
	}

	if m.phase == phaseDone {
		b.WriteString(renderDoneOutput(m))
	}

	if m.err != nil {
		fmt.Fprintf(&b, "\n  %s %s\n", ui.StyleCross.Render(), m.err.Error())
	}

	return b.String()
}

func renderDoneOutput(m model) string {
	var b strings.Builder

	vncURL, vncPass := "", ""
	if m.vncInfo != nil {
		vncURL = m.vncInfo.TunnelURL + "/vnc.html"
		vncPass = m.vncInfo.Password
	}
	serveURL := m.serveURL
	if serveURL == "" {
		serveURL = "http://localhost:8091"
	}
	serveKey := serve.GetAPIKey()
	amURL := m.amStatus.TunnelURL
	if amURL == "" {
		amURL = "http://localhost:8090"
	}
	amKey := ""
	if key, ok := m.amStatus.Extra["api_key"]; ok {
		amKey = key
	}

	data := map[string]string{
		"VNCURL":   vncURL,
		"VNCPass":  vncPass,
		"AMURL":    amURL,
		"AMKey":    amKey,
		"ServeURL": serveURL,
		"ServeKey": serveKey,
	}

	fmt.Fprintf(&b, "\n  VNC:       %s\n", ui.StyleValue.Render(vncURL))
	fmt.Fprintf(&b, "  Password:  %s\n", vncPass)
	if amURL != "" {
		fmt.Fprintf(&b, "  am-server: %s\n", ui.StyleValue.Render(amURL))
	}
	if amKey != "" {
		fmt.Fprintf(&b, "  am-key:    %s\n", amKey)
	}

	b.WriteString(`
  To log into web apps, open VNC URL and sign in to
  Gmail, Discord, Zalo, etc. in Chrome.

  Then create a Claude Project at claude.ai/projects
  and paste the instructions below:

`)
	b.WriteString(ui.StyleDim.Render("─────────────────────────────────────────") + "\n")
	b.WriteString(renderInstructions(data))
	b.WriteString(ui.StyleDim.Render("─────────────────────────────────────────") + "\n")

	return b.String()
}

//go:embed templates/instructions.txt
var instructionsTmpl string

func renderInstructions(data map[string]string) string {
	tmpl, err := template.New("instructions").Parse(instructionsTmpl)
	if err != nil {
		return fmt.Sprintf("template error: %s", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("template error: %s", err)
	}
	return buf.String()
}

// Commands

type cloudInitDoneMsg struct{}
type userCreatedMsg struct{}
type amServerReadyMsg struct{ status service.Status }
type serveReadyMsg struct{ tunnelURL string }
func waitForCloudInit() tea.Cmd {
	return func() tea.Msg {
		shell.RunShellTimeout(5*time.Minute,
			`cloud-init status --wait 2>/dev/null || true`)
		for i := 0; i < 30; i++ {
			res, _ := shell.RunShellTimeout(5*time.Second,
				`fuser /var/lib/dpkg/lock-frontend 2>/dev/null`)
			if res.ExitCode != 0 {
				return cloudInitDoneMsg{}
			}
			time.Sleep(2 * time.Second)
		}
		return cloudInitDoneMsg{}
	}
}

func installNextTool(index int) tea.Cmd {
	return func() tea.Msg {
		tools := provision.AllTools()
		if index >= len(tools) {
			return nil
		}
		t := tools[index]
		if t.Check() {
			return ui.ToolInstalledMsg{Index: index}
		}
		ctx, cancel := context.WithTimeout(context.Background(), provision.DefaultTimeout())
		defer cancel()
		if err := t.Install(ctx); err != nil {
			if !t.Check() {
				return ui.ToolErrorMsg{Index: index, Err: err}
			}
		}
		return ui.ToolInstalledMsg{Index: index}
	}
}

func createUser() tea.Cmd {
	return func() tea.Msg {
		if err := provision.EnsureClaudeUser(); err != nil {
			return ui.ErrMsg{Err: fmt.Errorf("failed to create claude user: %w", err)}
		}
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

func startAMServer() tea.Cmd {
	return func() tea.Msg {
		am := service.NewAMServer()
		if err := am.Start(); err != nil {
			return ui.ErrMsg{Err: fmt.Errorf("am-server: %w", err)}
		}
		return amServerReadyMsg{status: am.Status()}
	}
}

func startServe() tea.Cmd {
	return func() tea.Msg {
		// Start cbx serve as a daemon via nohup
		shell.RunShellTimeout(10*time.Second,
			`nohup cbx serve > /tmp/cbx-serve.log 2>&1 &`)

		// Wait for tunnel URL
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			url := serve.GetTunnelURL()
			if url != "" {
				return serveReadyMsg{tunnelURL: url}
			}
		}
		// No tunnel but server may still be running
		return serveReadyMsg{tunnelURL: ""}
	}
}

