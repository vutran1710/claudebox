package main

import (
	"fmt"
	"os"

	"github.com/vutran1710/claudebox/internal/activate"
	"github.com/vutran1710/claudebox/internal/setup"
	"github.com/vutran1710/claudebox/internal/status"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "setup":
		if err := setup.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
			os.Exit(1)
		}
	case "activate":
		if err := activate.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "activate failed: %v\n", err)
			os.Exit(1)
		}
	case "status":
		status.Run()
	case "help", "--help", "-h":
		printHelp()
	case "version", "--version", "-v":
		fmt.Printf("cbx %s\n", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("cbx — ClaudeBox CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  cbx setup      Install tools, authenticate Claude, start VNC")
	fmt.Println("  cbx activate   Start am-server and configure Chrome Lite MCP (optional)")
	fmt.Println("  cbx status     Show status of all services")
	fmt.Println("  cbx help       Show detailed help")
	fmt.Println("  cbx version    Show version")
}

func printHelp() {
	fmt.Println(`cbx — ClaudeBox CLI

REQUIRED (core dev server):

  cbx setup
    Installs all dev tools, authenticates Claude Code, starts VNC + Chrome.
    After this, you can SSH in and start coding immediately.

    Installed tools: Claude Code CLI, Node.js, Python, Rust, Go, Docker,
    GitHub CLI, Vercel CLI, Playwright, Chromium, Cloudflared, and more.

    This is all you need for a remote dev environment.

OPTIONAL (message polling):

  cbx activate
    Starts am-server (message aggregation) and configures Chrome Lite MCP
    for Claude Code. Only needed if you want Claude to read messages from
    Gmail, Discord, Zalo, Messenger, or Slack.

    Before running:
      1. Open the VNC URL from 'cbx setup' output
      2. Log into your messaging apps in Chrome
      3. Then run 'cbx activate'

    After activating, Claude Code can:
      - Read messages from your apps via Chrome Lite MCP
      - Push structured messages to am-server
      - Reply to messages when you ask

OTHER:

  cbx status
    Shows health of all services: Claude Code, VNC, Chrome, am-server,
    and Chrome Lite MCP configuration.

  cbx version
    Print the cbx version.`)
}
