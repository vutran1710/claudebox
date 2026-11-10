// Package boxconfig owns the box's policy file: the roles a key may hold, and
// the slash commands that exist.
//
// Two sections, because there are two questions — what a caller's sessions may
// touch, and which commands they may run. It is a package rather than a file
// inside internal/api because two binaries need the same bytes: cbx reads it
// to decide what a request may do, and cbx-setuptool ships the default.
//
// Keys are not here. They live in the session database, because the API
// rewrites this file — that is what PUT /commands is — and a credential the
// caller can edit is not a constraint on that caller.
package boxconfig

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

// Default is the config shipped with the binary: cbx.example.yaml at the
// project root, so what ships and what a reader sees cannot drift apart.
func Default() []byte { return claudebox.CommandsExample }

// Role is what a key holding it may do: the rules its sessions are refused.
//
// Named here and referenced by keys in the database, so tightening a rule is
// one edit to this file rather than a rewrite of every key that should obey it.
//
// There is deliberately no permission mode. Every session runs acceptEdits —
// the only mode that both works unattended and honours these rules. Measured
// against Claude Code 2.1.236: under bypassPermissions a deny rule is ignored
// entirely, and manual needs somebody present to approve each edit. A setting
// whose other values are broken or unsafe is not a setting worth having.
type Role struct {
	// Deny is passed to Claude Code as permission rules. These are what stop
	// a session writing outside its own directory — including the box's own
	// guide, which only somebody with ssh should be able to change.
	//
	// Absent and empty mean different things, and the difference is the safe
	// one: a role that says nothing about deny gets DefaultDeny, because an
	// unfinished role should not be an unrestricted one. `deny: []` denies
	// nothing, and has to be written on purpose.
	Deny []string `yaml:"deny" json:"deny"`
	// SessionEnv names the variables this role's keys may set on a session they
	// create. Absent permits none.
	SessionEnv []string `yaml:"session_env" json:"session_env,omitempty"`
	// QueryEnv names the variables a query may set for its own turn only, such
	// as a credential that expires before the session does. Absent permits none.
	QueryEnv []string `yaml:"query_env" json:"query_env,omitempty"`
}

type Command struct {
	Name         string `yaml:"name" json:"name"`
	Effect       string `yaml:"effect" json:"effect"`
	RequiresArgs bool   `yaml:"requires_args" json:"requires_args"`
	Description  string `yaml:"description" json:"description,omitempty"`
}

type Config struct {
	Version int `yaml:"version" json:"version"`
	// Roles are the kinds a key may hold, by name.
	Roles map[string]Role `yaml:"roles" json:"roles"`
	// Permissions is what roles were called first. Read so a box configured
	// before the rename keeps working rather than locking every caller out.
	Permissions map[string]Role `yaml:"permissions" json:"-"`
	Commands    []Command       `yaml:"commands" json:"commands"`

	// path is where this was read from, so an error names the file somebody
	// has to edit rather than the one they probably have not got.
	path string `yaml:"-"`
}

// DefaultPath is where the config lives. Config, not state: hand-edited
// policy, the opposite of sessions.db, which cbx can rebuild.
func DefaultPath() string { return filepath.Join(configDir(), "cbx.yaml") }

// LegacyPath is what the file was called before it held roles as well as
// commands. Read when cbx.yaml is absent, so a box provisioned earlier keeps
// working without anyone editing it.
func LegacyPath() string { return filepath.Join(configDir(), "commands.yaml") }

func configDir() string {
	if s := os.Getenv("XDG_CONFIG_HOME"); s != "" {
		return filepath.Join(s, "cbx")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "cbx")
	}
	return filepath.Join(home, ".config", "cbx")
}

// Load reads the config, falling back to the previous filename and then to the
// embedded default. A malformed file is an error rather than a fallback:
// serving the default would answer requests with a policy nobody chose.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if legacy, lerr := os.ReadFile(LegacyPath()); lerr == nil {
			raw = legacy
		} else {
			raw = Default()
		}
	} else if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg, perr := Parse(raw)
	if perr != nil {
		return nil, perr
	}
	cfg.path = path
	return cfg, nil
}

