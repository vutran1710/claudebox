// Command cbx manages Claude Code sessions on the machine it runs on.
//
// It is non-interactive by contract. Its caller is the master Claude session,
// which has a shell but no terminal and no human: nothing here reads stdin, no
// command prompts, the exit code is the result, and output is one
// tab-separated fact per line so it can be cut without parsing a TUI.
//
// It knows nothing about SSH or remote machines. Provisioning a box is
// cbx-setuptool's job.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vutran1710/claudebox/internal/api"
	"github.com/vutran1710/claudebox/internal/cbx"
	"github.com/vutran1710/claudebox/internal/store"
)

var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cbx",
		Short: "Manage Claude Code sessions on this machine",
		Long: `cbx manages Claude Code sessions on the machine it runs on.

It is non-interactive: it never reads stdin, never prompts, and prints one
tab-separated fact per line. The exit code is the result. This is deliberate —
it is driven by the master Claude session, which has no terminal.

It does not know about SSH or other machines. Setting up a box is
cbx-setuptool's job.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newCmd(), lsCmd(), killCmd(), resumeCmd(), exportCmd(), serveCmd(), apiKeyCmd())
	return root
}

// withApp opens the session database and runs fn. Every command needs it, and
// none of them should each remember to close it.
func withApp(fn func(*cbx.App) error) error {
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		return err
	}
	defer st.Close()
	return fn(cbx.New(st))
}

func newCmd() *cobra.Command {
	var repo string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Start a Claude Code session",
		Long: `Starts a detached Claude Code session and enables Remote Control,
printing the URL to open it from a phone.

The name maps to a directory under the workspace root: an existing directory is
used as-is, --repo clones into a new one, and otherwise an empty directory is
created.

Refuses if a session of that name is already running — use resume to get its
URL, or kill it first.`,
		Example: "  cbx new my-app\n  cbx new my-app --repo owner/repo",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return withApp(func(a *cbx.App) error { return a.New(args[0], repo) })
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "Git repo to clone (owner/repo or a full URL)")
	return cmd
}

func lsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List sessions and whether they are running",
		Long: `Lists every recorded session as: name, status, directory, URL.

Status comes from reconciling the database against tmux, so a session that was
started and has since died reports "stopped" rather than disappearing. Sessions
started outside cbx are listed too.`,
		Example: "  cbx ls\n  cbx ls | awk -F'\\t' '$2==\"running\"'",
		Args:    cobra.NoArgs,
		RunE:    func(_ *cobra.Command, _ []string) error { return withApp(func(a *cbx.App) error { return a.List() }) },
	}
}

func killCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "kill <name>",
		Short:   "Stop a session and forget it",
		Long:    "Stops the session and removes its record. Killing a session that is not running succeeds — the intent is that it be gone.",
		Example: "  cbx kill my-app",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return withApp(func(a *cbx.App) error { return a.Kill(args[0]) })
		},
	}
}

func resumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <name>",
		Short: "Print how to reach a running session",
		Long: `Prints a running session's directory, Remote Control URL, and the tmux
command to attach to it.

It does not attach. Attaching needs a terminal and cbx never assumes it has
one; run the printed command yourself if you are at one.`,
		Example: "  cbx resume my-app",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return withApp(func(a *cbx.App) error { return a.Resume(args[0]) })
		},
	}
}

func exportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <skills|rules|db>",
		Short: "Print what this box has, to stdout",
		Long: `Writes part of this box's configuration to stdout so it can be read,
diffed, or saved.

  skills   installed skills, with their descriptions
  rules    CLAUDE.md and settings that shape sessions
  db       the session database, tab-separated

This is the mirror of cbx-setuptool, which pushes config from a laptop to a
box. Export reads out from the box, so the master session can answer what it
has and what it has been doing without anyone opening an SSH connection.`,
		Example: "  cbx export skills\n  cbx export db > sessions.tsv",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return withApp(func(a *cbx.App) error { return a.Export(args[0]) })
		},
	}
}

func serveCmd() *cobra.Command {
	var addr string
	var detach, stop bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP API",
		Long: `Runs the API that drives headless Claude sessions: create one, send it a
prompt, and get the answer back in a single response.

Binds 127.0.0.1 by default. Over plain HTTP a public listener would put the
bearer key and every prompt on the wire in cleartext, so reaching this from
elsewhere is a deliberate act — an ssh port-forward, or a tunnel.

Runs in the foreground so a supervisor can own it: a systemd unit on a box,
PID 1 in a container. --detach is for a machine with neither, and gives up
restart-on-failure and start-on-boot in exchange. Never use it as a
container's CMD; a PID 1 that forks and exits takes the container with it.`,
		Example: "  cbx serve\n  cbx serve --detach\n  cbx serve --stop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case stop:
				if err := api.StopDetached(api.DefaultLockPath(), 5*time.Second); err != nil {
					return err
				}
				fmt.Println("stopped\tok")
				return nil
			case detach:
				if _, running := api.RunningPID(api.DefaultLockPath()); running {
					return api.ErrAlreadyRunning
				}
				pid, err := api.Detach([]string{"serve", "--addr", addr}, api.DefaultLogPath())
				if err != nil {
					return err
				}
				fmt.Printf("pid\t%d\n", pid)
				fmt.Printf("addr\thttp://%s\n", addr)
				fmt.Printf("log\t%s\n", api.DefaultLogPath())
				return nil
			}
			return withApp(func(a *cbx.App) error {
				key, err := api.LoadOrCreateKey(api.DefaultKeyPath())
				if err != nil {
					return err
				}
				srv := api.New(a.Store, key)
				srv.Version = version
				return srv.Serve(cmd.Context(), api.Options{Addr: addr, Out: os.Stdout})
			})
		},
	}
	cmd.Flags().StringVar(&addr, "addr", api.DefaultAddr, "Address to bind")
	cmd.Flags().BoolVar(&detach, "detach", false, "Run in the background (prefer a supervisor where there is one)")
	cmd.Flags().BoolVar(&stop, "stop", false, "Stop a detached server and its query children")
	return cmd
}

func apiKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-key <show|rotate>",
		Short: "Show or replace the API key",
		Long: `Prints the key the API accepts, or issues a new one.

Rotation takes effect immediately: the old key stops working on the next
request, because the reason to rotate is usually that it should already have
stopped working.`,
		Example: "  cbx api-key show\n  cbx api-key rotate",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			switch args[0] {
			case "show":
				key, err := api.LoadOrCreateKey(api.DefaultKeyPath())
				if err != nil {
					return err
				}
				fmt.Printf("key\t%s\n", key)
			case "rotate":
				key, err := api.RotateKey(api.DefaultKeyPath())
				if err != nil {
					return err
				}
				fmt.Printf("key\t%s\n", key)
				if _, running := api.RunningPID(api.DefaultLockPath()); running {
					fmt.Fprintln(os.Stderr, "warning: a server is running with the old key — restart it, or rotate through POST /auth/rotate instead")
				}
			default:
				return fmt.Errorf("unknown api-key command %q (show, rotate)", args[0])
			}
			return nil
		},
	}
	return cmd
}
