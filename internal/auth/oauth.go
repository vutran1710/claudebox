package auth

import (
	"fmt"
	"strings"
	"time"
)

type OAuthResult struct {
	RemoteControlURL string
}

func NavigateToOAuth() (string, error) {
	if err := startClaudeInTmux(); err != nil {
		return "", fmt.Errorf("failed to start Claude in tmux: %w", err)
	}
	time.Sleep(5 * time.Second)

	for i := 0; i < 15; i++ {
		pane, _ := capturePane()

		switch {
		case strings.Contains(pane, "oauth/authorize"):
			url, err := extractOAuthURL()
			if err != nil {
				return "", err
			}
			return url, nil
		case strings.Contains(pane, "Not logged in"):
			sendKeys("'/login' Enter")
		case strings.Contains(pane, "Choose the text style"):
			sendKeys("Enter")
		case strings.Contains(pane, "Bypass Permissions"):
			sendKeys("Down")
			time.Sleep(500 * time.Millisecond)
			sendKeys("Enter")
		case strings.Contains(pane, "Select login method"):
			sendKeys("Enter")
		case strings.Contains(pane, "Security notes"):
			sendKeys("Enter")
		case strings.Contains(pane, "trust this folder"):
			sendKeys("Enter")
		}
		time.Sleep(3 * time.Second)
	}

	return "", fmt.Errorf("OAuth URL not found after navigating prompts")
}

func SubmitAuthCode(code string) (*OAuthResult, error) {
	sendKeys(fmt.Sprintf("'%s' Enter", code))
	time.Sleep(3 * time.Second)

	for i := 0; i < 10; i++ {
		pane, _ := capturePane()

		switch {
		case strings.Contains(pane, "bypass permissions on"):
			goto loginDone
		case strings.Contains(pane, "Login successful"):
			sendKeys("Enter")
		case strings.Contains(pane, "Security notes"):
			sendKeys("Enter")
		case strings.Contains(pane, "trust this folder"):
			sendKeys("Enter")
		case strings.Contains(pane, "Bypass Permissions"):
			sendKeys("Down")
			time.Sleep(500 * time.Millisecond)
			sendKeys("Enter")
		}
		time.Sleep(3 * time.Second)
	}

loginDone:
	sendKeys("'/remote-control' Enter")
	time.Sleep(3 * time.Second)

	found, _ := waitForPattern("Enable Remote Control", 10*time.Second)
	if found {
		sendKeys("Enter")
		time.Sleep(5 * time.Second)
	}

	rcURL := ""
	waitForPattern(`claude\.(com|ai)/code`, 20*time.Second)
	rcURL = extractRemoteControlURL()

	// Kill the login session — it ran as root, actual sessions should run as claude user
	killSession()

	return &OAuthResult{RemoteControlURL: rcURL}, nil
}
