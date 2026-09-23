// Command cbx-setuptool provisions a ClaudeBox machine from your laptop.
//
// It is the interactive half of the split: a person runs it, watches progress,
// pastes an auth code, and answers questions. It drives a remote box over SSH
// and never runs on the box itself — which is what lets cbx, its counterpart,
// assume it is never interactive and never has a terminal.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vutran1710/claudebox/internal/setuptool"
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
		Use:   "cbx-setuptool",
		Short: "Provision a ClaudeBox machine from your laptop",
		Long: `cbx-setuptool prepares a remote machine to run Claude Code sessions.

It runs on your laptop and drives the box over SSH: installs the tool chain,
signs Claude Code in, authenticates gh/vercel/supabase with tokens, copies your
skills and settings across, and installs cbx so the master session can manage
its own sessions afterwards.

It is interactive by design. Its counterpart, cbx, runs on the box and never
is.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(setupCmd(), authCmd(), migrateCmd(), statusCmd(), apiCmd())
	return root
}

// target resolves the shared --host/--user flags.
func target(host, user string) (setuptool.Target, error) {
	if host == "" {
		return setuptool.Target{}, fmt.Errorf("--host is required")
	}
	return setuptool.NewTarget(host, user)
}

func setupCmd() *cobra.Command {
	var host, user, binary, cbxVersion string
	var skipAuth, skipClaude, withAPI bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install and configure everything on a box",
		Long: `Runs the whole provisioning flow against a box:

  1. install the tool chain (node, gh, vercel, supabase, claude)
  2. install cbx — downloaded from a release, or uploaded with --binary
  3. sign Claude Code in — interactive, you complete it in a browser
  4. authenticate gh / vercel / supabase from tokens
  5. copy your skills, agents and settings across

Each step is skipped if it is already done, so re-running is cheap and safe
after a failure.

Step 3 needs you: the Claude subscription login is a browser OAuth with no
token path. Everything else can be answered from environment variables.`,
		Example: "  cbx-setuptool setup --host 203.0.113.9\n" +
			"  cbx-setuptool setup --host 203.0.113.9 --with-api\n" +
			"  cbx-setuptool setup --host 203.0.113.9 --binary ./cbx-linux",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			t, err := target(host, user)
			if err != nil {
				return err
			}
			return runSetup(t, binary, cbxVersion, skipAuth, skipClaude, withAPI)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "IP or hostname of the box (required)")
	cmd.Flags().StringVar(&user, "user", "root", "SSH user")
	cmd.Flags().StringVar(&binary, "binary", "", "Upload this locally built linux cbx instead of downloading a release (for testing an unreleased build)")
	cmd.Flags().StringVar(&cbxVersion, "cbx-version", "", "Release tag of cbx to install (default: this tool's own version, or the latest release)")
	cmd.Flags().BoolVar(&skipAuth, "skip-auth", false, "Skip the CLI token prompts")
	cmd.Flags().BoolVar(&skipClaude, "skip-claude-login", false, "Install everything but leave Claude Code signed out (sign in later with another setup run)")
	cmd.Flags().BoolVar(&withAPI, "with-api", false, "Install and start the HTTP API as a systemd service")
	return cmd
}

func apiCmd() *cobra.Command {
	var host, user string
	cmd := &cobra.Command{
		Use:   "api <install|key|rotate|forward|expose|url>",
		Short: "Manage the HTTP API on the box",
		Long: `Installs or operates the API server.

  install   write the systemd unit, start it, print the key
  key       print the key the box currently accepts
  rotate    issue a new key and restart the server onto it
  forward   print the ssh command that reaches the API from here
  expose    open a public HTTPS tunnel and print its URL
  url       print the tunnel's current URL

The API binds 127.0.0.1, so reaching it is a deliberate act. forward needs
nothing installed and encrypts the hop, but only from a machine that can ssh
to the box. expose installs a Cloudflare quick tunnel instead, which a phone
or a Claude Project can reach — its hostname changes every time the tunnel
restarts, and url reads the current one.

