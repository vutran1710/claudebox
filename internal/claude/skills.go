package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Where a slash command comes from on disk.
//
// Priming has to look, because the CLI does not report that `/name` resolved
// to nothing: it hands the text to the model, which answers in prose. Claude
// Code 2.1.236 prefixed "Unknown command:"; 2.1.286 does not, and the model's
// wording is not a contract to match against.

// Installed reports whether `/ref` would find anything.
//
// A plugin whose install path cannot be found reports installed: refusing a
// skill that works is worse than accepting one that does not. Org-synced
// plugins live under opaque ids that do not map back to a plugin name.
func Installed(home, dir, ref string) bool {
	if plugin, skill, isPlugin := strings.Cut(ref, ":"); isPlugin {
		where := pluginPaths(home, plugin)
		if len(where) == 0 {
			return true
		}
		for _, p := range where {
			if isSkill(filepath.Join(p, "skills", skill)) {
				return true
			}
		}
		return false
	}
	for _, root := range []string{filepath.Join(home, ".claude"), filepath.Join(dir, ".claude")} {
		if isSkill(filepath.Join(root, "skills", ref)) {
			return true
		}
		if _, err := os.Stat(filepath.Join(root, "commands", ref+".md")); err == nil {
			return true
		}
	}
	return false
}

func isSkill(dir string) bool {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// pluginPaths finds where a plugin was installed. Its key in the manifest
// carries the marketplace it came from, as `name@marketplace`.
func pluginPaths(home, plugin string) []string {
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var manifest struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	var where []string
	for key, installs := range manifest.Plugins {
		if name, _, _ := strings.Cut(key, "@"); name != plugin {
			continue
		}
		for _, i := range installs {
			if i.InstallPath != "" {
				where = append(where, i.InstallPath)
			}
		}
	}
	return where
}
