package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

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

func runSetup(t setuptool.Target, binary string, skipAuth, skipClaude bool) error {
	fmt.Printf("\nProvisioning %s\n\n", t)

	fmt.Println("Tools")
	for _, s := range setuptool.InstallSteps() {
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
	if binary == "" {
		step(skip, "cbx", "no --binary given, skipping")
	} else if err := setuptool.InstallCBX(t, binary); err != nil {
		step(cross, "cbx", err.Error())
		return err
	} else {
		step(tick, "cbx", "/usr/local/bin/cbx")
	}

	fmt.Println("\nClaude Code")
	if skipClaude {
		step(skip, "login", "skipped — run setup again to sign in")
	} else if setuptool.ClaudeLoggedIn(t) {
		step(skip, "login", "already signed in")
	} else {
		fmt.Println("\n  Claude Code will open on the box. Type /login, complete sign-in in")
		fmt.Println("  your browser, then press Ctrl-D to return here.")
		fmt.Print("\n  Press Enter to continue: ")
		bufio.NewReader(os.Stdin).ReadString('\n')
		if err := setuptool.ClaudeLogin(t); err != nil {
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
	copied, dropped, err := setuptool.MigrateConfig(t, setuptool.MigrateOptions{})
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

	fmt.Printf("\n%s ready. Start the master session:\n\n    ssh %s cbx new master\n\n", t, t)
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
	fmt.Println()
	return nil
}