Either way the bearer key is the only thing standing between the internet and
these sessions.`,
		Example: "  cbx-setuptool api install --host 203.0.113.9\n" +
			"  cbx-setuptool api expose --host 203.0.113.9",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			t, err := target(host, user)
			if err != nil {
				return err
			}
			return runAPI(t, args[0])
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "IP or hostname of the box (required)")
	cmd.Flags().StringVar(&user, "user", "root", "SSH user")
	return cmd
}

func authCmd() *cobra.Command {
	var host, user string
	cmd := &cobra.Command{
		Use:   "auth [tool]",
		Short: "Authenticate a CLI tool on the box with a token",
		Long: `Authenticates gh, vercel or supabase on the box.

Tokens are piped over SSH into the tool's own login rather than passed as
arguments — an argument is visible in the box's process table to every other
user and lands in shell history.

With no tool named, every unauthenticated tool is offered in turn. A token
already exported locally (GH_TOKEN, VERCEL_TOKEN, SUPABASE_ACCESS_TOKEN) is
used without asking.

Claude Code is not here: its subscription login is a browser OAuth with no
token path. Use setup for that.`,
		Example: "  cbx-setuptool auth --host 203.0.113.9\n  cbx-setuptool auth github --host 203.0.113.9",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			t, err := target(host, user)
			if err != nil {
				return err
			}
			only := ""
			if len(args) == 1 {
				only = args[0]
			}
			return runAuth(t, only)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "IP or hostname of the box (required)")
	cmd.Flags().StringVar(&user, "user", "root", "SSH user")
	return cmd
}

func migrateCmd() *cobra.Command {
	var host, user, claudeDir string
	var filter []string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Copy local Claude config to the box",
		Long: `Copies the parts of a Claude configuration directory that shape a session.

--claude-dir names the directory to copy from, defaulting to ~/.claude. It is
a flag rather than a fixed path because a machine may keep more than one, and
the one worth shipping to a box is not always the one Claude Code reads here.

--filter names what to copy, relative to that directory. Entries may be
directories or files. The default is:

  skills, agents, rules, settings.json,
  plugins/installed_plugins.json, plugins/known_marketplaces.json

Not caches, not session transcripts, and not the plugin bundles themselves —
the box re-fetches those from the manifest, a few kilobytes instead of a few
hundred megabytes.

settings.json is rewritten on the way: home paths are remapped and hooks
calling binaries the box lacks are dropped and reported.

Symlinks are followed when they point at a file, so a configuration directory
whose entries link into a dotfiles repository migrates rather than arriving
empty. A symlink to a directory is reported, not followed.`,
		Example: "  cbx-setuptool migrate --host 203.0.113.9\n" +
			"  cbx-setuptool migrate --host 203.0.113.9 --filter skills,rules,agents\n" +
			"  cbx-setuptool migrate --host 203.0.113.9 --claude-dir ~/dotfiles/claude",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			t, err := target(host, user)
			if err != nil {
				return err
			}
			dir, err := expandHome(claudeDir)
			if err != nil {
				return err
			}
			copied, dropped, err := setuptool.MigrateConfig(t, setuptool.MigrateOptions{
				Dir: dir, Only: filter,
			})
			for _, c := range copied {
				fmt.Printf("copied\t%s\t%d\n", c.Path, c.Files)
			}
			for _, d := range dropped {
				fmt.Printf("dropped\t%s\t%s\n", d.Path, d.Reason)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "IP or hostname of the box (required)")
	cmd.Flags().StringVar(&user, "user", "root", "SSH user")
	cmd.Flags().StringVar(&claudeDir, "claude-dir", "", "Local Claude directory to copy from (default ~/.claude)")
	cmd.Flags().StringSliceVar(&filter, "filter", nil, "What to copy, comma-separated (default skills,agents,rules,settings.json,plugins manifest)")
	return cmd
}

// expandHome resolves a leading ~ so --claude-dir ~/x works when a shell has
// not already done it.
func expandHome(path string) (string, error) {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func statusCmd() *cobra.Command {
	var host, user string
	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Report what is installed and authenticated on the box",
		Long:    "Checks each tool and each token login, so you can see what a re-run of setup would actually do.",
		Example: "  cbx-setuptool status --host 203.0.113.9",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			t, err := target(host, user)
			if err != nil {
				return err
			}
			return runStatus(t)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "IP or hostname of the box (required)")
	cmd.Flags().StringVar(&user, "user", "root", "SSH user")
	return cmd
}
