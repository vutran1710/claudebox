package vnc

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
	TunnelLog       = "/tmp/cloudflared-vnc.log"
	Resolution      = "1280x800"
	ChromeMCPExtDir = "/opt/chrome-lite-mcp/extension"
	ChromeMCPServer = "/opt/chrome-lite-mcp/server/index.js"
)

type VNCInfo struct {
	TunnelURL string
	Password  string
}

func IsRunning() bool {
	return shell.ProcessRunning(fmt.Sprintf("Xvfb :%d", DisplayNum))
}

func Stop() {
	shell.RunShellTimeout(10*time.Second, `tmux kill-session -t vnc 2>/dev/null || true`)
	shell.RunShellTimeout(10*time.Second, fmt.Sprintf(`
pkill -f "Xvfb :%d" 2>/dev/null || true
pkill -f "x11vnc" 2>/dev/null || true
pkill -f "fluxbox" 2>/dev/null || true
pkill -f "websockify.*%d" 2>/dev/null || true
pkill -f "cloudflared.*%d" 2>/dev/null || true
`, DisplayNum, NoVNCPort, NoVNCPort))
}

func isFullyRunning() bool {
	return shell.ProcessRunning(fmt.Sprintf("Xvfb :%d", DisplayNum)) &&
		shell.ProcessRunning("websockify") &&
		shell.ProcessRunning("cloudflared")
}

func Start() (*VNCInfo, error) {
	if isFullyRunning() {
		info, err := getExistingInfo()
		if err == nil && info.TunnelURL != "" && info.Password != "" {
			return info, nil
		}
	}
	// Kill stale partial state and start fresh
	Stop()

	password := generatePassword()

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`x11vnc -storepasswd "%s" "%s" 2>/dev/null`, password, PasswdFile))

	display := fmt.Sprintf(":%d", DisplayNum)

	// Write a startup script and launch it in a detached tmux session.
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

node %s >/tmp/chrome-lite-mcp.log 2>&1 &
disown
sleep 1

DISPLAY=%s chromium --no-sandbox --disable-gpu --no-first-run --disable-dev-shm-usage --window-size=1280,800 --load-extension=%s >/dev/null 2>&1 &
disown

exec cloudflared tunnel --url http://localhost:%d 2>&1 | tee %s
`, display, Resolution, display, display, VNCPort, PasswdFile, NoVNCPort, VNCPort, ChromeMCPServer, display, ChromeMCPExtDir, NoVNCPort, TunnelLog)

	os.WriteFile(startScript, []byte(scriptContent), 0755)

	shell.RunShellTimeout(10*time.Second,
		fmt.Sprintf(`tmux new-session -d -s %s 'bash %s'`, tmuxSession, startScript))

	tunnelURL := ""
	for {
		time.Sleep(2 * time.Second)
		res, _ := shell.RunShellTimeout(5*time.Second,
			fmt.Sprintf(`grep -o 'https://[^ ]*\.trycloudflare\.com' "%s" 2>/dev/null | tail -1`, TunnelLog))
		url := strings.TrimSpace(res.Stdout)
		if url != "" {
			tunnelURL = url
			break
		}
	}

	saveVNCState(password, tunnelURL)

	return &VNCInfo{
		TunnelURL: tunnelURL,
		Password:  password,
	}, nil
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

func getExistingInfo() (*VNCInfo, error) {
	password, tunnelURL := loadVNCState()
	if tunnelURL == "" {
		res, _ := shell.RunShellTimeout(5*time.Second,
			fmt.Sprintf(`grep -o 'https://[^ ]*\.trycloudflare\.com' "%s" 2>/dev/null | tail -1`, TunnelLog))
		tunnelURL = strings.TrimSpace(res.Stdout)
	}
	return &VNCInfo{TunnelURL: tunnelURL, Password: password}, nil
}

const stateFile = "/root/.cbx-vnc-state"

func saveVNCState(password, tunnelURL string) {
	data := fmt.Sprintf("password=%s\ntunnel_url=%s\n", password, tunnelURL)
	shell.RunShellTimeout(5*time.Second, fmt.Sprintf(`printf '%%s' "%s" > %s`, data, stateFile))
}

func loadVNCState() (password, tunnelURL string) {
	res, _ := shell.RunShellTimeout(5*time.Second, fmt.Sprintf("cat %s 2>/dev/null", stateFile))
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.HasPrefix(line, "password=") {
			password = strings.TrimPrefix(line, "password=")
		}
		if strings.HasPrefix(line, "tunnel_url=") {
			tunnelURL = strings.TrimPrefix(line, "tunnel_url=")
		}
	}
	return
}

func GetTunnelURL() string {
	_, url := loadVNCState()
	if url != "" {
		return url
	}
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`grep -o 'https://[^ ]*\.trycloudflare\.com' "%s" 2>/dev/null | tail -1`, TunnelLog))
	return strings.TrimSpace(res.Stdout)
}

func GetPassword() string {
	pw, _ := loadVNCState()
	return pw
}

func ChromeRunning() bool {
	return shell.ProcessRunning("chromium")
}

func ChromePID() string {
	res, _ := shell.RunShellTimeout(5*time.Second, `pgrep -f "chromium" | head -1`)
	return strings.TrimSpace(res.Stdout)
}
