package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Argv is a value, so what gets run is asserted without Claude installed. The
// payloads below are real output captured from Claude Code 2.1.236 — a
// simplified fixture would pass while being wrong about the shape.

func argsOf(t *testing.T, r Request) string {
	t.Helper()
	return strings.Join(Args(r), " ")
}

func TestFirstQueryCreatesTheConversation(t *testing.T) {
	got := argsOf(t, Request{SessionID: "abc", Prompt: "hi", Fresh: true})
	if !strings.Contains(got, "--session-id abc") {
		t.Errorf("args %q do not create the conversation", got)
	}
	if strings.Contains(got, "--resume") {
		t.Error("resumed a conversation that does not exist yet")
	}
}

func TestLaterQueriesResumeIt(t *testing.T) {
	got := argsOf(t, Request{SessionID: "abc", Prompt: "hi"})
	if !strings.Contains(got, "--resume abc") {
		t.Errorf("args %q do not resume", got)
	}
	if strings.Contains(got, "--session-id") {
		t.Error("tried to create a conversation that already exists")
	}
}

func TestOutputIsAlwaysJSON(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		got := argsOf(t, Request{SessionID: "abc", Prompt: "hi", Fresh: fresh})
		if !strings.Contains(got, "--output-format json") {
			t.Errorf("fresh=%v: args %q do not ask for json", fresh, got)
		}
		if !strings.Contains(got, "-p ") {
			t.Errorf("fresh=%v: args %q are not print mode", fresh, got)
		}
	}
}

func TestSystemPromptIsAppendedNotReplaced(t *testing.T) {
	got := argsOf(t, Request{SessionID: "a", Prompt: "hi", SystemPrompt: "be terse"})
	if !strings.Contains(got, "--append-system-prompt be terse") {
		t.Errorf("args %q do not append the system prompt", got)
	}
	// --system-prompt would discard the box's own CLAUDE.md.
	if strings.Contains(got, " --system-prompt ") {
		t.Error("replaced the system prompt instead of appending to it")
	}
}

func TestSystemPromptIsOmittedWhenEmpty(t *testing.T) {
	if got := argsOf(t, Request{SessionID: "a", Prompt: "hi"}); strings.Contains(got, "append-system-prompt") {
		t.Errorf("args %q pass an empty system prompt", got)
	}
}

func TestPermissionModeIsPassedThrough(t *testing.T) {
	cases := []struct{ given, want string }{
		{AcceptEdits, "--permission-mode acceptEdits"},
		{Manual, "--permission-mode manual"},
		{"", "--permission-mode " + DefaultPermissionMode},
	}
	for _, c := range cases {
		got := argsOf(t, Request{SessionID: "a", Prompt: "hi", PermissionMode: c.given})
		if !strings.Contains(got, c.want) {
			t.Errorf("mode %q: args %q missing %q", c.given, got, c.want)
		}
	}
}

func TestPromptGoesAfterASeparator(t *testing.T) {
	args := Args(Request{SessionID: "a", Prompt: "--not-a-flag"})
	if args[len(args)-1] != "--not-a-flag" || args[len(args)-2] != "--" {
		t.Errorf("args %v do not end with `-- <prompt>` — a dash-leading prompt would be read as a flag", args)
	}
}

