package status

import (
	"fmt"

	"github.com/vutran1710/claudebox/internal/activate"
	"github.com/vutran1710/claudebox/internal/auth"
	"github.com/vutran1710/claudebox/internal/ui"
	"github.com/vutran1710/claudebox/internal/vnc"
)

func Run() {
	fmt.Println()
	fmt.Println(ui.StyleBold.Render("  ClaudeBox Status"))
	fmt.Println()

	loggedIn := auth.IsAlreadyLoggedIn()
	if loggedIn {
		fmt.Println(ui.StatusLine("Claude Code", true, "authenticated"))
	} else {
		fmt.Println(ui.StatusLine("Claude Code", false, "not authenticated"))
	}

	vncRunning := vnc.IsRunning()
	if vncRunning {
		fmt.Println(ui.StatusLine("VNC", true, "running"))
		tunnelURL := vnc.GetTunnelURL()
		if tunnelURL != "" {
			fmt.Printf("                       %s\n", ui.StyleValue.Render(tunnelURL+"/vnc.html"))
		}
		password := vnc.GetPassword()
		if password != "" {
			fmt.Printf("                       %s\n", ui.StyleDim.Render("password: "+password))
		}
	} else {
		fmt.Println(ui.StatusLine("VNC", false, "not running"))
	}

	chromeRunning := vnc.ChromeRunning()
	if chromeRunning {
		fmt.Println(ui.StatusLine("Chrome", true, fmt.Sprintf("running (PID %s)", vnc.ChromePID())))
	} else {
		fmt.Println(ui.StatusLine("Chrome", false, "not running"))
	}

	amRunning := activate.IsAMServerRunning()
	if amRunning {
		fmt.Println(ui.StatusLine("am-server", true, "running (port 8090)"))
		tunnelURL := activate.GetAMTunnelURL()
		if tunnelURL != "" {
			fmt.Printf("                       %s\n", ui.StyleValue.Render(tunnelURL))
		}
		apiKey := activate.ReadAPIKey()
		if apiKey != "" {
			fmt.Printf("                       %s\n", ui.StyleDim.Render("key: "+apiKey))
		}
	} else {
		fmt.Println(ui.StatusLine("am-server", false, "not running"))
	}

	pollers := activate.GetPollerStatus()
	hasPollers := false
	for _, p := range pollers {
		if p.Active {
			hasPollers = true
			break
		}
	}

	fmt.Println()
	fmt.Println("  " + ui.StyleBold.Render("Pollers"))
	if !hasPollers {
		fmt.Println("    " + ui.StyleDim.Render("not activated — run: cbx activate"))
	} else {
		for _, p := range pollers {
			check := ui.StyleCheck.Render()
			if !p.Active {
				check = ui.StyleCross.Render()
			}
			detail := p.Schedule
			if p.LastRun != "" {
				detail += fmt.Sprintf(" (last: %s, %s)", p.LastRun, p.Status)
			} else {
				detail += " (no runs yet)"
			}
			fmt.Printf("    %s %s %s\n",
				ui.StyleLabel.Render(p.Name),
				check,
				ui.StyleDim.Render(detail))
		}
	}

	fmt.Println()
}
