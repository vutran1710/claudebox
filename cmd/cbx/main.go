package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/vutran1710/claudebox/internal/activate"
	"github.com/vutran1710/claudebox/internal/code"
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
  cbx code my-name -g owner/repo        # custom name, repo dir`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return code.Run(name, repo, project)
		},
	}

	cmd.Flags().StringVarP(&repo, "github", "g", "", "GitHub repo (owner/repo) to clone or find")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Existing project directory in /workspace")
	return cmd
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

