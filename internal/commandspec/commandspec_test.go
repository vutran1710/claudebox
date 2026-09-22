package commandspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The spec is the only gate the API has. Claude Code reports is_error: false
// for a command that does not exist and for one that refuses to run without a
// terminal, so nothing downstream can tell a refusal from a success — these
// tests are what stand between a caller and a silent no-op.

func TestTheShippedDefaultIsValid(t *testing.T) {
	s, err := Parse(Default())
	if err != nil {
		t.Fatalf("the embedded default does not parse: %v", err)
	}
	if len(s.Commands) == 0 {
		t.Fatal("the embedded default declares no commands")
	}
}

func TestClearIsNeverForwarded(t *testing.T) {
	s, err := Parse(Default())
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Resolve("/clear")
	if err != nil {
		t.Fatalf("/clear is not allowed by the default spec: %v", err)
	}
	// Forwarded, /clear reports success, forks a new session id and leaves the
	// transcript intact. Measured, not assumed.
	if c.Effect != RotateSession {
		t.Errorf("/clear effect = %q, want %q — forwarding it does not clear anything", c.Effect, RotateSession)
	}
}

func TestACommandNotInTheSpecIsRefused(t *testing.T) {
	s := &Spec{Commands: []Command{{Name: "/model", Effect: Forward}}}
	if _, err := s.Resolve("/definitely-not-declared"); err == nil {
		t.Fatal("an undeclared command was allowed — deny by default is the whole point")
	}
}

func TestRequiresArgsIsEnforced(t *testing.T) {
	s := &Spec{Commands: []Command{{Name: "/model", Effect: Forward, RequiresArgs: true}}}
	if _, err := s.Resolve("/model"); err == nil {
		t.Error("bare /model was allowed — it only prints its usage and changes nothing")
	}
	if _, err := s.Resolve("/model sonnet"); err != nil {
		t.Errorf("/model sonnet was refused: %v", err)
	}
}

func TestResolveIgnoresSurroundingWhitespace(t *testing.T) {
	s := &Spec{Commands: []Command{{Name: "/compact", Effect: Forward}}}
	if _, err := s.Resolve("  /compact  "); err != nil {
		t.Errorf("padded command was refused: %v", err)
	}
}

func TestAnEmptyCommandIsRefused(t *testing.T) {
	s := &Spec{Commands: []Command{{Name: "/compact", Effect: Forward}}}
	if _, err := s.Resolve("   "); err == nil {
		t.Error("an empty command was accepted")
	}
}

func TestAMalformedSpecIsAnErrorNotAnEmptyAllowlist(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"not yaml", "commands: [oh dear: ["},
		{"unknown effect", "commands:\n  - name: /x\n    effect: teleport\n"},
		{"no name", "commands:\n  - effect: forward\n"},
		{"name without a slash", "commands:\n  - name: model\n    effect: forward\n"},
		{"declared twice", "commands:\n  - name: /x\n    effect: forward\n  - name: /x\n    effect: forward\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse([]byte(c.yaml)); err == nil {
				t.Error("parsed without error — a bad spec must not degrade into an empty allowlist")
			}
		})
	}
}

func TestLoadFallsBackToTheDefaultWhenAbsent(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nothing-here.yaml"))
	if err != nil {
		t.Fatalf("Load on a missing file: %v", err)
	}
	if _, err := s.Resolve("/clear"); err != nil {
		t.Errorf("the fallback spec does not allow /clear: %v", err)
	}
}

func TestLoadRefusesAMalformedFileRatherThanFallingBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "commands.yaml")
	if err := os.WriteFile(p, []byte("commands: [broken: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Error("a malformed spec silently became the default — the operator's policy was replaced without saying so")
	}
}

func TestRefusalNamesTheFileToEdit(t *testing.T) {
	s := &Spec{}
	_, err := s.Resolve("/whatever")
	if err == nil || !strings.Contains(err.Error(), "commands.yaml") {
		t.Errorf("error %v does not say where to allow the command", err)
	}
}
