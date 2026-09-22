// Package claude drives Claude Code in print mode.
//
// This is the seam the API queries through, and the reason it exists rather
// than reusing internal/tmux: a headless query needs an answer back, and
// scraping one out of a pane is the most fragile code in this project. Print
// mode returns a JSON object and an exit code instead, and skips the
// workspace trust dialog that made pane-driven startup unreliable.
//
// Argv construction is pure and lives apart from the exec, so what gets run is
// asserted as a value without a real Claude anywhere near the test.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Permission modes Claude Code accepts. Validated when a session is created,
// so an invalid one is refused by the API rather than by a child process
// nobody is watching.
const (
	AcceptEdits       = "acceptEdits"
	Auto              = "auto"
	BypassPermissions = "bypassPermissions"
	Manual            = "manual"
)

// DefaultPermissionMode is what a headless session runs as when none is named.
//
// Named here rather than buried in a call for the same reason
// tmux.autonomousClaude is: it is a deliberate risk and should be visible
// where it is taken. A query has nobody to answer a permission prompt — there
// is no terminal and no human — so a prompting session would simply stall.
// The box is single-tenant and owned by whoever ran cbx.
const DefaultPermissionMode = BypassPermissions

// Effort levels Claude Code accepts.
const (
	Low    = "low"
	Medium = "medium"
	High   = "high"
	XHigh  = "xhigh"
	Max    = "max"
)

// ValidEffort reports whether a level is one Claude Code accepts. A closed
// set, so a typo is refused at session creation rather than by a child
// process nobody is watching.
func ValidEffort(e string) bool {
	switch e {
	case "", Low, Medium, High, XHigh, Max:
		return true
	}
	return false
}

// model names are an open set — aliases like "opus", full names like
// "claude-fable-5", and context variants like "opus[1m]" — so this validates
// the shape rather than a list that would go stale with every release.
//
// The leading character matters most: --model takes its value before the "--"
// that ends option parsing, so a value beginning with a dash would be read as
// another flag. Third time this distinction has come up in this project.
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\[\]-]{0,63}$`)

// ValidModel reports whether a model name is safe to pass as an argument.
func ValidModel(m string) bool { return m == "" || modelName.MatchString(m) }

// ValidPermissionMode reports whether a mode is one Claude Code accepts.
func ValidPermissionMode(m string) bool {
	switch m {
	case "", AcceptEdits, Auto, BypassPermissions, Manual:
		return true
	}
	return false
}

// Request is one turn to run against a conversation.
type Request struct {
	Dir          string
	Prompt       string
	SessionID    string
	SystemPrompt string
	// PermissionMode is empty for DefaultPermissionMode.
	PermissionMode string
	// Model and Effort are empty for whatever the box is configured to use.
	Model  string
	Effort string
	// Fresh creates the conversation rather than resuming it. True for the
	// first turn, and again when a resume finds no conversation to continue.
	Fresh bool
}

// Result is what print mode reports back.
type Result struct {
	Answer string
	// SessionID is the conversation Claude actually used. It is not always
	// the one asked for: a command handled locally forks a new id and reports
	// success, so the caller must compare rather than assume.
	SessionID  string
	Turns      int
	IsError    bool
	DurationMS int
}

// Args builds the command line for a request. Pure, so the flags are testable
// without running anything.
func Args(r Request) []string {
	args := []string{"-p", "--output-format", "json"}
	if r.Fresh {
		// The conversation does not exist yet; --resume would fail.
		args = append(args, "--session-id", r.SessionID)
	} else {
		args = append(args, "--resume", r.SessionID)
	}
	mode := r.PermissionMode
	if mode == "" {
		mode = DefaultPermissionMode
	}
	args = append(args, "--permission-mode", mode)
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	if r.Effort != "" {
		args = append(args, "--effort", r.Effort)
	}
	if r.SystemPrompt != "" {
		// Append, never replace: the box's own CLAUDE.md still applies.
		args = append(args, "--append-system-prompt", r.SystemPrompt)
	}
	// Last, and after every flag, so a prompt beginning with a dash cannot be
	// read as one.
	return append(args, "--", r.Prompt)
}

// noConversation is how print mode reports a --resume it cannot satisfy. It
// is plain text on stderr, not JSON, so it can never be mistaken for a result.
const noConversation = "No conversation found with session ID"

// Runner executes claude and returns its combined output. Injected so the API
// can be tested without Claude installed.
type Runner func(ctx context.Context, dir string, args []string) ([]byte, error)

type Client struct {
	run Runner
	bin string
}

func New() *Client { return &Client{run: shellRun, bin: "claude"} }

// WithRunner exists for tests.
func (c *Client) WithRunner(r Runner) *Client { c.run = r; return c }

// shellRun runs claude in dir, in its own process group.
//
// The process group is what makes cancellation complete: killing only the
// parent leaves its children running, writing to a transcript nothing is
// tracking. Cancel sends SIGTERM to the group and WaitDelay escalates, so a
// child that ignores it still goes.
func shellRun(ctx context.Context, dir string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "IS_SANDBOX=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd.CombinedOutput()
}

// Query runs one turn and returns the answer.
//
// A request that asks to resume a conversation which does not exist is retried
// as a fresh one. That is not defensive: the server can die between finishing
// a turn and recording that it happened, leaving a row that believes in a
// conversation nothing created.
func (c *Client) Query(ctx context.Context, r Request) (*Result, error) {
	out, err := c.run(ctx, r.Dir, Args(r))
	if err != nil && !r.Fresh && strings.Contains(string(out), noConversation) {
		r.Fresh = true
		out, err = c.run(ctx, r.Dir, Args(r))
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("claude: %w: %s", err, trim(string(out)))
	}
	return Parse(out)
}

// Parse reads print mode's JSON result.
func Parse(out []byte) (*Result, error) {
	var raw struct {
		Result     string `json:"result"`
		SessionID  string `json:"session_id"`
		NumTurns   int    `json:"num_turns"`
		IsError    bool   `json:"is_error"`
		DurationMS int    `json:"duration_ms"`
	}
	// Claude may print warnings before the object; decode from the first
	// brace rather than failing on them.
	if i := indexBrace(out); i > 0 {
		out = out[i:]
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse claude output: %w: %s", err, trim(string(out)))
	}
	return &Result{
		Answer:     raw.Result,
		SessionID:  raw.SessionID,
		Turns:      raw.NumTurns,
		IsError:    raw.IsError,
		DurationMS: raw.DurationMS,
	}, nil
}

func indexBrace(b []byte) int {
	for i, c := range b {
		if c == '{' {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// TranscriptPath is where Claude Code keeps a conversation.
//
// The directory is the working directory with every separator and dot turned
// into a dash, which is derivable from what the session row already stores —
// so deleting a session can delete its history without recording a second
// path that could drift from the first.
func TranscriptPath(home, dir, sessionID string) string {
	// Symlinks are resolved first, because Claude Code names the directory by
	// its real path: a session started in /tmp on a Mac files its transcript
	// under /private/tmp, and looking for the other one finds nothing.
	abs := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		abs = resolved
	} else if resolved, err := filepath.Abs(dir); err == nil {
		abs = resolved
	}
	escaped := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(abs)
	return filepath.Join(home, ".claude", "projects", escaped, sessionID+".jsonl")
}