func TestValidPermissionMode(t *testing.T) {
	for _, ok := range []string{"", AcceptEdits, Auto, BypassPermissions, Manual} {
		if !ValidPermissionMode(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"yolo", "bypass", "BypassPermissions"} {
		if ValidPermissionMode(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Captured from Claude Code 2.1.236.
const realResult = `{"is_error":false,"duration_api_ms":0,"num_turns":1,"session_id":"11111111-1111-1111-1111-111111111111","usage":{"output_tokens":3},"subtype":"success","result":"BANANA","type":"result","duration_ms":2100}`

func TestResultParsesAnswerAndSessionId(t *testing.T) {
	r, err := Parse([]byte(realResult))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.Answer != "BANANA" {
		t.Errorf("Answer = %q", r.Answer)
	}
	if r.SessionID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("SessionID = %q", r.SessionID)
	}
	if r.Turns != 1 || r.DurationMS != 2100 || r.IsError {
		t.Errorf("unexpected: %+v", r)
	}
}

func TestParseSkipsNoiseBeforeTheObject(t *testing.T) {
	r, err := Parse([]byte("some warning on stderr\n" + realResult))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.Answer != "BANANA" {
		t.Errorf("Answer = %q", r.Answer)
	}
}

// A locally-handled command spends no tokens and returns an empty result while
// reporting success. Parsing must surface that faithfully rather than treating
// it as a failure — deciding what it means is the spec's job, not the parser's.
func TestALocallyHandledCommandParsesAsSuccess(t *testing.T) {
	const cleared = `{"is_error":false,"num_turns":0,"duration_api_ms":0,"session_id":"918dc472-621c-464f-8d7f-2ceec3b10615","subtype":"success","result":"","type":"result","duration_ms":21}`
	r, err := Parse([]byte(cleared))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.IsError || r.Turns != 0 || r.Answer != "" {
		t.Errorf("unexpected: %+v", r)
	}
	if r.SessionID != "918dc472-621c-464f-8d7f-2ceec3b10615" {
		t.Errorf("SessionID = %q — the fork must be visible to the caller", r.SessionID)
	}
}

func TestGarbageIsAnErrorNotAnEmptyAnswer(t *testing.T) {
	if _, err := Parse([]byte("total nonsense")); err == nil {
		t.Error("unparseable output became a successful empty answer")
	}
}

func TestAMissingConversationFallsBackToCreating(t *testing.T) {
	var seen [][]string
	c := New().WithRunner(func(_ context.Context, _ string, args []string) ([]byte, error) {
		seen = append(seen, args)
		if len(seen) == 1 {
			return []byte(noConversation + ": abc"), errors.New("exit status 1")
		}
		return []byte(realResult), nil
	})

	r, err := c.Query(context.Background(), Request{SessionID: "abc", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if r.Answer != "BANANA" {
		t.Errorf("Answer = %q", r.Answer)
	}
	if len(seen) != 2 {
		t.Fatalf("ran %d times, want 2", len(seen))
	}
	if !strings.Contains(strings.Join(seen[0], " "), "--resume") {
		t.Error("first attempt did not try to resume")
	}
	// The server can die between finishing a turn and recording that it
	// happened, leaving a row that believes in a conversation nothing created.
	if !strings.Contains(strings.Join(seen[1], " "), "--session-id") {
		t.Error("the retry did not create the conversation")
	}
}

func TestAFreshRequestDoesNotRetry(t *testing.T) {
	calls := 0
	c := New().WithRunner(func(_ context.Context, _ string, _ []string) ([]byte, error) {
		calls++
		return []byte(noConversation), errors.New("exit status 1")
	})
	if _, err := c.Query(context.Background(), Request{SessionID: "abc", Prompt: "hi", Fresh: true}); err == nil {
		t.Error("a failing fresh request reported success")
	}
	if calls != 1 {
		t.Errorf("ran %d times, want 1 — there is nothing to fall back to", calls)
	}
}

func TestCancellationIsReportedAsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := New().WithRunner(func(ctx context.Context, _ string, _ []string) ([]byte, error) {
		return nil, ctx.Err()
	})
	_, err := c.Query(ctx, Request{SessionID: "abc", Prompt: "hi", Fresh: true})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled — a cancelled query is not a Claude failure", err)
	}
}

func TestTranscriptPathMatchesClaudesLayout(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "dot.test_dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := TranscriptPath(home, dir, "abc-123")

	// Verified against Claude Code 2.1.236: separators, dots and underscores
	// all become dashes, and the path is taken after symlinks are resolved.
	if filepath.Base(got) != "abc-123.jsonl" {
		t.Errorf("base = %q", filepath.Base(got))
	}
	parent := filepath.Base(filepath.Dir(got))
	if strings.ContainsAny(parent, "/._") {
		t.Errorf("escaped directory %q still contains an unescaped character", parent)
	}
	if !strings.HasSuffix(parent, "dot-test-dir") {
		t.Errorf("escaped directory %q does not end with the escaped leaf", parent)
	}
	if !strings.HasPrefix(got, filepath.Join(home, ".claude", "projects")) {
		t.Errorf("path %q is not under the projects directory", got)
	}
}

func TestModelAndEffortArePassedThrough(t *testing.T) {
	got := argsOf(t, Request{SessionID: "a", Prompt: "hi", Model: "opus", Effort: XHigh})
	for _, want := range []string{"--model opus", "--effort xhigh"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
}

func TestModelAndEffortAreOmittedWhenEmpty(t *testing.T) {
	got := argsOf(t, Request{SessionID: "a", Prompt: "hi"})
	for _, unwanted := range []string{"--model", "--effort"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("args %q pass an empty %s", got, unwanted)
		}
	}
}

func TestValidEffort(t *testing.T) {
	for _, ok := range []string{"", Low, Medium, High, XHigh, Max} {
		if !ValidEffort(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"extreme", "XHIGH", "1", "high "} {
		if ValidEffort(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidModelAcceptsAliasesNamesAndVariants(t *testing.T) {
	for _, ok := range []string{"", "opus", "sonnet", "claude-fable-5", "opus[1m]", "sonnet[1m]"} {
		if !ValidModel(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
}

// --model takes its value before the "--" that ends option parsing, so a name
// beginning with a dash would be read as another flag. The third time this
// distinction has bitten in this project.
func TestValidModelRejectsWhatClaudeWouldReadAsAFlag(t *testing.T) {
	for _, bad := range []string{
		"--dangerously-skip-permissions",
		"-p",
		"opus --effort max",
		"opus;rm -rf /",
		"opus\nsonnet",
	} {
		if ValidModel(bad) {
			t.Errorf("%q accepted — it would reach claude as an argument", bad)
		}
	}
}
