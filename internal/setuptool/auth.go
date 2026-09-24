package setuptool

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// ToolAuth describes how one CLI is authenticated with a token.
//
// Each of these tools reads a token from somewhere different, and getting it
// wrong is silent: the tool installs, reports success, and only fails later
// when a session tries to use it. Keeping the recipes in one table makes the
// differences visible instead of scattered through provisioning scripts.
type ToolAuth struct {
	Name string
	// Where to get a token, shown to the person running the tool.
	TokenURL string
	// Help is a one-line description of what the token is for.
	Help string
	// login builds the remote command that consumes the token on stdin.
	// Taking it on stdin keeps the secret out of the process table, where a
	// command-line argument would be visible to every user on the box.
	login func() string
	// verify reports whether the tool is already authenticated.
	verify string
}

// SupportedTools is the set cbx-setuptool can authenticate. Claude Code is not
// here: its subscription login is an interactive browser OAuth with no token
// path, so it is handled separately.
func SupportedTools() []ToolAuth {
	return []ToolAuth{
		{
			Name:     "github",
			TokenURL: "https://github.com/settings/tokens",
			Help:     "lets sessions clone private repos and open pull requests",
			login:    func() string { return "gh auth login --with-token" },
			verify:   "gh auth status",
		},
		{
			Name:     "vercel",
			TokenURL: "https://vercel.com/account/tokens",
			Help:     "lets sessions deploy and inspect Vercel projects",
			// vercel has no stdin login; it reads VERCEL_TOKEN from the
			// environment or --token. Writing the config file directly is the
			// only way to persist it without putting the token in argv.
			// printf is a shell builtin, so the token never becomes the argv
			// of a forked process and never appears in the process table.
			// umask makes the file private from the moment it exists rather
			// than chmod-ing a briefly world-readable one.
			login: func() string {
				return `set -e; TOKEN=$(cat); ` +
					`mkdir -p "$HOME/.local/share/com.vercel.cli"; ` +
					`umask 077; ` +
					`printf '{"token":"%s"}\n' "$TOKEN" > "$HOME/.local/share/com.vercel.cli/auth.json"`
			},
			verify: "vercel whoami",
		},
		{
			Name:     "supabase",
			TokenURL: "https://supabase.com/dashboard/account/tokens",
			Help:     "lets sessions manage Supabase projects and edge functions",
			login:    func() string { return "supabase login --token \"$(cat)\"" },
			verify:   "supabase projects list",
		},
	}
}

// Authenticate sends a token to the target and runs the tool's login, then
// verifies it took.
//
// The token goes over stdin rather than in the command, so it never appears in
// the box's process table or in shell history. It is also never written to a
// file by cbx — whatever the tool does with it afterwards is the tool's
// business.
func Authenticate(t Target, tool ToolAuth, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("%s: empty token", tool.Name)
	}
	if strings.ContainsAny(token, "\n\r") {
		return fmt.Errorf("%s: token contains a newline", tool.Name)
	}

	cmd := exec_ssh(t, tool.login())
	cmd.Stdin = strings.NewReader(token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s login on %s: %w: %s", tool.Name, t, err, strings.TrimSpace(string(out)))
	}
	if _, err := Run(t, tool.verify+" >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("%s: the token was accepted but %q still fails — is the token valid and unexpired?", tool.Name, tool.verify)
	}
	return nil
}

// IsAuthenticated reports whether a tool is already logged in on the target,
// so the tool can skip asking for a token it does not need.
func IsAuthenticated(t Target, tool ToolAuth) bool {
	_, err := Run(t, tool.verify+" >/dev/null 2>&1")
	return err == nil
}

