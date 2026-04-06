package status

import (
	"fmt"

	"github.com/vutran1710/claudebox/internal/activate"
	"github.com/vutran1710/claudebox/internal/auth"
	"github.com/vutran1710/claudebox/internal/serve"
	"github.com/vutran1710/claudebox/internal/service"
	"github.com/vutran1710/claudebox/internal/session"
	"github.com/vutran1710/claudebox/internal/ui"
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

	// VNC
	vnc := service.NewVNC()
	vncStatus := vnc.Status()
	if vncStatus.Running {
		fmt.Println(ui.StatusLine("VNC", true, "running"))
		if vncStatus.TunnelURL != "" {
			fmt.Printf("                       %s\n", ui.StyleValue.Render(vncStatus.TunnelURL+"/vnc.html"))
		}
		if pw, ok := vncStatus.Extra["password"]; ok {
			fmt.Printf("                       %s\n", ui.StyleDim.Render("password: "+pw))
		}
	} else {
		fmt.Println(ui.StatusLine("VNC", false, "not running"))
	}

	// Chrome
	if chrome, ok := vncStatus.Extra["chrome"]; ok && chrome == "running" {
		fmt.Println(ui.StatusLine("Chrome", true, "running"))
	} else {
		fmt.Println(ui.StatusLine("Chrome", false, "not running"))
	}

	// am-server
	am := service.NewAMServer()
	amStatus := am.Status()
	if amStatus.Running {
		fmt.Println(ui.StatusLine("am-server", true, fmt.Sprintf("running (port %d)", amStatus.Port)))
		if amStatus.TunnelURL != "" {
			fmt.Printf("                       %s\n", ui.StyleValue.Render(amStatus.TunnelURL))
		}
		if key, ok := amStatus.Extra["api_key"]; ok {
			fmt.Printf("                       %s\n", ui.StyleDim.Render("key: "+key))
		}
	} else {
		fmt.Println(ui.StatusLine("am-server", false, "not running"))
	}

	// Chrome Lite MCP
	chromeMCP := activate.IsChromeMCPConfigured()
	if chromeMCP {
		fmt.Println(ui.StatusLine("Chrome Lite MCP", true, "configured"))
	} else {
		fmt.Println(ui.StatusLine("Chrome Lite MCP", false, "not configured"))
	}

	// API Daemon
	serveRunning := serve.IsRunning()
	if serveRunning {
		fmt.Println(ui.StatusLine("API Daemon", true, fmt.Sprintf("running (port %d)", serve.GetPort())))
	} else {
		fmt.Println(ui.StatusLine("API Daemon", false, "not running"))
	}

	// Sessions
	fmt.Println()
	fmt.Println("  " + ui.StyleBold.Render("Sessions"))
	mgr := session.NewTmuxManager()
	sessions, _ := mgr.List()
	if len(sessions) == 0 {
		fmt.Println("    " + ui.StyleDim.Render("no active sessions"))
	} else {
		for _, s := range sessions {
			fmt.Printf("    %s %s (%s)\n", ui.StyleCheck.Render(), s.Name, s.Status)
		}
	}
	fmt.Println()
}
