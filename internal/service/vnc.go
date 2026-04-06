package service

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	DisplayNum      = 99
	VNCPort         = 5900
	NoVNCPort       = 6080
	PasswdFile      = "/root/.vnc_passwd"
	VNCTunnelLog    = "/tmp/cloudflared-vnc.log"
	Resolution      = "1280x800"
	ChromeMCPExtDir = "/opt/chrome-lite-mcp/extension"
	ChromeMCPServer = "/opt/chrome-lite-mcp/server/index.js"
	VNCStateFile    = "/root/.cbx-vnc-state"
)

type VNC struct{}

func NewVNC() *VNC { return &VNC{} }

func (v *VNC) Name() string { return "VNC" }

func (v *VNC) Start() error {
	if v.isFullyRunning() {
		return nil
	}
	v.Stop()

	password := generatePassword()
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`x11vnc -storepasswd "%s" "%s" 2>/dev/null`, password, PasswdFile))

	display := fmt.Sprintf(":%d", DisplayNum)
	tmuxSession := "vnc"
	startScript := "/tmp/cbx-vnc-start.sh"

	shell.RunShellTimeout(5*time.Second, fmt.Sprintf(`tmux kill-session -t %s 2>/dev/null || true`, tmuxSession))

	scriptContent := fmt.Sprintf(`#!/bin/bash
set -m
export PATH="/root/.local/bin:/root/.npm-global/bin:/root/.cargo/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
export HOME="/root"

Xvfb %s -screen 0 %sx24 -ac >/dev/null 2>&1 &
disown
sleep 2

fluxbox -display %s >/dev/null 2>&1 &
disown

x11vnc -display %s -rfbport %d -forever -shared -rfbauth %s >/dev/null 2>&1 &
disown
sleep 2

websockify --web /usr/share/novnc %d localhost:%d >/dev/null 2>&1 &
disown
sleep 1

node %s >/tmp/chrome-mcp.log 2>&1 &
disown
sleep 1

DISPLAY=%s chromium --no-sandbox --disable-gpu --no-first-run --disable-dev-shm-usage --window-size=1280,800 --load-extension=%s >/dev/null 2>&1 &
disown

exec cloudflared tunnel --url http://localhost:%d 2>&1 | tee %s
`, display, Resolution, display, display, VNCPort, PasswdFile, NoVNCPort, VNCPort, ChromeMCPServer, display, ChromeMCPExtDir, NoVNCPort, VNCTunnelLog)

	os.WriteFile(startScript, []byte(scriptContent), 0755)
	shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`tmux new-session -d -s %s 'bash %s'`, tmuxSession, startScript))

	// Wait for tunnel URL
	tunnelURL := ""
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		res, _ := shell.RunShellTimeout(5*time.Second,
			fmt.Sprintf(`grep -o 'https://[^ ]*\.trycloudflare\.com' "%s" 2>/dev/null | tail -1`, VNCTunnelLog))
		url := strings.TrimSpace(res.Stdout)
		if url != "" {
			tunnelURL = url
			break
		}
	}

	saveState(password, tunnelURL)
	return nil
}

func (v *VNC) Stop() {
	shell.RunShellTimeout(10*time.Second, `tmux kill-session -t vnc 2>/dev/null || true`)
	shell.RunShellTimeout(10*time.Second, fmt.Sprintf(`
pkill -f "Xvfb :%d" 2>/dev/null || true
pkill -f "x11vnc" 2>/dev/null || true
pkill -f "fluxbox" 2>/dev/null || true
pkill -f "websockify.*%d" 2>/dev/null || true
pkill -f "cloudflared.*%d" 2>/dev/null || true
`, DisplayNum, NoVNCPort, NoVNCPort))
}

func (v *VNC) Status() Status {
	password, tunnelURL := loadState()
	running := shell.ProcessRunning(fmt.Sprintf("Xvfb :%d", DisplayNum))
	chromeRunning := shell.ProcessRunning("chromium")
	extra := map[string]string{}
	if password != "" {
		extra["password"] = password
	}
	if chromeRunning {
		extra["chrome"] = "running"
	}
	return Status{
		Name:      "VNC",
		Running:   running,
		Port:      NoVNCPort,
		TunnelURL: tunnelURL,
		Extra:     extra,
	}
}

func (v *VNC) isFullyRunning() bool {
	return shell.ProcessRunning(fmt.Sprintf("Xvfb :%d", DisplayNum)) &&
		shell.ProcessRunning("websockify") &&
		shell.ProcessRunning("cloudflared")
}

func generatePassword() string {
	b := make([]byte, 16)
	rand.Read(b)
	s := base64.StdEncoding.EncodeToString(b)
	s = strings.NewReplacer("/", "", "+", "", "=", "").Replace(s)
	if len(s) > 12 {
		s = s[:12]
	}
	return s
}

func saveState(password, tunnelURL string) {
	data := fmt.Sprintf("password=%s\ntunnel_url=%s\n", password, tunnelURL)
	os.WriteFile(VNCStateFile, []byte(data), 0644)
}

func loadState() (password, tunnelURL string) {
	data, _ := os.ReadFile(VNCStateFile)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "password=") {
			password = strings.TrimPrefix(line, "password=")
		}
		if strings.HasPrefix(line, "tunnel_url=") {
			tunnelURL = strings.TrimPrefix(line, "tunnel_url=")
		}
	}
	return
}
