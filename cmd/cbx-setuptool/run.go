package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/setuptool"
)

// This file is the interactive layer. Progress is printed as it happens rather
// than rendered in a full-screen TUI, because setup hands the terminal to a
// nested Claude Code for the login — and a Bubble Tea program that owns the
// screen has to be torn down and rebuilt around that, which is a great deal of
// machinery for a flow a person watches once per box.

const (
	tick  = "✓"
	skip  = "·"
	cross = "✗"
)

func step(status, name, detail string) {
	if detail != "" {
		fmt.Printf("  %s %-18s %s\n", status, name, detail)
		return
	}
	fmt.Printf("  %s %s\n", status, name)
}

func runSetup(t setuptool.Target, binary, cbxVersion, claudePath string, with []string, skipAuth, skipClaude, withAPI bool) error {
	steps, err := setuptool.Select(setuptool.InstallSteps(), with)
	if err != nil {
		return err
	}
	// Before the first step, not on the eighth: provisioning writes outside
	// the user's home, and a box that half-provisions is worse than one that
	// refused to start.
	if err := setuptool.CanEscalate(t); err != nil {
		return err
	}
	fmt.Printf("\nProvisioning %s\n\n", t)

	fmt.Println("Tools")
	for _, s := range steps {
		skipped, err := s.Run(t)
		switch {
		case err != nil:
			step(cross, s.Name, err.Error())
			return fmt.Errorf("stopped at %q — fix it and re-run; completed steps are skipped", s.Name)
		case skipped:
			step(skip, s.Name, "already installed")
		default:
			step(tick, s.Name, "")
		}
	}

	fmt.Println("\ncbx")
	if binary != "" {
		// Uploading an unreleased build on purpose. This is the reason the
		// upload path exists, so it wins over any version that was named.
		if err := setuptool.InstallCBX(t, binary); err != nil {
			step(cross, "cbx", err.Error())
			return err
		}
		step(tick, "cbx", binary+" → /usr/local/bin/cbx")
	} else {
		installed, err := setuptool.FetchCBX(t, cbxDefaultVersion(cbxVersion))
		if err != nil {
			step(cross, "cbx", err.Error())
			return err
		}
		step(tick, "cbx", installed)
	}

	fmt.Println("\nClaude Code")
	if skipClaude {
		step(skip, "login", "skipped — run setup again to sign in")
	} else if setuptool.ClaudeLoggedIn(t) {
		step(skip, "login", "already signed in")
	} else {
		fmt.Println("\n" + setuptool.LoginGuidance)
		if err := setuptool.ClaudeLogin(t, os.Stdout, os.Stdin); err != nil {
			step(cross, "login", err.Error())
			return err
		}
		if !setuptool.ClaudeLoggedIn(t) {
			step(cross, "login", "still not signed in")
			return fmt.Errorf("Claude Code is not signed in — run setup again")
		}
		step(tick, "login", "")
	}

	if !skipAuth {
		fmt.Println("\nCLI tokens")
		if err := runAuth(t, ""); err != nil {
			return err
		}
	}

	fmt.Println("\nConfig")
	if claudePath == "" {
		// Nothing, rather than ~/.claude. This step used to copy whoever ran
		// setup's personal configuration onto the box without being asked.
		step(skip, "claude config", "no --path given — nothing copied")
		return finishSetup(t, withAPI)
	}
	dir, err := expandHome(claudePath)
	if err != nil {
		return err
	}
	copied, dropped, err := setuptool.MigrateConfig(t, setuptool.MigrateOptions{Dir: dir})
	for _, c := range copied {
		if c.Files == 0 {
			// Reporting a tick here is how an empty directory once looked
			// like a successful copy.
			step(cross, c.Path, "nothing to copy")
			continue
		}
		step(tick, c.Path, fmt.Sprintf("%d files", c.Files))
	}
	for _, d := range dropped {
		step(skip, d.Path, "dropped — "+d.Reason)
	}
	if err != nil {
		step(cross, "migrate", err.Error())
		return err
	}

	return finishSetup(t, withAPI)
}

// finishSetup installs the API if it was asked for, and says how to reach the
// box. Shared because the config step returns early when it has nothing to
// copy, and both paths still have to finish.
func finishSetup(t setuptool.Target, withAPI bool) error {
	if withAPI {
		fmt.Println("\nAPI")
		if err := setuptool.UploadCommandSpec(t); err != nil {
			step(cross, setuptool.ConfigName, err.Error())
			return err
		}
		step(tick, setuptool.ConfigName, "~/.config/cbx/"+setuptool.ConfigName)
		if err := setuptool.InstallAPI(t, setuptool.APIOptions{}); err != nil {
			step(cross, "service", err.Error())
			return err
		}
		step(tick, "service", "cbx-api, enabled and started")
		// Issues the box's first key if it has none: the API is unusable
		// without one, and a box being set up is exactly the box that has
		// none yet.
		key, err := setuptool.APIKey(t)
		if err != nil {
			step(cross, "key", err.Error())
			return err
		}
		step(tick, "key", key)
		fmt.Printf("\n  Reach it from here:\n\n    %s\n", setuptool.ForwardCommand(t, setuptool.DefaultAPIAddr))
	}

	fmt.Printf("\n%s ready. Start the master session:\n\n    ssh %s cbx new master\n\n", t, t)
	return nil
}