// TokenFromEnv returns a token for a tool from the local environment, so
// someone who already has GITHUB_TOKEN exported is not asked to paste it.
func TokenFromEnv(tool ToolAuth) string {
	for _, k := range envKeys[tool.Name] {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

var envKeys = map[string][]string{
	"github":   {"GH_TOKEN", "GITHUB_TOKEN"},
	"vercel":   {"VERCEL_TOKEN"},
	"supabase": {"SUPABASE_ACCESS_TOKEN"},
}

// ClaudeLoggedIn reports whether Claude Code is signed in on the box.
func ClaudeLoggedIn(t Target) bool {
	out, err := Run(t, toolPath+`claude auth status --json 2>/dev/null`)
	if err != nil {
		return false
	}
	return strings.Contains(out, `"loggedIn": true`) || strings.Contains(out, `"loggedIn":true`)
}

// LoginGuidance is what the operator is told before the URL appears.
const LoginGuidance = "  A sign-in URL will appear. Open it, approve, and paste the code back here."

// loginDir holds the pipe and the transcript. Under the user's home rather
// than /tmp: the box may be shared, and a fifo somebody else can pre-create is
// a fifo somebody else can read the code out of.
const loginDir = `"$HOME/.cache/cbx"`

// claudeLoginStart begins the sign-in and leaves it waiting for a code.
//
// `claude auth login`, not the Claude Code TUI. The TUI works only with a
// terminal attached, which meant signing in was something you did on the box
// rather than something setuptool did for you — and the URL stayed hidden
// behind a slash command nobody could see from outside.
//
// The pipe is held open by a sleeping writer: without one the login reads EOF
// the moment it starts and gives up before the operator has seen the URL.
const claudeLoginStart = `set -e
d=` + loginDir + `
mkdir -p "$d" && chmod 700 "$d"
rm -f "$d/login.in" "$d/login.out"
mkfifo -m 600 "$d/login.in"
setsid sh -c "sleep 1800 > $d/login.in" >/dev/null 2>&1 &
sleep 1
setsid sh -c "claude auth login --claudeai < $d/login.in > $d/login.out 2>&1" >/dev/null 2>&1 &
sleep 2
echo started`

// loginURL matches the authorize link cloudflare-style escapes wrap twice.
var loginURL = regexp.MustCompile(`https://claude\.[a-z]+/[^\s\]\x1b]*oauth/authorize\?[^\s\]\x1b]+`)

// osc8 is the hyperlink escape a terminal renders as a link. Claude Code emits
// the URL inside one, so the raw text carries it twice with control bytes
// between — unreadable to anything but a terminal.
var osc8 = regexp.MustCompile(`\x1b?\]8;;[^\x07\x1b]*(\x07|\x1b\\)?`)

// FindLoginURL pulls the sign-in URL out of what the login printed.
func FindLoginURL(out string) string {
	return loginURL.FindString(osc8.ReplaceAllString(out, ""))
}

// ClaudeLogin signs Claude Code in on the box, driven from here.
//
// There is no token path for a subscription account — it is a browser OAuth,
// so a person has to approve it. What this removes is the need for that person
// to be *on the box*: the URL is printed here and the code is read from here.
//
// in is read for the code, so this works from a pipe as well as a terminal.
func ClaudeLogin(t Target, out io.Writer, in io.Reader) error {
	if _, err := remote(t, claudeLoginStart); err != nil {
		return fmt.Errorf("start the sign-in: %w", err)
	}
	defer remote(t, `pkill -f "claude auth login" >/dev/null 2>&1; rm -f `+loginDir+`/login.in`)

	var url string
	for deadline := time.Now().Add(60 * time.Second); ; {
		printed, err := remote(t, `cat `+loginDir+`/login.out 2>/dev/null || true`)
		if err == nil {
			if url = FindLoginURL(printed); url != "" {
				break
			}
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("no sign-in URL appeared within 60s")
		}
		time.Sleep(2 * time.Second)
	}

	fmt.Fprintf(out, "\n  Open this, approve, and paste the code back:\n\n    %s\n\n  code: ", url)
	code, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && strings.TrimSpace(code) == "" {
		return fmt.Errorf("no code was given: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("no code was given")
	}
	if _, err := remote(t, `printf "%s\n" `+shq(code)+` > `+loginDir+`/login.in`); err != nil {
		return fmt.Errorf("send the code: %w", err)
	}

	for deadline := time.Now().Add(60 * time.Second); ; {
		if ClaudeLoggedIn(t) {
			return nil
		}
		if !time.Now().Before(deadline) {
			printed, _ := remote(t, `tail -5 `+loginDir+`/login.out 2>/dev/null || true`)
			return fmt.Errorf("the code was sent but the box is still signed out: %s", strings.TrimSpace(printed))
		}
		time.Sleep(3 * time.Second)
	}
}
