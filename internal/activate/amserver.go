package activate

import (
	"fmt"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	AMServerBin   = "/usr/local/bin/am-server"
	AMServiceFile = "/etc/systemd/system/am-server.service"
	AMTunnelFile  = "/etc/systemd/system/am-tunnel.service"
	AMConfigPath  = "/root/.agent-mesh/config.toml"
)

type AMServerInfo struct {
	TunnelURL string
	APIKey    string
}

func StartAMServer() (*AMServerInfo, error) {
	if !shell.FileExists(AMServerBin) {
		return nil, fmt.Errorf("am-server binary not found at %s", AMServerBin)
	}

	serviceUnit := `[Unit]
Description=Agent Mesh Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/am-server
Restart=always
RestartSec=5
Environment=DATA_DIR=/root/.agent-mesh

[Install]
WantedBy=multi-user.target
`
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`printf '%%s' '%s' > %s`, serviceUnit, AMServiceFile))

	tunnelUnit := `[Unit]
Description=Cloudflare tunnel for am-server
After=am-server.service

[Service]
Type=simple
ExecStart=/usr/local/bin/cloudflared tunnel --url http://localhost:8090 --no-tls-verify
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`
	shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`printf '%%s' '%s' > %s`, tunnelUnit, AMTunnelFile))

	shell.RunShellTimeout(5*time.Second, "fuser -k 8090/tcp 2>/dev/null || true")
	time.Sleep(1 * time.Second)

	shell.RunShellTimeout(10*time.Second, "systemctl daemon-reload")
	shell.RunShellTimeout(10*time.Second, "systemctl enable am-server am-tunnel")
	shell.RunShellTimeout(10*time.Second, "systemctl start am-server")
	time.Sleep(2 * time.Second)
	shell.RunShellTimeout(10*time.Second, "systemctl start am-tunnel")

	tunnelURL := ""
	for i := 0; i < 15; i++ {
		time.Sleep(1 * time.Second)
		res, _ := shell.RunShellTimeout(5*time.Second,
			`journalctl -u am-tunnel --no-pager -n 30 | grep -o 'https://[^ ]*\.trycloudflare\.com' | tail -1`)
		url := strings.TrimSpace(res.Stdout)
		if url != "" {
			tunnelURL = url
			break
		}
	}

	apiKey := ReadAPIKey()
	saveAMState(tunnelURL, apiKey)

	return &AMServerInfo{
		TunnelURL: tunnelURL,
		APIKey:    apiKey,
	}, nil
}

func ReadAPIKey() string {
	res, _ := shell.RunShellTimeout(5*time.Second,
		fmt.Sprintf(`grep api_key %s 2>/dev/null | awk -F'"' '{print $2}'`, AMConfigPath))
	return strings.TrimSpace(res.Stdout)
}

func IsAMServerRunning() bool {
	return shell.SystemdActive("am-server")
}

func IsAMTunnelRunning() bool {
	return shell.SystemdActive("am-tunnel")
}

func GetAMTunnelURL() string {
	_, url := loadAMState()
	if url != "" {
		return url
	}
	res, _ := shell.RunShellTimeout(5*time.Second,
		`journalctl -u am-tunnel --no-pager -n 30 | grep -o 'https://[^ ]*\.trycloudflare\.com' | tail -1`)
	return strings.TrimSpace(res.Stdout)
}

const amStateFile = "/root/.cbx-am-state"

func saveAMState(tunnelURL, apiKey string) {
	data := fmt.Sprintf("tunnel_url=%s\napi_key=%s\n", tunnelURL, apiKey)
	shell.RunShellTimeout(5*time.Second, fmt.Sprintf(`printf '%%s' "%s" > %s`, data, amStateFile))
}

func loadAMState() (apiKey, tunnelURL string) {
	res, _ := shell.RunShellTimeout(5*time.Second, fmt.Sprintf("cat %s 2>/dev/null", amStateFile))
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.HasPrefix(line, "api_key=") {
			apiKey = strings.TrimPrefix(line, "api_key=")
		}
		if strings.HasPrefix(line, "tunnel_url=") {
			tunnelURL = strings.TrimPrefix(line, "tunnel_url=")
		}
	}
	return
}
