package setuptool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Step is one thing setuptool does to a box. Splitting provisioning into
// named, individually-checkable steps means a re-run skips what is already
// done and the TUI has something honest to display.
type Step struct {
	Name string
	// Check reports whether the step is already satisfied. A step whose Check
	// passes is skipped, which is what makes a re-run cheap and idempotent.
	Check func(Target) bool
	// Do performs the step.
	Do func(Target) error
}

// toolPath prefixes remote commands. A non-interactive ssh session reads no rc
// file, so ~/.local/bin — where the Claude Code installer puts claude — is not
// on PATH. $HOME expands on the box, so this is right for whichever user.
const toolPath = `export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.cargo/bin:/usr/local/go/bin:$PATH"; `

// remote runs a script with the tool PATH already set.
func remote(t Target, script string) (string, error) { return Run(t, toolPath+script) }

// has reports whether a binary resolves on the box, with the tool PATH set.
func has(t Target, bin string) bool {
	_, err := remote(t, "command -v "+bin+" >/dev/null 2>&1")
	return err == nil
}

// onDefaultPath reports whether a binary resolves *without* the tool PATH —
// that is, to a plain `ssh box <bin>` and to anything else that does not know
// to prepend $HOME/.local/bin.
//
// A step's Check must use this wherever the step claims to put something on
// the PATH, or the Check passes on the strength of a prefix the rest of the
// world does not set, and the step is skipped while the binary stays hidden.
func onDefaultPath(t Target, bin string) bool {
	_, err := Run(t, "command -v "+bin+" >/dev/null 2>&1")
	return err == nil
}

// InstallSteps is the tool chain a box needs.
//
// Every Do is followed by its own Check in Run, so a step that exits 0 without
// installing anything is caught. A `curl | bash` that silently does nothing
// reported success on a real droplet and left the box without the tool.
func InstallSteps() []Step {
	apt := func(pkgs string) func(Target) error {
		return func(t Target) error {
			_, err := remote(t, `export DEBIAN_FRONTEND=noninteractive
while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 2; done
apt-get update -qq && apt-get install -y -qq `+pkgs)
			return err
		}
	}
	return []Step{
		{
			Name:  "system packages",
			Check: func(t Target) bool { return has(t, "tmux") && has(t, "git") && has(t, "jq") },
			Do:    apt("curl wget git unzip jq build-essential ca-certificates gnupg tmux"),
		},
		{
			Name:  "node",
			Check: func(t Target) bool { return onDefaultPath(t, "node") && onDefaultPath(t, "npm") },
			Do: func(t Target) error {
				// The npm global prefix is /usr/local, not ~/.npm-global, so
				// globally-installed CLIs land on the default PATH. A
				// non-interactive ssh session reads no rc file, so anything
				// under $HOME is invisible to `ssh box vercel ...` and to any
				// tool that does not know to prepend it.
				_, err := remote(t, `curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && `+
					`DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nodejs && `+
					`npm config set prefix /usr/local`)
				return err
			},
		},
		{
			Name:  "github cli",
			Check: func(t Target) bool { return onDefaultPath(t, "gh") },
			Do: func(t Target) error {
				_, err := remote(t, `curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg && `+
					`echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list && `+
					`DEBIAN_FRONTEND=noninteractive apt-get update -qq && apt-get install -y -qq gh`)
				return err
			},
		},
		{
			Name:  "vercel cli",
			Check: func(t Target) bool { return onDefaultPath(t, "vercel") },
			Do:    func(t Target) error { _, err := remote(t, `npm install -g vercel`); return err },
		},
		{
			Name:  "supabase cli",
			Check: func(t Target) bool { return onDefaultPath(t, "supabase") },
			Do: func(t Target) error {
				// The install script drops a binary in the working directory
				// rather than onto PATH, which is why an earlier version
				// reported success while `supabase` resolved nowhere.
				_, err := remote(t, `cd /tmp && curl -fsSL https://github.com/supabase/cli/releases/latest/download/supabase_linux_$(dpkg --print-architecture).tar.gz | tar -xz supabase && `+
					`install -m 0755 /tmp/supabase /usr/local/bin/supabase && rm -f /tmp/supabase`)
				return err
			},
		},
		{
			Name: "claude code",
			// Checked on the default PATH, not the tool PATH: the installer
			// puts claude under $HOME/.local/bin, and the step's job is to
			// make it reachable without that prefix.
			Check: func(t Target) bool { return onDefaultPath(t, "claude") },
			Do: func(t Target) error {
				// Locate the binary rather than guessing where the installer
				// put it, link it onto the default PATH, and verify the link
				// itself. Ending with `command -v claude` would not do: that
				// runs with the tool PATH set and reports success even when
				// the symlink was never made.
				_, err := remote(t, `curl -fsSL https://claude.ai/install.sh | bash || true
src=$(command -v claude 2>/dev/null || true)
if [ -z "$src" ]; then echo "claude is not on PATH after install" >&2; exit 1; fi
ln -sf "$src" /usr/local/bin/claude
test -x /usr/local/bin/claude`)
				return err
			},
		},
	}
}

