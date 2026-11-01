package claude

import (
	"os"
	"path/filepath"
	"testing"
)

// What `/name` resolves to is read off the disk because the CLI will not say.
// These pin the places it is read from, and the two cases where not finding a
// plugin must not be reported as the skill being absent.

func TestASlashCommandIsFoundWhereClaudeCodeKeepsIt(t *testing.T) {
	cases := []struct {
		what  string
		ref   string
		write string
		found bool
	}{
		{"a personal skill", "inline", "home/.claude/skills/inline/SKILL.md", true},
		{"a skill in the project being worked on", "deploy", "dir/.claude/skills/deploy/SKILL.md", true},
		{"a skill whose file is lowercase", "oddly", "home/.claude/skills/oddly/skill.md", true},
		{"a personal command", "handoff", "home/.claude/commands/handoff.md", true},
		{"a command in the project", "release", "dir/.claude/commands/release.md", true},
		{"nothing of that name anywhere", "not-a-real-skill", "", false},
		{"a directory holding no skill", "hollow", "home/.claude/skills/hollow/README.md", false},
		{"a command without the .md", "bare", "home/.claude/commands/bare", false},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			root := t.TempDir()
			home, dir := filepath.Join(root, "home"), filepath.Join(root, "dir")
			if c.write != "" {
				write(t, filepath.Join(root, c.write))
			}
			if got := Installed(home, dir, c.ref); got != c.found {
				t.Errorf("Installed(%q) = %v, want %v", c.ref, got, c.found)
			}
		})
	}
}

func TestAPluginSkillIsFoundThroughTheManifest(t *testing.T) {
	cases := []struct {
		what     string
		ref      string
		manifest bool
		skill    bool
		found    bool
	}{
		{"the plugin is installed and has the skill", "helper:fix", true, true, true},
		{"the plugin is installed and does not have it", "helper:absent", true, false, false},
		{"the plugin is not in the manifest", "stranger:fix", true, true, true},
		{"there is no manifest at all", "helper:fix", false, false, true},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			root := t.TempDir()
			home, dir := filepath.Join(root, "home"), filepath.Join(root, "dir")
			installed := filepath.Join(root, "plugins", "helper", "1.0.0")
			if c.manifest {
				write(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
				body := `{"version":2,"plugins":{"helper@a-marketplace":[{"installPath":` +
					`"` + installed + `"}]}}`
				if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.skill {
				write(t, filepath.Join(installed, "skills", "fix", "SKILL.md"))
			}
			if got := Installed(home, dir, c.ref); got != c.found {
				t.Errorf("Installed(%q) = %v, want %v", c.ref, got, c.found)
			}
		})
	}
}

// write creates a file and the directories above it.
func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
