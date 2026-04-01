package vnc

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	DisplayNum = 99
	VNCPort    = 5900
	NoVNCPort  = 6080
	PasswdFile = "/root/.vnc_passwd"
	TunnelLog  = "/tmp/cloudflared-vnc.log"
	Resolution = "1280x800"
)

type VNCInfo struct {
	TunnelURL string
	Password  string
}

func IsRunning() bool {
	return shell.ProcessRunning(fmt.Sprintf("Xvfb :%d", DisplayNum))
}

func Stop() {
	shell.RunShellTimeout(10*time.Second, fmt.Sprintf(`
pkill -f "Xvfb :%d" 2>/dev/null || true
pkill -f "x11vnc" 2>/dev/null || true
pkill -f "fluxbox" 2>/dev/null || true
pkill -f "websockify.*%d" 2>/dev/null || true
pkill -f "cloudflared.*%d" 2>/dev/null || true
`, DisplayNum, NoVNCPort, NoVNCPort))
}

func Start() (*VNCInfo, error) {
	if IsRunning() {
		return getExistingInfo()
	}

	password := generatePassword()

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`x11vnc -storepasswd "%s" "%s" 2>/dev/null`, password, PasswdFile))

	display := fmt.Sprintf(":%d", DisplayNum)

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`Xvfb "%s" -screen 0 "%sx24" -ac > /dev/null 2>&1 &`, display, Resolution))
	time.Sleep(1 * time.Second)

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`fluxbox -display "%s" > /dev/null 2>&1 &`, display))
	time.Sleep(1 * time.Second)

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`x11vnc -display "%s" -rfbport "%d" -forever -shared -rfbauth "%s" > /dev/null 2>&1 &`,
			display, VNCPort, PasswdFile))
	time.Sleep(1 * time.Second)

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`websockify --web /usr/share/novnc "%d" "localhost:%d" > /dev/null 2>&1 &`,
			NoVNCPort, VNCPort))

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`DISPLAY="%s" chromium --no-sandbox --disable-gpu --no-first-run --disable-dev-shm-usage --window-size=1280,800 > /dev/null 2>&1 &`, display))

	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`cloudflared tunnel --url "http://localhost:%d" > "%s" 2>&1 &`, NoVNCPort, TunnelLog))

	tunnelURL := ""
	for i := 0; i < 15; i++ {
		time.Sleep(1 * time.Second)
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