// Run performs a step and confirms it worked.
//
// Check is re-run after Do regardless of the exit code. A `curl | bash` that
// exits 0 having installed nothing is the failure mode this exists for: it
// reported "✓ Supabase CLI" on a real droplet where the binary did not exist.
func (s Step) Run(t Target) (skipped bool, err error) {
	if s.Check != nil && s.Check(t) {
		return true, nil
	}
	if err := s.Do(t); err != nil {
		if s.Check == nil || !s.Check(t) {
			return false, err
		}
	}
	if s.Check != nil && !s.Check(t) {
		return false, fmt.Errorf("%s: reported success but is still not installed", s.Name)
	}
	return false, nil
}

// InstallCBX puts a locally built cbx on the box.
//
// The binary is uploaded rather than downloaded from a release, so an
// unreleased build can be tested on real metal — which is the whole reason
// deployment moved off CI.
func InstallCBX(t Target, localBinary string) error {
	if localBinary == "" {
		return fmt.Errorf("no cbx binary given: build one with GOOS=linux GOARCH=amd64 go build -o cbx-linux ./cmd/cbx")
	}
	data, err := os.Open(localBinary)
	if err != nil {
		return fmt.Errorf("read %s: %w", localBinary, err)
	}
	defer data.Close()

	// Refuse a binary that cannot run on the target. Uploading a darwin build
	// to a linux box produces "cannot execute binary file" much later, at a
	// point where the cause is not obvious.
	if err := checkELF(localBinary); err != nil {
		return err
	}
	if err := Upload(t, localBinary, "/usr/local/bin/cbx"); err != nil {
		return err
	}
	_, err = Run(t, "chmod +x /usr/local/bin/cbx")
	return err
}

// checkELF verifies a file is a Linux executable by its magic bytes.
func checkELF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if magic != [4]byte{0x7f, 'E', 'L', 'F'} {
		return fmt.Errorf("%s is not a Linux binary — build one with GOOS=linux GOARCH=amd64", filepath.Base(path))
	}
	return nil
}

// MigrateOptions is what to copy, and from where.
type MigrateOptions struct {
	// Dir is the local configuration directory. Empty means ~/.claude.
	// Not fixed, because a machine may keep more than one, and the one worth
	// shipping to a box is not always the one Claude Code reads here.
	Dir string
	// Only names what to copy, relative to Dir. Empty means DefaultFilter.
	Only []string
}

// DefaultFilter is what a box gets when nobody says otherwise.
//
// Not everything under the directory: caches, transcripts and plugin bundles
// are either large or meaningless elsewhere. Plugins travel as their manifest,
// a few kilobytes the box re-fetches from, rather than a few hundred megabytes.
var DefaultFilter = []string{
	"skills", "agents", "rules", "settings.json",
	"plugins/installed_plugins.json", "plugins/known_marketplaces.json",
}

// Copied records what one filter entry actually sent. The count is here
// because "copied agents" while sending nothing is the failure this whole file
// exists to avoid — a step that reports success having done nothing.
type Copied struct {
	Path  string
	Files int
}

// DefaultClaudeDir is where Claude Code keeps configuration on this machine.
func DefaultClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// MigrateConfig copies local Claude configuration to a box.
//
// It copies whatever the filter names and the directory has. An earlier
// version skipped symlinks, so a configuration directory whose entries link
// into a dotfiles repository arrived empty and was reported as copied.
// entry is one thing to copy, already resolved on disk.
type entry struct {
	Rel   string
	Local string
	IsDir bool
}

