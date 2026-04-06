package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/vutran1710/claudebox/internal/activate"
	"github.com/vutran1710/claudebox/internal/code"
	"github.com/vutran1710/claudebox/internal/serve"
	"github.com/vutran1710/claudebox/internal/setup"
	"github.com/vutran1710/claudebox/internal/status"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "cbx",
		Short:   "ClaudeBox CLI",
		Version: version,
	}

	root.AddCommand(setupCmd())
	root.AddCommand(activateCmd())
	root.AddCommand(codeCmd())
	root.AddCommand(serveCmd())
	root.AddCommand(showCmd())
	root.AddCommand(statusCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Install tools, authenticate Claude, start VNC",
		Long: `Installs all dev tools, authenticates Claude Code via OAuth,
and starts VNC + Chrome. Run as root.

  ssh -t root@<host> 'cbx setup'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Run()
		},
	}
}

func activateCmd() *cobra.Command {
	var withPoller bool

	cmd := &cobra.Command{
		Use:   "activate",
		Short: "Start am-server, Claude session, optional poller",
		Long: `Starts am-server, configures Chrome Lite MCP, and spawns a Claude Code
session with remote-control and dangerously-skip-permissions enabled.
Run as claude user.

  ssh -t claude@<host> 'cbx activate'
  ssh -t claude@<host> 'cbx activate --poller'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return activate.Run(withPoller)
		},
	}

	cmd.Flags().BoolVar(&withPoller, "poller", false, "Also spawn a message polling session")
	return cmd
}

func codeCmd() *cobra.Command {
	var repo string
	var project string
	var headless bool

	cmd := &cobra.Command{
		Use:   "code [name]",
		Short: "Spawn a new Claude Code session",
		Long: `Spawn a new Claude Code tmux session with remote-control and
dangerously-skip-permissions enabled. Prints the remote control URL.

Examples:
  cbx code                              # session in /workspace
  cbx code my-project                   # named session in /workspace
  cbx code -g vutran1710/claudebox      # clone/find repo, session named "claudebox"
  cbx code -p my-app                    # open existing project in /workspace/my-app
  cbx code --headless -g owner/repo     # non-interactive (for use from another Claude session)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return code.Run(name, repo, project, headless)
		},
	}

	cmd.Flags().StringVarP(&repo, "github", "g", "", "GitHub repo (owner/repo) to clone or find")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Existing project directory in /workspace")
	cmd.Flags().BoolVar(&headless, "headless", false, "Non-interactive mode (no TUI)")
	return cmd
}

func serveCmd() *cobra.Command {
	var port int
	var stop bool
	var daemon bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the session management API daemon",
		Long: `Runs an HTTP API for managing Claude Code sessions.

  cbx serve                  # start in foreground
  cbx serve -d               # start as background daemon
  cbx serve --stop           # stop the daemon

API endpoints:
  POST   /sessions           Create session { name, github, project }
  GET    /sessions           List sessions
  DELETE /sessions/{name}    Kill session
  GET    /health             Health check`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if stop {
				if err := serve.Stop(); err != nil {
					return err
				}
				fmt.Println("cbx serve stopped")
				return nil
			}
			if daemon {
				return startDaemon(port)
			}
			return serve.New(port).Start()
		},
	}

	cmd.Flags().IntVarP(&port, "port", "P", serve.DefaultPort, "Port to listen on")
	cmd.Flags().BoolVar(&stop, "stop", false, "Stop the daemon")
	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "Run as background daemon")
	return cmd
}

func startDaemon(port int) error {
	// Re-exec ourselves in the background
	exe, _ := os.Executable()
	args := []string{exe, "serve", "--port", fmt.Sprintf("%d", port)}

	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}

	proc, err := os.StartProcess(exe, args, attr)
	if err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	proc.Release()
	fmt.Printf("cbx serve started (port %d)\n", port)
	return nil
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [item]",
		Short: "Show configuration values",
		Long: `Show configuration values.

  cbx show api-key           Show the API key for cbx serve`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "api-key":
				key := serve.GetAPIKey()
				if key == "" {
					return fmt.Errorf("no API key found — run 'cbx serve' first")
				}
				fmt.Println(key)
				return nil
			default:
				return fmt.Errorf("unknown item: %s (available: api-key)", args[0])
			}
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show status of all services",
		Run: func(cmd *cobra.Command, args []string) {
			status.Run()
		},
	}
}