// cbxDefaultVersion picks which release to install.
//
// This tool's own version by default: the two binaries are built and released
// from the same commit, so pairing them is what keeps a setuptool from
// writing a systemd unit for a cbx that has no serve command. A dev build has
// no release to match, so it takes the latest.
func cbxDefaultVersion(requested string) string {
	if requested != "" {
		return requested
	}
	if version == "dev" || version == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(version, "v")
}

// runAPI operates the API server on an already-provisioned box.
func runAPI(t setuptool.Target, args []string, role string) error {
	switch args[0] {
	case "install":
		if err := setuptool.UploadCommandSpec(t); err != nil {
			return err
		}
		if err := setuptool.InstallAPI(t, setuptool.APIOptions{}); err != nil {
			return err
		}
		key, err := setuptool.APIKey(t)
		if err != nil {
			// A box with no keys yet is not a failed install; it is an
			// install that needs one issued.
			fmt.Printf("service\tcbx-api\n")
			fmt.Printf("addr\thttp://%s\n", setuptool.DefaultAPIAddr)
			fmt.Fprintf(os.Stderr, "note: %v\n", err)
			return nil
		}
		fmt.Printf("service\tcbx-api\n")
		fmt.Printf("addr\thttp://%s\n", setuptool.DefaultAPIAddr)
		fmt.Printf("key\t%s\n", key)
		fmt.Printf("forward\t%s\n", setuptool.ForwardCommand(t, setuptool.DefaultAPIAddr))
	case "key":
		action := "list"
		label := ""
		if len(args) > 1 {
			action = args[1]
		}
		if len(args) > 2 {
			label = args[2]
		}
		out, err := setuptool.Keys(t, action, label, role)
		if err != nil {
			return err
		}
		if out != "" {
			fmt.Println(out)
		}
	case "forward":
		fmt.Printf("forward\t%s\n", setuptool.ForwardCommand(t, setuptool.DefaultAPIAddr))
	case "expose":
		if err := setuptool.InstallTunnel(t, setuptool.DefaultAPIAddr); err != nil {
			return err
		}
		url, err := setuptool.TunnelURL(t, 60*time.Second)
		if err != nil {
			return err
		}
		fmt.Printf("url\t%s\n", url)
		if key, kerr := setuptool.APIKey(t); kerr == nil {
			fmt.Printf("key\t%s\n", key)
		}
		fmt.Fprintln(os.Stderr, "note: the tunnel is public — a key is the only thing protecting these sessions, and the URL changes whenever it restarts")
	case "url":
		url, err := setuptool.TunnelURL(t, 10*time.Second)
		if err != nil {
			return err
		}
		fmt.Printf("url\t%s\n", url)
	default:
		return fmt.Errorf("unknown api command %q (install, key, forward, expose, url)", args[0])
	}
	return nil
}

// runAuth offers each unauthenticated tool a token. A tool already logged in
// is left alone, and a token already in the local environment is used without
// asking — nobody should have to paste something they have already exported.
func runAuth(t setuptool.Target, only string) error {
	in := bufio.NewReader(os.Stdin)
	for _, tool := range setuptool.SupportedTools() {
		if only != "" && tool.Name != only {
			continue
		}
		if setuptool.IsAuthenticated(t, tool) {
			step(skip, tool.Name, "already authenticated")
			continue
		}

		token := setuptool.TokenFromEnv(tool)
		from := "from the environment"
		if token == "" {
			fmt.Printf("\n  %s — %s\n", tool.Name, tool.Help)
			fmt.Printf("  Token: %s\n", tool.TokenURL)
			fmt.Printf("  Paste a token (or press Enter to skip): ")
			line, _ := in.ReadString('\n')
			token = strings.TrimSpace(line)
			from = ""
		}
		if token == "" {
			step(skip, tool.Name, "skipped")
			continue
		}
		if err := setuptool.Authenticate(t, tool, token); err != nil {
			// A bad token is worth reporting and moving past: the others are
			// independent, and stopping here would strand them.
			step(cross, tool.Name, err.Error())
			continue
		}
		step(tick, tool.Name, from)
	}
	return nil
}

func runStatus(t setuptool.Target) error {
	fmt.Printf("\n%s\n\nTools\n", t)
	for _, s := range setuptool.InstallSteps() {
		if s.Check(t) {
			step(tick, s.Name, "")
		} else {
			step(cross, s.Name, "not installed")
		}
	}

	fmt.Println("\nAuth")
	if setuptool.ClaudeLoggedIn(t) {
		step(tick, "claude", "")
	} else {
		step(cross, "claude", "not signed in")
	}
	for _, tool := range setuptool.SupportedTools() {
		if setuptool.IsAuthenticated(t, tool) {
			step(tick, tool.Name, "")
		} else {
			step(cross, tool.Name, "not authenticated")
		}
	}

	fmt.Println("\nAPI")
	if setuptool.APIRunning(t) {
		step(tick, "cbx-api", "running on "+setuptool.DefaultAPIAddr)
	} else {
		step(cross, "cbx-api", "not running — `cbx-setuptool api install`")
	}
	if setuptool.TunnelRunning(t) {
		if url, err := setuptool.TunnelURL(t, 5*time.Second); err == nil {
			step(tick, "cbx-tunnel", url)
		} else {
			step(tick, "cbx-tunnel", "running, no URL in the journal yet")
		}
	} else {
		step(skip, "cbx-tunnel", "not exposed — `cbx-setuptool api expose`")
	}
	fmt.Println()
	return nil
}