// Plan decides what would be copied, and why anything named was not.
//
// Pure, and run before anything touches the network: a filter entry that
// escapes the directory or names something absent is answered immediately,
// rather than after an ssh timeout against a box that was never the problem.
func Plan(dir string, only []string) ([]entry, []Dropped, error) {
	if dir == "" {
		d, err := DefaultClaudeDir()
		if err != nil {
			return nil, nil, err
		}
		dir = d
	}
	if info, err := os.Stat(dir); err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	} else if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", dir)
	}
	if len(only) == 0 {
		only = DefaultFilter
	}

	var plan []entry
	var dropped []Dropped
	for _, rel := range only {
		rel = strings.TrimSpace(strings.Trim(rel, "/"))
		if rel == "" {
			continue
		}
		// The filter names what to copy inside the directory a caller pointed
		// at, and nothing outside it.
		if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			dropped = append(dropped, Dropped{rel, "escapes the configuration directory"})
			continue
		}
		local := filepath.Join(dir, rel)
		info, err := os.Stat(local) // Stat, not Lstat: a symlinked entry is followed.
		if os.IsNotExist(err) {
			dropped = append(dropped, Dropped{rel, "not present in " + dir})
			continue
		}
		if err != nil {
			return nil, dropped, err
		}
		plan = append(plan, entry{Rel: rel, Local: local, IsDir: info.IsDir()})
	}
	return plan, dropped, nil
}

// MigrateConfig copies local Claude configuration to a box.
func MigrateConfig(t Target, opts MigrateOptions) ([]Copied, []Dropped, error) {
	plan, dropped, err := Plan(opts.Dir, opts.Only)
	if err != nil {
		return nil, dropped, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, dropped, err
	}
	remoteHome, err := remoteHomeDir(t)
	if err != nil {
		return nil, dropped, err
	}
	if _, err := Run(t, "mkdir -p "+shq(remoteHome+"/.claude/plugins")); err != nil {
		return nil, dropped, err
	}

	var copied []Copied
	for _, e := range plan {
		dest := remoteHome + "/.claude/" + e.Rel

		// settings.json cannot be copied verbatim: it names the operator's
		// home directory and binaries the box does not have, and every hook it
		// carries fires on every edit inside a session.
		if filepath.Base(e.Rel) == "settings.json" {
			d, err := uploadPortableSettings(t, e.Local, dest, home, remoteHome)
			if err != nil {
				return copied, dropped, err
			}
			dropped = append(dropped, d...)
			copied = append(copied, Copied{e.Rel, 1})
			continue
		}
		if e.IsDir {
			n, err := uploadDir(t, e.Local, dest)
			if err != nil {
				return copied, dropped, err
			}
			copied = append(copied, Copied{e.Rel, n})
			continue
		}
		if _, err := Run(t, "mkdir -p "+shq(filepath.ToSlash(filepath.Dir(dest)))); err != nil {
			return copied, dropped, err
		}
		if err := Upload(t, e.Local, dest); err != nil {
			return copied, dropped, err
		}
		copied = append(copied, Copied{e.Rel, 1})
	}
	return copied, dropped, nil
}

// uploadPortableSettings rewrites settings.json for the target before sending
// it, and reports what it removed.
func uploadPortableSettings(t Target, local, dest, localHome, remoteHome string) ([]Dropped, error) {
	raw, err := os.ReadFile(local)
	if err != nil {
		return nil, fmt.Errorf("read settings.json: %w", err)
	}
	out, dropped, err := PortableSettings(raw, localHome, remoteHome, func(bin string) bool {
		return has(t, bin)
	})
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "cbx-settings-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()
	return dropped, Upload(t, tmp.Name(), dest)
}

func remoteHomeDir(t Target) (string, error) {
	out, err := Run(t, `printf '%s' "$HOME"`)
	if err != nil {
		return "", err
	}
	h := strings.TrimSpace(out)
	if h == "" {
		return "", fmt.Errorf("could not resolve $HOME on %s", t)
	}
	return h, nil
}

// uploadDir copies every file in a directory and reports how many went.
//
// os.Stat rather than the walk's own mode, so a symlinked file is copied as
// the file it points at. Anything that is not a file after that — a directory
// link, a socket, a broken link — is simply not copied.
func uploadDir(t Target, localDir, remoteDir string) (int, error) {
	var sent int
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		target, err := os.Stat(path)
		if err != nil || !target.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		dest := remoteDir + "/" + filepath.ToSlash(rel)
		if _, err := Run(t, "mkdir -p "+shq(filepath.ToSlash(filepath.Dir(dest)))); err != nil {
			return err
		}
		if err := Upload(t, path, dest); err != nil {
			return err
		}
		sent++
		return nil
	})
	return sent, err
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