// Parse validates the config. Every rejection here is one the server would
// otherwise discover per request, against a caller who cannot fix it.
func Parse(raw []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	seen := map[string]bool{}
	for i, cmd := range c.Commands {
		switch {
		case cmd.Name == "":
			return nil, fmt.Errorf("command %d has no name", i)
		case !strings.HasPrefix(cmd.Name, "/"):
			return nil, fmt.Errorf("command %q must start with '/'", cmd.Name)
		case cmd.Effect != Forward && cmd.Effect != RotateSession:
			return nil, fmt.Errorf("command %q has unknown effect %q (%s, %s)", cmd.Name, cmd.Effect, Forward, RotateSession)
		case seen[cmd.Name]:
			return nil, fmt.Errorf("command %q is declared twice", cmd.Name)
		}
		seen[cmd.Name] = true
	}

	if c.Roles == nil {
		c.Roles = map[string]Role{}
	}
	for name, r := range c.Permissions {
		if _, taken := c.Roles[name]; !taken {
			c.Roles[name] = r
		}
	}
	c.Permissions = nil

	for name := range c.Roles {
		if name == "" {
			return nil, fmt.Errorf("a role has no name")
		}
	}
	return &c, nil
}

// Role returns a named role.
//
// An unknown name is an error rather than a default. A key referring to a role
// nobody defined should stop working loudly, not fall back to whatever seemed
// reasonable — the fallback is exactly the boundary somebody meant to tighten.
func (c *Config) Role(name string) (*Role, error) {
	if name == "" {
		return nil, fmt.Errorf("this key has no role")
	}
	r, ok := c.Roles[name]
	if !ok {
		return nil, fmt.Errorf("role %q is not defined in %s", name, c.where())
	}
	if r.Deny == nil {
		// Said nothing rather than said nothing-is-denied.
		r.Deny = DefaultDeny()
	}
	r.Deny = expandHome(r.Deny)
	return &r, nil
}

// expandHome replaces ${HOME} with the home directory of the user the server
// runs as.
//
// A rule protecting the box's own guide has to name a real path, and that path
// is not knowable when the file is written: the service runs as whoever was
// provisioned, which on a box that refuses root over ssh is not root. A
// hardcoded /root there denies nothing and leaves the guide writable — the
// opposite of what the role says it does.
func expandHome(rules []string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root"
	}
	out := make([]string, len(rules))
	for i, rule := range rules {
		out[i] = strings.ReplaceAll(rule, "${HOME}", home)
	}
	return out
}

func (c *Config) where() string {
	if c.path != "" {
		return c.path
	}
	return DefaultPath()
}

// Resolve reports how to carry out a command request, or why it is refused.
//
// The whole input is taken, not just the name, because whether a command is
// acceptable depends on whether it was given arguments: bare `/model` prints
// its usage and changes nothing, which reads as success.
func (c *Config) Resolve(input string) (*Command, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("command is required")
	}
	name, args, _ := strings.Cut(input, " ")
	for i := range c.Commands {
		cmd := &c.Commands[i]
		if cmd.Name != name {
			continue
		}
		if cmd.RequiresArgs && strings.TrimSpace(args) == "" {
			return nil, fmt.Errorf("%s requires an argument", name)
		}
		return cmd, nil
	}
	return nil, fmt.Errorf("%s is not an allowed command — add it to %s", name, c.where())
}

// WriteCommands replaces the commands section, leaving the roles as they were.
//
// Safe to expose over HTTP precisely because keys are not in this file: the
// worst a caller can do here is change which slash commands exist, never what
// its own sessions are allowed to touch.
func WriteCommands(path string, commands []Command) error {
	current, err := Load(path)
	if err != nil {
		return err
	}
	current.Commands = commands
	return write(path, current)
}

func write(path string, cfg *Config) error {
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// DefaultDeny is what a role denies when it says nothing: the box's own guide,
// skills and policy file.
//
// These shape every future session, so one able to rewrite them could change
// what every later session believes. Only somebody with ssh should.
func DefaultDeny() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root"
	}
	return []string{
		"Write(//" + home + "/.claude/**)",
		"Edit(//" + home + "/.claude/**)",
		"Write(//" + home + "/.config/cbx/**)",
		"Edit(//" + home + "/.config/cbx/**)",
	}
}
