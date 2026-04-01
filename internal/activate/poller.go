package activate

import (
	"fmt"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/shell"
	"github.com/vutran1710/claudebox/internal/ui"
)

const (
	PollRunnerPath = "/opt/claudebox/polling/poll-runner.sh"
	PollingDir     = "/opt/claudebox/polling"
)

type Poller struct {
	Name     string
	File     string
	Schedule string
}

var Pollers = []Poller{
	{Name: "discord", File: "check-discord-messages.md", Schedule: "*/5 * * * *"},
	{Name: "gmail-personal", File: "check-gmail-personal.md", Schedule: "1,6,11,16,21,26,31,36,41,46,51,56 * * * *"},
	{Name: "gmail-work", File: "check-gmail-work.md", Schedule: "2,7,12,17,22,27,32,37,42,47,52,57 * * * *"},
	{Name: "zalo", File: "check-zalo-messages.md", Schedule: "3,8,13,18,23,28,33,38,43,48,53,58 * * * *"},
}

func InstallPollers(apiKey string) ([]ui.PollerInfo, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("AM_API_KEY is empty")
	}

	cronEnv := fmt.Sprintf("DISPLAY=:99 AM_API_KEY=%s PATH=/usr/local/share/devbox-tools/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin HOME=/root", apiKey)

	var lines []string
	lines = append(lines, "# ClaudeBox message polling")
	lines = append(lines, cronEnv)
	lines = append(lines, "")

	var infos []ui.PollerInfo
	for _, p := range Pollers {
		logFile := fmt.Sprintf("/var/log/poll-%s.log", p.Name)
		line := fmt.Sprintf("%s %s %s/%s >> %s 2>&1",
			p.Schedule, PollRunnerPath, PollingDir, p.File, logFile)
		lines = append(lines, line)
		infos = append(infos, ui.PollerInfo{Name: p.Name, Schedule: "every 5m"})
	}

	cronJobs := strings.Join(lines, "\n")

	script := fmt.Sprintf(
		`(crontab -l 2>/dev/null | grep -v "ClaudeBox message polling" | grep -v "poll-runner.sh" || true; echo '%s') | crontab -`,
		cronJobs)

	_, err := shell.RunShellTimeout(10*time.Second, script)
	if err != nil {
		return nil, fmt.Errorf("failed to install crontab: %w", err)
	}

	return infos, nil
}

func GetPollerStatus() []PollerStatus {
	var result []PollerStatus
	for _, p := range Pollers {
		logFile := fmt.Sprintf("/var/log/poll-%s.log", p.Name)
		lastRun := ""
		status := "no runs yet"

		res, _ := shell.RunShellTimeout(5*time.Second,
			fmt.Sprintf(`stat -c '%%Y' %s 2>/dev/null`, logFile))
		if ts := strings.TrimSpace(res.Stdout); ts != "" {
			res2, _ := shell.RunShellTimeout(5*time.Second,
				fmt.Sprintf(`python3 -c "import time; print(int(time.time()) - %s)" 2>/dev/null || echo ""`, ts))
			if secs := strings.TrimSpace(res2.Stdout); secs != "" {
				lastRun = formatDuration(secs)
				status = "ok"
			}
		}

		active := false
		cronRes, _ := shell.RunShellTimeout(5*time.Second, "crontab -l 2>/dev/null")
		if strings.Contains(cronRes.Stdout, p.File) {
			active = true
		}

		result = append(result, PollerStatus{
			Name:     p.Name,
			Active:   active,
			LastRun:  lastRun,
			Status:   status,
			Schedule: "every 5m",
		})
	}
	return result
}

type PollerStatus struct {
	Name     string
	Active   bool
	LastRun  string
	Status   string
	Schedule string
}

func formatDuration(secsStr string) string {
	var secs int
	fmt.Sscanf(secsStr, "%d", &secs)
	if secs < 60 {
		return fmt.Sprintf("%ds ago", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm ago", secs/60)
	}
	return fmt.Sprintf("%dh ago", secs/3600)
}
