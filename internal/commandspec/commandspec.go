// Package commandspec owns the list of slash commands the API will run.
//
// It is a package rather than a file inside internal/api because two binaries
// need the same bytes: cbx reads the spec to decide what a request may do, and
// cbx-setuptool ships the default to a box. One definition, imported twice.
package commandspec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	claudebox "github.com/vutran1710/claudebox"
)

// Effects a command can have.
const (
	// Forward runs the command through `claude -p` and returns its output.
	Forward = "forward"
	// RotateSession is performed by cbx itself. /clear needs this: forwarded,
	// it reports success, forks a new conversation id, and leaves the
	// transcript intact.
	RotateSession = "rotate-session"
)

// Default is the spec shipped with the binary, written out when a box has
// none and uploaded by cbx-setuptool. It is commands.example.yaml at the
// project root, so what ships and what a reader sees cannot drift apart.
func Default() []byte { return claudebox.CommandsExample }

type Command struct {
	Name         string `yaml:"name"`
	Effect       string `yaml:"effect"`
	RequiresArgs bool   `yaml:"requires_args"`
	Description  string `yaml:"description"`
}

type Spec struct {
	Version  int       `yaml:"version"`
	Commands []Command `yaml:"commands"`
}

// DefaultPath is where the spec lives. Config, not state: this is hand-edited
// policy, the opposite of sessions.db, which cbx can rebuild.
func DefaultPath() string {
	if s := os.Getenv("XDG_CONFIG_HOME"); s != "" {
		return filepath.Join(s, "cbx", "commands.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "cbx", "commands.yaml")
	}
	return filepath.Join(home, ".config", "cbx", "commands.yaml")
}

// Load reads a spec, falling back to the embedded default when the file does
// not exist. A malformed file is an error rather than a fallback: silently
// serving the default would answer requests with a policy nobody chose.
func Load(path string) (*Spec, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		raw = claudebox.CommandsExample
	} else if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse validates a spec. Every rejection here is one the server would
// otherwise discover per request, against a caller who cannot fix it.
func Parse(raw []byte) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse command spec: %w", err)
	}
	seen := map[string]bool{}
	for i, c := range s.Commands {
		switch {
		case c.Name == "":
			return nil, fmt.Errorf("command %d has no name", i)
		case !strings.HasPrefix(c.Name, "/"):
			return nil, fmt.Errorf("command %q must start with '/'", c.Name)
		case c.Effect != Forward && c.Effect != RotateSession:
			return nil, fmt.Errorf("command %q has unknown effect %q (%s, %s)", c.Name, c.Effect, Forward, RotateSession)
		case seen[c.Name]:
			return nil, fmt.Errorf("command %q is declared twice", c.Name)
		}
		seen[c.Name] = true
	}
	return &s, nil
}

// Resolve reports how to carry out a request, or why it is refused.
//
// The whole input is taken, not just the name, because whether a command is
// acceptable depends on whether it was given arguments: bare `/model` prints
// its usage and changes nothing, which reads as success.
func (s *Spec) Resolve(input string) (*Command, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("command is required")
	}
	name, args, _ := strings.Cut(input, " ")
	for i := range s.Commands {
		c := &s.Commands[i]
		if c.Name != name {
			continue
		}
		if c.RequiresArgs && strings.TrimSpace(args) == "" {
			return nil, fmt.Errorf("%s requires an argument", name)
		}
		return c, nil
	}
	return nil, fmt.Errorf("%s is not an allowed command — add it to %s", name, DefaultPath())
}
