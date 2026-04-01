package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
)

type StepState int

const (
	StepPending StepState = iota
	StepRunning
	StepDone
	StepError
)

type Step struct {
	Name  string
	State StepState
	Error string
}

func RenderStepList(steps []Step, spin spinner.Model) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.State {
		case StepPending:
			fmt.Fprintf(&b, "  %s %s\n", StyleDim.Render("○"), StyleDim.Render(s.Name))
		case StepRunning:
			fmt.Fprintf(&b, "  %s %s\n", StyleSpin.Render(spin.View()), s.Name)
		case StepDone:
			fmt.Fprintf(&b, "  %s %s\n", StyleCheck.Render(), s.Name)
		case StepError:
			fmt.Fprintf(&b, "  %s %s — %s\n", StyleCross.Render(), s.Name, StyleDim.Render(s.Error))
		}
	}
	return b.String()
}

func RenderSummaryBox(title string, items []KV) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", StyleBold.Render(title))
	for _, kv := range items {
		if kv.Indent {
			fmt.Fprintf(&b, "    %s\n", StyleValue.Render(kv.Value))
		} else {
			fmt.Fprintf(&b, "  %s %s\n", StyleLabel.Render(kv.Key), StyleValue.Render(kv.Value))
		}
	}
	return StyleBox.Render(b.String())
}

type KV struct {
	Key    string
	Value  string
	Indent bool
}

func NewSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = StyleSpin
	return s
}

func NewTextInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60
	return ti
}

func StatusLine(name string, ok bool, detail string) string {
	status := StyleCheck.Render()
	if !ok {
		status = StyleCross.Render()
	}
	return fmt.Sprintf("  %s %s %s",
		StyleLabel.Render(name),
		status,
		StyleDim.Render(detail),
	)
}

// Bubble Tea message types
type ToolInstalledMsg struct{ Index int }
type ToolErrorMsg struct {
	Index int
	Err   error
}
type OAuthURLMsg struct{ URL string }
type AuthCompleteMsg struct{ RemoteControlURL string }
type VNCReadyMsg struct {
	URL      string
	Password string
}
type ActivateDoneMsg struct {
	AMURL   string
	AMKey   string
	Pollers []PollerInfo
}
type PollerInfo struct {
	Name     string
	Schedule string
}
type ErrMsg struct{ Err error }

func (e ErrMsg) Error() string { return e.Err.Error() }
