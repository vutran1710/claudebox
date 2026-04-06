package service

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
)

const (
	AMServerBin      = "/usr/local/bin/am-server"
	AMServerPort     = 8090
	AMConfigFile     = "/root/.agent-mesh/config.toml"
	AMTunnelLog      = "/tmp/cloudflared-am.log"
	AMStateFile      = "/root/.cbx-am-state"
	AMSystemdService = "am-server"
	AMTunnelService  = "am-tunnel"
)

type AMServer struct{}

func NewAMServer() *AMServer { return &AMServer{} }

func (a *AMServer) Name() string { return "am-server" }

func (a *AMServer) Start() error {
	if !shell.FileExists(AMServerBin) {
		return fmt.Errorf("am-server binary not found at %s", AMServerBin)
	}

	// Create systemd service
	unit := fmt.Sprintf(`[Unit]
Description=AM Server
After=network.target

[Service]
ExecStart=%s
Restart=always
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
`, AMServerBin)
	os.WriteFile(fmt.Sprintf("/etc/systemd/system/%s.service", AMSystemdService), []byte(unit), 0644)

	shell.RunShellTimeout(10*time.Second, fmt.Sprintf(
		`systemctl daemon-reload && systemctl enable %s && systemctl start %s`,
		AMSystemdService, AMSystemdService))

	// Wait for it to start
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		if shell.SystemdActive(AMSystemdService) {
			break
		}
	}

	// Start tunnel
	tunnelUnit := fmt.Sprintf(`[Unit]
Description=AM Server Tunnel
After=%s.service
Requires=%s.service

[Service]
ExecStart=/usr/local/bin/cloudflared tunnel --no-tls-verify --url http://localhost:%d
Restart=always
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=multi-user.target
`, AMSystemdService, AMSystemdService, AMServerPort, AMTunnelLog, AMTunnelLog)
	os.WriteFile(fmt.Sprintf("/etc/systemd/system/%s.service", AMTunnelService), []byte(tunnelUnit), 0644)

	shell.RunShellTimeout(10*time.Second, fmt.Sprintf(
		`systemctl daemon-reload && systemctl enable %s && systemctl start %s`,
		AMTunnelService, AMTunnelService))

	// Wait for tunnel URL
	tunnelURL := ""
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		res, _ := shell.RunShellTimeout(5*time.Second,
			fmt.Sprintf(`grep -o 'https://[^ ]*\.trycloudflare\.com' "%s" 2>/dev/null | tail -1`, AMTunnelLog))
		url := strings.TrimSpace(res.Stdout)
		if url != "" {
			tunnelURL = url
			break
		}
	}

	apiKey := a.readAPIKey()
	saveAMState(tunnelURL, apiKey)
	return nil
}

func (a *AMServer) Stop() {
	shell.RunShellTimeout(10*time.Second, fmt.Sprintf(
		`systemctl stop %s %s 2>/dev/null || true`, AMTunnelService, AMSystemdService))
}

func (a *AMServer) Status() Status {
	running := shell.SystemdActive(AMSystemdService)
	tunnelURL, apiKey := loadAMState()
	extra := map[string]string{}
	if apiKey != "" {
		extra["api_key"] = apiKey
	}
	return Status{
		Name:      "am-server",
		Running:   running,
		Port:      AMServerPort,
		TunnelURL: tunnelURL,
		Extra:     extra,
	}
}

func (a *AMServer) readAPIKey() string {
	data, _ := os.ReadFile(AMConfigFile)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "api_key") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), `"`)
			}
		}
	}
	return ""
}

func saveAMState(tunnelURL, apiKey string) {
	data := fmt.Sprintf("tunnel_url=%s\napi_key=%s\n", tunnelURL, apiKey)
	os.WriteFile(AMStateFile, []byte(data), 0644)
}

func loadAMState() (tunnelURL, apiKey string) {
	data, _ := os.ReadFile(AMStateFile)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "tunnel_url=") {
			tunnelURL = strings.TrimPrefix(line, "tunnel_url=")
		}
		if strings.HasPrefix(line, "api_key=") {
			apiKey = strings.TrimPrefix(line, "api_key=")
		}
	}
	return
}
