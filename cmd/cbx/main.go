package main

import (
	"fmt"
	"os"

	"github.com/vutran1710/claudebox/internal/activate"
	"github.com/vutran1710/claudebox/internal/code"
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
		withPoller := hasFlag("--poller")
		if err := activate.Run(withPoller); err != nil {
			fmt.Fprintf(os.Stderr, "activate failed: %v\n", err)
			os.Exit(1)
		}
	case "code":
		name := ""
		if len(os.Args) > 2 && os.Args[2] != "" && os.Args[2][0] != '-' {
			name = os.Args[2]
		}
		if err := code.Run(name); err != nil {
			fmt.Fprintf(os.Stderr, "code failed: %v\n", err)
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

func hasFlag(flag string) bool {
	for _, arg := range os.Args[2:] {
		if arg == flag {
			return true
		}
	}
	return false
}

func printUsage() {
	fmt.Println("cbx — ClaudeBox CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  cbx setup                Install tools, authenticate Claude, start VNC")
	fmt.Println("  cbx activate [--poller]  Start am-server, Claude session, optional poller")
	fmt.Println("  cbx code [name]          Spawn a new Claude Code session")
	fmt.Println("  cbx status               Show status of all services")
	fmt.Println("  cbx help                 Show detailed help")
	fmt.Println("  cbx version              Show version")
}

func printHelp() {
	fmt.Println(`cbx — ClaudeBox CLI

REQUIRED (core dev server):

  cbx setup
    Installs all dev tools, authenticates Claude Code, starts VNC + Chrome.
    Run as root: ssh -t root@<host> 'cbx setup'

SERVICES + SESSIONS:

  cbx activate [--poller]
    Starts am-server, configures Chrome Lite MCP, and spawns a Claude Code
    session with remote-control and dangerously-skip-permissions enabled.
    Run as claude user: ssh -t claude@<host> 'cbx activate'

    --poller    Also spawn a second Claude session for message polling

  cbx code [name]
    Spawn a new Claude Code tmux session with remote-control and
    dangerously-skip-permissions. Optional name (default: claude-<timestamp>).
    Run as claude user: ssh -t claude@<host> 'cbx code my-project'

    Prints the remote control URL for mobile access.

OTHER:

  cbx status
    Shows health of all services: Claude Code, VNC, Chrome, am-server,
    Chrome Lite MCP, and active tmux sessions.

  cbx version
    Print the cbx version.

WORKFLOW:

  1. Deploy via GitHub Actions
  2. ssh -t root@<host> 'cbx setup'       # install tools + authenticate
  3. Log into apps via VNC (optional)
  4. ssh -t claude@<host> 'cbx activate'   # start services + Claude session
  5. Access via Remote Control URL or: ssh claude@<host> 'tmux attach -t claude-main'`)
}
